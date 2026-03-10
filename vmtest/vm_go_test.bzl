"""vm_go_test rule: boot a VM with agent, run Go tests against it."""

_QEMU_TOOLCHAIN_TYPE = "@rules_qemu//qemu:toolchain_type"
_SWTPM_TOOLCHAIN_TYPE = "@rules_qemu//qemu:swtpm_type"
_SWTPM_SETUP_TOOLCHAIN_TYPE = "@rules_qemu//qemu:swtpm_setup_type"

def _vm_go_test_impl(ctx):
    qemu_info = ctx.toolchains[_QEMU_TOOLCHAIN_TYPE].qemu_info
    test_binary = ctx.executable.test

    env_lines = []
    runfiles_files = []

    # QEMU binary paths from toolchain
    if qemu_info.qemu_system:
        env_lines.append('export VMTEST_QEMU="{}"'.format(qemu_info.qemu_system.short_path))
        runfiles_files.append(qemu_info.qemu_system)
    if qemu_info.qemu_img:
        env_lines.append('export VMTEST_QEMU_IMG="{}"'.format(qemu_info.qemu_img.short_path))
        runfiles_files.append(qemu_info.qemu_img)

    # Boot configuration
    if ctx.file.kernel:
        env_lines.append('export VMTEST_KERNEL="{}"'.format(ctx.file.kernel.short_path))
        runfiles_files.append(ctx.file.kernel)
    if ctx.file.initrd:
        env_lines.append('export VMTEST_INITRD="{}"'.format(ctx.file.initrd.short_path))
        runfiles_files.append(ctx.file.initrd)
    if ctx.attr.cmdline:
        env_lines.append('export VMTEST_CMDLINE="{}"'.format(ctx.attr.cmdline))
    if ctx.file.iso:
        env_lines.append('export VMTEST_ISO="{}"'.format(ctx.file.iso.short_path))
        runfiles_files.append(ctx.file.iso)

    # UEFI firmware: explicit attrs always used; toolchain defaults only for non-kernel boot
    ovmf_code = ctx.file.ovmf_code if ctx.file.ovmf_code else (qemu_info.ovmf_code if not ctx.file.kernel else None)
    ovmf_vars = ctx.file.ovmf_vars if ctx.file.ovmf_vars else (qemu_info.ovmf_vars if not ctx.file.kernel else None)

    if ovmf_code:
        env_lines.append('export VMTEST_OVMF_CODE="{}"'.format(ovmf_code.short_path))
        runfiles_files.append(ovmf_code)
    if ovmf_vars:
        env_lines.append('export VMTEST_OVMF_VARS="{}"'.format(ovmf_vars.short_path))
        runfiles_files.append(ovmf_vars)

    # Disk
    if ctx.file.disk:
        env_lines.append('export VMTEST_DISK="{}"'.format(ctx.file.disk.short_path))
        runfiles_files.append(ctx.file.disk)
    elif ctx.attr.disk_size:
        env_lines.append('export VMTEST_DISK_SIZE="{}"'.format(ctx.attr.disk_size))

    # VM resources
    env_lines.append('export VMTEST_MEMORY="{}"'.format(ctx.attr.memory))
    env_lines.append('export VMTEST_CPUS="{}"'.format(ctx.attr.cpus))

    # TPM
    if ctx.attr.tpm:
        env_lines.append('export VMTEST_TPM="true"')
        swtpm_info = ctx.toolchains[_SWTPM_TOOLCHAIN_TYPE]
        swtpm_setup_info = ctx.toolchains[_SWTPM_SETUP_TOOLCHAIN_TYPE]
        if swtpm_info:
            swtpm_exe = swtpm_info.executable
            env_lines.append('export VMTEST_SWTPM="{}"'.format(swtpm_exe.short_path))
            runfiles_files.append(swtpm_exe)
        if swtpm_setup_info:
            swtpm_setup_exe = swtpm_setup_info.executable
            env_lines.append('export VMTEST_SWTPM_SETUP="{}"'.format(swtpm_setup_exe.short_path))
            runfiles_files.append(swtpm_setup_exe)

    # Network
    env_lines.append('export VMTEST_NETWORK="{}"'.format(ctx.attr.network))
    if ctx.attr.network == "bridge":
        env_lines.append('export VMTEST_BRIDGE="{}"'.format(ctx.attr.bridge_name))

    # Write the test wrapper script
    test_script = ctx.actions.declare_file(ctx.label.name + ".sh")
    ctx.actions.write(
        output = test_script,
        content = """\
#!/bin/bash
set -euo pipefail
{env}
exec "{test}" "$@"
""".format(
            env = "\n".join(env_lines),
            test = test_binary.short_path,
        ),
        is_executable = True,
    )

    runfiles = ctx.runfiles(files = runfiles_files)
    runfiles = runfiles.merge(ctx.attr.test[DefaultInfo].default_runfiles)

    # Merge runfiles from swtpm toolchains
    if ctx.attr.tpm:
        swtpm_default = ctx.toolchains[_SWTPM_TOOLCHAIN_TYPE].default
        if swtpm_default and swtpm_default.default_runfiles:
            runfiles = runfiles.merge(swtpm_default.default_runfiles)
        swtpm_setup_default = ctx.toolchains[_SWTPM_SETUP_TOOLCHAIN_TYPE].default
        if swtpm_setup_default and swtpm_setup_default.default_runfiles:
            runfiles = runfiles.merge(swtpm_setup_default.default_runfiles)

    return [DefaultInfo(
        executable = test_script,
        runfiles = runfiles,
    )]

vm_go_test = rule(
    implementation = _vm_go_test_impl,
    test = True,
    attrs = {
        "test": attr.label(
            mandatory = True,
            executable = True,
            cfg = "target",
            doc = "Go test binary to run (a go_test target)",
        ),
        "kernel": attr.label(
            allow_single_file = True,
            doc = "Kernel image for direct boot",
        ),
        "initrd": attr.label(
            allow_single_file = True,
            doc = "Initrd for direct kernel boot",
        ),
        "cmdline": attr.string(
            doc = "Kernel command line for direct boot",
        ),
        "iso": attr.label(
            allow_single_file = True,
            doc = "ISO image for UEFI boot",
        ),
        "disk": attr.label(
            allow_single_file = True,
            doc = "Pre-built disk image",
        ),
        "disk_size": attr.string(
            doc = "Size for new disk (e.g., '10G')",
        ),
        "ovmf_code": attr.label(
            allow_single_file = True,
            doc = "OVMF CODE firmware (overrides toolchain)",
        ),
        "ovmf_vars": attr.label(
            allow_single_file = True,
            doc = "OVMF VARS template (overrides toolchain)",
        ),
        "memory": attr.string(
            default = "2G",
            doc = "VM memory size",
        ),
        "cpus": attr.int(
            default = 2,
            doc = "Number of CPUs",
        ),
        "timeout_minutes": attr.int(
            default = 10,
            doc = "Test timeout in minutes",
        ),
        "tpm": attr.bool(
            default = False,
            doc = "Enable TPM 2.0 emulation",
        ),
        "network": attr.string(
            default = "user",
            values = ["user", "bridge", "none"],
            doc = "Network mode: user (SLIRP), bridge (TAP), none",
        ),
        "bridge_name": attr.string(
            default = "mltt-br0",
            doc = "Bridge name (only used when network = 'bridge')",
        ),
    },
    toolchains = [
        _QEMU_TOOLCHAIN_TYPE,
        config_common.toolchain_type(_SWTPM_TOOLCHAIN_TYPE, mandatory = False),
        config_common.toolchain_type(_SWTPM_SETUP_TOOLCHAIN_TYPE, mandatory = False),
    ],
)
