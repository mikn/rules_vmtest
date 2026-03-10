package runner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

type RunnerConfig struct {
	// Core configuration
	ServerName string
	WorkDir    string
	Mode       string // "build" or "run"

	// Tool paths
	ToolPaths ToolPaths

	// Network configuration
	TapDevice       string
	SecondTapDevice string
	MacAddress      string
	BridgeName      string // Network bridge name (default: "mltt-br0")

	// VM configuration
	DiskSize string
	Memory   string
	CPUs     int

	// Optional pristine TPM source
	TPMPristinePath string

	// Optional ISO (determines boot mode)
	ISOPath string

	// Transit network configuration for host tap device validation
	TransitNetwork TransitNetwork

	// Output mode configuration
	OutputMode string // "direct" or "tagged"

	// Bootstrap behavior
	ShutdownMode bool

	// Advanced options
	NoCleanup bool
}

type Runner struct {
	config   *RunnerConfig
	ctx      context.Context
	cancel   context.CancelFunc
	pm       *ProcessManager
	streamer *OutputStreamer
	workDir  string
}

func (r *Runner) tpmDir() string   { return filepath.Join(r.workDir, "tpm") }
func (r *Runner) diskPath() string { return filepath.Join(r.workDir, "disk.img") }
func (r *Runner) varsPath() string { return filepath.Join(r.workDir, "vars.fd") }

func NewRunner(config *RunnerConfig) *Runner {
	ctx, cancel := context.WithCancel(context.Background())
	if config.BridgeName == "" {
		config.BridgeName = "mltt-br0"
	}
	tp := &config.ToolPaths
	if tp.QemuSystem == "" {
		tp.QemuSystem = "qemu-system-x86_64"
	}
	if tp.QemuImg == "" {
		tp.QemuImg = "qemu-img"
	}
	if tp.Swtpm == "" {
		tp.Swtpm = "swtpm"
	}
	if tp.SwtpmSetup == "" {
		tp.SwtpmSetup = "swtpm_setup"
	}
	return &Runner{
		config:   config,
		ctx:      ctx,
		cancel:   cancel,
		pm:       NewProcessManager(ctx),
		streamer: NewOutputStreamer(),
	}
}

func (r *Runner) Run() error {
	if err := r.checkBridgeSetup(); err != nil {
		return fmt.Errorf("bridge check failed: %w", err)
	}

	// Validate OVMF paths
	if r.config.ToolPaths.OVMFCode == "" {
		return fmt.Errorf("OVMF CODE path not configured")
	}
	if !fileExists(r.config.ToolPaths.OVMFCode) {
		return fmt.Errorf("OVMF CODE not found at %s", r.config.ToolPaths.OVMFCode)
	}
	if r.config.Mode == "build" {
		if r.config.ToolPaths.OVMFVars == "" {
			return fmt.Errorf("OVMF VARS path not configured")
		}
		if !fileExists(r.config.ToolPaths.OVMFVars) {
			return fmt.Errorf("OVMF VARS template not found at %s", r.config.ToolPaths.OVMFVars)
		}
	}

	// Resolve work directory
	r.workDir = r.config.WorkDir
	if !filepath.IsAbs(r.workDir) {
		abs, err := filepath.Abs(r.workDir)
		if err != nil {
			return fmt.Errorf("failed to resolve work directory: %w", err)
		}
		r.workDir = abs
	}

	if err := os.MkdirAll(r.workDir, 0755); err != nil {
		return fmt.Errorf("failed to create work directory: %w", err)
	}

	if !r.config.NoCleanup {
		defer func() {
			r.pm.CleanupAll()
		}()
	}

	if r.config.Mode == "build" {
		return r.runBuildMode()
	} else {
		return r.runRunMode()
	}
}

func (r *Runner) Cancel() {
	r.cancel()
}

func (r *Runner) setupTPMState() error {
	tpmDir := r.tpmDir()

	if r.config.TPMPristinePath != "" {
		pristineTPM := filepath.Join(r.config.TPMPristinePath, "tpm2-00.permall")

		if !fileExists(pristineTPM) {
			fmt.Printf("Creating pristine TPM state at %s\n", r.config.TPMPristinePath)
			if err := r.initializeTPMState(r.config.TPMPristinePath); err != nil {
				return fmt.Errorf("failed to create pristine TPM state: %w", err)
			}
		}

		fmt.Printf("Copying pristine TPM state to work directory\n")
		if err := os.MkdirAll(tpmDir, 0700); err != nil {
			return fmt.Errorf("failed to create work TPM directory: %w", err)
		}

		return copyDir(r.config.TPMPristinePath, tpmDir)
	}

	fmt.Println("Creating fresh TPM state in work directory...")
	return r.initializeTPMState(tpmDir)
}

func (r *Runner) runBuildMode() error {
	fmt.Printf("Running in build mode for %s\n", r.config.ServerName)

	tapName, err := r.useTapDevice()
	if err != nil {
		return fmt.Errorf("failed to use tap device: %w", err)
	}

	// Setup OVMF vars
	varsPath := filepath.Join(r.workDir, "vars.fd")
	if err := os.MkdirAll(filepath.Dir(varsPath), 0755); err != nil {
		return fmt.Errorf("failed to create vars directory: %w", err)
	}
	if err := copyFile(r.config.ToolPaths.OVMFVars, varsPath); err != nil {
		return fmt.Errorf("failed to copy OVMF VARS template: %w", err)
	}
	fmt.Printf("Copied OVMF VARS template to %s\n", varsPath)

	isoPath := r.config.ISOPath
	if isoPath != "" && !fileExists(isoPath) {
		return fmt.Errorf("ISO not found at %s", isoPath)
	}

	// Create initial disk
	size := r.config.DiskSize
	if size == "" {
		size = "50G"
	}
	diskPath := r.diskPath()
	if err := os.MkdirAll(filepath.Dir(diskPath), 0755); err != nil {
		return fmt.Errorf("failed to create parent directory: %w", err)
	}
	if err := exec.Command(r.config.ToolPaths.QemuImg, "create", "-f", "raw", diskPath, size).Run(); err != nil {
		return fmt.Errorf("qemu-img create failed: %w", err)
	}
	fmt.Printf("Created disk image: %s (%s)\n", diskPath, size)

	// Setup TPM state
	if err := r.setupTPMState(); err != nil {
		return fmt.Errorf("failed to setup TPM state: %w", err)
	}

	// Start TPM emulator
	tpmSocket, pidFile, err := r.startTPMWithDir(r.tpmDir())
	if err != nil {
		return fmt.Errorf("failed to start TPM: %w", err)
	}
	r.pm.AddPIDFile(pidFile)

	time.Sleep(500 * time.Millisecond)

	bootFromISO := isoPath != ""

	args := r.buildQEMUArgs(r.config.ToolPaths.OVMFCode, varsPath, r.diskPath(),
		isoPath, tpmSocket, bootFromISO, tapName, r.config.SecondTapDevice, false)

	cmd := exec.CommandContext(r.ctx, r.config.ToolPaths.QemuSystem, args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start QEMU: %w", err)
	}

	r.streamer.AttachTagged(stdout, "[qemu-stdout]", Detached)
	r.streamer.AttachTagged(stderr, "[qemu-stderr]", Detached)

	select {
	case err := <-func() chan error {
		errChan := make(chan error, 1)
		go func() {
			errChan <- cmd.Wait()
		}()
		return errChan
	}():
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				if exitErr.ExitCode() != 0 {
					return fmt.Errorf("QEMU exited with error: %w", err)
				}
			} else {
				return fmt.Errorf("QEMU failed: %w", err)
			}
		}
	case <-time.After(20 * time.Minute):
		cmd.Process.Signal(syscall.SIGTERM)
		return fmt.Errorf("provisioning timeout after 20 minutes")
	case <-r.ctx.Done():
		cmd.Process.Signal(syscall.SIGTERM)
		return r.ctx.Err()
	}

	r.pm.cleanupPIDFile(pidFile)

	fmt.Printf("Build completed for %s\n", r.config.ServerName)
	fmt.Printf("Artifacts written to:\n")
	fmt.Printf("  Disk: %s\n", r.diskPath())
	fmt.Printf("  Work TPM state: %s\n", r.tpmDir())
	return nil
}

func (r *Runner) runRunMode() error {
	fmt.Printf("Running in run mode for %s\n", r.config.ServerName)

	tapName, err := r.useTapDevice()
	if err != nil {
		return fmt.Errorf("failed to use tap device: %w", err)
	}

	varsPath := filepath.Join(r.workDir, "vars.fd")
	if !fileExists(varsPath) {
		return fmt.Errorf("OVMF VARS not found at %s (run build mode first)", varsPath)
	}

	diskPath := r.diskPath()
	isoPath := r.config.ISOPath
	if err := r.setupRunArtifacts(diskPath); err != nil {
		return fmt.Errorf("failed to setup run artifacts: %w", err)
	}

	workTPM := r.tpmDir()
	tpmSocket, pidFile, err := r.startTPMWithDir(workTPM)
	if err != nil {
		return fmt.Errorf("failed to start TPM: %w", err)
	}

	if r.config.OutputMode == "tagged" {
		r.pm.AddPIDFile(pidFile)
	}

	bootFromISO := isoPath != ""

	workDisk := r.diskPath()
	directMode := r.config.OutputMode != "tagged"
	args := r.buildQEMUArgs(r.config.ToolPaths.OVMFCode, varsPath, workDisk, isoPath, tpmSocket, bootFromISO, tapName, r.config.SecondTapDevice, directMode)

	qemuCmd := exec.CommandContext(r.ctx, r.config.ToolPaths.QemuSystem, args...)

	if r.config.OutputMode == "tagged" {
		stdout, err := qemuCmd.StdoutPipe()
		if err != nil {
			return fmt.Errorf("failed to create stdout pipe: %w", err)
		}
		stderr, err := qemuCmd.StderrPipe()
		if err != nil {
			return fmt.Errorf("failed to create stderr pipe: %w", err)
		}

		if err := qemuCmd.Start(); err != nil {
			return fmt.Errorf("failed to start QEMU: %w", err)
		}
		r.pm.AddProcess(qemuCmd)

		r.streamer.AttachTagged(stdout, fmt.Sprintf("[%s]", r.config.ServerName), Monitored)
		r.streamer.AttachTagged(stderr, fmt.Sprintf("[%s:err]", r.config.ServerName), Monitored)

		fmt.Printf("VM started for %s (tagged output mode)\n", r.config.ServerName)

		r.streamer.Wait()
		qemuCmd.Wait()
	} else {
		r.pm.AddPIDFile(pidFile)

		qemuCmd.Stdin = os.Stdin
		qemuCmd.Stdout = os.Stdout
		qemuCmd.Stderr = os.Stderr

		if err := qemuCmd.Start(); err != nil {
			r.pm.cleanupPIDFile(pidFile)
			return fmt.Errorf("failed to start QEMU: %w", err)
		}

		r.pm.AddProcess(qemuCmd)

		fmt.Printf("VM started for %s (direct output mode)\n", r.config.ServerName)
		fmt.Printf("Press Ctrl-A X to exit QEMU\n")

		if err := qemuCmd.Wait(); err != nil {
			return fmt.Errorf("QEMU exited: %w", err)
		}
	}

	return nil
}

func (r *Runner) setupRunArtifacts(diskPath string) error {
	if err := os.MkdirAll(r.workDir, 0755); err != nil {
		return fmt.Errorf("failed to create work directory: %w", err)
	}

	dst := r.diskPath()
	if !fileExists(diskPath) {
		if !fileExists(dst) {
			size := r.config.DiskSize
			if size == "" {
				size = "50G"
			}
			if err := exec.Command(r.config.ToolPaths.QemuImg, "create", "-f", "raw", dst, size).Run(); err != nil {
				return fmt.Errorf("qemu-img create failed: %w", err)
			}
			fmt.Printf("Created disk image: %s (%s)\n", dst, size)
		}
	} else if !fileExists(dst) || isNewer(diskPath, dst) {
		if err := copyFile(diskPath, dst); err != nil {
			return fmt.Errorf("failed to copy disk: %w", err)
		}
	}

	varsFile := r.varsPath()
	if !fileExists(varsFile) {
		return fmt.Errorf("OVMF VARS not found at %s - run build first", varsFile)
	}
	fmt.Printf("Using existing OVMF VARS from: %s\n", varsFile)

	tpmStateFile := filepath.Join(r.tpmDir(), "tpm2-00.permall")
	if !fileExists(tpmStateFile) {
		return fmt.Errorf("TPM state not found at %s - run build first", tpmStateFile)
	}
	fmt.Printf("Using existing TPM state from: %s\n", r.tpmDir())
	return nil
}
