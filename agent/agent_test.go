package main

import (
	"net"
	"net/rpc"
	"net/rpc/jsonrpc"
	"os"
	"path/filepath"
	"testing"

	"github.com/mikn/rules_vmtest/agentpb"
)

func setupTestAgent(t *testing.T) *rpc.Client {
	t.Helper()

	server := rpc.NewServer()
	if err := server.Register(&Agent{}); err != nil {
		t.Fatal(err)
	}

	// Use net.Pipe for in-process testing
	clientConn, serverConn := net.Pipe()
	t.Cleanup(func() {
		clientConn.Close()
		serverConn.Close()
	})

	go server.ServeCodec(jsonrpc.NewServerCodec(serverConn))

	return jsonrpc.NewClient(clientConn)
}

func TestAgentReady(t *testing.T) {
	client := setupTestAgent(t)
	var req, resp struct{}
	if err := client.Call("Agent.Ready", &req, &resp); err != nil {
		t.Fatalf("Ready failed: %v", err)
	}
}

func TestAgentExecSuccess(t *testing.T) {
	client := setupTestAgent(t)

	req := &agentpb.ExecRequest{Command: "echo hello"}
	resp := &agentpb.ExecResponse{}
	if err := client.Call("Agent.Exec", req, resp); err != nil {
		t.Fatalf("Exec failed: %v", err)
	}
	if resp.Stdout != "hello\n" {
		t.Errorf("expected stdout 'hello\\n', got %q", resp.Stdout)
	}
	if resp.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", resp.ExitCode)
	}
}

func TestAgentExecFailure(t *testing.T) {
	client := setupTestAgent(t)

	req := &agentpb.ExecRequest{Command: "exit 42"}
	resp := &agentpb.ExecResponse{}
	if err := client.Call("Agent.Exec", req, resp); err != nil {
		t.Fatalf("Exec failed: %v", err)
	}
	if resp.ExitCode != 42 {
		t.Errorf("expected exit code 42, got %d", resp.ExitCode)
	}
}

func TestAgentExecStderr(t *testing.T) {
	client := setupTestAgent(t)

	req := &agentpb.ExecRequest{Command: "echo error >&2"}
	resp := &agentpb.ExecResponse{}
	if err := client.Call("Agent.Exec", req, resp); err != nil {
		t.Fatalf("Exec failed: %v", err)
	}
	if resp.Stderr != "error\n" {
		t.Errorf("expected stderr 'error\\n', got %q", resp.Stderr)
	}
}

func TestAgentStatExists(t *testing.T) {
	client := setupTestAgent(t)

	// Create a temp file to stat
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "testfile")
	if err := os.WriteFile(tmpFile, []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	req := &agentpb.FileRequest{Path: tmpFile}
	resp := &agentpb.FileResponse{}
	if err := client.Call("Agent.Stat", req, resp); err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if !resp.Exists {
		t.Error("expected file to exist")
	}
	if resp.Size != 7 {
		t.Errorf("expected size 7, got %d", resp.Size)
	}
	if resp.IsDir {
		t.Error("expected not a directory")
	}
}

func TestAgentStatNotExists(t *testing.T) {
	client := setupTestAgent(t)

	req := &agentpb.FileRequest{Path: "/nonexistent/path/that/does/not/exist"}
	resp := &agentpb.FileResponse{}
	if err := client.Call("Agent.Stat", req, resp); err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if resp.Exists {
		t.Error("expected file not to exist")
	}
}

func TestAgentWriteFile(t *testing.T) {
	client := setupTestAgent(t)

	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "written")

	req := &agentpb.WriteFileRequest{
		Path:    target,
		Content: []byte("test content"),
		Mode:    0644,
	}
	resp := &agentpb.ExecResponse{}
	if err := client.Call("Agent.WriteFile", req, resp); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("failed to read written file: %v", err)
	}
	if string(data) != "test content" {
		t.Errorf("expected 'test content', got %q", string(data))
	}
}

func TestAgentCheckPortClosed(t *testing.T) {
	client := setupTestAgent(t)

	// Use a port that is almost certainly not listening
	req := &agentpb.PortRequest{Port: 19999, Address: "localhost"}
	resp := &agentpb.PortResponse{}
	if err := client.Call("Agent.CheckPort", req, resp); err != nil {
		t.Fatalf("CheckPort failed: %v", err)
	}
	if resp.Open {
		t.Error("expected port to be closed")
	}
}

func TestAgentCheckPortOpen(t *testing.T) {
	// Start a listener on a random port
	ln, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	port := ln.Addr().(*net.TCPAddr).Port

	client := setupTestAgent(t)
	req := &agentpb.PortRequest{Port: port, Address: "localhost"}
	resp := &agentpb.PortResponse{}
	if err := client.Call("Agent.CheckPort", req, resp); err != nil {
		t.Fatalf("CheckPort failed: %v", err)
	}
	if !resp.Open {
		t.Error("expected port to be open")
	}
}
