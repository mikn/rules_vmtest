package vm

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
)

// Bridge represents a network bridge with pre-created TAP devices.
type Bridge struct {
	Name    string
	Subnet  string
	taps    []string
	nextTap int
	mu      sync.Mutex
}

// NewBridge validates that a bridge exists and discovers its TAP devices.
// On macOS, returns a vmnet-backed bridge with no TAP management.
func NewBridge(name string) (*Bridge, error) {
	if runtime.GOOS == "darwin" {
		return &Bridge{Name: name}, nil
	}

	if err := validateBridge(name); err != nil {
		return nil, fmt.Errorf("NewBridge: %w", err)
	}

	taps, err := discoverTaps(name)
	if err != nil {
		return nil, fmt.Errorf("NewBridge: %w", err)
	}
	if len(taps) == 0 {
		return nil, fmt.Errorf("NewBridge: bridge %q has no TAP devices attached", name)
	}

	return &Bridge{
		Name: name,
		taps: taps,
	}, nil
}

// AllocateTap returns the next available TAP device from the bridge's pool.
func (b *Bridge) AllocateTap() (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if runtime.GOOS == "darwin" {
		return "", fmt.Errorf("AllocateTap: macOS uses vmnet, not TAP devices")
	}

	if b.nextTap >= len(b.taps) {
		return "", fmt.Errorf("AllocateTap: no more TAP devices available on bridge %q (have %d)", b.Name, len(b.taps))
	}

	tap := b.taps[b.nextTap]
	b.nextTap++
	return tap, nil
}

// Release returns a TAP device to the pool. Currently a no-op since we use
// sequential allocation. Designed for future reuse support.
func (b *Bridge) Release(tap string) {
	// No-op for v1 — TAPs are allocated sequentially per test run
}

// discoverTaps finds TAP devices attached to the given bridge by reading sysfs.
func discoverTaps(bridge string) ([]string, error) {
	bridgeDir := filepath.Join("/sys/class/net", bridge, "brif")
	entries, err := os.ReadDir(bridgeDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read bridge interfaces at %s: %w", bridgeDir, err)
	}

	var taps []string
	for _, e := range entries {
		name := e.Name()
		// Only include devices that look like our TAP naming convention
		if strings.HasPrefix(name, "mltt-tap") {
			taps = append(taps, name)
		}
	}

	sort.Strings(taps)
	return taps, nil
}
