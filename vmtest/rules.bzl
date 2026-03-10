"""Macros for QEMU VM targets using go_binary with x_defs."""

load("@rules_go//go:def.bzl", "go_binary")

def _add_tool_deps(x_defs, data_deps, ovmf_code, ovmf_vars, swtpm, swtpm_setup):
    """Wire OVMF and swtpm tool paths into x_defs and data_deps."""
    if ovmf_code:
        x_defs["main.vmOVMFCode"] = "$(rlocationpath " + ovmf_code + ")"
        data_deps.append(ovmf_code)
    if ovmf_vars:
        x_defs["main.vmOVMFVars"] = "$(rlocationpath " + ovmf_vars + ")"
        data_deps.append(ovmf_vars)
    if swtpm:
        x_defs["main.vmSwtpm"] = "$(rlocationpath " + swtpm + ")"
        data_deps.append(swtpm)
    if swtpm_setup:
        x_defs["main.vmSwtpmSetup"] = "$(rlocationpath " + swtpm_setup + ")"
        data_deps.append(swtpm_setup)

def _add_transit_network(x_defs, transit_ipv4, transit_ipv6):
    """Wire transit network addresses into x_defs for host tap validation."""
    if transit_ipv4:
        x_defs["main.vmTransitIPv4"] = transit_ipv4
    if transit_ipv6:
        x_defs["main.vmTransitIPv6"] = transit_ipv6

def qemu_vm(
        name,
        server_name,
        tap_device,
        mac_address,
        runner_lib = "@rules_vmtest//vmtest/cmd:cmd_lib",
        orchestrator_lib = "@rules_vmtest//orchestrator:orchestrator_lib",
        ovmf_code = None,
        ovmf_vars = None,
        swtpm = None,
        swtpm_setup = None,
        bridge_name = "mltt-br0",
        transit_ipv4 = None,
        transit_ipv6 = None,
        memory = "6G",
        cpus = 2,
        disk_size = "50G",
        second_tap_device = None,
        iso = None,
        tpm_pristine_path = None,
        needs_running = None,
        visibility = None,
        **kwargs):
    """Creates a QEMU VM build target using go_binary with x_defs.

    Args:
        name: The target name
        server_name: The VM server name (e.g., "dev001")
        tap_device: The tap device to use (e.g., "mltt-tap1")
        mac_address: MAC address for the VM
        runner_lib: Label for the VM runner go_library
        orchestrator_lib: Label for the orchestrator go_library
        ovmf_code: Label for OVMF CODE firmware file
        ovmf_vars: Label for OVMF VARS template file
        swtpm: Label for swtpm binary
        swtpm_setup: Label for swtpm_setup binary
        bridge_name: Network bridge name
        transit_ipv4: Expected IPv4 address on host tap device (e.g., "192.168.177.1/28")
        transit_ipv6: Expected IPv6 address on host tap device (e.g., "fd00:6000:100::1/64")
        memory: VM memory size
        cpus: Number of CPUs
        disk_size: Disk size
        second_tap_device: Optional second tap device
        iso: ISO target for provisioning
        tpm_pristine_path: Workspace-relative path to pristine TPM state
        needs_running: Dependencies that need to be running during build
        visibility: Target visibility
        **kwargs: Additional arguments passed to go_binary
    """

    # Build x_defs dictionary
    x_defs = {
        "main.vmMode": "build",
        "main.vmServer": server_name,
        "main.vmTap": tap_device,
        "main.vmMac": mac_address,
        "main.vmMemory": memory,
        "main.vmCPUs": str(cpus),
        "main.vmDiskSize": disk_size,
        "main.vmBridgeName": bridge_name,
    }

    if second_tap_device:
        x_defs["main.vmSecondTap"] = second_tap_device

    if tpm_pristine_path:
        x_defs["main.vmTPMPristinePath"] = tpm_pristine_path

    data_deps = []

    _add_tool_deps(x_defs, data_deps, ovmf_code, ovmf_vars, swtpm, swtpm_setup)
    _add_transit_network(x_defs, transit_ipv4, transit_ipv6)

    # Add ISO runfile path if provided
    if iso:
        x_defs["main.vmISORunfile"] = "$(rlocationpath " + iso + ")"
        data_deps.append(iso)

    if needs_running:
        # Build the actual VM binary
        vm_binary_name = name + "_vm"
        go_binary(
            name = vm_binary_name,
            embed = [runner_lib],
            x_defs = x_defs,
            data = data_deps,
            visibility = ["//visibility:private"],
        )

        # Build orchestrator that runs dependencies
        dep_runfiles = ["$(rlocationpath " + d + ")" for d in needs_running]

        go_binary(
            name = name,
            embed = [orchestrator_lib],
            x_defs = {
                "main.vmPrimary": "$(rlocationpath :" + vm_binary_name + ")",
                "main.vmDependencies": ",".join(dep_runfiles),
            },
            data = needs_running + [":" + vm_binary_name],
            args = ["--run", "$(rlocationpath :" + vm_binary_name + ")"],
            visibility = visibility,
            **kwargs
        )
    else:
        go_binary(
            name = name,
            embed = [runner_lib],
            x_defs = x_defs,
            data = data_deps,
            visibility = visibility,
            **kwargs
        )

def qemu_vm_run(
        name,
        server_name,
        tap_device,
        mac_address,
        runner_lib = "@rules_vmtest//vmtest/cmd:cmd_lib",
        orchestrator_lib = "@rules_vmtest//orchestrator:orchestrator_lib",
        ovmf_code = None,
        ovmf_vars = None,
        swtpm = None,
        swtpm_setup = None,
        bridge_name = "mltt-br0",
        transit_ipv4 = None,
        transit_ipv6 = None,
        memory = "6G",
        cpus = 2,
        disk_size = "50G",
        second_tap_device = None,
        needs_running = None,
        visibility = None,
        **kwargs):
    """Creates a QEMU VM run target using go_binary with x_defs.

    Args:
        name: The target name
        server_name: The VM server name
        tap_device: Tap device name
        mac_address: MAC address
        runner_lib: Label for the VM runner go_library
        orchestrator_lib: Label for the orchestrator go_library
        ovmf_code: Label for OVMF CODE firmware file
        ovmf_vars: Label for OVMF VARS template file
        swtpm: Label for swtpm binary
        swtpm_setup: Label for swtpm_setup binary
        bridge_name: Network bridge name
        transit_ipv4: Expected IPv4 address on host tap device
        transit_ipv6: Expected IPv6 address on host tap device
        memory: VM memory size
        cpus: Number of CPUs
        disk_size: Disk size
        second_tap_device: Optional second tap device
        needs_running: Dependencies that need to be running
        visibility: Target visibility
        **kwargs: Additional arguments passed to go_binary
    """

    x_defs = {
        "main.vmMode": "run",
        "main.vmServer": server_name,
        "main.vmTap": tap_device,
        "main.vmMac": mac_address,
        "main.vmMemory": memory,
        "main.vmCPUs": str(cpus),
        "main.vmDiskSize": disk_size,
        "main.vmBridgeName": bridge_name,
    }

    if second_tap_device:
        x_defs["main.vmSecondTap"] = second_tap_device

    data_deps = []

    _add_tool_deps(x_defs, data_deps, ovmf_code, ovmf_vars, swtpm, swtpm_setup)
    _add_transit_network(x_defs, transit_ipv4, transit_ipv6)

    if needs_running:
        vm_binary_name = name + "_vm"
        go_binary(
            name = vm_binary_name,
            embed = [runner_lib],
            x_defs = x_defs,
            data = data_deps,
            visibility = visibility,
        )

        dep_runfiles = ["$(rlocationpath " + d + ")" for d in needs_running]

        go_binary(
            name = name,
            embed = [orchestrator_lib],
            x_defs = {
                "main.vmPrimary": "$(rlocationpath :" + vm_binary_name + ")",
                "main.vmDependencies": ",".join(dep_runfiles),
            },
            data = needs_running + [":" + vm_binary_name],
            visibility = visibility,
            **kwargs
        )
    else:
        go_binary(
            name = name,
            embed = [runner_lib],
            x_defs = x_defs,
            data = data_deps,
            visibility = visibility,
            **kwargs
        )

def qemu_cluster_build(
        name,
        nodes,
        runner_lib = "@rules_vmtest//vmtest/cmd:cmd_lib",
        orchestrator_lib = "@rules_vmtest//orchestrator:orchestrator_lib",
        ovmf_code = None,
        ovmf_vars = None,
        swtpm = None,
        swtpm_setup = None,
        bridge_name = "mltt-br0",
        transit_ipv4 = None,
        transit_ipv6 = None,
        bgp_server = None,
        default_iso = None,
        default_tpm_pristine_base = None,
        visibility = None,
        **kwargs):
    """Creates a cluster build target where all nodes are built together.

    Args:
        name: The target name (e.g., "cluster_build")
        nodes: List of node configurations (server_name, tap_device, mac_address, etc.)
        runner_lib: Label for the VM runner go_library
        orchestrator_lib: Label for the orchestrator go_library
        ovmf_code: Label for OVMF CODE firmware file
        ovmf_vars: Label for OVMF VARS template file
        swtpm: Label for swtpm binary
        swtpm_setup: Label for swtpm_setup binary
        bridge_name: Network bridge name
        transit_ipv4: Expected IPv4 address on host tap device (e.g., "192.168.177.1/28")
        transit_ipv6: Expected IPv6 address on host tap device (e.g., "fd00:6000:100::1/64")
        bgp_server: BGP server target to run
        default_iso: Default ISO target for provisioning
        default_tpm_pristine_base: Default workspace-relative base path for TPM states
        visibility: Target visibility
        **kwargs: Additional arguments passed to targets
    """

    if not nodes:
        fail("qemu_cluster_build requires at least one node")

    build_vm_targets = []

    for i, node in enumerate(nodes):
        build_node_name = "_%s_node%d" % (name, i)

        node_iso = node.get("iso", default_iso)
        tpm_path = node.get("tpm_pristine_path", None)
        if tpm_path == None and default_tpm_pristine_base:
            tpm_path = default_tpm_pristine_base + "/" + node["server_name"]

        qemu_vm(
            name = build_node_name,
            server_name = node["server_name"],
            tap_device = node["tap_device"],
            mac_address = node["mac_address"],
            runner_lib = runner_lib,
            orchestrator_lib = orchestrator_lib,
            ovmf_code = ovmf_code,
            ovmf_vars = ovmf_vars,
            swtpm = swtpm,
            swtpm_setup = swtpm_setup,
            bridge_name = bridge_name,
            transit_ipv4 = transit_ipv4,
            transit_ipv6 = transit_ipv6,
            memory = node.get("memory", "6G"),
            cpus = node.get("cpus", 2),
            disk_size = node.get("disk_size", "50G"),
            second_tap_device = node.get("second_tap_device", None),
            iso = node_iso,
            tpm_pristine_path = tpm_path,
            needs_running = None,
            visibility = ["//visibility:private"],
        )
        build_vm_targets.append(":" + build_node_name)

    build_deps = []
    data_deps = []

    if bgp_server:
        build_deps.append(bgp_server)
        data_deps.append(bgp_server)

    build_deps.extend(build_vm_targets[:-1])
    data_deps.extend(build_vm_targets)

    go_binary(
        name = name,
        embed = [orchestrator_lib],
        x_defs = {
            "main.vmPrimary": "$(rlocationpath %s)" % build_vm_targets[-1],
            "main.vmDependencies": ",".join(["$(rlocationpath %s)" % d for d in build_deps]),
        },
        data = data_deps,
        visibility = visibility,
        **kwargs
    )

def qemu_cluster_run(
        name,
        nodes,
        runner_lib = "@rules_vmtest//vmtest/cmd:cmd_lib",
        orchestrator_lib = "@rules_vmtest//orchestrator:orchestrator_lib",
        ovmf_code = None,
        ovmf_vars = None,
        swtpm = None,
        swtpm_setup = None,
        bridge_name = "mltt-br0",
        transit_ipv4 = None,
        transit_ipv6 = None,
        bgp_server = None,
        visibility = None,
        **kwargs):
    """Creates a cluster run target where all nodes run together.

    Args:
        name: The target name (e.g., "cluster")
        nodes: List of node configurations (server_name, tap_device, mac_address, etc.)
        runner_lib: Label for the VM runner go_library
        orchestrator_lib: Label for the orchestrator go_library
        ovmf_code: Label for OVMF CODE firmware file
        ovmf_vars: Label for OVMF VARS template file
        swtpm: Label for swtpm binary
        swtpm_setup: Label for swtpm_setup binary
        bridge_name: Network bridge name
        transit_ipv4: Expected IPv4 address on host tap device
        transit_ipv6: Expected IPv6 address on host tap device
        bgp_server: BGP server target to run
        visibility: Target visibility
        **kwargs: Additional arguments passed to targets
    """

    if not nodes:
        fail("qemu_cluster_run requires at least one node")

    run_vm_targets = []

    for i, node in enumerate(nodes):
        run_node_name = "_%s_node%d" % (name, i)

        x_defs = {
            "main.vmMode": "run",
            "main.vmServer": node["server_name"],
            "main.vmTap": node["tap_device"],
            "main.vmMac": node["mac_address"],
            "main.vmMemory": "6G",
            "main.vmCPUs": "2",
            "main.vmDiskSize": "50G",
            "main.vmBridgeName": bridge_name,
        }

        if node.get("second_tap_device"):
            x_defs["main.vmSecondTap"] = node["second_tap_device"]

        data_deps = []

        _add_tool_deps(x_defs, data_deps, ovmf_code, ovmf_vars, swtpm, swtpm_setup)
        _add_transit_network(x_defs, transit_ipv4, transit_ipv6)

        go_binary(
            name = run_node_name,
            embed = [runner_lib],
            x_defs = x_defs,
            data = data_deps,
            visibility = ["//visibility:private"],
        )
        run_vm_targets.append(":" + run_node_name)

    run_deps = []
    data_deps = []

    if bgp_server:
        run_deps.append(bgp_server)
        data_deps.append(bgp_server)

    run_deps.extend(run_vm_targets[:-1])
    data_deps.extend(run_vm_targets)

    go_binary(
        name = name,
        embed = [orchestrator_lib],
        x_defs = {
            "main.vmPrimary": "$(rlocationpath %s)" % run_vm_targets[-1],
            "main.vmDependencies": ",".join(["$(rlocationpath %s)" % d for d in run_deps]),
        },
        data = data_deps,
        visibility = visibility,
        **kwargs
    )
