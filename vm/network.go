package vm

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// validateTapDevice checks that a tap device exists.
// On non-Linux systems this is a no-op since TAP networking is Linux-only.
func validateTapDevice(name string) error {
	if runtime.GOOS != "linux" {
		return nil
	}
	cmd := exec.Command("ip", "link", "show", name)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tap device %q not found: %s", name, strings.TrimSpace(string(output)))
	}
	return nil
}

// validateBridge checks that a bridge exists and is UP.
// On non-Linux systems this is a no-op since bridge networking is Linux-only.
func validateBridge(name string) error {
	if runtime.GOOS != "linux" {
		return nil
	}
	cmd := exec.Command("ip", "link", "show", name)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("bridge %q not found", name)
	}
	outputStr := string(output)
	if !strings.Contains(outputStr, "state UP") {
		return fmt.Errorf("bridge %q exists but is not UP", name)
	}
	return nil
}
