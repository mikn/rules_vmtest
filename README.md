# rules_vmtest

Bazel rules and Go libraries for VM-based integration testing. Boots QEMU virtual machines in tests with bidirectional RPC via virtio-serial, providing NixOS-style test primitives in Go.

## Setup

```starlark
# MODULE.bazel
bazel_dep(name = "rules_vmtest", version = "0.1.0")
bazel_dep(name = "rules_qemu", version = "0.1.0")  # QEMU + OVMF + swtpm toolchains
```

## Quick Start

The recommended pattern uses `vmtest_config` to generate a Go library that wires up VM configuration from Bazel attributes:

```starlark
# BUILD.bazel
load("@rules_vmtest//vmtest:defs.bzl", "vmtest_config")
load("@rules_go//go:def.bzl", "go_test")

vmtest_config(
    name = "vmconfig",
    importpath = "example.com/myproject/tests/vmconfig",
    kernel = "//my:kernel",
    initrd = "//my:rootfs",
    cmdline = "console=ttyS0 rdinit=/init",
    memory = "1G",
    cpus = 2,
)

go_test(
    name = "my_test",
    srcs = ["my_test.go"],
    deps = [":vmconfig"],
    tags = ["exclusive", "requires-kvm"],
)
```

```go
package mytest

import (
    "testing"
    "example.com/myproject/tests/vmconfig"
    "github.com/stretchr/testify/assert"
)

func TestKernelBoot(t *testing.T) {
    m := vmconfig.New(t)
    out := m.Succeed(t, "uname -r")
    assert.Contains(t, out, "6.12")
}
```

## Machine API

The `machine` package provides NixOS-style test primitives:

```go
m := vmconfig.New(t)  // boots VM, connects agent, registers cleanup

// Command execution
out := m.Succeed(t, "echo hello")       // fatal on non-zero exit
m.Fail(t, "false")                       // fatal on zero exit
m.WaitUntilSucceeds(t, "curl localhost") // retry until success

// Systemd
m.WaitForUnit(t, "nginx.service")

// Network
m.WaitForOpenPort(t, 8080)
m.WaitForClosedPort(t, 8080)

// Filesystem
m.WaitForFile(t, "/var/run/app.pid")
m.CopyFromHost(t, "testdata/config.yaml", "/etc/app/config.yaml")

// Background services
m.Background(t, "my-test-server", "/usr/bin/server --port 8080")

// VM control
m.Shutdown(t)
```

All `WaitFor*` methods accept retry options: `machine.WithRetryTimeout(2*time.Minute)`, `machine.WithRetryInterval(500*time.Millisecond)`.

### Port Forwarding

Forward guest ports to the host to enable direct network access (e.g., gRPC, HTTP) from test code:

```starlark
vmtest_config(
    name = "vmconfig",
    importpath = "example.com/myproject/tests/vmconfig",
    kernel = "//my:kernel",
    initrd = "//my:rootfs",
    port_forwards = [8080, 50051],  # guest ports to forward
)
```

```go
func TestGRPC(t *testing.T) {
    m := vmconfig.New(t)
    m.WaitForOpenPort(t, 50051)

    // Get the QEMU-assigned host port and dial directly
    hostPort := m.ForwardedPort(50051)
    conn, _ := grpc.Dial(fmt.Sprintf("localhost:%d", hostPort), grpc.WithInsecure())
    client := pb.NewMyServiceClient(conn)
    // ... test via direct gRPC from host
}
```

Port forwarding uses QEMU's SLIRP user-mode networking with ephemeral host ports (`hostfwd=tcp::0-:PORT`). Guest services must bind `0.0.0.0` (not `127.0.0.1`) because SLIRP sends packets to the guest's network interface IP, not localhost.

## Starlark Rules

Exported from `@rules_vmtest//vmtest:defs.bzl`:

| Rule | Purpose |
|------|---------|
| `vmtest_config` | Generates a Go library with VM config from Bazel attributes (recommended) |
| `vm_go_test` | Wraps a `go_test` with `VMTEST_*` env vars for VM boot configuration |
| `vm_test` | Shell-based VM test (boot + match serial output) |

Additional macros in `@rules_vmtest//vmtest:rules.bzl`:

| Macro | Purpose |
|-------|---------|
| `qemu_vm` | Build-mode VM binary (for creating disk images, etc.) |
| `qemu_vm_run` | Run-mode VM binary (interactive, with tmux) |
| `qemu_cluster_build` | Multi-node cluster for build workflows |
| `qemu_cluster_run` | Multi-node cluster for interactive use |

## Go Packages

### `vm` — Core VM lifecycle

Low-level VM management with functional options:

```go
import "github.com/mikn/rules_vmtest/vm"

v, err := vm.Start(ctx,
    vm.WithKernelBoot(kernel, initrd, "console=ttyS0"),
    vm.WithMemory("2G"),
    vm.WithCPUs(2),
    vm.WithSerialCapture(),
    vm.WithQMP(),
    vm.WithAgent(),
    vm.WithTPM("swtpm", "swtpm_setup"),
    vm.WithUEFI(ovmfCode, ovmfVars),
    vm.WithISO(isoPath),
    vm.WithDisk("50G"),
    vm.WithTapDevice("tap0", "br0"),
    vm.With9PShare("shared", "/host/path"),
)
defer v.Kill()

serial := v.Serial()
serial.OnLine(func(line string) { fmt.Println(line) })
serial.WaitFor(ctx, regexp.MustCompile(`login:`))
```

### `machine` — High-level test API

Wraps `vm` + agent connection with test-friendly methods (see Machine API above).

### `agentpb` — Shared RPC types

Request/response types for host-guest communication: `ExecRequest`, `ExecResponse`, `UnitRequest`, `UnitResponse`, `FileRequest`, `FileResponse`, `PortRequest`, `PortResponse`, etc.

### `agent` — Guest agent binary

Static Go binary (~4MB) that runs inside the VM, serving RPC over virtio-serial (`/dev/virtio-ports/org.vmtest.agent`). Implements command execution, systemd unit queries, port checks, and file operations.

## Guest Agent Setup

The guest VM must have the agent binary running. For systemd-based images, include the agent and its service unit:

```starlark
load("@rules_pkg//pkg:tar.bzl", "pkg_tar")

pkg_tar(
    name = "rootfs_with_agent",
    deps = [
        ":my_rootfs",
        "@rules_qemu//agent:agent_tar",
    ],
)
```

The included systemd unit has `ConditionVirtualization=vm`, so the agent only starts when running inside a virtual machine.

For auto-injection into initrds, use the `test_initrd` macro:

```starlark
load("@rules_vmtest//vmtest:test_initrd.bzl", "test_initrd")

test_initrd(
    name = "test_rootfs",
    rootfs = ":base_rootfs",
)
```

## Architecture

```
Host (Go test)                    Guest (VM)
     |                                |
     |--- virtio-serial (RPC) ------->| agent binary
     |                                |   /dev/virtio-ports/org.vmtest.agent
     |<-- serial console (logs) ------| ttyS0
     |                                |
     |--- QMP (VM control) ---------->| QEMU monitor
```

Communication uses `net/rpc` with `jsonrpc` codec over virtio-serial. Zero external dependencies on both sides.
