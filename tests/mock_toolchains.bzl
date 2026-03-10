"""Mock toolchain rules for testing."""

def _mock_swtpm_toolchain_impl(ctx):
    """Creates a mock toolchain that mimics the ARM toolchain_utils pattern."""
    return [platform_common.ToolchainInfo(
        executable = ctx.executable.executable,
        default = DefaultInfo(
            default_runfiles = ctx.runfiles(files = [ctx.executable.executable]),
        ),
    )]

mock_swtpm_toolchain = rule(
    implementation = _mock_swtpm_toolchain_impl,
    attrs = {
        "executable": attr.label(
            mandatory = True,
            executable = True,
            cfg = "target",
        ),
    },
)
