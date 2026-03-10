package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/bazelbuild/rules_go/go/runfiles"
	"rules_vmtest/runner"
)

// Build-time variables set via x_defs
var (
	// VM identity
	vmServer    = ""
	vmTap       = ""
	vmSecondTap = ""
	vmMac       = ""

	// VM resources
	vmMemory   = ""
	vmCPUs     = ""
	vmDiskSize = ""

	// Mode: "build" or "run"
	vmMode = ""

	// TPM pristine state path (workspace-relative)
	vmTPMPristinePath = ""

	// Build mode only - runfile paths
	vmISORunfile = ""

	// Tool paths (set via x_defs, empty = use system PATH)
	vmOVMFCode   = "" // Runfile path to OVMF CODE
	vmOVMFVars   = "" // Runfile path to OVMF VARS
	vmSwtpm      = "" // Runfile path to swtpm binary
	vmSwtpmSetup = "" // Runfile path to swtpm_setup binary

	// Network
	vmBridgeName    = "" // Bridge name (default: "mltt-br0")
	vmTransitIPv4   = "" // Expected IPv4 address on host tap device (e.g., "192.168.177.1/28")
	vmTransitIPv6   = "" // Expected IPv6 address on host tap device (e.g., "fd00:6000:100::1/64")
)

func main() {
	if vmMode == "" {
		fmt.Fprintf(os.Stderr, "Error: vmMode not set via x_defs\n")
		os.Exit(1)
	}

	r, err := runfiles.New()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to initialize runfiles: %v\n", err)
		os.Exit(1)
	}

	if vmServer == "" {
		fmt.Fprintf(os.Stderr, "Error: vmServer not set via x_defs\n")
		os.Exit(1)
	}
	if vmTap == "" {
		fmt.Fprintf(os.Stderr, "Error: vmTap not set via x_defs\n")
		os.Exit(1)
	}
	if vmMac == "" {
		fmt.Fprintf(os.Stderr, "Error: vmMac not set via x_defs\n")
		os.Exit(1)
	}
	if vmMemory == "" {
		fmt.Fprintf(os.Stderr, "Error: vmMemory not set via x_defs\n")
		os.Exit(1)
	}
	if vmCPUs == "" {
		fmt.Fprintf(os.Stderr, "Error: vmCPUs not set via x_defs\n")
		os.Exit(1)
	}
	if vmDiskSize == "" {
		fmt.Fprintf(os.Stderr, "Error: vmDiskSize not set via x_defs\n")
		os.Exit(1)
	}

	cpus, err := strconv.Atoi(vmCPUs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid vmCPUs value %q: %v\n", vmCPUs, err)
		os.Exit(1)
	}

	workspace := os.Getenv("BUILD_WORKSPACE_DIRECTORY")
	if workspace == "" {
		fmt.Fprintf(os.Stderr, "Error: BUILD_WORKSPACE_DIRECTORY not set\n")
		os.Exit(1)
	}

	// Build tool paths
	toolPaths := runner.DefaultToolPaths()

	// Resolve OVMF paths from runfiles
	if vmOVMFCode != "" {
		ovmfCode, err := r.Rlocation(vmOVMFCode)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to resolve OVMF CODE from runfiles %q: %v\n", vmOVMFCode, err)
			os.Exit(1)
		}
		toolPaths.OVMFCode = ovmfCode
	}
	if vmOVMFVars != "" {
		ovmfVars, err := r.Rlocation(vmOVMFVars)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to resolve OVMF VARS from runfiles %q: %v\n", vmOVMFVars, err)
			os.Exit(1)
		}
		toolPaths.OVMFVars = ovmfVars
	}
	if vmSwtpm != "" {
		swtpm, err := r.Rlocation(vmSwtpm)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to resolve swtpm from runfiles %q: %v\n", vmSwtpm, err)
			os.Exit(1)
		}
		toolPaths.Swtpm = swtpm
	}
	if vmSwtpmSetup != "" {
		swtpmSetup, err := r.Rlocation(vmSwtpmSetup)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to resolve swtpm_setup from runfiles %q: %v\n", vmSwtpmSetup, err)
			os.Exit(1)
		}
		toolPaths.SwtpmSetup = swtpmSetup
	}

	var config runner.RunnerConfig

	config.ServerName = vmServer
	config.Mode = vmMode
	config.TapDevice = vmTap
	config.SecondTapDevice = vmSecondTap
	config.MacAddress = vmMac
	config.Memory = vmMemory
	config.CPUs = cpus
	config.DiskSize = vmDiskSize
	config.ToolPaths = toolPaths
	config.BridgeName = vmBridgeName
	config.TransitNetwork = runner.TransitNetwork{
		IPv4: vmTransitIPv4,
		IPv6: vmTransitIPv6,
	}

	config.WorkDir = filepath.Join(workspace, ".vms", vmServer)

	if vmTPMPristinePath != "" {
		config.TPMPristinePath = filepath.Join(workspace, vmTPMPristinePath)
	}

	// For build mode, resolve ISO from runfiles
	if vmMode == "build" && vmISORunfile != "" {
		isoPath, err := r.Rlocation(vmISORunfile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to resolve ISO from runfiles %q: %v\n", vmISORunfile, err)
			os.Exit(1)
		}
		config.ISOPath = isoPath
	}

	flag.StringVar(&config.OutputMode, "output", "direct", "Output mode: direct or tagged")
	flag.BoolVar(&config.ShutdownMode, "shutdown-mode", false, "Shutdown instead of reboot after bootstrap")
	flag.BoolVar(&config.NoCleanup, "no-cleanup", false, "Don't cleanup on exit")
	flag.Parse()

	rnr := runner.NewRunner(&config)

	if config.Mode == "run" && config.OutputMode == "direct" {
		if err := rnr.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	} else {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

		done := make(chan error, 1)
		go func() {
			done <- rnr.Run()
		}()

		select {
		case err := <-done:
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
		case sig := <-sigChan:
			fmt.Printf("\nReceived signal %v, shutting down...\n", sig)
			rnr.Cancel()
			<-done
		}
	}
}
