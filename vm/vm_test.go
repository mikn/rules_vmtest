package vm

import (
	"strings"
	"testing"
)

func TestApplyDefaults(t *testing.T) {
	c := &Config{}
	applyDefaults(c)

	if c.memory != "2G" {
		t.Errorf("expected memory=2G, got %s", c.memory)
	}
	if c.cpus != 2 {
		t.Errorf("expected cpus=2, got %d", c.cpus)
	}
	if c.machine != "q35,accel=kvm" {
		t.Errorf("expected machine=q35,accel=kvm, got %s", c.machine)
	}
	if c.qemuBinary != "qemu-system-x86_64" {
		t.Errorf("expected qemuBinary=qemu-system-x86_64, got %s", c.qemuBinary)
	}
	if c.name != "vmtest" {
		t.Errorf("expected name=vmtest, got %s", c.name)
	}
}

func TestApplyDefaultsDoesNotOverride(t *testing.T) {
	c := &Config{
		memory:     "4G",
		cpus:       8,
		machine:    "virt",
		qemuBinary: "/usr/local/bin/qemu",
		name:       "myvm",
	}
	applyDefaults(c)

	if c.memory != "4G" {
		t.Errorf("memory should not be overridden, got %s", c.memory)
	}
	if c.cpus != 8 {
		t.Errorf("cpus should not be overridden, got %d", c.cpus)
	}
	if c.machine != "virt" {
		t.Errorf("machine should not be overridden, got %s", c.machine)
	}
}

func TestFunctionalOptions(t *testing.T) {
	c := &Config{}
	opts := []Option{
		WithMemory("8G"),
		WithCPUs(4),
		WithMachine("q35,accel=kvm,smm=on"),
		WithKernelBoot("/boot/vmlinuz", "/boot/initrd", "console=ttyS0"),
		WithDisk("20G"),
		WithUserNet(),
		WithMacAddress("52:54:00:00:00:01"),
		WithQMP(),
		WithSerialCapture(),
		WithName("test-vm"),
		WithWorkDir("/tmp/test"),
		WithQemuBinary("/usr/bin/qemu"),
		WithQemuImg("/usr/bin/qemu-img"),
	}

	for _, o := range opts {
		o(c)
	}

	if c.memory != "8G" {
		t.Errorf("memory: got %s", c.memory)
	}
	if c.cpus != 4 {
		t.Errorf("cpus: got %d", c.cpus)
	}
	if c.kernel != "/boot/vmlinuz" {
		t.Errorf("kernel: got %s", c.kernel)
	}
	if c.initrd != "/boot/initrd" {
		t.Errorf("initrd: got %s", c.initrd)
	}
	if c.cmdline != "console=ttyS0" {
		t.Errorf("cmdline: got %s", c.cmdline)
	}
	if c.diskSize != "20G" {
		t.Errorf("diskSize: got %s", c.diskSize)
	}
	if c.networkMode != networkUser {
		t.Errorf("networkMode: got %d", c.networkMode)
	}
	if !c.enableQMP {
		t.Error("enableQMP should be true")
	}
	if c.serialMode != serialSocket {
		t.Errorf("serialMode: got %d", c.serialMode)
	}
}

func TestBuildArgsKernelBoot(t *testing.T) {
	c := &Config{
		name:    "test",
		machine: "q35,accel=kvm",
		memory:  "1G",
		cpus:    1,
		kernel:  "/boot/vmlinuz",
		initrd:  "/boot/initrd",
		cmdline: "console=ttyS0 root=/dev/sda1",
	}

	args := buildArgs(c)
	argStr := strings.Join(args, " ")

	if !strings.Contains(argStr, "-kernel /boot/vmlinuz") {
		t.Error("missing -kernel")
	}
	if !strings.Contains(argStr, "-initrd /boot/initrd") {
		t.Error("missing -initrd")
	}
	if !strings.Contains(argStr, "-append console=ttyS0 root=/dev/sda1") {
		t.Error("missing -append")
	}
	// Kernel boot should NOT have UEFI firmware
	if strings.Contains(argStr, "pflash") {
		t.Error("kernel boot should not include pflash")
	}
}

func TestBuildArgsUEFIBoot(t *testing.T) {
	c := &Config{
		name:             "test",
		machine:          "q35,accel=kvm,smm=on",
		memory:           "4G",
		cpus:             2,
		ovmfCode:         "/usr/share/OVMF/OVMF_CODE.fd",
		resolvedVarsPath: "/tmp/vars.fd",
		resolvedDiskPath: "/tmp/disk.img",
	}

	args := buildArgs(c)
	argStr := strings.Join(args, " ")

	if !strings.Contains(argStr, "pflash") {
		t.Error("UEFI boot should include pflash")
	}
	if !strings.Contains(argStr, "OVMF_CODE.fd") {
		t.Error("missing OVMF CODE path")
	}
	if !strings.Contains(argStr, "vars.fd") {
		t.Error("missing OVMF VARS path")
	}
}

func TestBuildArgsISOBoot(t *testing.T) {
	c := &Config{
		name:             "test",
		machine:          "q35,accel=kvm",
		memory:           "2G",
		cpus:             2,
		ovmfCode:         "/ovmf/code.fd",
		resolvedVarsPath: "/tmp/vars.fd",
		resolvedDiskPath: "/tmp/disk.img",
		iso:              "/tmp/boot.iso",
	}

	args := buildArgs(c)
	argStr := strings.Join(args, " ")

	if !strings.Contains(argStr, "boot.iso") {
		t.Error("missing ISO path")
	}
	if !strings.Contains(argStr, "scsi-cd") {
		t.Error("ISO boot should include SCSI CD device")
	}
	if !strings.Contains(argStr, "bootindex=0") {
		t.Error("ISO should have bootindex=0")
	}
}

func TestBuildArgsUserNetwork(t *testing.T) {
	c := &Config{
		name:        "test",
		machine:     "q35,accel=kvm",
		memory:      "1G",
		cpus:        1,
		networkMode: networkUser,
		macAddress:  "52:54:00:00:00:01",
	}

	args := buildArgs(c)
	argStr := strings.Join(args, " ")

	if !strings.Contains(argStr, "user,id=net0") {
		t.Error("missing user netdev")
	}
	if !strings.Contains(argStr, "mac=52:54:00:00:00:01") {
		t.Error("missing MAC address")
	}
}

func TestBuildArgsTapNetwork(t *testing.T) {
	c := &Config{
		name:        "test",
		machine:     "q35,accel=kvm",
		memory:      "1G",
		cpus:        1,
		networkMode: networkTap,
		tapDevice:   "tap0",
		secondTap:   "tap1",
	}

	args := buildArgs(c)
	argStr := strings.Join(args, " ")

	if !strings.Contains(argStr, "tap,id=net0,ifname=tap0") {
		t.Error("missing primary tap")
	}
	if !strings.Contains(argStr, "tap,id=net1,ifname=tap1") {
		t.Error("missing second tap")
	}
}

func TestBuildArgsNoNetwork(t *testing.T) {
	c := &Config{
		name:        "test",
		machine:     "q35,accel=kvm",
		memory:      "1G",
		cpus:        1,
		networkMode: networkNone,
	}

	args := buildArgs(c)
	argStr := strings.Join(args, " ")

	if !strings.Contains(argStr, "-nic none") {
		t.Error("no-network should have -nic none")
	}
}

func TestBuildArgsTPM(t *testing.T) {
	c := &Config{
		name:          "test",
		machine:       "q35,accel=kvm",
		memory:        "1G",
		cpus:          1,
		tpmSocketPath: "/tmp/swtpm.sock",
	}

	args := buildArgs(c)
	argStr := strings.Join(args, " ")

	if !strings.Contains(argStr, "tpm-tis") {
		t.Error("missing TPM device")
	}
	if !strings.Contains(argStr, "swtpm.sock") {
		t.Error("missing TPM socket path")
	}
}

func TestBuildArgs9P(t *testing.T) {
	c := &Config{
		name:    "test",
		machine: "q35,accel=kvm",
		memory:  "1G",
		cpus:    1,
		shares:  []share9P{{tag: "vmtest", hostPath: "/tmp/share"}},
	}

	args := buildArgs(c)
	argStr := strings.Join(args, " ")

	if !strings.Contains(argStr, "virtio-9p-pci") {
		t.Error("missing 9P device")
	}
	if !strings.Contains(argStr, "mount_tag=vmtest") {
		t.Error("missing mount tag")
	}
	if !strings.Contains(argStr, "/tmp/share") {
		t.Error("missing host path")
	}
}

func TestBuildArgsQMP(t *testing.T) {
	c := &Config{
		name:          "test",
		machine:       "q35,accel=kvm",
		memory:        "1G",
		cpus:          1,
		enableQMP:     true,
		qmpSocketPath: "/tmp/qmp.sock",
	}

	args := buildArgs(c)
	argStr := strings.Join(args, " ")

	if !strings.Contains(argStr, "-qmp") {
		t.Error("missing QMP arg")
	}
	if !strings.Contains(argStr, "qmp.sock") {
		t.Error("missing QMP socket path")
	}
}

func TestBuildArgsSerialSocket(t *testing.T) {
	c := &Config{
		name:       "test",
		machine:    "q35,accel=kvm",
		memory:     "1G",
		cpus:       1,
		serialMode: serialSocket,
	}

	args := buildArgs(c)
	argStr := strings.Join(args, " ")

	// serialSocket mode uses -serial stdio (piped via cmd.StdoutPipe)
	if !strings.Contains(argStr, "-serial stdio") {
		t.Error("serial socket mode should use -serial stdio")
	}
	if !strings.Contains(argStr, "-display none") {
		t.Error("serial socket mode should use -display none")
	}
	if strings.Contains(argStr, "-nographic") {
		t.Error("serial socket mode should not use nographic")
	}
}

func TestBuildArgsAgent(t *testing.T) {
	c := &Config{
		name:          "test",
		machine:       "q35,accel=kvm",
		memory:        "1G",
		cpus:          1,
		enableAgent:   true,
		agentSockPath: "/tmp/vmtest-sock-1234/agent.sock",
	}

	args := buildArgs(c)
	argStr := strings.Join(args, " ")

	if !strings.Contains(argStr, "virtio-serial-pci") {
		t.Error("missing virtio-serial-pci device")
	}
	if !strings.Contains(argStr, "vmtest-agent") {
		t.Error("missing agent chardev")
	}
	if !strings.Contains(argStr, "org.vmtest.agent") {
		t.Error("missing virtserialport name")
	}
	if !strings.Contains(argStr, "agent.sock") {
		t.Error("missing agent socket path")
	}
}

func TestBuildArgsAgentDisabled(t *testing.T) {
	c := &Config{
		name:    "test",
		machine: "q35,accel=kvm",
		memory:  "1G",
		cpus:    1,
	}

	args := buildArgs(c)
	argStr := strings.Join(args, " ")

	if strings.Contains(argStr, "vmtest-agent") {
		t.Error("agent args should not appear when agent is disabled")
	}
}

func TestWithAgent(t *testing.T) {
	c := &Config{}
	WithAgent()(c)
	if !c.enableAgent {
		t.Error("enableAgent should be true")
	}
}

func TestBuildArgsSerialStdio(t *testing.T) {
	c := &Config{
		name:       "test",
		machine:    "q35,accel=kvm",
		memory:     "1G",
		cpus:       1,
		serialMode: serialStdio,
	}

	args := buildArgs(c)
	argStr := strings.Join(args, " ")

	if !strings.Contains(argStr, "-nographic") {
		t.Error("serial stdio mode should use nographic")
	}
}
