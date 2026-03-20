# rules_vmtest

Bazel VM testing framework. Boots QEMU VMs in tests with bidirectional RPC (virtio-serial), providing NixOS-style test primitives (`Succeed`, `WaitForUnit`, `WaitForOpenPort`) in Go.

## Commands

```bash
bazel test //...                    # Run all tests (unit + analysis)
bazel run //:gazelle                # Regenerate BUILD files after Go changes
```

## Structure

- `vm/` — Core VM lifecycle: `Start()`, functional options, QMP, serial console, TPM, networking, agent connection
- `machine/` — High-level test API: `New(t)`, `Succeed()`, `WaitForUnit()`, `WaitForOpenPort()`, `Background()`
- `agent/` — Guest-side RPC server (static Go binary, ~4MB). Runs inside VM on virtio-serial port
- `agentpb/` — Shared RPC types (request/response structs, `net/rpc` + `jsonrpc` codec)
- `runner/` — VM runner for build/run mode (not testing)
- `orchestrator/` — Multi-process tmux orchestrator
- `vmtest/defs.bzl` — Public Starlark API: `vm_test`, `vm_go_test`, `vmtest_config`
- `vmtest/config.bzl` — `vmtest_config`: generates `go_library` that sets `VMTEST_*` env vars via `init()`
- `vmtest/vm_test.bzl` — Shell-based VM test rule
- `vmtest/vm_go_test.bzl` — Go VM test rule
- `vmtest/test_initrd.bzl` — Auto-injects agent into rootfs
- `examples/` — Usage patterns (kernel_boot, systemd_service)
- `scripts/` — Bridge/TAP network setup scripts

## Key Patterns

- **Functional options**: `vm.WithKernelBoot(...)`, `vm.WithISO(...)`, `vm.WithTPM(...)`, etc.
- **Agent RPC**: Host connects via Unix socket → virtio-serial → guest agent. Protocol: `net/rpc` + `jsonrpc`.
- **Serial console**: `serial.OnLine()` returns `<-chan struct{}` for synchronization. `serial.WaitFor()` for pattern matching. Never use both `Lines()` channel and `WaitFor()` concurrently.
- **Testing cleanup**: `Kill()` → `serial.Close()` → `serial.Wait()` → OnLine goroutine drains. Prevents `t.Logf` race.
- **vmtest_config**: Generates Go `init()` with `runfiles.Rlocation()` calls. Single-dep pattern — re-exports `machine.New`.
- **Unix socket 108-byte limit**: Always use `/tmp` for sockets, not deep Bazel sandbox paths.

## Code Quality

- **Go**: Fail fast — `return fmt.Errorf(...)` on all errors. No silent fallbacks. No `_` error ignoring.
- **Tests**: Mock via `net/rpc` server over Unix socket (see `machine_integration_test.go`). Test error paths first.
- **Starlark**: Validate all rule attrs. Use `ctx.actions.declare_file()`. Propagate runfiles correctly (critical for `vmtest_config`).
- **Agent binary**: Must be `pure = "on"`, `static = "on"`, stripped. Zero CGO. Only stdlib deps in agent itself.
- **Serial safety**: Goroutines consuming serial output must be properly synchronized with test cleanup. See `OnLine()` return channel pattern.
- **No shell parsing**: Use structured RPC for guest commands. `WaitForUnit` calls `Agent.UnitStatus` directly, not `systemctl | grep`.

## Pitfalls & Learnings

### Machine primitives

- **`Machine.Background()`**: Uses `Agent.Background` RPC → `systemd-run --no-block --collect --` with argv (no shell). For shell commands in background: `m.Background(t, "bash", "-c", "cmd1 && cmd2")`.
- **`Machine.ForwardedPort(guestPort)`**: Returns the host port that forwards to the given guest port. Delegates to `vm.ForwardedPort()`. Returns 0 if not configured.
- **`WithPortForward(guestPort)`**: Machine option that requests a host-to-guest port forward. Wraps `vm.WithPortForward()`. Forces user-mode networking. Guest services must bind `0.0.0.0`.
- **`VMTEST_PORT_FORWARDS` env var**: Comma-separated list of guest ports (e.g., `"50051,50052"`). Parsed in `buildVMOptsFromEnv` after `VMTEST_NETWORK`, so port forwards override `network = "none"`. Set by `vmtest_config` from the `port_forwards` BUILD attribute.
- **E2E vmtest performance**: Uses `//build/linux/base:base_rootfs` (Debian Bookworm + systemd) + Debian kernel from `@usi_base`. Boots in ~2s with systemd, all 11 machine primitives pass in ~5s.

### vmtest Starlark macros

- **`test_initrd`**: Auto-injects agent into rootfs by merging agent_tar with the user's rootfs tar, then creating a cpio initrd. The agent tar must contain the agent binary and its systemd service file.
- **`vmtest_config`**: Generates a `go_library` that calls `runfiles.Rlocation()` for kernel and initrd paths. The generated init() function sets environment variables that `machine.New(t)` reads. This is the glue between Bazel runfiles and Go test code.
- **`vm_go_test`**: Wraps `go_test` with VM-specific attributes (kernel, initrd, cpus, memory, cmdline). The test binary runs on the host and communicates with the VM via the agent.

### Agent RPC

- **Guest DNS**: VMs may have no DNS resolver. The agent's `resolveAddr()` maps `"localhost"` → `"127.0.0.1"` to avoid lookups that would hang.
- **Go os/exec pipe gotcha**: `cmd.Run()` blocks until ALL inherited fds close (including backgrounded processes). The `Background` RPC uses `systemd-run` to fully detach processes.

### Serial console

- **Lines may be dropped**: Both `lines` and `logChan` use non-blocking sends. If the consumer is slow, lines are dropped.
- **Cleanup order matters**: `Kill()` → `serial.Close()` → `serial.Wait()` → OnLine goroutine drains. Wrong order causes `t.Logf` data races.

### VM lifecycle

- **Unix socket path limit**: 108 bytes max for `sun_path`. Bazel sandbox paths easily exceed this. Always use `/tmp` for QEMU monitor and agent sockets.
- **9P symlinks are dangling**: Symlinks to host absolute paths don't resolve inside the guest. Copy files into the shared directory.
- **virtio-serial without udev**: Scan `/sys/class/virtio-ports/vport*/name` and create `/dev/virtio-ports/` symlinks manually.

### Monorepo integration

- **Bulldozer integration test**: `runner_lib` has OVMF `x_defs` baked in at `go_library` level (not `go_test`). The new `vm` package resolves OVMF from runfiles explicitly.
- **`STRIP_PROFILE_SERVER`**: Strips perl, python from initrds. Available tools: bash, coreutils, util-linux, systemd, udev.

## Self-Correction Protocol

When you receive a correction from the user about rules_vmtest patterns, machine primitives, agent behavior, or VM lifecycle, update the "Pitfalls & Learnings" section of this file to capture the correction. This prevents repeating the same mistakes across conversations.

## Releasing

See [RELEASING.md](RELEASING.md). Tag push → GitHub release → `publish-to-bcr` reusable workflow auto-opens BCR PR. Depends on `rules_qemu` and `rules_linux` — publish those first.
