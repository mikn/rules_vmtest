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

## Releasing

See [RELEASING.md](RELEASING.md). Tag push → GitHub release → `publish-to-bcr` reusable workflow auto-opens BCR PR. Depends on `rules_qemu` and `rules_linux` — publish those first.
