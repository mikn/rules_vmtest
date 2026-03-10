package runner

import (
	"fmt"
	"os/exec"
	"strings"
)

func (r *Runner) checkBridgeSetup() error {
	bridgeName := r.config.BridgeName

	cmd := exec.Command("ip", "link", "show", bridgeName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("bridge %s not found", bridgeName)
	}

	// Check if bridge has UP flag (administratively up)
	// Note: Bridge may show "state DOWN" if no interfaces attached, but that's OK
	// We check for the UP flag in the interface flags: <...,UP>
	outputStr := string(output)
	if !strings.Contains(outputStr, "<") || !strings.Contains(outputStr, "UP") {
		return fmt.Errorf("bridge %s exists but is not administratively UP", bridgeName)
	}

	// Check host tap device has transit IPs if a second tap device is configured
	if r.config.SecondTapDevice != "" {
		hostTap := r.config.SecondTapDevice
		cmd = exec.Command("ip", "addr", "show", hostTap)
		output, err = cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("host tap device %s not found - ensure bridge setup has been run", hostTap)
		}

		addrStr := string(output)
		if r.config.TransitNetwork.IPv4 != "" {
			if !strings.Contains(addrStr, r.config.TransitNetwork.IPv4) {
				return fmt.Errorf("host tap %s missing IPv4 transit address (%s)", hostTap, r.config.TransitNetwork.IPv4)
			}
		}
		if r.config.TransitNetwork.IPv6 != "" {
			if !strings.Contains(addrStr, r.config.TransitNetwork.IPv6) {
				return fmt.Errorf("host tap %s missing IPv6 transit address (%s)", hostTap, r.config.TransitNetwork.IPv6)
			}
		}
	}

	fmt.Printf("Bridge %s is configured and ready\n", bridgeName)
	return nil
}

func (r *Runner) useTapDevice() (string, error) {
	tapName := r.config.TapDevice
	if tapName == "" {
		return "", fmt.Errorf("no tap device specified in configuration")
	}

	cmd := exec.Command("ip", "link", "show", tapName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("tap device '%s' not found - ensure bridge setup has been run\nCommand output: %s\nError: %v", tapName, string(output), err)
	}

	fmt.Printf("Using tap device %s on bridge %s\n", tapName, r.config.BridgeName)

	if r.config.SecondTapDevice != "" {
		cmd = exec.Command("ip", "link", "show", r.config.SecondTapDevice)
		output, err = cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("second tap device '%s' not found - ensure bridge setup has been run\nCommand output: %s\nError: %v", r.config.SecondTapDevice, string(output), err)
		}
		fmt.Printf("Using second tap device %s for host connectivity\n", r.config.SecondTapDevice)
	}

	fmt.Printf("VM will obtain IP from IPAM on transit network\n")

	return tapName, nil
}
