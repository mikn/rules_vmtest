package runner

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
func DefaultToolPaths() ToolPaths {
	return ToolPaths{
		QemuSystem: "qemu-system-x86_64",
		QemuImg:    "qemu-img",
		Swtpm:      "swtpm",
		SwtpmSetup: "swtpm_setup",
	}
}
