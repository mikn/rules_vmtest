"""vmtest_config: a go_library that configures a VM via init() + runfiles."""

load("@rules_go//go:def.bzl", "go_library")
load("@rules_linux//linux:providers.bzl", "LinuxKernelInfo")

_QEMU_TOOLCHAIN_TYPE = "@rules_qemu//qemu:toolchain_type"
_SWTPM_TOOLCHAIN_TYPE = "@rules_qemu//qemu:swtpm_type"
_SWTPM_SETUP_TOOLCHAIN_TYPE = "@rules_qemu//qemu:swtpm_setup_type"

def _to_rlocationpath(ctx, file):
    """Convert a File to its rlocation path for use with runfiles.Rlocation()."""
    short = file.short_path
    if short.startswith("../"):
        return short[3:]
    return ctx.workspace_name + "/" + short

def _vmtest_config_impl(ctx):
    qemu_info = ctx.toolchains[_QEMU_TOOLCHAIN_TYPE].qemu_info
    pkg_name = ctx.attr.package_name

    # Collect files that need rlocation resolution and literal env vars.
    rlocation_setenvs = []  # list of (env_var, rlocation_path)
    literal_setenvs = []  # list of (env_var, value)
    data_files = []

    # QEMU binaries from toolchain
    if qemu_info.qemu_system:
        rlocation_setenvs.append(("VMTEST_QEMU", _to_rlocationpath(ctx, qemu_info.qemu_system)))
        data_files.append(qemu_info.qemu_system)
    if qemu_info.qemu_img:
        rlocation_setenvs.append(("VMTEST_QEMU_IMG", _to_rlocationpath(ctx, qemu_info.qemu_img)))
        data_files.append(qemu_info.qemu_img)

    # Boot configuration
    if ctx.attr.kernel:
        kernel_info = ctx.attr.kernel[LinuxKernelInfo]
        rlocation_setenvs.append(("VMTEST_KERNEL", _to_rlocationpath(ctx, kernel_info.vmlinuz)))
        data_files.append(kernel_info.vmlinuz)
    if ctx.file.initrd:
        rlocation_setenvs.append(("VMTEST_INITRD", _to_rlocationpath(ctx, ctx.file.initrd)))
        data_files.append(ctx.file.initrd)
    if ctx.attr.cmdline:
        literal_setenvs.append(("VMTEST_CMDLINE", ctx.attr.cmdline))
    if ctx.file.iso:
        rlocation_setenvs.append(("VMTEST_ISO", _to_rlocationpath(ctx, ctx.file.iso)))
        data_files.append(ctx.file.iso)

    # UEFI firmware
    ovmf_code = ctx.file.ovmf_code if ctx.file.ovmf_code else (qemu_info.ovmf_code if not ctx.attr.kernel else None)
    ovmf_vars = ctx.file.ovmf_vars if ctx.file.ovmf_vars else (qemu_info.ovmf_vars if not ctx.attr.kernel else None)
    if ovmf_code:
        rlocation_setenvs.append(("VMTEST_OVMF_CODE", _to_rlocationpath(ctx, ovmf_code)))
        data_files.append(ovmf_code)
    if ovmf_vars:
        rlocation_setenvs.append(("VMTEST_OVMF_VARS", _to_rlocationpath(ctx, ovmf_vars)))
        data_files.append(ovmf_vars)

    # Disk
    if ctx.file.disk:
        rlocation_setenvs.append(("VMTEST_DISK", _to_rlocationpath(ctx, ctx.file.disk)))
        data_files.append(ctx.file.disk)
    elif ctx.attr.disk_size:
        literal_setenvs.append(("VMTEST_DISK_SIZE", ctx.attr.disk_size))

    # VM resources
    literal_setenvs.append(("VMTEST_MEMORY", ctx.attr.memory))
    literal_setenvs.append(("VMTEST_CPUS", str(ctx.attr.cpus)))

    # TPM
    if ctx.attr.tpm:
        literal_setenvs.append(("VMTEST_TPM", "true"))
        swtpm_info = ctx.toolchains[_SWTPM_TOOLCHAIN_TYPE]
        swtpm_setup_info = ctx.toolchains[_SWTPM_SETUP_TOOLCHAIN_TYPE]
        if swtpm_info:
            swtpm_exe = swtpm_info.executable
            rlocation_setenvs.append(("VMTEST_SWTPM", _to_rlocationpath(ctx, swtpm_exe)))
            data_files.append(swtpm_exe)
        if swtpm_setup_info:
            swtpm_setup_exe = swtpm_setup_info.executable
            rlocation_setenvs.append(("VMTEST_SWTPM_SETUP", _to_rlocationpath(ctx, swtpm_setup_exe)))
            data_files.append(swtpm_setup_exe)

    # Machine type and accelerator from toolchain
    if qemu_info.machine_type:
        literal_setenvs.append(("VMTEST_MACHINE_TYPE", qemu_info.machine_type))
    if qemu_info.accel:
        literal_setenvs.append(("VMTEST_ACCEL", qemu_info.accel))

    # Network
    literal_setenvs.append(("VMTEST_NETWORK", ctx.attr.network))
    if ctx.attr.network == "bridge":
        literal_setenvs.append(("VMTEST_BRIDGE", ctx.attr.bridge_name))

    # Port forwards
    if ctx.attr.port_forwards:
        literal_setenvs.append(("VMTEST_PORT_FORWARDS", ",".join([str(p) for p in ctx.attr.port_forwards])))

    # --- Generate Go source ---
    init_lines = []
    if rlocation_setenvs:
        init_lines.append("\tr, err := runfiles.New()")
        init_lines.append('\tif err != nil { panic("vmtest: " + err.Error()) }')
        for env_var, rlocation_path in rlocation_setenvs:
            init_lines.append('\tvmtestMustSetenv(r, "%s", "%s")' % (env_var, rlocation_path))
    for env_var, value in literal_setenvs:
        init_lines.append('\tos.Setenv("%s", "%s")' % (env_var, value))

    content = """\
package {pkg}

import (
\t"os"
\t"testing"

\t"github.com/bazelbuild/rules_go/go/runfiles"
\t"github.com/mikn/rules_vmtest/machine"
)

func init() {{
{init_body}
}}

func vmtestMustSetenv(r *runfiles.Runfiles, key, rlocationPath string) {{
\tp, err := r.Rlocation(rlocationPath)
\tif err != nil {{ panic("vmtest: " + key + ": " + err.Error()) }}
\tos.Setenv(key, p)
}}

// New boots a VM configured by this package.
func New(t testing.TB, opts ...machine.Option) *machine.Machine {{
\treturn machine.New(t, opts...)
}}

// Re-exports for convenience.
type (
\tMachine     = machine.Machine
\tOption      = machine.Option
\tRetryOption = machine.RetryOption
)

var (
\tWithRetryTimeout  = machine.WithRetryTimeout
\tWithRetryInterval = machine.WithRetryInterval
\tWithUserNetwork   = machine.WithUserNetwork
\tWithVMOption      = machine.WithVMOption
\tWithPortForward   = machine.WithPortForward
)
""".format(
        pkg = pkg_name,
        init_body = "\n".join(init_lines),
    )

    go_file = ctx.actions.declare_file(ctx.label.name + ".go")
    ctx.actions.write(go_file, content)

    # Merge toolchain runfiles (shared libraries, etc.)
    data_runfiles = ctx.runfiles(files = data_files)
    if ctx.attr.tpm:
        swtpm_default = ctx.toolchains[_SWTPM_TOOLCHAIN_TYPE].default
        if swtpm_default and swtpm_default.default_runfiles:
            data_runfiles = data_runfiles.merge(swtpm_default.default_runfiles)
        swtpm_setup_default = ctx.toolchains[_SWTPM_SETUP_TOOLCHAIN_TYPE].default
        if swtpm_setup_default and swtpm_setup_default.default_runfiles:
            data_runfiles = data_runfiles.merge(swtpm_setup_default.default_runfiles)

    return [DefaultInfo(
        files = depset([go_file]),
        runfiles = data_runfiles,
    )]

_vmtest_config = rule(
    implementation = _vmtest_config_impl,
    attrs = {
        "package_name": attr.string(mandatory = True),
        "kernel": attr.label(providers = [LinuxKernelInfo]),
        "initrd": attr.label(allow_single_file = True),
        "cmdline": attr.string(),
        "iso": attr.label(allow_single_file = True),
        "disk": attr.label(allow_single_file = True),
        "disk_size": attr.string(),
        "ovmf_code": attr.label(allow_single_file = True),
        "ovmf_vars": attr.label(allow_single_file = True),
        "memory": attr.string(default = "2G"),
        "cpus": attr.int(default = 2),
        "tpm": attr.bool(default = False),
        "network": attr.string(default = "user", values = ["user", "bridge", "none"]),
        "bridge_name": attr.string(default = "mltt-br0"),
        "port_forwards": attr.int_list(default = []),
    },
    toolchains = [
        _QEMU_TOOLCHAIN_TYPE,
        config_common.toolchain_type(_SWTPM_TOOLCHAIN_TYPE, mandatory = False),
        config_common.toolchain_type(_SWTPM_SETUP_TOOLCHAIN_TYPE, mandatory = False),
    ],
)

def vmtest_config(name, importpath, visibility = None, tags = [], **kwargs):
    """Creates a go_library that configures a VM via init() and runfiles.

    Usage in BUILD:
        vmtest_config(
            name = "vmconfig",
            importpath = "my.module/path/to/vmconfig",
            kernel = "//my:kernel",
            initrd = "//my:initrd",
            memory = "1G",
        )

    Usage in Go:
        import "my.module/path/to/vmconfig"

        func TestFoo(t *testing.T) {
            m := vmconfig.New(t)
            m.Succeed(t, "echo hello")
        }

    Args:
        name: Target name. Also used as the Go package name.
        importpath: Go import path for the generated library.
        visibility: Target visibility.
        tags: Tags applied to all generated targets.
        **kwargs: VM configuration (kernel, initrd, memory, cpus, etc.).
    """
    pkg_name = importpath.split("/")[-1]

    _vmtest_config(
        name = name + "_gen",
        package_name = pkg_name,
        tags = tags + ["manual"],
        **kwargs
    )

    go_library(
        name = name,
        srcs = [":" + name + "_gen"],
        importpath = importpath,
        # data includes the gen target for its runfiles (QEMU, kernel, etc.)
        data = [":" + name + "_gen"],
        visibility = visibility,
        deps = [
            "@rules_vmtest//machine",
            "@rules_go//go/runfiles",
        ],
        tags = tags,
    )
