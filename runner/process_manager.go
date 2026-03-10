package runner

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/mikn/rules_vmtest/vm"
)

// ProcessManager handles cleanup of all spawned processes
type ProcessManager struct {
	processes []*exec.Cmd
	pidFiles  []string
	mu        sync.Mutex
	ctx       context.Context
	cancel    context.CancelFunc
}

func NewProcessManager(ctx context.Context) *ProcessManager {
	pmCtx, cancel := context.WithCancel(ctx)
	return &ProcessManager{
		processes: make([]*exec.Cmd, 0),
		pidFiles:  make([]string, 0),
		ctx:       pmCtx,
		cancel:    cancel,
	}
}

func (pm *ProcessManager) AddProcess(cmd *exec.Cmd) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.processes = append(pm.processes, cmd)
}

func (pm *ProcessManager) AddPIDFile(pidFile string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.pidFiles = append(pm.pidFiles, pidFile)
}

func (pm *ProcessManager) cleanupPIDFile(pidFile string) {
	if pidData, err := os.ReadFile(pidFile); err == nil {
		if pid, err := strconv.Atoi(strings.TrimSpace(string(pidData))); err == nil {
			syscall.Kill(pid, syscall.SIGTERM)
			time.Sleep(500 * time.Millisecond)
			syscall.Kill(pid, syscall.SIGKILL)
		}
	}
	os.Remove(pidFile)
}

func (pm *ProcessManager) CleanupAll() {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.cancel()

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

// StreamType defines whether a stream should be monitored or detached
type StreamType int

const (
	Monitored StreamType = iota
	Detached
)

// OutputStreamer manages multiple output streams
type OutputStreamer struct {
	monitored sync.WaitGroup
	detached  sync.WaitGroup
}

func NewOutputStreamer() *OutputStreamer {
	return &OutputStreamer{}
}

func (s *OutputStreamer) AttachTagged(reader io.Reader, tag string, streamType StreamType) {
	wg := &s.monitored
	if streamType == Detached {
		wg = &s.detached
	}

	wg.Add(1)
	go func() {
		defer wg.Done()

		buf := make([]byte, 4096)
		pending := []byte{}
		lineBuf := []byte{}

		for {
			n, err := reader.Read(buf)
			if n > 0 {
				data := append(pending, buf[:n]...)
				pending = nil

				output, incomplete := vm.ProcessEscapeSequences(data)
				pending = incomplete

				lineBuf = append(lineBuf, output...)

				for {
					idx := bytes.IndexByte(lineBuf, '\n')
					if idx < 0 {
						break
					}
					if idx > 0 {
						fmt.Printf("%s %s\n", tag, string(lineBuf[:idx]))
					}
					lineBuf = lineBuf[idx+1:]
				}

				if len(lineBuf) > 4096 {
					fmt.Printf("%s %s\n", tag, string(lineBuf))
					lineBuf = nil
				}
			}

			if err != nil {
				if len(pending) > 0 {
					lineBuf = append(lineBuf, pending...)
				}
				if len(lineBuf) > 0 {
					fmt.Printf("%s %s\n", tag, string(lineBuf))
				}
				break
			}
		}
	}()
}

func (s *OutputStreamer) AttachFilter(reader io.Reader, tag string, streamType StreamType, patterns ...string) {
	wg := &s.monitored
	if streamType == Detached {
		wg = &s.detached
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			line := scanner.Text()
			for _, pattern := range patterns {
				if strings.Contains(line, pattern) {
					fmt.Fprintln(os.Stderr, tag, line)
					break
				}
			}
		}
	}()
}

func (s *OutputStreamer) MatchSync(reader io.Reader, pattern *regexp.Regexp, callback func([]string)) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		if matches := pattern.FindStringSubmatch(scanner.Text()); len(matches) > 0 {
			callback(matches)
			return
		}
	}
}

func (s *OutputStreamer) Wait() {
	s.monitored.Wait()
}

func (s *OutputStreamer) WaitAll() {
	s.monitored.Wait()
	s.detached.Wait()
}
