package vm

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"regexp"
	"sync"
)

// SerialConsole provides line-buffered access to the VM's serial output.
type SerialConsole struct {
	reader  io.ReadCloser
	lines   chan string
	logChan chan string // buffered channel for async OnLine callbacks
	done    chan struct{}
	mu      sync.Mutex
	closed  bool
}

func newSerialConsole(r io.ReadCloser) *SerialConsole {
	sc := &SerialConsole{
		reader:  r,
		lines:   make(chan string, 1024),
		logChan: make(chan string, 8192),
		done:    make(chan struct{}),
	}
	go sc.readLoop()
	return sc
}

func (sc *SerialConsole) readLoop() {
	defer close(sc.done)
	defer close(sc.lines)
	defer close(sc.logChan)

	buf := make([]byte, 4096)
	var pending []byte
	var lineBuf []byte

	for {
		n, err := sc.reader.Read(buf)
		if n > 0 {
			data := append(pending, buf[:n]...)
			pending = nil

			output, incomplete := ProcessEscapeSequences(data)
			pending = incomplete

			lineBuf = append(lineBuf, output...)
			for {
				idx := bytes.IndexByte(lineBuf, '\n')
				if idx < 0 {
					break
				}
				line := string(bytes.TrimRight(lineBuf[:idx], "\r"))
				lineBuf = lineBuf[idx+1:]
				// Non-blocking sends — dropping lines is better than
				// stalling the read pipeline (which blocks QEMU I/O).
				select {
				case sc.logChan <- line:
				default:
				}
				select {
				case sc.lines <- line:
				default:
				}
			}

			// Flush oversized buffer
			if len(lineBuf) > 4096 {
				select {
				case sc.lines <- string(lineBuf):
				default:
				}
				lineBuf = nil
			}
		}
		if err != nil {
			if len(pending) > 0 {
				lineBuf = append(lineBuf, pending...)
			}
			if len(lineBuf) > 0 {
				select {
				case sc.lines <- string(lineBuf):
				default:
				}
			}
			return
		}
	}
}

// ReadLine reads the next line from serial output.
func (sc *SerialConsole) ReadLine(ctx context.Context) (string, error) {
	select {
	case line, ok := <-sc.lines:
		if !ok {
			return "", fmt.Errorf("serial console closed")
		}
		return line, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// WaitFor blocks until a line matching the pattern appears.
// Returns the regex match groups.
func (sc *SerialConsole) WaitFor(ctx context.Context, pattern *regexp.Regexp) ([]string, error) {
	for {
		line, err := sc.ReadLine(ctx)
		if err != nil {
			return nil, err
		}
		if matches := pattern.FindStringSubmatch(line); matches != nil {
			return matches, nil
		}
	}
}

// OnLine sets a callback that is invoked for every serial line.
// The callback runs in a separate goroutine so slow callbacks (e.g., fmt.Printf
// blocked on pipe backpressure) don't stall the serial read pipeline.
// Must be called before serial output starts flowing or lines may be missed.
// Returns a channel that is closed when the goroutine exits (after the serial
// console is closed and all buffered lines are drained).
func (sc *SerialConsole) OnLine(fn func(string)) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		for line := range sc.logChan {
			fn(line)
		}
	}()
	return done
}

// Lines returns a channel of all serial output lines.
// WARNING: consuming from this channel competes with ReadLine/WaitFor.
// Prefer OnLine for logging while using WaitFor for pattern matching.
func (sc *SerialConsole) Lines() <-chan string {
	return sc.lines
}

// Close closes the serial reader.
func (sc *SerialConsole) Close() error {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if sc.closed {
		return nil
	}
	sc.closed = true
	return sc.reader.Close()
}

// Wait blocks until the readLoop goroutine exits, which happens after Close
// causes the underlying reader to return an error. After Wait returns, the
// logChan is closed and any OnLine goroutines will have drained.
func (sc *SerialConsole) Wait() {
	<-sc.done
}

// ProcessEscapeSequences strips ANSI escape sequences from serial data.
// Returns cleaned output and any incomplete escape sequence at the end.
func ProcessEscapeSequences(data []byte) (output []byte, incomplete []byte) {
	var result []byte
	i := 0

	for i < len(data) {
		if data[i] != '\x1b' {
			result = append(result, data[i])
			i++
			continue
		}

		if i+1 >= len(data) {
			return result, data[i:]
		}

		switch data[i+1] {
		case 'c', '7', '8':
			i += 2
		case '[':
			j := i + 2
			for j < len(data) && ((data[j] >= '0' && data[j] <= '9') || data[j] == ';' || data[j] == '?' || data[j] == ' ') {
				j++
			}
			if j >= len(data) {
				return result, data[i:]
			}
			// Strip screen-clearing and cursor-positioning sequences
			if data[j] == 'J' || data[j] == 'H' || data[j] == 'f' || data[j] == 's' || data[j] == 'u' {
				i = j + 1
			} else {
				result = append(result, data[i:j+1]...)
				i = j + 1
			}
		default:
			if i+1 < len(data) {
				result = append(result, data[i], data[i+1])
				i += 2
			} else {
				return result, data[i:]
			}
		}
	}

	return result, nil
}

// ScanForMarker reads serial output looking for VMTEST_PASS or VMTEST_FAIL markers.
// Returns nil on VMTEST_PASS, an error with the failure reason on VMTEST_FAIL,
// and an error on timeout or connection close.
func (sc *SerialConsole) ScanForMarker(ctx context.Context) error {
	passPattern := regexp.MustCompile(`VMTEST_PASS`)
	failPattern := regexp.MustCompile(`VMTEST_FAIL:\s*(.*)`)

	scanner := bufio.NewScanner(channelReader{ch: sc.lines, ctx: ctx})
	for scanner.Scan() {
		line := scanner.Text()
		if passPattern.MatchString(line) {
			return nil
		}
		if m := failPattern.FindStringSubmatch(line); m != nil {
			reason := "unknown"
			if len(m) > 1 {
				reason = m[1]
			}
			return fmt.Errorf("VM test failed: %s", reason)
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return fmt.Errorf("serial console closed without test result")
}

// channelReader adapts a string channel to io.Reader for use with bufio.Scanner.
type channelReader struct {
	ch  <-chan string
	ctx context.Context
	buf []byte
}

func (r channelReader) Read(p []byte) (int, error) {
	if len(r.buf) > 0 {
		n := copy(p, r.buf)
		r.buf = r.buf[n:]
		return n, nil
	}
	select {
	case line, ok := <-r.ch:
		if !ok {
			return 0, fmt.Errorf("channel closed")
		}
		data := []byte(line + "\n")
		n := copy(p, data)
		if n < len(data) {
			r.buf = data[n:]
		}
		return n, nil
	case <-r.ctx.Done():
		return 0, r.ctx.Err()
	}
}
