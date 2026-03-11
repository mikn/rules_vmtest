package runner

import "runtime"

// ToolPaths holds paths to external tool binaries.
type ToolPaths struct {
	QemuSystem string // Path to qemu-system-{arch} binary
	QemuImg    string // Path to qemu-img binary
	OVMFCode   string // Path to OVMF_CODE firmware file (resolved)
	OVMFVars   string // Path to OVMF_VARS template file (resolved)
	Swtpm      string // Path to swtpm binary
	SwtpmSetup string // Path to swtpm_setup binary
}

// TransitNetwork holds the expected transit network addresses for validation.
type TransitNetwork struct {
	IPv4 string // Expected IPv4 address on the host tap device (e.g., "192.168.177.1/28")
	IPv6 string // Expected IPv6 address on the host tap device (e.g., "fd00:6000:100::1/64")
}

// DefaultToolPaths returns ToolPaths using system PATH lookups.
// The QEMU binary defaults to qemu-system-aarch64 on ARM64 hosts, or
// qemu-system-x86_64 on all other hosts. Bazel rules provide explicit paths
// from the toolchain, so these defaults only matter for non-Bazel usage.
func DefaultToolPaths() ToolPaths {
	qemuSystem := "qemu-system-x86_64"
	if runtime.GOARCH == "arm64" {
		qemuSystem = "qemu-system-aarch64"
	}
	return ToolPaths{
		QemuSystem: qemuSystem,
		QemuImg:    "qemu-img",
		Swtpm:      "swtpm",
		SwtpmSetup: "swtpm_setup",
	}
}
