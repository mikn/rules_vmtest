package vm

import (
	"context"
	"net"
	"net/rpc"
	"net/rpc/jsonrpc"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rules_vmtest/agentpb"
)

// MockAgent implements the RPC service for testing.
type MockAgent struct{}

func (m *MockAgent) Ready(_ *struct{}, _ *struct{}) error { return nil }

func (m *MockAgent) Exec(req *agentpb.ExecRequest, resp *agentpb.ExecResponse) error {
	resp.Stdout = "hello\n"
	resp.Stderr = ""
	resp.ExitCode = 0
	return nil
}

func (m *MockAgent) Stat(req *agentpb.FileRequest, resp *agentpb.FileResponse) error {
	if req.Path == "/etc/hostname" {
		resp.Exists = true
		resp.Size = 10
		resp.IsDir = false
	}
	return nil
}

func (m *MockAgent) WriteFile(req *agentpb.WriteFileRequest, resp *agentpb.ExecResponse) error {
	return nil
}

func (m *MockAgent) UnitStatus(req *agentpb.UnitRequest, resp *agentpb.UnitResponse) error {
	if req.Unit == "nginx.service" {
		resp.ActiveState = "active"
		resp.SubState = "running"
	} else {
		resp.ActiveState = "inactive"
		resp.SubState = "dead"
	}
	return nil
}

func (m *MockAgent) CheckPort(req *agentpb.PortRequest, resp *agentpb.PortResponse) error {
	if req.Port == 80 {
		resp.Open = true
	}
	return nil
}

func startMockServer(t *testing.T) string {
	t.Helper()
	sockDir := t.TempDir()
	sockPath := filepath.Join(sockDir, "agent.sock")

	server := rpc.NewServer()
	if err := server.RegisterName("Agent", &MockAgent{}); err != nil {
		t.Fatal(err)
	}

	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go server.ServeCodec(jsonrpc.NewServerCodec(conn))
		}
	}()

	return sockPath
}

func TestConnectAgentAndReady(t *testing.T) {
	sockPath := startMockServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	agent, err := ConnectAgent(ctx, sockPath)
	if err != nil {
		t.Fatalf("ConnectAgent failed: %v", err)
	}
	defer agent.Close()
}

func TestAgentExec(t *testing.T) {
	sockPath := startMockServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	agent, err := ConnectAgent(ctx, sockPath)
	if err != nil {
		t.Fatalf("ConnectAgent failed: %v", err)
	}
	defer agent.Close()

	resp, err := agent.Exec(ctx, "echo hello", 0)
	if err != nil {
		t.Fatalf("Exec failed: %v", err)
	}
	if resp.Stdout != "hello\n" {
		t.Errorf("expected stdout 'hello\\n', got %q", resp.Stdout)
	}
	if resp.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", resp.ExitCode)
	}
}

func TestAgentStat(t *testing.T) {
	sockPath := startMockServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	agent, err := ConnectAgent(ctx, sockPath)
	if err != nil {
		t.Fatalf("ConnectAgent failed: %v", err)
	}
	defer agent.Close()

	resp, err := agent.Stat("/etc/hostname")
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if !resp.Exists {
		t.Error("expected file to exist")
	}
	if resp.Size != 10 {
		t.Errorf("expected size 10, got %d", resp.Size)
	}
}

func TestAgentUnitStatus(t *testing.T) {
	sockPath := startMockServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	agent, err := ConnectAgent(ctx, sockPath)
	if err != nil {
		t.Fatalf("ConnectAgent failed: %v", err)
	}
	defer agent.Close()

	resp, err := agent.UnitStatus("nginx.service")
	if err != nil {
		t.Fatalf("UnitStatus failed: %v", err)
	}
	if resp.ActiveState != "active" {
		t.Errorf("expected ActiveState 'active', got %q", resp.ActiveState)
	}
	if resp.SubState != "running" {
		t.Errorf("expected SubState 'running', got %q", resp.SubState)
	}
}

func TestAgentCheckPort(t *testing.T) {
	sockPath := startMockServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	agent, err := ConnectAgent(ctx, sockPath)
	if err != nil {
		t.Fatalf("ConnectAgent failed: %v", err)
	}
	defer agent.Close()

	open, err := agent.CheckPort(80)
	if err != nil {
		t.Fatalf("CheckPort failed: %v", err)
	}
	if !open {
		t.Error("expected port 80 to be open")
	}

	open, err = agent.CheckPort(9999)
	if err != nil {
		t.Fatalf("CheckPort failed: %v", err)
	}
	if open {
		t.Error("expected port 9999 to be closed")
	}
}

func TestConnectAgentTimeout(t *testing.T) {
	// Use a socket path that doesn't exist — should timeout
	sockPath := filepath.Join(t.TempDir(), "nonexistent.sock")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := ConnectAgent(ctx, sockPath)
	if err == nil {
		t.Fatal("expected error for nonexistent socket")
	}
	if !strings.Contains(err.Error(), "context") {
		t.Errorf("expected context error, got: %v", err)
	}
}
