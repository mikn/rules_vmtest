package runner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// --- buildQEMUArgs tests ---

// newTestRunner creates a minimal Runner for testing buildQEMUArgs without
// starting any processes or requiring real toolchains.
func newTestRunner(t *testing.T, cfg *RunnerConfig) *Runner {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return &Runner{
		config: cfg,
		ctx:    ctx,
		cancel: cancel,
	}
}

func TestBuildQEMUArgsDefaults(t *testing.T) {
	r := newTestRunner(t, &RunnerConfig{
		ServerName: "test-vm",
	})

	args := r.buildQEMUArgs(
		"/fw/OVMF_CODE.fd", "/work/vars.fd", "/work/disk.img",
		"", "/tmp/tpm.sock", false, "tap0", "", false,
	)

	assertContainsArg(t, args, "-name", "test-vm")
	assertContainsArg(t, args, "-m", "6G")
	assertContainsArg(t, args, "-smp", "2")
	assertContainsArg(t, args, "-cpu", "host")
	assertContainsValue(t, args, "-device", "virtio-rng-pci")
}

func TestBuildQEMUArgsCustomMemoryAndCPUs(t *testing.T) {
	r := newTestRunner(t, &RunnerConfig{
		ServerName: "custom-vm",
		Memory:     "16G",
		CPUs:       8,
	})

	args := r.buildQEMUArgs(
		"/fw/OVMF_CODE.fd", "/work/vars.fd", "/work/disk.img",
		"", "/tmp/tpm.sock", false, "tap0", "", false,
	)

	assertContainsArg(t, args, "-m", "16G")
	assertContainsArg(t, args, "-smp", "8")
}

func TestBuildQEMUArgsMachineType(t *testing.T) {
	r := newTestRunner(t, &RunnerConfig{
		ServerName:  "machine-vm",
		MachineType: "virt,accel=hvf",
	})

	args := r.buildQEMUArgs(
		"/fw/OVMF_CODE.fd", "/work/vars.fd", "/work/disk.img",
		"", "/tmp/tpm.sock", false, "tap0", "", false,
	)

	assertContainsArg(t, args, "-machine", "virt,accel=hvf")
}

func TestBuildQEMUArgsTCGUsesCPUMax(t *testing.T) {
	r := newTestRunner(t, &RunnerConfig{
		ServerName:  "tcg-vm",
		MachineType: "q35,accel=tcg",
	})

	args := r.buildQEMUArgs(
		"/fw/OVMF_CODE.fd", "/work/vars.fd", "/work/disk.img",
		"", "/tmp/tpm.sock", false, "tap0", "", false,
	)

	assertContainsArg(t, args, "-cpu", "max")
}

func TestBuildQEMUArgsKVMUsesCPUHost(t *testing.T) {
	r := newTestRunner(t, &RunnerConfig{
		ServerName:  "kvm-vm",
		MachineType: "q35,accel=kvm,smm=on",
	})

	args := r.buildQEMUArgs(
		"/fw/OVMF_CODE.fd", "/work/vars.fd", "/work/disk.img",
		"", "/tmp/tpm.sock", false, "tap0", "", false,
	)

	assertContainsArg(t, args, "-cpu", "host")
}

func TestBuildQEMUArgsNetwork(t *testing.T) {
	r := newTestRunner(t, &RunnerConfig{
		ServerName: "net-vm",
	})

	args := r.buildQEMUArgs(
		"/fw/OVMF_CODE.fd", "/work/vars.fd", "/work/disk.img",
		"", "/tmp/tpm.sock", false, "tap0", "", false,
	)

	assertContainsArg(t, args, "-netdev", "tap,id=net0,ifname=tap0,script=no,downscript=no")
	assertContainsValue(t, args, "-device", "virtio-net-pci,netdev=net0")
}

func TestBuildQEMUArgsNetworkMacAddress(t *testing.T) {
	r := newTestRunner(t, &RunnerConfig{
		ServerName: "mac-vm",
		MacAddress: "52:54:00:12:34:56",
	})

	args := r.buildQEMUArgs(
		"/fw/OVMF_CODE.fd", "/work/vars.fd", "/work/disk.img",
		"", "/tmp/tpm.sock", false, "tap0", "", false,
	)

	assertContainsValue(t, args, "-device", "virtio-net-pci,netdev=net0,mac=52:54:00:12:34:56")
}

func TestBuildQEMUArgsSecondTap(t *testing.T) {
	r := newTestRunner(t, &RunnerConfig{
		ServerName: "dual-net-vm",
	})

	args := r.buildQEMUArgs(
		"/fw/OVMF_CODE.fd", "/work/vars.fd", "/work/disk.img",
		"", "/tmp/tpm.sock", false, "tap0", "tap1", false,
	)

	assertContainsArg(t, args, "-netdev", "tap,id=net1,ifname=tap1,script=no,downscript=no")
	assertContainsValue(t, args, "-device", "virtio-net-pci,netdev=net1,addr=0x6")
}

func TestBuildQEMUArgsNoSecondTap(t *testing.T) {
	r := newTestRunner(t, &RunnerConfig{
		ServerName: "single-net-vm",
	})

	args := r.buildQEMUArgs(
		"/fw/OVMF_CODE.fd", "/work/vars.fd", "/work/disk.img",
		"", "/tmp/tpm.sock", false, "tap0", "", false,
	)

	for i, arg := range args {
		if arg == "-netdev" && i+1 < len(args) && strings.Contains(args[i+1], "net1") {
			t.Error("second network interface should not be present when secondTapName is empty")
		}
	}
}

func TestBuildQEMUArgsTPM(t *testing.T) {
	r := newTestRunner(t, &RunnerConfig{
		ServerName: "tpm-vm",
	})

	args := r.buildQEMUArgs(
		"/fw/OVMF_CODE.fd", "/work/vars.fd", "/work/disk.img",
		"", "/tmp/tpm.sock", false, "tap0", "", false,
	)

	assertContainsArg(t, args, "-chardev", "socket,id=char0,path=/tmp/tpm.sock")
	assertContainsArg(t, args, "-tpmdev", "emulator,id=tpm0,chardev=char0")
	assertContainsValue(t, args, "-device", "tpm-tis,tpmdev=tpm0")
}

func TestBuildQEMUArgsUEFIFirmware(t *testing.T) {
	r := newTestRunner(t, &RunnerConfig{
		ServerName: "uefi-vm",
	})

	args := r.buildQEMUArgs(
		"/fw/OVMF_CODE.fd", "/work/vars.fd", "/work/disk.img",
		"", "/tmp/tpm.sock", false, "tap0", "", false,
	)

	assertContainsArg(t, args, "-global", "driver=cfi.pflash01,property=secure,value=on")
	assertContainsValue(t, args, "-drive", "if=pflash,format=raw,unit=0,file=/fw/OVMF_CODE.fd,readonly=on")
	assertContainsValue(t, args, "-drive", "if=pflash,format=raw,unit=1,file=/work/vars.fd")
}

func TestBuildQEMUArgsDiskOnly(t *testing.T) {
	r := newTestRunner(t, &RunnerConfig{
		ServerName: "disk-vm",
	})

	args := r.buildQEMUArgs(
		"/fw/OVMF_CODE.fd", "/work/vars.fd", "/work/disk.img",
		"", "/tmp/tpm.sock", false, "tap0", "", false,
	)

	assertContainsValue(t, args, "-drive", "file=/work/disk.img,format=raw,if=none,id=disk")
	assertContainsValue(t, args, "-device", "virtio-blk-pci,drive=disk,addr=0x5,bootindex=0")

	// No SCSI or CD-ROM when not booting from ISO
	for _, arg := range args {
		if strings.Contains(arg, "scsi") || strings.Contains(arg, "cdrom") {
			t.Errorf("unexpected SCSI/cdrom arg when not booting from ISO: %s", arg)
		}
	}
}

func TestBuildQEMUArgsBootFromISO(t *testing.T) {
	r := newTestRunner(t, &RunnerConfig{
		ServerName: "iso-vm",
	})

	args := r.buildQEMUArgs(
		"/fw/OVMF_CODE.fd", "/work/vars.fd", "/work/disk.img",
		"/images/boot.iso", "/tmp/tpm.sock", true, "tap0", "", false,
	)

	assertContainsValue(t, args, "-device", "virtio-scsi-pci,id=scsi0,addr=0x4")
	assertContainsValue(t, args, "-drive", "file=/images/boot.iso,format=raw,cache=none,media=cdrom,if=none,id=cd0")
	assertContainsValue(t, args, "-device", "scsi-cd,bus=scsi0.0,drive=cd0,bootindex=0")
	// Disk has bootindex=1 when ISO is primary
	assertContainsValue(t, args, "-device", "virtio-blk-pci,drive=disk,addr=0x5,bootindex=1")
}

func TestBuildQEMUArgsDirectMode(t *testing.T) {
	r := newTestRunner(t, &RunnerConfig{
		ServerName: "direct-vm",
	})

	args := r.buildQEMUArgs(
		"/fw/OVMF_CODE.fd", "/work/vars.fd", "/work/disk.img",
		"", "/tmp/tpm.sock", false, "tap0", "", true,
	)

	assertContainsFlag(t, args, "-nographic")

	for i, arg := range args {
		if arg == "-serial" && i+1 < len(args) && args[i+1] == "stdio" {
			t.Error("-serial stdio should not be present in direct mode")
		}
	}
}

func TestBuildQEMUArgsSerialMode(t *testing.T) {
	r := newTestRunner(t, &RunnerConfig{
		ServerName: "serial-vm",
	})

	args := r.buildQEMUArgs(
		"/fw/OVMF_CODE.fd", "/work/vars.fd", "/work/disk.img",
		"", "/tmp/tpm.sock", false, "tap0", "", false,
	)

	assertContainsArg(t, args, "-serial", "stdio")
	assertContainsArg(t, args, "-display", "none")

	for _, arg := range args {
		if arg == "-nographic" {
			t.Error("-nographic should not be present in serial (non-direct) mode")
		}
	}
}

// --- defaultMachineType tests ---

func TestDefaultMachineType(t *testing.T) {
	mt := defaultMachineType()
	if mt == "" {
		t.Fatal("defaultMachineType returned empty string")
	}

	switch runtime.GOOS {
	case "darwin":
		if !strings.Contains(mt, "hvf") {
			t.Errorf("expected hvf accelerator on darwin, got %s", mt)
		}
	case "linux":
		if !strings.Contains(mt, "kvm") {
			t.Errorf("expected kvm accelerator on linux, got %s", mt)
		}
	}

	if runtime.GOOS == "linux" && runtime.GOARCH == "amd64" {
		if mt != "q35,accel=kvm,smm=on" {
			t.Errorf("expected q35,accel=kvm,smm=on on linux/amd64, got %s", mt)
		}
	}
}

// --- NewRunner defaults tests ---

func TestNewRunnerDefaultBridge(t *testing.T) {
	r := NewRunner(&RunnerConfig{
		ServerName: "test",
	})
	defer r.Cancel()

	if r.config.BridgeName != "mltt-br0" {
		t.Errorf("expected default bridge mltt-br0, got %s", r.config.BridgeName)
	}
}

func TestNewRunnerDefaultToolPaths(t *testing.T) {
	r := NewRunner(&RunnerConfig{
		ServerName: "test",
	})
	defer r.Cancel()

	tp := r.config.ToolPaths
	if tp.QemuSystem == "" {
		t.Error("QemuSystem should not be empty")
	}
	if tp.QemuImg != "qemu-img" {
		t.Errorf("expected default QemuImg=qemu-img, got %s", tp.QemuImg)
	}
	if tp.Swtpm != "swtpm" {
		t.Errorf("expected default Swtpm=swtpm, got %s", tp.Swtpm)
	}
	if tp.SwtpmSetup != "swtpm_setup" {
		t.Errorf("expected default SwtpmSetup=swtpm_setup, got %s", tp.SwtpmSetup)
	}
}

func TestNewRunnerExplicitToolPathsPreserved(t *testing.T) {
	r := NewRunner(&RunnerConfig{
		ServerName: "test",
		ToolPaths: ToolPaths{
			QemuSystem: "/custom/qemu-system-x86_64",
			QemuImg:    "/custom/qemu-img",
			Swtpm:      "/custom/swtpm",
			SwtpmSetup: "/custom/swtpm_setup",
		},
	})
	defer r.Cancel()

	tp := r.config.ToolPaths
	if tp.QemuSystem != "/custom/qemu-system-x86_64" {
		t.Errorf("expected custom QemuSystem, got %s", tp.QemuSystem)
	}
	if tp.QemuImg != "/custom/qemu-img" {
		t.Errorf("expected custom QemuImg, got %s", tp.QemuImg)
	}
	if tp.Swtpm != "/custom/swtpm" {
		t.Errorf("expected custom Swtpm, got %s", tp.Swtpm)
	}
	if tp.SwtpmSetup != "/custom/swtpm_setup" {
		t.Errorf("expected custom SwtpmSetup, got %s", tp.SwtpmSetup)
	}
}

func TestNewRunnerExplicitBridgePreserved(t *testing.T) {
	r := NewRunner(&RunnerConfig{
		ServerName: "test",
		BridgeName: "custom-br0",
	})
	defer r.Cancel()

	if r.config.BridgeName != "custom-br0" {
		t.Errorf("expected custom-br0, got %s", r.config.BridgeName)
	}
}

// --- Runner path helpers tests ---

func TestRunnerPathHelpers(t *testing.T) {
	r := newTestRunner(t, &RunnerConfig{ServerName: "test"})
	r.workDir = "/work/test-vm"

	if r.tpmDir() != "/work/test-vm/tpm" {
		t.Errorf("tpmDir: got %s, want /work/test-vm/tpm", r.tpmDir())
	}
	if r.diskPath() != "/work/test-vm/disk.img" {
		t.Errorf("diskPath: got %s, want /work/test-vm/disk.img", r.diskPath())
	}
	if r.varsPath() != "/work/test-vm/vars.fd" {
		t.Errorf("varsPath: got %s, want /work/test-vm/vars.fd", r.varsPath())
	}
}

// --- ProcessManager tests ---

func TestNewProcessManager(t *testing.T) {
	ctx := context.Background()
	pm := NewProcessManager(ctx)

	if pm == nil {
		t.Fatal("NewProcessManager returned nil")
	}
	if len(pm.processes) != 0 {
		t.Errorf("expected empty processes, got %d", len(pm.processes))
	}
	if len(pm.pidFiles) != 0 {
		t.Errorf("expected empty pidFiles, got %d", len(pm.pidFiles))
	}
}

func TestProcessManagerAddPIDFile(t *testing.T) {
	ctx := context.Background()
	pm := NewProcessManager(ctx)

	pm.AddPIDFile("/tmp/test.pid")
	pm.AddPIDFile("/tmp/test2.pid")

	if len(pm.pidFiles) != 2 {
		t.Fatalf("expected 2 pidFiles, got %d", len(pm.pidFiles))
	}
	if pm.pidFiles[0] != "/tmp/test.pid" {
		t.Errorf("expected /tmp/test.pid, got %s", pm.pidFiles[0])
	}
	if pm.pidFiles[1] != "/tmp/test2.pid" {
		t.Errorf("expected /tmp/test2.pid, got %s", pm.pidFiles[1])
	}
}

func TestProcessManagerCleanupPIDFileWithRealPID(t *testing.T) {
	// Write a PID file pointing to a non-existent process (PID will be invalid)
	tmpDir := t.TempDir()
	pidFile := filepath.Join(tmpDir, "test.pid")
	// Use a very high PID that is almost certainly not running
	if err := os.WriteFile(pidFile, []byte("999999999"), 0644); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	pm := NewProcessManager(ctx)

	// Should not panic even when the PID doesn't exist
	pm.cleanupPIDFile(pidFile)

	// PID file should be removed
	if _, err := os.Stat(pidFile); !os.IsNotExist(err) {
		t.Error("expected PID file to be removed after cleanup")
	}
}

func TestProcessManagerCleanupPIDFileMissing(t *testing.T) {
	ctx := context.Background()
	pm := NewProcessManager(ctx)

	// Should not panic when PID file doesn't exist
	pm.cleanupPIDFile("/nonexistent/path/test.pid")
}

func TestProcessManagerCleanupAllEmpty(t *testing.T) {
	ctx := context.Background()
	pm := NewProcessManager(ctx)

	// CleanupAll on an empty manager should not panic
	pm.CleanupAll()
}

func TestProcessManagerCleanupAllRemovesPIDFiles(t *testing.T) {
	tmpDir := t.TempDir()
	pidFile1 := filepath.Join(tmpDir, "a.pid")
	pidFile2 := filepath.Join(tmpDir, "b.pid")

	// Write PID files with non-existent PIDs
	os.WriteFile(pidFile1, []byte("999999998"), 0644)
	os.WriteFile(pidFile2, []byte("999999997"), 0644)

	ctx := context.Background()
	pm := NewProcessManager(ctx)
	pm.AddPIDFile(pidFile1)
	pm.AddPIDFile(pidFile2)

	pm.CleanupAll()

	if _, err := os.Stat(pidFile1); !os.IsNotExist(err) {
		t.Error("expected pidFile1 to be removed")
	}
	if _, err := os.Stat(pidFile2); !os.IsNotExist(err) {
		t.Error("expected pidFile2 to be removed")
	}
}

// --- OutputStreamer tests ---

func TestNewOutputStreamer(t *testing.T) {
	s := NewOutputStreamer()
	if s == nil {
		t.Fatal("NewOutputStreamer returned nil")
	}
}

// --- Network validation tests ---
// checkBridgeSetup and useTapDevice require real network interfaces, so we
// test the error paths that don't depend on system state.

func TestUseTapDeviceEmptyConfig(t *testing.T) {
	r := NewRunner(&RunnerConfig{
		ServerName: "test",
		TapDevice:  "",
	})
	defer r.Cancel()

	_, err := r.useTapDevice()
	if err == nil {
		t.Fatal("expected error for empty tap device")
	}
	if !strings.Contains(err.Error(), "no tap device specified") {
		t.Errorf("expected 'no tap device specified' error, got: %s", err.Error())
	}
}

// --- isNewer tests ---

func TestIsNewerSrcNewer(t *testing.T) {
	tmpDir := t.TempDir()
	dst := filepath.Join(tmpDir, "dst")
	src := filepath.Join(tmpDir, "src")

	if err := os.WriteFile(dst, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	// Set dst mtime to the past to guarantee src is newer
	past := time.Now().Add(-10 * time.Second)
	if err := os.Chtimes(dst, past, past); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("new"), 0644); err != nil {
		t.Fatal(err)
	}

	if !isNewer(src, dst) {
		t.Error("expected src to be newer than dst")
	}
}

func TestIsNewerDstMissing(t *testing.T) {
	tmpDir := t.TempDir()
	src := filepath.Join(tmpDir, "src")
	if err := os.WriteFile(src, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	if !isNewer(src, "/nonexistent/dst") {
		t.Error("expected isNewer to return true when dst does not exist")
	}
}

func TestIsNewerSrcMissing(t *testing.T) {
	tmpDir := t.TempDir()
	dst := filepath.Join(tmpDir, "dst")
	if err := os.WriteFile(dst, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	if isNewer("/nonexistent/src", dst) {
		t.Error("expected isNewer to return false when src does not exist")
	}
}

// --- DefaultToolPaths tests ---

func TestDefaultToolPaths(t *testing.T) {
	tp := DefaultToolPaths()

	if tp.QemuImg != "qemu-img" {
		t.Errorf("expected qemu-img, got %s", tp.QemuImg)
	}
	if tp.Swtpm != "swtpm" {
		t.Errorf("expected swtpm, got %s", tp.Swtpm)
	}
	if tp.SwtpmSetup != "swtpm_setup" {
		t.Errorf("expected swtpm_setup, got %s", tp.SwtpmSetup)
	}

	switch runtime.GOARCH {
	case "arm64":
		if tp.QemuSystem != "qemu-system-aarch64" {
			t.Errorf("expected qemu-system-aarch64 on arm64, got %s", tp.QemuSystem)
		}
	default:
		if tp.QemuSystem != "qemu-system-x86_64" {
			t.Errorf("expected qemu-system-x86_64 on %s, got %s", runtime.GOARCH, tp.QemuSystem)
		}
	}
}

// --- Full QEMU args integration: verify arg ordering and completeness ---

func TestBuildQEMUArgsFullArgCount(t *testing.T) {
	r := newTestRunner(t, &RunnerConfig{
		ServerName:  "count-vm",
		MachineType: "q35,accel=kvm,smm=on",
		Memory:      "4G",
		CPUs:        4,
		MacAddress:  "52:54:00:aa:bb:cc",
	})

	args := r.buildQEMUArgs(
		"/fw/OVMF_CODE.fd", "/work/vars.fd", "/work/disk.img",
		"/images/boot.iso", "/tmp/tpm.sock", true, "tap0", "tap1", false,
	)

	// Verify all major sections are present by checking key flags
	requiredFlags := []string{
		"-name", "-machine", "-cpu", "-m", "-smp",
		"-netdev", "-chardev", "-tpmdev", "-global",
		"-serial", "-display",
	}
	for _, flag := range requiredFlags {
		found := false
		for _, arg := range args {
			if arg == flag {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing required flag %s in args", flag)
		}
	}
}

// --- Test helpers ---

// assertContainsArg checks that args contains a consecutive pair [flag, value].
func assertContainsArg(t *testing.T, args []string, flag, value string) {
	t.Helper()
	for i := 0; i < len(args)-1; i++ {
		if args[i] == flag && args[i+1] == value {
			return
		}
	}
	t.Errorf("expected args to contain [%s %s], args: %v", flag, value, args)
}

// assertContainsValue checks that at least one occurrence of flag has the given
// value. This is useful when a flag like "-device" appears multiple times.
func assertContainsValue(t *testing.T, args []string, flag, value string) {
	t.Helper()
	for i := 0; i < len(args)-1; i++ {
		if args[i] == flag && args[i+1] == value {
			return
		}
	}
	t.Errorf("expected args to contain [%s %s]\nargs: %s", flag, value, formatArgs(args))
}

// assertContainsFlag checks that a standalone flag is present in args.
func assertContainsFlag(t *testing.T, args []string, flag string) {
	t.Helper()
	for _, arg := range args {
		if arg == flag {
			return
		}
	}
	t.Errorf("expected args to contain flag %s, args: %v", flag, args)
}

func formatArgs(args []string) string {
	var parts []string
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "-") && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			parts = append(parts, fmt.Sprintf("  %s %s", args[i], args[i+1]))
			i++
		} else {
			parts = append(parts, fmt.Sprintf("  %s", args[i]))
		}
	}
	return "\n" + strings.Join(parts, "\n")
}
