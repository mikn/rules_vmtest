package runner

import (
	"fmt"

	"rules_vmtest/vm"
)

func (r *Runner) initializeTPMState(tpmDir string) error {
	return vm.InitializeTPMState(r.config.ToolPaths.SwtpmSetup, tpmDir)
}

func (r *Runner) startTPMWithDir(tpmDir string) (tpmSocket string, pidFile string, err error) {
	socketPath, pid, err := vm.StartSwtpm(r.ctx, r.config.ToolPaths.Swtpm, tpmDir)
	if err != nil {
		return "", "", err
	}

	fmt.Printf("TPM emulator started\n")
	fmt.Printf("  Socket: %s\n", socketPath)

	return socketPath, pid, nil
}
