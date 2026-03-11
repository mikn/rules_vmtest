"""Macro to build a test initrd with the vmtest agent injected."""

load("@rules_linux//linux:defs.bzl", "STRIP_PROFILE_NONE", "initrd")
load("@rules_pkg//pkg:tar.bzl", "pkg_tar")

def test_initrd(name, rootfs, strip_profile = None, **kwargs):
    """Build an initrd from a rootfs tar with the vmtest agent automatically injected.

    This merges the agent binary and systemd service into the rootfs before
    converting to cpio.zst via rules_linux.

    Args:
        name: Target name. Output will be <name>.cpio.zst.
        rootfs: Label of the rootfs tar file.
        strip_profile: Strip profile for initrd compression. Defaults to STRIP_PROFILE_NONE.
        **kwargs: Additional arguments passed to the initrd rule.
    """
    pkg_tar(
        name = name + "_with_agent",
        deps = [rootfs, "@rules_qemu//agent:agent_tar"],
    )

    initrd(
        name = name,
        rootfs = ":" + name + "_with_agent",
        strip_profile = strip_profile or STRIP_PROFILE_NONE,
        **kwargs
    )
