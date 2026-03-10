package main

import (
	"fmt"
	"net/rpc"
	"net/rpc/jsonrpc"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	portName    = "org.vmtest.agent"
	udevPath    = "/dev/virtio-ports/" + portName
	sysfsGlob   = "/sys/class/virtio-ports/vport*"
)

// findDevice locates the virtio-serial char device for portName.
// Checks the udev symlink first, falls back to scanning sysfs.
func findDevice() (string, error) {
	// Fast path: udev-managed system
	if _, err := os.Stat(udevPath); err == nil {
		return udevPath, nil
	}

	// Slow path: scan sysfs for the named port (no udev)
	matches, _ := filepath.Glob(sysfsGlob)
	for _, dir := range matches {
		data, err := os.ReadFile(filepath.Join(dir, "name"))
		if err != nil {
			continue
		}
		if strings.TrimSpace(string(data)) == portName {
			dev := "/dev/" + filepath.Base(dir)
			if _, err := os.Stat(dev); err == nil {
				return dev, nil
			}
		}
	}
	return "", fmt.Errorf("virtio-serial port %q not found", portName)
}

func main() {
	var devPath string
	var err error

	for range 60 {
		devPath, err = findDevice()
		if err == nil {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "vmtest-agent: %v\n", err)
		os.Exit(1)
	}

	f, err := os.OpenFile(devPath, os.O_RDWR, 0)
	if err != nil {
		fmt.Fprintf(os.Stderr, "vmtest-agent: failed to open %s: %v\n", devPath, err)
		os.Exit(1)
	}
	defer f.Close()

	server := rpc.NewServer()
	if err := server.Register(&Agent{}); err != nil {
		fmt.Fprintf(os.Stderr, "vmtest-agent: failed to register agent: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "vmtest-agent: serving on %s\n", devPath)
	server.ServeCodec(jsonrpc.NewServerCodec(f))
}
