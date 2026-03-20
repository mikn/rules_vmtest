package machine

import (
	"testing"
	"time"
)

func TestApplyRetryOptsDefaults(t *testing.T) {
	rc := applyRetryOpts(nil)
	if rc.timeout != defaultRetryTimeout {
		t.Errorf("expected timeout %v, got %v", defaultRetryTimeout, rc.timeout)
	}
	if rc.interval != defaultRetryInterval {
		t.Errorf("expected interval %v, got %v", defaultRetryInterval, rc.interval)
	}
}

func TestApplyRetryOptsCustom(t *testing.T) {
	rc := applyRetryOpts([]RetryOption{
		WithRetryTimeout(30 * time.Second),
		WithRetryInterval(500 * time.Millisecond),
	})
	if rc.timeout != 30*time.Second {
		t.Errorf("expected timeout 30s, got %v", rc.timeout)
	}
	if rc.interval != 500*time.Millisecond {
		t.Errorf("expected interval 500ms, got %v", rc.interval)
	}
}

func TestBuildVMOptsFromEnvEmpty(t *testing.T) {
	// With no env vars set, should still return a valid (possibly empty) slice
	opts := buildVMOptsFromEnv(t)
	// Default is user network
	if len(opts) == 0 {
		t.Error("expected at least one option (user network default)")
	}
}

func TestBuildVMOptsFromEnvKernel(t *testing.T) {
	t.Setenv("VMTEST_KERNEL", "/boot/vmlinuz")
	t.Setenv("VMTEST_INITRD", "/boot/initrd")
	t.Setenv("VMTEST_CMDLINE", "console=ttyS0")
	t.Setenv("VMTEST_MEMORY", "4G")
	t.Setenv("VMTEST_CPUS", "4")

	opts := buildVMOptsFromEnv(t)
	// Should have kernel boot + memory + cpus + default network = at least 4 options
	if len(opts) < 4 {
		t.Errorf("expected at least 4 options, got %d", len(opts))
	}
}

func TestBuildVMOptsFromEnvUEFI(t *testing.T) {
	t.Setenv("VMTEST_OVMF_CODE", "/ovmf/code.fd")
	t.Setenv("VMTEST_OVMF_VARS", "/ovmf/vars.fd")
	t.Setenv("VMTEST_ISO", "/boot.iso")
	t.Setenv("VMTEST_DISK_SIZE", "10G")

	opts := buildVMOptsFromEnv(t)
	if len(opts) < 3 {
		t.Errorf("expected at least 3 options, got %d", len(opts))
	}
}

func TestBuildVMOptsFromEnvTPM(t *testing.T) {
	t.Setenv("VMTEST_TPM", "true")
	t.Setenv("VMTEST_SWTPM", "/usr/bin/swtpm")
	t.Setenv("VMTEST_SWTPM_SETUP", "/usr/bin/swtpm_setup")

	opts := buildVMOptsFromEnv(t)
	// Should include TPM option
	if len(opts) < 2 {
		t.Errorf("expected at least 2 options (TPM + network), got %d", len(opts))
	}
}

func TestBuildVMOptsFromEnvNetworkNone(t *testing.T) {
	t.Setenv("VMTEST_NETWORK", "none")

	opts := buildVMOptsFromEnv(t)
	if len(opts) != 1 {
		t.Errorf("expected 1 option (no network), got %d", len(opts))
	}
}

func TestWithPortForwardOption(t *testing.T) {
	mc := &machineConfig{}
	WithPortForward(50051)(mc)
	WithPortForward(50052)(mc)

	if len(mc.vmOpts) != 2 {
		t.Fatalf("expected 2 vm options, got %d", len(mc.vmOpts))
	}
}

func TestBuildVMOptsFromEnvPortForwards(t *testing.T) {
	t.Setenv("VMTEST_PORT_FORWARDS", "50051,50052")

	opts := buildVMOptsFromEnv(t)
	// Should have default user net + 2 port forward options
	// Port forwards come after VMTEST_NETWORK, overriding the default
	if len(opts) < 3 {
		t.Errorf("expected at least 3 options (user net + 2 port forwards), got %d", len(opts))
	}
}

func TestBuildVMOptsFromEnvPortForwardsOverridesNone(t *testing.T) {
	t.Setenv("VMTEST_NETWORK", "none")
	t.Setenv("VMTEST_PORT_FORWARDS", "50051")

	opts := buildVMOptsFromEnv(t)
	// Should have: WithNoNetwork (1) + WithPortForward (1) = 2
	// The WithPortForward forces user mode, overriding the "none" at runtime
	if len(opts) != 2 {
		t.Errorf("expected 2 options (no network + port forward override), got %d", len(opts))
	}
}
