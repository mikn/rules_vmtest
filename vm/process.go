package vm

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// processManager tracks spawned processes for cleanup.
type processManager struct {
	processes []*exec.Cmd
	pidFiles  []string
	mu        sync.Mutex
}

func newProcessManager() *processManager {
	return &processManager{}
}

func (pm *processManager) addProcess(cmd *exec.Cmd) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.processes = append(pm.processes, cmd)
}

func (pm *processManager) addPIDFile(path string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.pidFiles = append(pm.pidFiles, path)
}

func (pm *processManager) cleanupAll() {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	for _, pidFile := range pm.pidFiles {
		pm.cleanupPIDFile(pidFile)
	}

	for _, cmd := range pm.processes {
		if cmd.Process != nil {
			cmd.Process.Signal(syscall.SIGTERM)
			time.Sleep(500 * time.Millisecond)
			if cmd.ProcessState == nil {
				cmd.Process.Kill()
			}
			cmd.Wait()
		}
	}
}

func (pm *processManager) cleanupPIDFile(pidFile string) {
	if pidData, err := os.ReadFile(pidFile); err == nil {
		if pid, err := strconv.Atoi(strings.TrimSpace(string(pidData))); err == nil {
			syscall.Kill(pid, syscall.SIGTERM)
			time.Sleep(500 * time.Millisecond)
			syscall.Kill(pid, syscall.SIGKILL)
		}
	}
	os.Remove(pidFile)
}
