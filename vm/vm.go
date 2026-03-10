package vm

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// VM represents a running QEMU virtual machine.
type VM struct {
	config  *Config
	cmd     *exec.Cmd
	pm      *processManager
	workDir string
	serial  *SerialConsole
	qmp     *QMPClient
	ctx     context.Context
	cancel  context.CancelFunc
}

// Start boots a VM with the given options and returns immediately.
func Start(ctx context.Context, opts ...Option) (*VM, error) {
	cfg := &Config{}
	for _, o := range opts {
		o(cfg)
	}
	applyDefaults(cfg)

	vmCtx, cancel := context.WithCancel(ctx)

	// Work directory
	workDir := cfg.workDir
	if workDir == "" {
		var err error
		workDir, err = os.MkdirTemp("", "vmtest-*")
		if err != nil {
			cancel()
			return nil, fmt.Errorf("failed to create work directory: %w", err)
		}
	} else {
		if err := os.MkdirAll(workDir, 0755); err != nil {
			cancel()
			return nil, fmt.Errorf("failed to create work directory: %w", err)
		}
	}

	vm := &VM{
		config:  cfg,
		pm:      newProcessManager(),
		workDir: workDir,
		ctx:     vmCtx,
		cancel:  cancel,
	}

	// Validate tap networking if configured
	if cfg.networkMode == networkTap {
		if cfg.tapBridge != "" {
			if err := validateBridge(cfg.tapBridge); err != nil {
				cancel()
				return nil, err
			}
		}
		if err := validateTapDevice(cfg.tapDevice); err != nil {
			cancel()
			return nil, err
		}
		if cfg.secondTap != "" {
			if err := validateTapDevice(cfg.secondTap); err != nil {
				cancel()
				return nil, err
			}
		}
	}

	// Setup OVMF vars (copy template to work dir for UEFI boot)
	if cfg.ovmfVars != "" {
		varsPath := filepath.Join(workDir, "vars.fd")
		if err := CopyFile(cfg.ovmfVars, varsPath); err != nil {
			cancel()
			return nil, fmt.Errorf("failed to copy OVMF VARS template: %w", err)
		}
		cfg.resolvedVarsPath = varsPath
	}

	// Setup disk
	if cfg.existingDisk != "" {
		cfg.resolvedDiskPath = cfg.existingDisk
	} else if cfg.diskSize != "" {
		diskPath := filepath.Join(workDir, "disk.img")
		if err := CreateDisk(cfg.qemuImg, diskPath, cfg.diskSize); err != nil {
			cancel()
			return nil, err
		}
		cfg.resolvedDiskPath = diskPath
	}

	// Setup TPM
	if cfg.swtpm != "" {
		if err := vm.setupTPM(); err != nil {
			cancel()
			return nil, fmt.Errorf("failed to setup TPM: %w", err)
		}
	}

	// Setup sockets — use /tmp to stay within the 108-byte Unix socket path limit.
	if cfg.enableQMP || cfg.enableAgent {
		sockDir, err := os.MkdirTemp("/tmp", "vmtest-sock-*")
		if err != nil {
			cancel()
			return nil, fmt.Errorf("failed to create socket directory: %w", err)
		}
		if cfg.enableQMP {
			cfg.qmpSocketPath = filepath.Join(sockDir, "qmp.sock")
		}
		if cfg.enableAgent {
			cfg.agentSockPath = filepath.Join(sockDir, "agent.sock")
		}
	}

	// Build and start QEMU
	args := buildArgs(cfg)
	cmd := exec.CommandContext(vmCtx, cfg.qemuBinary, args...)

	// For serial capture mode, pipe stdout. For serial stdio mode, attach terminal.
	if cfg.serialMode == serialSocket {
		// Serial output goes to QEMU's stdout via -serial stdio.
		// Capture it via StdoutPipe — survives guest reboots with no buffering issues.
		pipe, err := cmd.StdoutPipe()
		if err != nil {
			cancel()
			return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
		}
		vm.serial = newSerialConsole(pipe)
	} else if cfg.serialMode == serialStdio {
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}

	if err := cmd.Start(); err != nil {
		vm.pm.cleanupAll()
		cancel()
		return nil, fmt.Errorf("failed to start QEMU: %w", err)
	}
	vm.cmd = cmd
	vm.pm.addProcess(cmd)

	// Connect QMP
	if cfg.enableQMP {
		qmpClient, err := newQMPClient(cfg.qmpSocketPath)
		if err != nil {
			vm.Kill()
			return nil, fmt.Errorf("failed to connect QMP: %w", err)
		}
		vm.qmp = qmpClient
	}

	return vm, nil
}

// Wait blocks until the VM process exits. Returns the exit error if any.
func (vm *VM) Wait() error {
	if vm.cmd == nil {
		return nil
	}
	return vm.cmd.Wait()
}

// Stop sends an ACPI powerdown via QMP for graceful shutdown.
// Falls back to Kill if QMP is not available.
func (vm *VM) Stop() error {
	if vm.qmp != nil {
		return vm.qmp.SystemPowerdown()
	}
	return vm.Kill()
}

// Kill forcefully terminates the QEMU process and cleans up.
func (vm *VM) Kill() error {
	vm.cancel()
	if vm.serial != nil {
		vm.serial.Close()
		vm.serial.Wait() // wait for readLoop to exit, closing logChan
	}
	if vm.qmp != nil {
		vm.qmp.Close()
	}
	vm.pm.cleanupAll()
	return nil
}

// QMP returns the QMP client, or nil if QMP is not enabled.
func (vm *VM) QMP() *QMPClient {
	return vm.qmp
}

// Serial returns the serial console, or nil if serial capture is not enabled.
func (vm *VM) Serial() *SerialConsole {
	return vm.serial
}

// WorkDir returns the temporary work directory for this VM.
func (vm *VM) WorkDir() string {
	return vm.workDir
}

// AgentSocketPath returns the virtio-serial agent socket path, or empty if agent is not enabled.
func (vm *VM) AgentSocketPath() string {
	return vm.config.agentSockPath
}
