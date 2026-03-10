package machine

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mikn/rules_vmtest/vm"
)

// Default retry settings for polling operations.
const (
	defaultRetryTimeout  = 60 * time.Second
	defaultRetryInterval = 1 * time.Second
)

// Machine wraps a VM with an agent connection for high-level test primitives.
type Machine struct {
	vm    *vm.VM
	agent *vm.AgentConn
	t     testing.TB
}

// Option configures a Machine.
type Option func(*machineConfig)

type machineConfig struct {
	vmOpts []vm.Option
}

// WithBridge configures bridge networking, allocating a TAP device for this VM.
func WithBridge(b *vm.Bridge) Option {
	return func(c *machineConfig) {
		tap, err := b.AllocateTap()
		if err != nil {
			panic(fmt.Sprintf("WithBridge: %v", err))
		}
		c.vmOpts = append(c.vmOpts, vm.WithTapDevice(tap, b.Name))
	}
}

// WithUserNetwork configures user-mode (SLIRP) networking.
func WithUserNetwork() Option {
	return func(c *machineConfig) {
		c.vmOpts = append(c.vmOpts, vm.WithUserNet())
	}
}

// WithVMOption passes a raw vm.Option through to the underlying VM.
func WithVMOption(opt vm.Option) Option {
	return func(c *machineConfig) {
		c.vmOpts = append(c.vmOpts, opt)
	}
}

// RetryOption configures retry behavior for polling operations.
type RetryOption func(*retryConfig)

type retryConfig struct {
	timeout  time.Duration
	interval time.Duration
}

// WithRetryTimeout sets the maximum duration to retry.
func WithRetryTimeout(d time.Duration) RetryOption {
	return func(c *retryConfig) { c.timeout = d }
}

// WithRetryInterval sets the polling interval.
func WithRetryInterval(d time.Duration) RetryOption {
	return func(c *retryConfig) { c.interval = d }
}

func applyRetryOpts(opts []RetryOption) retryConfig {
	rc := retryConfig{
		timeout:  defaultRetryTimeout,
		interval: defaultRetryInterval,
	}
	for _, o := range opts {
		o(&rc)
	}
	return rc
}

// New boots a VM and connects the agent. It reads configuration from VMTEST_*
// environment variables and applies any additional options.
// The VM and agent connection are cleaned up via t.Cleanup.
func New(t testing.TB, opts ...Option) *Machine {
	t.Helper()

	mc := &machineConfig{}
	for _, o := range opts {
		o(mc)
	}

	vmOpts := buildVMOptsFromEnv(t)
	// Always enable agent and serial capture
	vmOpts = append(vmOpts, vm.WithAgent(), vm.WithSerialCapture(), vm.WithQMP())
	vmOpts = append(vmOpts, mc.vmOpts...)

	ctx := context.Background()
	v, err := vm.Start(ctx, vmOpts...)
	if err != nil {
		t.Fatalf("machine.New: failed to start VM: %v", err)
	}

	// Log serial output. OnLine returns a done channel closed after the
	// goroutine exits — we wait on it in cleanup to guarantee no t.Logf
	// calls happen after the test finishes.
	var logDone <-chan struct{}
	if serial := v.Serial(); serial != nil {
		logDone = serial.OnLine(func(line string) {
			t.Logf("[serial] %s", line)
		})
	}

	// Connect agent
	agentCtx, agentCancel := context.WithTimeout(ctx, 120*time.Second)
	defer agentCancel()

	agent, err := vm.ConnectAgent(agentCtx, v.AgentSocketPath())
	if err != nil {
		v.Kill()
		t.Fatalf("machine.New: failed to connect agent: %v", err)
	}

	m := &Machine{vm: v, agent: agent, t: t}
	t.Cleanup(func() {
		m.agent.Close()
		m.vm.Kill()    // closes serial reader, waits for readLoop → logChan closed
		if logDone != nil {
			<-logDone // wait for OnLine goroutine to drain and exit
		}
	})

	return m
}

// Execute runs a command on the guest and returns stdout, stderr, exit code.
func (m *Machine) Execute(cmd string) (stdout, stderr string, exitCode int, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 900*time.Second)
	defer cancel()

	resp, err := m.agent.Exec(ctx, cmd, 0)
	if err != nil {
		return "", "", -1, err
	}
	return resp.Stdout, resp.Stderr, resp.ExitCode, nil
}

// Succeed runs a command and fails the test if the exit code is non-zero.
// Returns stdout.
func (m *Machine) Succeed(t testing.TB, cmd string) string {
	t.Helper()
	stdout, stderr, exitCode, err := m.Execute(cmd)
	if err != nil {
		t.Fatalf("Succeed(%q): RPC error: %v", cmd, err)
	}
	if exitCode != 0 {
		t.Fatalf("Succeed(%q): exit code %d\nstdout: %s\nstderr: %s", cmd, exitCode, stdout, stderr)
	}
	return stdout
}

// Background starts a process as a transient systemd unit, detached from the
// RPC call. Takes an argv (no shell evaluation). The process is managed by
// systemd — visible via systemctl, logs via journalctl.
// Fails the test if systemd-run itself fails.
func (m *Machine) Background(t testing.TB, argv ...string) {
	t.Helper()
	if _, err := m.agent.Background(argv...); err != nil {
		t.Fatalf("Background(%v): %v", argv, err)
	}
}

// Fail runs a command and fails the test if the exit code is zero.
func (m *Machine) Fail(t testing.TB, cmd string) {
	t.Helper()
	_, _, exitCode, err := m.Execute(cmd)
	if err != nil {
		t.Fatalf("Fail(%q): RPC error: %v", cmd, err)
	}
	if exitCode == 0 {
		t.Fatalf("Fail(%q): expected non-zero exit code, got 0", cmd)
	}
}

// WaitUntilSucceeds retries a command until it succeeds or times out.
func (m *Machine) WaitUntilSucceeds(t testing.TB, cmd string, opts ...RetryOption) string {
	t.Helper()
	rc := applyRetryOpts(opts)

	deadline := time.Now().Add(rc.timeout)
	var lastStdout, lastStderr string
	var lastExitCode int
	var lastErr error

	for time.Now().Before(deadline) {
		lastStdout, lastStderr, lastExitCode, lastErr = m.Execute(cmd)
		if lastErr == nil && lastExitCode == 0 {
			return lastStdout
		}
		time.Sleep(rc.interval)
	}

	if lastErr != nil {
		t.Fatalf("WaitUntilSucceeds(%q): timed out after %s: RPC error: %v", cmd, rc.timeout, lastErr)
	}
	t.Fatalf("WaitUntilSucceeds(%q): timed out after %s: exit code %d\nstdout: %s\nstderr: %s",
		cmd, rc.timeout, lastExitCode, lastStdout, lastStderr)
	return "" // unreachable
}

// WaitForUnit waits until a systemd unit reaches "active" state.
func (m *Machine) WaitForUnit(t testing.TB, unit string, opts ...RetryOption) {
	t.Helper()
	rc := applyRetryOpts(opts)

	deadline := time.Now().Add(rc.timeout)
	for time.Now().Before(deadline) {
		resp, err := m.agent.UnitStatus(unit)
		if err == nil {
			if resp.ActiveState == "active" {
				return
			}
			if resp.ActiveState == "failed" {
				t.Fatalf("WaitForUnit(%q): unit is in 'failed' state (sub: %s)", unit, resp.SubState)
			}
		}
		time.Sleep(rc.interval)
	}
	t.Fatalf("WaitForUnit(%q): timed out after %s", unit, rc.timeout)
}

// WaitForOpenPort waits until a TCP port is open on the guest.
func (m *Machine) WaitForOpenPort(t testing.TB, port int, opts ...RetryOption) {
	t.Helper()
	rc := applyRetryOpts(opts)

	deadline := time.Now().Add(rc.timeout)
	for time.Now().Before(deadline) {
		open, err := m.agent.CheckPort(port)
		if err == nil && open {
			return
		}
		time.Sleep(rc.interval)
	}
	t.Fatalf("WaitForOpenPort(%d): timed out after %s", port, rc.timeout)
}

// WaitForClosedPort waits until a TCP port is closed on the guest.
func (m *Machine) WaitForClosedPort(t testing.TB, port int, opts ...RetryOption) {
	t.Helper()
	rc := applyRetryOpts(opts)

	deadline := time.Now().Add(rc.timeout)
	for time.Now().Before(deadline) {
		open, err := m.agent.CheckPort(port)
		if err == nil && !open {
			return
		}
		time.Sleep(rc.interval)
	}
	t.Fatalf("WaitForClosedPort(%d): timed out after %s", port, rc.timeout)
}

// WaitForFile waits until a file exists on the guest.
func (m *Machine) WaitForFile(t testing.TB, path string, opts ...RetryOption) {
	t.Helper()
	rc := applyRetryOpts(opts)

	deadline := time.Now().Add(rc.timeout)
	for time.Now().Before(deadline) {
		resp, err := m.agent.Stat(path)
		if err == nil && resp.Exists {
			return
		}
		time.Sleep(rc.interval)
	}
	t.Fatalf("WaitForFile(%q): timed out after %s", path, rc.timeout)
}

// CopyFromHost reads a file from the host and writes it to the guest.
func (m *Machine) CopyFromHost(t testing.TB, hostPath, guestPath string) {
	t.Helper()
	data, err := os.ReadFile(hostPath)
	if err != nil {
		t.Fatalf("CopyFromHost: failed to read host file %q: %v", hostPath, err)
	}
	if err := m.agent.WriteFile(guestPath, data, 0644); err != nil {
		t.Fatalf("CopyFromHost: failed to write guest file %q: %v", guestPath, err)
	}
}

// Listen opens a TCP listener on the given port inside the guest.
// The listener stays open until the VM exits. Useful for testing port checks.
func (m *Machine) Listen(t testing.TB, port int) {
	t.Helper()
	if err := m.agent.Listen(port); err != nil {
		t.Fatalf("Listen(%d): %v", port, err)
	}
}

// Shutdown sends an ACPI powerdown to the VM.
func (m *Machine) Shutdown(t testing.TB) {
	t.Helper()
	if err := m.vm.Stop(); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}

// QMP returns the QMP client for low-level VM control.
func (m *Machine) QMP() *vm.QMPClient {
	return m.vm.QMP()
}

// Serial returns the serial console for raw log access.
func (m *Machine) Serial() *vm.SerialConsole {
	return m.vm.Serial()
}

// buildVMOptsFromEnv reads VMTEST_* environment variables and returns vm.Options.
func buildVMOptsFromEnv(t testing.TB) []vm.Option {
	t.Helper()
	var opts []vm.Option

	if v := os.Getenv("VMTEST_QEMU"); v != "" {
		opts = append(opts, vm.WithQemuBinary(v))
	}
	if v := os.Getenv("VMTEST_QEMU_IMG"); v != "" {
		opts = append(opts, vm.WithQemuImg(v))
	}

	kernel := os.Getenv("VMTEST_KERNEL")
	initrd := os.Getenv("VMTEST_INITRD")
	cmdline := os.Getenv("VMTEST_CMDLINE")
	if kernel != "" {
		opts = append(opts, vm.WithKernelBoot(kernel, initrd, cmdline))
	}

	ovmfCode := os.Getenv("VMTEST_OVMF_CODE")
	ovmfVars := os.Getenv("VMTEST_OVMF_VARS")
	if ovmfCode != "" && ovmfVars != "" {
		opts = append(opts, vm.WithUEFI(ovmfCode, ovmfVars))
	}

	if v := os.Getenv("VMTEST_ISO"); v != "" {
		opts = append(opts, vm.WithISO(v))
	}
	if v := os.Getenv("VMTEST_DISK"); v != "" {
		opts = append(opts, vm.WithExistingDisk(v))
	}
	if v := os.Getenv("VMTEST_DISK_SIZE"); v != "" {
		opts = append(opts, vm.WithDisk(v))
	}
	if v := os.Getenv("VMTEST_MEMORY"); v != "" {
		opts = append(opts, vm.WithMemory(v))
	}
	if v := os.Getenv("VMTEST_CPUS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			t.Fatalf("VMTEST_CPUS: invalid value %q: %v", v, err)
		}
		opts = append(opts, vm.WithCPUs(n))
	}

	swtpm := os.Getenv("VMTEST_SWTPM")
	swtpmSetup := os.Getenv("VMTEST_SWTPM_SETUP")
	if os.Getenv("VMTEST_TPM") == "true" && swtpm != "" && swtpmSetup != "" {
		opts = append(opts, vm.WithTPM(swtpm, swtpmSetup))
	}

	switch strings.ToLower(os.Getenv("VMTEST_NETWORK")) {
	case "none":
		opts = append(opts, vm.WithNoNetwork())
	case "bridge":
		bridge := os.Getenv("VMTEST_BRIDGE")
		if bridge == "" {
			bridge = "mltt-br0"
		}
		// Bridge networking is set up by WithBridge machine option, not env vars.
		// This is a fallback for when the Starlark rule sets it.
		_ = bridge
	default:
		opts = append(opts, vm.WithUserNet())
	}

	return opts
}
