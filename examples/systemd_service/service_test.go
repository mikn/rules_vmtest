// Package systemd_service demonstrates testing a systemd service inside a VM.
//
// This pattern is useful for integration-testing daemons that are managed by
// systemd: wait for the unit to activate, verify the port is open, then
// exercise the service's API.
package systemd_service

import (
	"strings"
	"testing"
	"time"

	"github.com/mikn/rules_vmtest/examples/systemd_service/vmconfig"
)

func TestSystemdService(t *testing.T) {
	m := vmconfig.New(t)

	t.Run("WaitForMultiUserTarget", func(t *testing.T) {
		m.WaitForUnit(t, "multi-user.target",
			vmconfig.WithRetryTimeout(30*time.Second))
	})

	t.Run("ServiceActivation", func(t *testing.T) {
		m.WaitForUnit(t, "my-daemon.service",
			vmconfig.WithRetryTimeout(30*time.Second))

		m.WaitForOpenPort(t, 8080,
			vmconfig.WithRetryTimeout(10*time.Second))

		out := m.Succeed(t, "curl -sf http://127.0.0.1:8080/health")
		if !strings.Contains(out, "ok") {
			t.Fatalf("health check: got %q", out)
		}
	})

	t.Run("BackgroundProcess", func(t *testing.T) {
		// Launch a process as a transient systemd unit (argv, no shell).
		m.Background(t, "bash", "-c", "sleep 2 && touch /tmp/bg-done")

		m.WaitForFile(t, "/tmp/bg-done",
			vmconfig.WithRetryTimeout(10*time.Second))
	})

	t.Run("RetryUntilReady", func(t *testing.T) {
		out := m.WaitUntilSucceeds(t, "cat /proc/sys/net/ipv4/ip_forward",
			vmconfig.WithRetryTimeout(5*time.Second),
			vmconfig.WithRetryInterval(500*time.Millisecond))
		t.Logf("ip_forward = %s", strings.TrimSpace(out))
	})

	t.Run("PortLifecycle", func(t *testing.T) {
		m.Listen(t, 9090)
		m.WaitForOpenPort(t, 9090)
		m.WaitForClosedPort(t, 9999)
	})
}
