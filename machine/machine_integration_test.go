package machine

import (
	"context"
	"net"
	"net/rpc"
	"net/rpc/jsonrpc"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"rules_vmtest/agentpb"
	"rules_vmtest/vm"
)

// testAgent is a configurable mock agent for testing Machine methods
// against Go's testing framework without starting a real VM.
type testAgent struct {
	execFn      func(*agentpb.ExecRequest, *agentpb.ExecResponse) error
	statFn      func(*agentpb.FileRequest, *agentpb.FileResponse) error
	unitFn      func(*agentpb.UnitRequest, *agentpb.UnitResponse) error
	checkPortFn func(*agentpb.PortRequest, *agentpb.PortResponse) error
	listenFn    func(*agentpb.PortRequest, *agentpb.PortResponse) error
	bgFn        func(*agentpb.BackgroundRequest, *agentpb.BackgroundResponse) error
	writeFileFn func(*agentpb.WriteFileRequest, *agentpb.ExecResponse) error
}

func (a *testAgent) Ready(_ *struct{}, _ *struct{}) error { return nil }

func (a *testAgent) Exec(req *agentpb.ExecRequest, resp *agentpb.ExecResponse) error {
	if a.execFn != nil {
		return a.execFn(req, resp)
	}
	return nil
}

func (a *testAgent) Stat(req *agentpb.FileRequest, resp *agentpb.FileResponse) error {
	if a.statFn != nil {
		return a.statFn(req, resp)
	}
	return nil
}

func (a *testAgent) UnitStatus(req *agentpb.UnitRequest, resp *agentpb.UnitResponse) error {
	if a.unitFn != nil {
		return a.unitFn(req, resp)
	}
	resp.ActiveState = "active"
	resp.SubState = "running"
	return nil
}

func (a *testAgent) CheckPort(req *agentpb.PortRequest, resp *agentpb.PortResponse) error {
	if a.checkPortFn != nil {
		return a.checkPortFn(req, resp)
	}
	return nil
}

func (a *testAgent) Listen(req *agentpb.PortRequest, resp *agentpb.PortResponse) error {
	if a.listenFn != nil {
		return a.listenFn(req, resp)
	}
	resp.Open = true
	return nil
}

func (a *testAgent) Background(req *agentpb.BackgroundRequest, resp *agentpb.BackgroundResponse) error {
	if a.bgFn != nil {
		return a.bgFn(req, resp)
	}
	resp.Unit = "run-test.service"
	return nil
}

func (a *testAgent) WriteFile(req *agentpb.WriteFileRequest, resp *agentpb.ExecResponse) error {
	if a.writeFileFn != nil {
		return a.writeFileFn(req, resp)
	}
	return nil
}

// newTestMachine creates a Machine backed by a mock agent RPC server.
// No VM is started — only the agent RPC connection is active.
func newTestMachine(t testing.TB, agent *testAgent) *Machine {
	t.Helper()
	sockPath := filepath.Join(t.TempDir(), "agent.sock")

	server := rpc.NewServer()
	if err := server.RegisterName("Agent", agent); err != nil {
		t.Fatal(err)
	}

	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go server.ServeCodec(jsonrpc.NewServerCodec(conn))
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	agentConn, err := vm.ConnectAgent(ctx, sockPath)
	if err != nil {
		t.Fatal(err)
	}

	m := &Machine{agent: agentConn, t: t}
	t.Cleanup(func() { m.agent.Close() })
	return m
}

func TestMachineSucceed(t *testing.T) {
	mock := &testAgent{
		execFn: func(req *agentpb.ExecRequest, resp *agentpb.ExecResponse) error {
			resp.Stdout = "hello\n"
			resp.ExitCode = 0
			return nil
		},
	}
	m := newTestMachine(t, mock)

	out := m.Succeed(t, "echo hello")
	if out != "hello\n" {
		t.Fatalf("got %q, want %q", out, "hello\n")
	}
}

func TestMachineExecute(t *testing.T) {
	mock := &testAgent{
		execFn: func(req *agentpb.ExecRequest, resp *agentpb.ExecResponse) error {
			resp.Stdout = "out\n"
			resp.Stderr = "err\n"
			resp.ExitCode = 42
			return nil
		},
	}
	m := newTestMachine(t, mock)

	stdout, stderr, code, err := m.Execute("test-cmd")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if stdout != "out\n" {
		t.Errorf("stdout: got %q, want %q", stdout, "out\n")
	}
	if stderr != "err\n" {
		t.Errorf("stderr: got %q, want %q", stderr, "err\n")
	}
	if code != 42 {
		t.Errorf("exit code: got %d, want 42", code)
	}
}

func TestMachineFail(t *testing.T) {
	mock := &testAgent{
		execFn: func(req *agentpb.ExecRequest, resp *agentpb.ExecResponse) error {
			resp.ExitCode = 1
			return nil
		},
	}
	m := newTestMachine(t, mock)
	m.Fail(t, "false")
}

// TestMachineSubtests demonstrates that a single Machine works correctly
// across multiple Go subtests — the standard testing pattern for sharing
// expensive setup (VM boot) across related test cases.
func TestMachineSubtests(t *testing.T) {
	mock := &testAgent{
		execFn: func(req *agentpb.ExecRequest, resp *agentpb.ExecResponse) error {
			switch req.Command {
			case "echo hello":
				resp.Stdout = "hello\n"
			case "false":
				resp.ExitCode = 1
			case "uname -r":
				resp.Stdout = "6.1.0-test\n"
			}
			return nil
		},
		unitFn: func(req *agentpb.UnitRequest, resp *agentpb.UnitResponse) error {
			resp.ActiveState = "active"
			resp.SubState = "running"
			return nil
		},
		checkPortFn: func(req *agentpb.PortRequest, resp *agentpb.PortResponse) error {
			resp.Open = req.Port == 80
			return nil
		},
	}
	m := newTestMachine(t, mock)

	t.Run("Succeed", func(t *testing.T) {
		out := m.Succeed(t, "echo hello")
		if out != "hello\n" {
			t.Errorf("got %q, want %q", out, "hello\n")
		}
	})

	t.Run("Fail", func(t *testing.T) {
		m.Fail(t, "false")
	})

	t.Run("Execute", func(t *testing.T) {
		stdout, _, code, err := m.Execute("uname -r")
		if err != nil {
			t.Fatal(err)
		}
		if code != 0 {
			t.Fatalf("exit code %d", code)
		}
		if stdout != "6.1.0-test\n" {
			t.Errorf("got %q", stdout)
		}
	})

	t.Run("WaitForUnit", func(t *testing.T) {
		m.WaitForUnit(t, "nginx.service")
	})

	t.Run("WaitForOpenPort", func(t *testing.T) {
		m.WaitForOpenPort(t, 80)
	})

	t.Run("WaitForClosedPort", func(t *testing.T) {
		m.WaitForClosedPort(t, 9999)
	})
}

func TestMachineWaitUntilSucceeds(t *testing.T) {
	var calls atomic.Int32
	mock := &testAgent{
		execFn: func(req *agentpb.ExecRequest, resp *agentpb.ExecResponse) error {
			n := calls.Add(1)
			if n < 3 {
				resp.ExitCode = 1
				return nil
			}
			resp.Stdout = "ok\n"
			resp.ExitCode = 0
			return nil
		},
	}
	m := newTestMachine(t, mock)

	out := m.WaitUntilSucceeds(t, "check",
		WithRetryTimeout(10*time.Second),
		WithRetryInterval(50*time.Millisecond),
	)
	if out != "ok\n" {
		t.Fatalf("got %q, want %q", out, "ok\n")
	}
	if n := calls.Load(); n < 3 {
		t.Fatalf("expected at least 3 calls, got %d", n)
	}
}

func TestMachineWaitForUnit(t *testing.T) {
	var calls atomic.Int32
	mock := &testAgent{
		unitFn: func(req *agentpb.UnitRequest, resp *agentpb.UnitResponse) error {
			n := calls.Add(1)
			if n < 2 {
				resp.ActiveState = "activating"
				resp.SubState = "start"
				return nil
			}
			resp.ActiveState = "active"
			resp.SubState = "running"
			return nil
		},
	}
	m := newTestMachine(t, mock)
	m.WaitForUnit(t, "test.service",
		WithRetryTimeout(5*time.Second),
		WithRetryInterval(50*time.Millisecond),
	)
}

func TestMachineWaitForOpenPort(t *testing.T) {
	mock := &testAgent{
		checkPortFn: func(req *agentpb.PortRequest, resp *agentpb.PortResponse) error {
			resp.Open = req.Port == 8080
			return nil
		},
	}
	m := newTestMachine(t, mock)
	m.WaitForOpenPort(t, 8080)
}

func TestMachineWaitForClosedPort(t *testing.T) {
	mock := &testAgent{
		checkPortFn: func(req *agentpb.PortRequest, resp *agentpb.PortResponse) error {
			resp.Open = false
			return nil
		},
	}
	m := newTestMachine(t, mock)
	m.WaitForClosedPort(t, 9999)
}

func TestMachineWaitForFile(t *testing.T) {
	mock := &testAgent{
		statFn: func(req *agentpb.FileRequest, resp *agentpb.FileResponse) error {
			if req.Path == "/tmp/marker" {
				resp.Exists = true
				resp.Size = 0
			}
			return nil
		},
	}
	m := newTestMachine(t, mock)
	m.WaitForFile(t, "/tmp/marker")
}

func TestMachineBackground(t *testing.T) {
	var gotArgv []string
	mock := &testAgent{
		bgFn: func(req *agentpb.BackgroundRequest, resp *agentpb.BackgroundResponse) error {
			gotArgv = req.Argv
			resp.Unit = "run-test.service"
			return nil
		},
	}
	m := newTestMachine(t, mock)
	m.Background(t, "bash", "-c", "sleep 10")

	if len(gotArgv) != 3 || gotArgv[0] != "bash" || gotArgv[1] != "-c" || gotArgv[2] != "sleep 10" {
		t.Fatalf("expected argv [bash -c sleep 10], got %v", gotArgv)
	}
}

func TestMachineListen(t *testing.T) {
	var gotPort int
	mock := &testAgent{
		listenFn: func(req *agentpb.PortRequest, resp *agentpb.PortResponse) error {
			gotPort = req.Port
			resp.Open = true
			return nil
		},
	}
	m := newTestMachine(t, mock)
	m.Listen(t, 8080)

	if gotPort != 8080 {
		t.Fatalf("expected port 8080, got %d", gotPort)
	}
}

func TestMachineCopyFromHost(t *testing.T) {
	var writtenPath string
	var writtenContent []byte
	mock := &testAgent{
		writeFileFn: func(req *agentpb.WriteFileRequest, resp *agentpb.ExecResponse) error {
			writtenPath = req.Path
			writtenContent = req.Content
			return nil
		},
	}
	m := newTestMachine(t, mock)

	hostFile := filepath.Join(t.TempDir(), "test.txt")
	if err := os.WriteFile(hostFile, []byte("host data"), 0644); err != nil {
		t.Fatal(err)
	}

	m.CopyFromHost(t, hostFile, "/tmp/guest.txt")

	if writtenPath != "/tmp/guest.txt" {
		t.Errorf("path: got %q, want %q", writtenPath, "/tmp/guest.txt")
	}
	if string(writtenContent) != "host data" {
		t.Errorf("content: got %q, want %q", writtenContent, "host data")
	}
}
