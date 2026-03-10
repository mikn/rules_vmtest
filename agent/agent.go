package main

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/mikn/rules_vmtest/agentpb"
)

var (
	execShell     string
	execShellOnce sync.Once
)

// getShell returns the shell to use for command execution.
// Prefers bash, falls back to sh for minimal rootfs environments.
func getShell() string {
	execShellOnce.Do(func() {
		if _, err := exec.LookPath("bash"); err == nil {
			execShell = "bash"
		} else {
			execShell = "sh"
		}
	})
	return execShell
}

const defaultExecTimeout = 900 // seconds

// Agent implements the guest-side RPC methods.
type Agent struct{}

// Ready is a no-op handshake endpoint.
func (a *Agent) Ready(_ *struct{}, _ *struct{}) error { return nil }

// Exec runs a shell command and returns stdout, stderr, and exit code.
// For long-running or background processes, use Background instead.
func (a *Agent) Exec(req *agentpb.ExecRequest, resp *agentpb.ExecResponse) error {
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = defaultExecTimeout
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, getShell(), "-c", req.Command)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	resp.Stdout = stdout.String()
	resp.Stderr = stderr.String()

	if exitErr, ok := err.(*exec.ExitError); ok {
		resp.ExitCode = exitErr.ExitCode()
		return nil
	} else if err != nil {
		return fmt.Errorf("Exec: %w", err)
	}
	return nil
}

// Background starts a process as a transient systemd unit via systemd-run.
// The argv is passed directly — no shell evaluation.
func (a *Agent) Background(req *agentpb.BackgroundRequest, resp *agentpb.BackgroundResponse) error {
	if len(req.Argv) == 0 {
		return fmt.Errorf("Background: empty argv")
	}
	args := append([]string{"--no-block", "--collect", "--"}, req.Argv...)
	cmd := exec.Command("systemd-run", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("Background: systemd-run %v failed: %w\n%s", req.Argv, err, out)
	}
	// Parse unit name from output: "Running as unit: run-uXXXX.service"
	if s := strings.TrimPrefix(strings.TrimSpace(string(out)), "Running as unit: "); s != string(out) {
		resp.Unit = strings.TrimSuffix(s, "\n")
	}
	return nil
}

// Stat checks file existence and metadata.
func (a *Agent) Stat(req *agentpb.FileRequest, resp *agentpb.FileResponse) error {
	info, err := os.Stat(req.Path)
	if os.IsNotExist(err) {
		resp.Exists = false
		return nil
	}
	if err != nil {
		return fmt.Errorf("Stat: %w", err)
	}
	resp.Exists = true
	resp.Size = info.Size()
	resp.IsDir = info.IsDir()
	return nil
}

// WriteFile writes content to a file on the guest.
func (a *Agent) WriteFile(req *agentpb.WriteFileRequest, resp *agentpb.ExecResponse) error {
	mode := os.FileMode(req.Mode)
	if mode == 0 {
		mode = 0644
	}
	if err := os.WriteFile(req.Path, req.Content, mode); err != nil {
		return fmt.Errorf("WriteFile: %w", err)
	}
	return nil
}

// UnitStatus queries a systemd unit's ActiveState and SubState.
func (a *Agent) UnitStatus(req *agentpb.UnitRequest, resp *agentpb.UnitResponse) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "systemctl", "show", "-p", "ActiveState,SubState", "--value", req.Unit)
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("UnitStatus: systemctl show failed: %w", err)
	}

	lines := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)
	if len(lines) >= 1 {
		resp.ActiveState = strings.TrimSpace(lines[0])
	}
	if len(lines) >= 2 {
		resp.SubState = strings.TrimSpace(lines[1])
	}
	return nil
}

// Listen opens a TCP listener on the given port. The listener stays open
// until the agent exits. Useful for testing WaitForOpenPort.
func (a *Agent) Listen(req *agentpb.PortRequest, resp *agentpb.PortResponse) error {
	addr := resolveAddr(req.Address)
	ln, err := net.Listen("tcp", net.JoinHostPort(addr, fmt.Sprintf("%d", req.Port)))
	if err != nil {
		return fmt.Errorf("Listen: %w", err)
	}
	// Keep the listener open in the background; it'll close when the agent exits.
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()
	resp.Open = true
	return nil
}

// resolveAddr normalizes "localhost" to "127.0.0.1" to avoid DNS lookups
// in environments without a resolver.
func resolveAddr(addr string) string {
	if addr == "" || addr == "localhost" {
		return "127.0.0.1"
	}
	return addr
}

// CheckPort tests whether a TCP port is open.
func (a *Agent) CheckPort(req *agentpb.PortRequest, resp *agentpb.PortResponse) error {
	addr := resolveAddr(req.Address)
	target := net.JoinHostPort(addr, fmt.Sprintf("%d", req.Port))
	conn, err := net.DialTimeout("tcp", target, 1*time.Second)
	if err != nil {
		resp.Open = false
		return nil
	}
	conn.Close()
	resp.Open = true
	return nil
}
