package vm

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// setupTPM initializes TPM state and starts the swtpm emulator.
func (vm *VM) setupTPM() error {
	tpmDir := filepath.Join(vm.workDir, "tpm")

	if vm.config.tpmStatePath != "" {
		// Copy pristine TPM state
		pristineState := filepath.Join(vm.config.tpmStatePath, "tpm2-00.permall")
		if !FileExists(pristineState) {
			// Initialize pristine state first
			if err := InitializeTPMState(vm.config.swtpmSetup, vm.config.tpmStatePath, vm.config.tpmPCRBanks); err != nil {
				return fmt.Errorf("failed to create pristine TPM state: %w", err)
			}
		}
		if err := CopyDir(vm.config.tpmStatePath, tpmDir); err != nil {
			return fmt.Errorf("failed to copy TPM state: %w", err)
		}
	} else {
		// Fresh initialization
		if err := InitializeTPMState(vm.config.swtpmSetup, tpmDir, vm.config.tpmPCRBanks); err != nil {
			return err
		}
	}

	// Start swtpm
	socketPath, pidFile, err := StartSwtpm(vm.ctx, vm.config.swtpm, tpmDir)
	if err != nil {
		return err
	}
	vm.config.tpmSocketPath = socketPath
	vm.pm.addPIDFile(pidFile)

	return nil
}

// InitializeTPMState creates a fresh TPM state directory using swtpm_setup.
// The pcrBanks parameter specifies which hash algorithms to pre-allocate
// (e.g., []string{"sha256"} or []string{"sha1", "sha256", "sha384", "sha512"}).
// Using fewer banks significantly reduces UEFI measurement time during firmware boot.
func InitializeTPMState(swtpmSetup, tpmDir string, pcrBanks []string) error {
	if err := os.MkdirAll(tpmDir, 0700); err != nil {
		return fmt.Errorf("failed to create TPM directory: %w", err)
	}

	tpmStateFile := filepath.Join(tpmDir, "tpm2-00.permall")
	if FileExists(tpmStateFile) {
		return nil
	}

	cmd := exec.Command(swtpmSetup,
		"--tpmstate", fmt.Sprintf("dir://%s", tpmDir),
		"--tpm2",
		"--pcr-banks", strings.Join(pcrBanks, ","),
		"--createek",
		"--display",
		"--decryption",
		"--overwrite",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("swtpm_setup failed: %w\nOutput: %s", err, string(output))
	}

	if !FileExists(tpmStateFile) {
		return fmt.Errorf("TPM state file not created after swtpm_setup")
	}

	return nil
}

// StartSwtpm starts the swtpm emulator with the given state directory.
// Returns the socket path and PID file path.
// The socket is placed in /tmp to avoid Unix socket path length limits (108 bytes)
// that would be exceeded with deep Bazel sandbox paths.
func StartSwtpm(ctx context.Context, swtpmBin, tpmDir string) (socketPath string, pidFile string, err error) {
	tpmStateFile := filepath.Join(tpmDir, "tpm2-00.permall")
	if !FileExists(tpmStateFile) {
		return "", "", fmt.Errorf("TPM state file does not exist at %s", tpmStateFile)
	}

	// Use /tmp for the socket to stay within the 108-byte Unix socket path limit
	// that would be exceeded with deep Bazel sandbox paths.
	tmpDir, err := os.MkdirTemp("/tmp", "swtpm-*")
	if err != nil {
		return "", "", fmt.Errorf("failed to create temp dir for swtpm socket: %w", err)
	}
	socketPath = filepath.Join(tmpDir, "sock")
	pidFile = filepath.Join(tpmDir, "swtpm.pid")
	os.Remove(socketPath)

	args := []string{
		"socket",
		"--tpmstate", fmt.Sprintf("dir=%s", tpmDir),
		"--ctrl", fmt.Sprintf("type=unixio,path=%s", socketPath),
		"--terminate",
		"--tpm2",
		"--pid", fmt.Sprintf("file=%s", pidFile),
		"--flags", "not-need-init,startup-clear",
	}

	cmd := exec.CommandContext(ctx, swtpmBin, args...)
	if err := cmd.Start(); err != nil {
		return "", "", fmt.Errorf("failed to start swtpm: %w", err)
	}

	// Wait for socket to be ready — not just existing as a file, but actually
	// accepting connections. swtpm creates the socket file before it finishes
	// loading TPM state, so a file existence check is insufficient.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("unix", socketPath, 100*time.Millisecond)
		if err == nil {
			conn.Close()
			return socketPath, pidFile, nil
		}
		if cmd.ProcessState != nil {
			return "", "", fmt.Errorf("swtpm exited unexpectedly")
		}
		time.Sleep(50 * time.Millisecond)
	}

	logContent, _ := os.ReadFile(filepath.Join(tpmDir, "tpm.log"))
	cmd.Process.Kill()
	return "", "", fmt.Errorf("TPM socket not created after 5 seconds. Log: %s", string(logContent))
}
