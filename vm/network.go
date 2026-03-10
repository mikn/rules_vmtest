package vm

import (
	"fmt"
	"os/exec"
	"strings"
)

// validateTapDevice checks that a tap device exists.
func validateTapDevice(name string) error {
	cmd := exec.Command("ip", "link", "show", name)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tap device %q not found: %s", name, strings.TrimSpace(string(output)))
	}
	return nil
}

// validateBridge checks that a bridge exists and is UP.
func validateBridge(name string) error {
	cmd := exec.Command("ip", "link", "show", name)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("bridge %q not found", name)
	}
	outputStr := string(output)
	if !strings.Contains(outputStr, "UP") {
		return fmt.Errorf("bridge %q exists but is not UP", name)
	}
	return nil
}
