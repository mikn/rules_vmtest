package vm

import (
	"context"
	"fmt"
	"net"
	"net/rpc"
	"net/rpc/jsonrpc"
	"time"

	"github.com/mikn/rules_vmtest/agentpb"
)

// AgentConn is the host-side RPC client for the guest agent.
type AgentConn struct {
	client *rpc.Client
	conn   net.Conn
}

// ConnectAgent dials the virtio-serial unix socket and performs a Ready handshake.
// It retries until the guest agent is up or the context is cancelled.
func ConnectAgent(ctx context.Context, socketPath string) (*AgentConn, error) {
	var conn net.Conn
	var err error

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		conn, err = net.DialTimeout("unix", socketPath, 2*time.Second)
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("ConnectAgent: context cancelled while waiting for agent socket %s: %w", socketPath, ctx.Err())
		case <-ticker.C:
		}
	}

	client := jsonrpc.NewClient(conn)

	// Handshake: call Agent.Ready to verify the agent is responsive.
	var readyReq, readyResp struct{}
	if err := client.Call("Agent.Ready", &readyReq, &readyResp); err != nil {
		conn.Close()
		return nil, fmt.Errorf("ConnectAgent: agent handshake failed: %w", err)
	}

	return &AgentConn{client: client, conn: conn}, nil
}

// Exec runs a command on the guest and returns the result.
func (a *AgentConn) Exec(ctx context.Context, cmd string, timeout int) (*agentpb.ExecResponse, error) {
	req := &agentpb.ExecRequest{Command: cmd, Timeout: timeout}
	resp := &agentpb.ExecResponse{}
	call := a.client.Go("Agent.Exec", req, resp, nil)
	select {
	case <-call.Done:
		if call.Error != nil {
			return nil, fmt.Errorf("Agent.Exec: %w", call.Error)
		}
		return resp, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Background starts a process as a transient systemd unit.
// The argv is passed directly to systemd-run (no shell evaluation).
func (a *AgentConn) Background(argv ...string) (string, error) {
	req := &agentpb.BackgroundRequest{Argv: argv}
	resp := &agentpb.BackgroundResponse{}
	if err := a.client.Call("Agent.Background", req, resp); err != nil {
		return "", fmt.Errorf("Agent.Background: %w", err)
	}
	return resp.Unit, nil
}

// Stat checks file existence and metadata on the guest.
func (a *AgentConn) Stat(path string) (*agentpb.FileResponse, error) {
	req := &agentpb.FileRequest{Path: path}
	resp := &agentpb.FileResponse{}
	if err := a.client.Call("Agent.Stat", req, resp); err != nil {
		return nil, fmt.Errorf("Agent.Stat: %w", err)
	}
	return resp, nil
}

// WriteFile writes content to a file on the guest.
func (a *AgentConn) WriteFile(path string, content []byte, mode int) error {
	req := &agentpb.WriteFileRequest{Path: path, Content: content, Mode: mode}
	resp := &agentpb.ExecResponse{}
	if err := a.client.Call("Agent.WriteFile", req, resp); err != nil {
		return fmt.Errorf("Agent.WriteFile: %w", err)
	}
	return nil
}

// UnitStatus queries a systemd unit's state on the guest.
func (a *AgentConn) UnitStatus(unit string) (*agentpb.UnitResponse, error) {
	req := &agentpb.UnitRequest{Unit: unit}
	resp := &agentpb.UnitResponse{}
	if err := a.client.Call("Agent.UnitStatus", req, resp); err != nil {
		return nil, fmt.Errorf("Agent.UnitStatus: %w", err)
	}
	return resp, nil
}

// Listen opens a TCP listener on the given port inside the guest.
// The listener stays open until the agent exits.
func (a *AgentConn) Listen(port int) error {
	req := &agentpb.PortRequest{Port: port, Address: "localhost"}
	resp := &agentpb.PortResponse{}
	if err := a.client.Call("Agent.Listen", req, resp); err != nil {
		return fmt.Errorf("Agent.Listen: %w", err)
	}
	return nil
}

// CheckPort checks whether a TCP port is open on the guest.
func (a *AgentConn) CheckPort(port int) (bool, error) {
	req := &agentpb.PortRequest{Port: port, Address: "localhost"}
	resp := &agentpb.PortResponse{}
	if err := a.client.Call("Agent.CheckPort", req, resp); err != nil {
		return false, fmt.Errorf("Agent.CheckPort: %w", err)
	}
	return resp.Open, nil
}

// Close shuts down the RPC client and underlying connection.
func (a *AgentConn) Close() error {
	a.client.Close()
	return a.conn.Close()
}
