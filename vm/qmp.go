package vm

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sync"
	"time"
)

// QMPClient communicates with QEMU via the QMP JSON protocol over a unix socket.
type QMPClient struct {
	conn   net.Conn
	reader *bufio.Reader
	mu     sync.Mutex
}

type qmpCommand struct {
	Execute   string      `json:"execute"`
	Arguments any `json:"arguments,omitempty"`
}

type qmpResponse struct {
	Return json.RawMessage `json:"return,omitempty"`
	Error  *qmpError       `json:"error,omitempty"`
}

type qmpError struct {
	Class string `json:"class"`
	Desc  string `json:"desc"`
}

func newQMPClient(socketPath string) (*QMPClient, error) {
	// Poll for socket availability (QEMU may take a moment to create it)
	var conn net.Conn
	var err error
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, statErr := os.Stat(socketPath); statErr == nil {
			conn, err = net.Dial("unix", socketPath)
			if err == nil {
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if conn == nil {
		if err != nil {
			return nil, fmt.Errorf("failed to connect to QMP socket %s: %w", socketPath, err)
		}
		return nil, fmt.Errorf("QMP socket %s not created after 10 seconds", socketPath)
	}

	reader := bufio.NewReader(conn)
	client := &QMPClient{conn: conn, reader: reader}

	// Read QMP greeting
	if _, err := reader.ReadBytes('\n'); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to read QMP greeting: %w", err)
	}

	// Send qmp_capabilities to enter command mode
	if _, err := client.execute("qmp_capabilities", nil); err != nil {
		conn.Close()
		return nil, fmt.Errorf("qmp_capabilities failed: %w", err)
	}

	return client, nil
}

func (q *QMPClient) execute(command string, arguments any) (json.RawMessage, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	cmd := qmpCommand{Execute: command, Arguments: arguments}
	data, err := json.Marshal(cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal QMP command: %w", err)
	}
	data = append(data, '\n')

	if _, err := q.conn.Write(data); err != nil {
		return nil, fmt.Errorf("failed to write QMP command: %w", err)
	}

	// Read response, skipping events
	for {
		line, err := q.reader.ReadBytes('\n')
		if err != nil {
			return nil, fmt.Errorf("failed to read QMP response: %w", err)
		}

		// Skip QMP events (they have an "event" field)
		var peek map[string]json.RawMessage
		if json.Unmarshal(line, &peek) == nil {
			if _, isEvent := peek["event"]; isEvent {
				continue
			}
		}

		var resp qmpResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			return nil, fmt.Errorf("failed to parse QMP response: %w", err)
		}
		if resp.Error != nil {
			return nil, fmt.Errorf("QMP error (%s): %s", resp.Error.Class, resp.Error.Desc)
		}
		return resp.Return, nil
	}
}

// SystemPowerdown sends an ACPI shutdown request.
func (q *QMPClient) SystemPowerdown() error {
	_, err := q.execute("system_powerdown", nil)
	return err
}

// Quit forces QEMU to exit immediately.
func (q *QMPClient) Quit() error {
	_, err := q.execute("quit", nil)
	return err
}

// Status returns the current VM run status (e.g., "running", "paused").
func (q *QMPClient) Status() (string, error) {
	raw, err := q.execute("query-status", nil)
	if err != nil {
		return "", err
	}
	var result struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("failed to parse status: %w", err)
	}
	return result.Status, nil
}

// Execute sends a raw QMP command and returns the result.
func (q *QMPClient) Execute(command string, arguments map[string]any) (json.RawMessage, error) {
	return q.execute(command, arguments)
}

// Close closes the QMP connection.
func (q *QMPClient) Close() error {
	return q.conn.Close()
}
