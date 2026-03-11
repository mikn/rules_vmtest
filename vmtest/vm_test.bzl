"""vm_test rule: boot a VM, run a test, determine pass/fail."""

_QEMU_TOOLCHAIN_TYPE = "@rules_qemu//qemu:toolchain_type"
_SWTPM_TOOLCHAIN_TYPE = "@rules_qemu//qemu:swtpm_type"
_SWTPM_SETUP_TOOLCHAIN_TYPE = "@rules_qemu//qemu:swtpm_setup_type"

def _vm_test_impl(ctx):
    qemu_info = ctx.toolchains[_QEMU_TOOLCHAIN_TYPE].qemu_info
    runner = ctx.executable._runner

    args = []
    runfiles_files = []

    # QEMU binary paths from toolchain
    if qemu_info.qemu_system:
        args.extend(["--qemu", qemu_info.qemu_system.short_path])
        runfiles_files.append(qemu_info.qemu_system)
    if qemu_info.qemu_img:
        args.extend(["--qemu-img", qemu_info.qemu_img.short_path])
        runfiles_files.append(qemu_info.qemu_img)

    # Boot configuration
    if ctx.file.kernel:
        args.extend(["--kernel", ctx.file.kernel.short_path])
        runfiles_files.append(ctx.file.kernel)
    if ctx.file.initrd:
        args.extend(["--initrd", ctx.file.initrd.short_path])
        runfiles_files.append(ctx.file.initrd)
    if ctx.attr.cmdline:
        args.extend(["--cmdline", ctx.attr.cmdline])
    if ctx.file.iso:
        args.extend(["--iso", ctx.file.iso.short_path])
        runfiles_files.append(ctx.file.iso)

    # UEFI firmware: explicit attrs override toolchain defaults
    ovmf_code = ctx.file.ovmf_code if ctx.file.ovmf_code else qemu_info.ovmf_code
    ovmf_vars = ctx.file.ovmf_vars if ctx.file.ovmf_vars else qemu_info.ovmf_vars

    if ovmf_code:
        args.extend(["--ovmf-code", ovmf_code.short_path])
        runfiles_files.append(ovmf_code)
    if ovmf_vars:
        args.extend(["--ovmf-vars", ovmf_vars.short_path])
        runfiles_files.append(ovmf_vars)

    # Disk
    if ctx.file.disk:
        args.extend(["--disk", ctx.file.disk.short_path])
        runfiles_files.append(ctx.file.disk)
    elif ctx.attr.disk_size:
        args.extend(["--disk-size", ctx.attr.disk_size])

    # VM resources
    args.extend(["--memory", ctx.attr.memory])
    args.extend(["--cpus", str(ctx.attr.cpus)])
    args.extend(["--timeout", str(ctx.attr.timeout_minutes)])

    # TPM — resolve swtpm from dedicated toolchain types
    if ctx.attr.tpm:
        args.append("--tpm")
        swtpm_info = ctx.toolchains[_SWTPM_TOOLCHAIN_TYPE]
        swtpm_setup_info = ctx.toolchains[_SWTPM_SETUP_TOOLCHAIN_TYPE]
        if swtpm_info:
            swtpm_exe = swtpm_info.executable
            args.extend(["--swtpm", swtpm_exe.short_path])
            runfiles_files.append(swtpm_exe)
        if swtpm_setup_info:
            swtpm_setup_exe = swtpm_setup_info.executable
            args.extend(["--swtpm-setup", swtpm_setup_exe.short_path])
            runfiles_files.append(swtpm_setup_exe)

    # Machine type and accelerator from toolchain
    if qemu_info.machine_type:
        args.extend(["--machine-type", qemu_info.machine_type])
    if qemu_info.accel:
        args.extend(["--accel", qemu_info.accel])

    # Network
    args.extend(["--network", ctx.attr.network])

    # 9P share directory: assemble test_script + data into a tree
    share_files = []
    if ctx.file.test_script:
        share_files.append(ctx.file.test_script)
    share_files.extend(ctx.files.data)

    if share_files:
        share_dir = ctx.actions.declare_directory(ctx.label.name + "_share")
        commands = ["mkdir -p " + share_dir.path]
        if ctx.file.test_script:
            commands.append("cp {src} {dst}/test.sh && chmod +x {dst}/test.sh".format(
                src = ctx.file.test_script.path,
                dst = share_dir.path,
            ))
        for f in ctx.files.data:
            commands.append("cp {src} {dst}/{name}".format(
                src = f.path,
                dst = share_dir.path,
                name = f.basename,
            ))
        ctx.actions.run_shell(
            outputs = [share_dir],
            inputs = share_files,
            command = " && ".join(commands),
            mnemonic = "PrepareVMTestShare",
        )
        args.extend(["--share-dir", share_dir.short_path])
        runfiles_files.append(share_dir)

    # Write the test wrapper script
    test_script = ctx.actions.declare_file(ctx.label.name + ".sh")

    # Quote arguments that may contain spaces
    quoted_args = []
    for a in args:
        if " " in a or "'" in a:
            quoted_args.append("'" + a.replace("'", "'\\''") + "'")
        else:
            quoted_args.append(a)

    ctx.actions.write(
        output = test_script,
        content = """\
#!/bin/bash
set -euo pipefail
RUNFILES_DIR="${{BASH_SOURCE[0]}}.runfiles"
exec "{runner}" {args} "$@"
""".format(
            runner = runner.short_path,
            args = " ".join(quoted_args),
        ),
        is_executable = True,
    )

    runfiles = ctx.runfiles(files = runfiles_files)
    runfiles = runfiles.merge(ctx.attr._runner[DefaultInfo].default_runfiles)

    # Merge runfiles from swtpm toolchains (shared libs for hermetic swtpm, etc.)
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

vm_test = rule(
    implementation = _vm_test_impl,
    test = True,
    attrs = {
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
        "test_script": attr.label(
            allow_single_file = True,
            doc = "Shell script to run inside the VM",
        ),
        "data": attr.label_list(
            allow_files = True,
            doc = "Additional files shared into the VM via 9P",
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
            values = ["user", "none"],
            doc = "Network mode",
        ),
        "_runner": attr.label(
            default = "@rules_vmtest//vmtest/cmd/testrunner",
            executable = True,
            cfg = "target",
        ),
    },
    toolchains = [
        _QEMU_TOOLCHAIN_TYPE,
        config_common.toolchain_type(_SWTPM_TOOLCHAIN_TYPE, mandatory = False),
        config_common.toolchain_type(_SWTPM_SETUP_TOOLCHAIN_TYPE, mandatory = False),
    ],
)
