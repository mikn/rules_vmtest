package machine

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mikn/rules_qemu/vm"
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
	return m.ExecuteWithTimeout(cmd, 120*time.Second)
}

// ExecuteWithTimeout runs a command on the guest with an explicit timeout.
func (m *Machine) ExecuteWithTimeout(cmd string, timeout time.Duration) (stdout, stderr string, exitCode int, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
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

// --- Output matchers for WaitUntilOutput ---

// OutputMatcher tests whether command output matches a condition.
type OutputMatcher func(stdout string) bool

// OutputContains returns a matcher that checks if stdout contains substr.
func OutputContains(substr string) OutputMatcher {
	return func(stdout string) bool { return strings.Contains(stdout, substr) }
}

// OutputEquals returns a matcher that checks if stdout (trimmed) equals exact.
func OutputEquals(exact string) OutputMatcher {
	return func(stdout string) bool { return strings.TrimSpace(stdout) == exact }
}

// OutputMatches returns a matcher that checks if stdout matches a regexp.
func OutputMatches(pattern string) OutputMatcher {
	re := regexp.MustCompile(pattern)
	return func(stdout string) bool { return re.MatchString(stdout) }
}

// WaitUntilOutput retries a command until the matcher matches stdout, or times out.
// Returns the matching stdout.
func (m *Machine) WaitUntilOutput(t testing.TB, cmd string, match OutputMatcher, opts ...RetryOption) string {
	t.Helper()
	rc := applyRetryOpts(opts)

	deadline := time.Now().Add(rc.timeout)
	var lastStdout, lastStderr string
	var lastExitCode int
	var lastErr error

	for time.Now().Before(deadline) {
		lastStdout, lastStderr, lastExitCode, lastErr = m.Execute(cmd)
		if lastErr == nil && lastExitCode == 0 && match(lastStdout) {
			return lastStdout
		}
		time.Sleep(rc.interval)
	}

	if lastErr != nil {
		t.Fatalf("WaitUntilOutput(%q): timed out after %s: RPC error: %v", cmd, rc.timeout, lastErr)
	}
	t.Fatalf("WaitUntilOutput(%q): timed out after %s: output did not match\nexit code: %d\nstdout: %s\nstderr: %s",
		cmd, rc.timeout, lastExitCode, lastStdout, lastStderr)
	return "" // unreachable
}

// --- ReadFile / FileHash / FileSize ---

// ReadFile reads a file from the guest and returns its contents.
func (m *Machine) ReadFile(t testing.TB, path string) []byte {
	t.Helper()
	data, err := m.agent.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", path, err)
	}
	return data
}

// FileHash returns the hex-encoded hash of a guest file.
// Supported algorithms: "sha256", "sha1", "md5".
func (m *Machine) FileHash(t testing.TB, path string, algo string) string {
	t.Helper()
	hex, err := m.agent.FileHash(path, algo)
	if err != nil {
		t.Fatalf("FileHash(%q, %q): %v", path, algo, err)
	}
	return hex
}

// FileSize returns the size of a file on the guest in bytes.
func (m *Machine) FileSize(t testing.TB, path string) int64 {
	t.Helper()
	resp, err := m.agent.Stat(path)
	if err != nil {
		t.Fatalf("FileSize(%q): %v", path, err)
	}
	if !resp.Exists {
		t.Fatalf("FileSize(%q): file does not exist", path)
	}
	return resp.Size
}

// --- HTTPGet / WaitForHTTP ---

// HTTPResponse holds the result of an HTTP GET executed inside the guest.
type HTTPResponse struct {
	StatusCode int
	Body       string
}

// HTTPGet makes an HTTP GET request inside the guest and returns the response.
func (m *Machine) HTTPGet(t testing.TB, url string) *HTTPResponse {
	t.Helper()
	resp, err := m.agent.HTTPGet(url)
	if err != nil {
		t.Fatalf("HTTPGet(%q): %v", url, err)
	}
	return &HTTPResponse{StatusCode: resp.StatusCode, Body: resp.Body}
}

// HTTPOption configures WaitForHTTP behavior.
type HTTPOption func(*httpConfig)

type httpConfig struct {
	statusCode   int
	bodyContains string
}

// HTTPStatus requires the response to have this status code.
func HTTPStatus(code int) HTTPOption {
	return func(c *httpConfig) { c.statusCode = code }
}

// HTTPBodyContains requires the response body to contain substr.
func HTTPBodyContains(substr string) HTTPOption {
	return func(c *httpConfig) { c.bodyContains = substr }
}

// WaitForHTTPOption configures WaitForHTTP behavior (either HTTP or retry settings).
type WaitForHTTPOption interface {
	applyHTTP(*httpConfig, *retryConfig)
}

func (f HTTPOption) applyHTTP(hc *httpConfig, _ *retryConfig) { f(hc) }

type httpRetryOption struct{ RetryOption }

func (o httpRetryOption) applyHTTP(_ *httpConfig, rc *retryConfig) { o.RetryOption(rc) }

// WithHTTPRetryTimeout sets the maximum duration for WaitForHTTP to retry.
func WithHTTPRetryTimeout(d time.Duration) WaitForHTTPOption {
	return httpRetryOption{WithRetryTimeout(d)}
}

// WithHTTPRetryInterval sets the polling interval for WaitForHTTP.
func WithHTTPRetryInterval(d time.Duration) WaitForHTTPOption {
	return httpRetryOption{WithRetryInterval(d)}
}

// WaitForHTTP polls an HTTP endpoint inside the guest until it responds
// and matches the given options, or times out.
func (m *Machine) WaitForHTTP(t testing.TB, url string, opts ...WaitForHTTPOption) *HTTPResponse {
	t.Helper()
	hc := httpConfig{statusCode: 200}
	rc := retryConfig{timeout: defaultRetryTimeout, interval: defaultRetryInterval}
	for _, o := range opts {
		o.applyHTTP(&hc, &rc)
	}

	deadline := time.Now().Add(rc.timeout)
	for time.Now().Before(deadline) {
		resp, err := m.agent.HTTPGet(url)
		if err == nil && resp.StatusCode == hc.statusCode {
			if hc.bodyContains == "" || strings.Contains(resp.Body, hc.bodyContains) {
				return &HTTPResponse{StatusCode: resp.StatusCode, Body: resp.Body}
			}
		}
		time.Sleep(rc.interval)
	}
	t.Fatalf("WaitForHTTP(%q): timed out after %s waiting for status %d", url, rc.timeout, hc.statusCode)
	return nil // unreachable
}

// --- DumpOnFailure ---

// DumpOption configures what DumpOnFailure collects.
type DumpOption func(*dumpConfig)

type dumpConfig struct {
	journalUnits []string
	dmesgFilters []string
	processGlob  string
	fileGlobs    []string
	hasOptions   bool
}

// DumpJournalUnits dumps journal logs for the specified systemd units.
func DumpJournalUnits(units ...string) DumpOption {
	return func(c *dumpConfig) {
		c.journalUnits = append(c.journalUnits, units...)
		c.hasOptions = true
	}
}

// DumpDmesg dumps kernel messages, optionally filtered by grep patterns.
func DumpDmesg(filters ...string) DumpOption {
	return func(c *dumpConfig) {
		c.dmesgFilters = filters
		c.hasOptions = true
	}
}

// DumpProcessList dumps processes matching a pattern.
func DumpProcessList(pattern string) DumpOption {
	return func(c *dumpConfig) {
		c.processGlob = pattern
		c.hasOptions = true
	}
}

// DumpFiles dumps contents of files matching the given globs.
func DumpFiles(globs ...string) DumpOption {
	return func(c *dumpConfig) {
		c.fileGlobs = append(c.fileGlobs, globs...)
		c.hasOptions = true
	}
}

// DumpOnFailure registers a t.Cleanup that dumps diagnostic information
// when the test fails. With no options, dumps failed units and dmesg.
func (m *Machine) DumpOnFailure(t testing.TB, opts ...DumpOption) {
	t.Helper()
	dc := &dumpConfig{}
	for _, o := range opts {
		o(dc)
	}

	t.Cleanup(func() {
		if !t.Failed() {
			return
		}

		t.Log("=== DumpOnFailure: collecting diagnostics ===")

		if !dc.hasOptions {
			// Default: dump failed units + dmesg
			m.dumpFailedUnits(t)
			m.dumpDmesg(t, nil)
			return
		}

		if len(dc.journalUnits) > 0 {
			for _, unit := range dc.journalUnits {
				lines, err := m.agent.JournalLines(unit, 100, "")
				if err == nil {
					t.Logf("[journal:%s]\n%s", unit, strings.Join(lines, "\n"))
				}
			}
		}

		if dc.dmesgFilters != nil {
			m.dumpDmesg(t, dc.dmesgFilters)
		}

		if dc.processGlob != "" {
			procs, err := m.agent.FindProcesses(dc.processGlob)
			if err == nil {
				for _, p := range procs {
					t.Logf("[process:%d] %s (state=%s uid=%d)", p.PID, p.Cmdline, p.State, p.UID)
				}
			}
		}

		for _, glob := range dc.fileGlobs {
			data, err := m.agent.ReadFile(glob)
			if err == nil {
				t.Logf("[file:%s]\n%s", glob, string(data))
			}
		}
	})
}

func (m *Machine) dumpFailedUnits(t testing.TB) {
	stdout, _, _, err := m.Execute("systemctl --failed --no-legend --no-pager --plain")
	if err != nil {
		return
	}
	for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
		if line == "" {
			continue
		}
		unit := strings.Fields(line)[0]
		lines, jErr := m.agent.JournalLines(unit, 50, "")
		if jErr == nil {
			t.Logf("[failed-unit:%s]\n%s", unit, strings.Join(lines, "\n"))
		}
	}
}

func (m *Machine) dumpDmesg(t testing.TB, filters []string) {
	cmd := "dmesg --color=never"
	if len(filters) > 0 {
		cmd = fmt.Sprintf("dmesg --color=never | grep -E %s", shellQuote(strings.Join(filters, "|")))
	}
	stdout, _, _, err := m.Execute(cmd)
	if err == nil {
		t.Logf("[dmesg]\n%s", stdout)
	}
}

// --- WaitForLog / JournalLines ---

// JournalOption configures JournalLines behavior.
type JournalOption func(*journalConfig)

type journalConfig struct {
	lines int
	since string
}

// JournalLines returns the number of lines to fetch (default 100).
func WithJournalLines(n int) JournalOption {
	return func(c *journalConfig) { c.lines = n }
}

// WithJournalSince filters entries newer than the given time spec (e.g. "5min ago").
func WithJournalSince(since string) JournalOption {
	return func(c *journalConfig) { c.since = since }
}

// JournalLines returns journal log lines for a systemd unit.
func (m *Machine) JournalLines(t testing.TB, unit string, opts ...JournalOption) []string {
	t.Helper()
	jc := journalConfig{lines: 100}
	for _, o := range opts {
		o(&jc)
	}

	lines, err := m.agent.JournalLines(unit, jc.lines, jc.since)
	if err != nil {
		t.Fatalf("JournalLines(%q): %v", unit, err)
	}
	return lines
}

// WaitForLog polls the journal for a unit until a line matching pattern appears.
func (m *Machine) WaitForLog(t testing.TB, unit string, pattern string, opts ...RetryOption) string {
	t.Helper()
	rc := applyRetryOpts(opts)
	re := regexp.MustCompile(pattern)

	deadline := time.Now().Add(rc.timeout)
	for time.Now().Before(deadline) {
		lines, err := m.agent.JournalLines(unit, 0, "")
		if err == nil {
			for _, line := range lines {
				if re.MatchString(line) {
					return line
				}
			}
		}
		time.Sleep(rc.interval)
	}
	t.Fatalf("WaitForLog(%q, %q): timed out after %s", unit, pattern, rc.timeout)
	return "" // unreachable
}

// --- FindProcesses / ProcessInfo ---

// ProcessInfo describes a running process on the guest.
type ProcessInfo struct {
	PID     int
	UID     int
	GID     int
	Cmdline string
	State   string
}

// FindProcesses returns processes whose command line matches the given regex pattern.
func (m *Machine) FindProcesses(t testing.TB, pattern string) []ProcessInfo {
	t.Helper()
	entries, err := m.agent.FindProcesses(pattern)
	if err != nil {
		t.Fatalf("FindProcesses(%q): %v", pattern, err)
	}
	procs := make([]ProcessInfo, len(entries))
	for i, e := range entries {
		procs[i] = ProcessInfo{
			PID:     e.PID,
			UID:     e.UID,
			GID:     e.GID,
			Cmdline: e.Cmdline,
			State:   e.State,
		}
	}
	return procs
}

// --- NetworkAddrs / NetworkRoutes / NetworkLinks ---

// Route describes a network route on the guest.
type Route struct {
	Destination string
	Gateway     string
	Interface   string
}

// Link describes a network interface on the guest.
type Link struct {
	Name  string
	State string
	MTU   int
	Addrs []string
}

// NetworkAddrs returns IP addresses assigned to the given interface.
func (m *Machine) NetworkAddrs(t testing.TB, iface string) []string {
	t.Helper()
	resp, err := m.agent.NetworkInfo(iface)
	if err != nil {
		t.Fatalf("NetworkAddrs(%q): %v", iface, err)
	}
	var addrs []string
	for _, a := range resp.Addrs {
		addrs = append(addrs, a.Address)
	}
	return addrs
}

// NetworkRoutes returns the routing table from the guest.
func (m *Machine) NetworkRoutes(t testing.TB) []Route {
	t.Helper()
	resp, err := m.agent.NetworkInfo("")
	if err != nil {
		t.Fatalf("NetworkRoutes: %v", err)
	}
	routes := make([]Route, len(resp.Routes))
	for i, r := range resp.Routes {
		routes[i] = Route{
			Destination: r.Destination,
			Gateway:     r.Gateway,
			Interface:   r.Interface,
		}
	}
	return routes
}

// NetworkLinks returns the list of network interfaces on the guest.
func (m *Machine) NetworkLinks(t testing.TB) []Link {
	t.Helper()
	resp, err := m.agent.NetworkInfo("")
	if err != nil {
		t.Fatalf("NetworkLinks: %v", err)
	}
	var links []Link
	for _, l := range resp.Links {
		link := Link{
			Name:  l.Name,
			State: l.State,
			MTU:   l.MTU,
		}
		// Collect addresses for this link
		for _, a := range resp.Addrs {
			if a.Interface == l.Name {
				link.Addrs = append(link.Addrs, a.Address)
			}
		}
		links = append(links, link)
	}
	return links
}

// --- Time helper ---

// Time executes fn, logs the elapsed time, and returns the duration.
func (m *Machine) Time(t testing.TB, label string, fn func()) time.Duration {
	t.Helper()
	start := time.Now()
	fn()
	elapsed := time.Since(start)
	t.Logf("[timing] %s: %s", label, elapsed)
	return elapsed
}

// shellQuote wraps a string in single quotes, escaping embedded single quotes.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
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
	if v := os.Getenv("VMTEST_MACHINE_TYPE"); v != "" {
		opts = append(opts, vm.WithMachineType(v))
	}
	if v := os.Getenv("VMTEST_ACCEL"); v != "" {
		opts = append(opts, vm.WithAccel(v))
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
