package vm

import "strings"

// Config holds the resolved configuration for a VM instance.
// Use functional Option values with Start() to build this.
type Config struct {
	// VM resources
	memory  string
	cpus    int
	machine string

	// Boot configuration (mutually exclusive modes)
	kernel  string
	initrd  string
	cmdline string

	ovmfCode string
	ovmfVars string
	iso      string

	// Storage
	diskSize     string
	existingDisk string

	// TPM
	swtpm        string
	swtpmSetup   string
	tpmStatePath string

	// Networking
	networkMode    networkMode
	tapDevice      string
	tapBridge      string
	macAddress     string
	secondTap      string
	shares         []share9P
	userNetOptions string

	// Boot order (QEMU -boot order=X, written to fw_cfg etc/boot-order)
	bootOrder string

	// Debugging
	debugConsolePath string // OVMF debug console output file

	// Guest interaction
	enableQMP    bool
	serialMode   serialMode
	enableSerial bool

	// Agent (virtio-serial backdoor)
	enableAgent   bool
	agentSockPath string

	// Tool paths
	qemuBinary string
	qemuImg    string

	// Work directory
	workDir string

	// Name (for socket paths, logging)
	name string

	// Derived settings (computed from other fields)
	smmEnabled bool // true if machine string contains "smm=on"

	// Runtime-resolved paths (set during Start, not by options)
	resolvedDiskPath string
	resolvedVarsPath string
	tpmSocketPath    string
	serialSocketPath string
	qmpSocketPath    string
}

type networkMode int

const (
	networkUser networkMode = iota
	networkTap
	networkNone
)

type serialMode int

const (
	serialNone serialMode = iota
	serialSocket
	serialStdio
)

type share9P struct {
	tag      string
	hostPath string
}

// Option configures a VM.
type Option func(*Config)

// WithMemory sets the VM memory size (e.g., "2G", "512M").
func WithMemory(size string) Option {
	return func(c *Config) { c.memory = size }
}

// WithCPUs sets the number of virtual CPUs.
func WithCPUs(n int) Option {
	return func(c *Config) { c.cpus = n }
}

// WithMachine sets the QEMU machine type (e.g., "q35,accel=kvm").
func WithMachine(machine string) Option {
	return func(c *Config) { c.machine = machine }
}

// WithKernelBoot configures direct kernel boot.
func WithKernelBoot(kernel, initrd, cmdline string) Option {
	return func(c *Config) {
		c.kernel = kernel
		c.initrd = initrd
		c.cmdline = cmdline
	}
}

// WithUEFI configures UEFI firmware boot.
func WithUEFI(ovmfCode, ovmfVars string) Option {
	return func(c *Config) {
		c.ovmfCode = ovmfCode
		c.ovmfVars = ovmfVars
	}
}

// WithISO sets an ISO image for booting (requires UEFI).
func WithISO(path string) Option {
	return func(c *Config) { c.iso = path }
}

// WithDisk creates and attaches a new raw disk of the given size.
func WithDisk(size string) Option {
	return func(c *Config) { c.diskSize = size }
}

// WithExistingDisk uses an existing disk image file.
func WithExistingDisk(path string) Option {
	return func(c *Config) { c.existingDisk = path }
}

// WithTPM enables TPM 2.0 emulation with the given swtpm binaries.
func WithTPM(swtpm, swtpmSetup string) Option {
	return func(c *Config) {
		c.swtpm = swtpm
		c.swtpmSetup = swtpmSetup
	}
}

// WithTPMState copies TPM state from a pristine path before starting.
func WithTPMState(pristinePath string) Option {
	return func(c *Config) { c.tpmStatePath = pristinePath }
}

// WithUserNet enables user-mode networking (SLIRP).
func WithUserNet() Option {
	return func(c *Config) { c.networkMode = networkUser }
}

// WithUserNetOptions enables user-mode networking with custom options
// (e.g., "hostfwd=tcp::2222-:22").
func WithUserNetOptions(options string) Option {
	return func(c *Config) {
		c.networkMode = networkUser
		c.userNetOptions = options
	}
}

// WithTapDevice configures tap device networking on a bridge.
func WithTapDevice(tap, bridge string) Option {
	return func(c *Config) {
		c.networkMode = networkTap
		c.tapDevice = tap
		c.tapBridge = bridge
	}
}

// WithSecondTap adds a second network interface on the given tap device.
func WithSecondTap(tap string) Option {
	return func(c *Config) { c.secondTap = tap }
}

// WithMacAddress sets the MAC address for the primary NIC.
func WithMacAddress(mac string) Option {
	return func(c *Config) { c.macAddress = mac }
}

// WithNoNetwork disables networking entirely.
func WithNoNetwork() Option {
	return func(c *Config) { c.networkMode = networkNone }
}

// WithQMP enables the QMP control socket.
func WithQMP() Option {
	return func(c *Config) { c.enableQMP = true }
}

// With9PShare adds a 9P host directory share accessible from the guest.
func With9PShare(tag, hostPath string) Option {
	return func(c *Config) {
		c.shares = append(c.shares, share9P{tag: tag, hostPath: hostPath})
	}
}

// WithDebugConsole enables the OVMF debug console (ioport 0x402) and writes output to the given file.
func WithDebugConsole(path string) Option {
	return func(c *Config) { c.debugConsolePath = path }
}

// WithBootOrder sets the QEMU boot order (e.g., "dc" for CD-ROM then disk).
// This is written to fw_cfg etc/boot-order, which OVMF reads to prioritize
// boot devices. Use this to skip slow PXE/DHCP discovery on NICs.
func WithBootOrder(order string) Option {
	return func(c *Config) { c.bootOrder = order }
}

// WithAgent enables the virtio-serial agent channel for bidirectional RPC
// with the guest. The guest must have the vmtest agent binary running.
func WithAgent() Option {
	return func(c *Config) { c.enableAgent = true }
}

// WithSerialCapture enables serial console capture over a unix socket.
func WithSerialCapture() Option {
	return func(c *Config) {
		c.enableSerial = true
		c.serialMode = serialSocket
	}
}

// WithSerialStdio sends serial output to stdin/stdout (nographic mode).
func WithSerialStdio() Option {
	return func(c *Config) {
		c.enableSerial = true
		c.serialMode = serialStdio
	}
}

// WithQemuBinary sets the path to the qemu-system binary.
func WithQemuBinary(path string) Option {
	return func(c *Config) { c.qemuBinary = path }
}

// WithQemuImg sets the path to the qemu-img binary.
func WithQemuImg(path string) Option {
	return func(c *Config) { c.qemuImg = path }
}

// WithWorkDir sets the working directory for VM artifacts.
func WithWorkDir(path string) Option {
	return func(c *Config) { c.workDir = path }
}

// WithName sets the VM name (used for socket paths and logging).
func WithName(name string) Option {
	return func(c *Config) { c.name = name }
}

func applyDefaults(c *Config) {
	if c.memory == "" {
		c.memory = "2G"
	}
	if c.cpus == 0 {
		c.cpus = 2
	}
	if c.machine == "" {
		c.machine = "q35,accel=kvm"
	}
	if c.qemuBinary == "" {
		c.qemuBinary = "qemu-system-x86_64"
	}
	if c.qemuImg == "" {
		c.qemuImg = "qemu-img"
	}
	if c.name == "" {
		c.name = "vmtest"
	}

	// Detect SMM from machine string
	c.smmEnabled = strings.Contains(c.machine, "smm=on")
}
