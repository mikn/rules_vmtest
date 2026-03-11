package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mikn/rules_qemu/vm"
)

func main() {
	var (
		kernel    = flag.String("kernel", "", "Path to kernel image")
		initrd    = flag.String("initrd", "", "Path to initrd")
		cmdline   = flag.String("cmdline", "", "Kernel command line")
		iso       = flag.String("iso", "", "Path to ISO image")
		ovmfCode  = flag.String("ovmf-code", "", "Path to OVMF CODE firmware")
		ovmfVars  = flag.String("ovmf-vars", "", "Path to OVMF VARS template")
		disk      = flag.String("disk", "", "Path to existing disk image")
		diskSize  = flag.String("disk-size", "", "Size for new disk (e.g., 10G)")
		memory    = flag.String("memory", "2G", "VM memory size")
		cpus      = flag.Int("cpus", 2, "Number of CPUs")
		tpm       = flag.Bool("tpm", false, "Enable TPM")
		swtpm     = flag.String("swtpm", "swtpm", "Path to swtpm binary")
		swtpmSetup = flag.String("swtpm-setup", "swtpm_setup", "Path to swtpm_setup binary")
		network     = flag.String("network", "user", "Network mode: user, none")
		timeout     = flag.Int("timeout", 10, "Timeout in minutes")
		shareDir    = flag.String("share-dir", "", "Path to 9P share directory")
		qemuBin     = flag.String("qemu", "", "Path to QEMU binary (defaults to qemu-system-aarch64 on ARM64, qemu-system-x86_64 otherwise)")
		qemuImg     = flag.String("qemu-img", "qemu-img", "Path to qemu-img binary")
		machineType = flag.String("machine-type", "", "QEMU machine type base (e.g., q35, virt). Combined with --accel.")
		accel       = flag.String("accel", "", "QEMU accelerator (e.g., kvm, hvf, tcg). Combined with --machine-type.")
		workDir     = flag.String("work-dir", "", "Work directory (temp if empty)")
	)
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*timeout)*time.Minute)
	defer cancel()

	// Handle signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	opts := []vm.Option{
		vm.WithMemory(*memory),
		vm.WithCPUs(*cpus),
		vm.WithQemuImg(*qemuImg),
		vm.WithName("vmtest"),
		vm.WithSerialCapture(),
	}
	if *qemuBin != "" {
		opts = append(opts, vm.WithQemuBinary(*qemuBin))
	}

	if *machineType != "" {
		opts = append(opts, vm.WithMachineType(*machineType))
	}
	if *accel != "" {
		opts = append(opts, vm.WithAccel(*accel))
	}

	if *workDir != "" {
		opts = append(opts, vm.WithWorkDir(*workDir))
	}

	// Boot mode
	if *kernel != "" {
		opts = append(opts, vm.WithKernelBoot(*kernel, *initrd, *cmdline))
	}
	if *ovmfCode != "" && *ovmfVars != "" {
		opts = append(opts, vm.WithUEFI(*ovmfCode, *ovmfVars))
	}
	if *iso != "" {
		opts = append(opts, vm.WithISO(*iso))
	}

	// Disk
	if *disk != "" {
		opts = append(opts, vm.WithExistingDisk(*disk))
	} else if *diskSize != "" {
		opts = append(opts, vm.WithDisk(*diskSize))
	}

	// TPM
	if *tpm {
		opts = append(opts, vm.WithTPM(*swtpm, *swtpmSetup))
	}

	// Network
	switch *network {
	case "none":
		opts = append(opts, vm.WithNoNetwork())
	default:
		opts = append(opts, vm.WithUserNet())
	}

	// 9P share
	if *shareDir != "" {
		opts = append(opts, vm.With9PShare("vmtest", *shareDir))
	}

	machine, err := vm.Start(ctx, opts...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error starting VM: %v\n", err)
		os.Exit(1)
	}
	defer machine.Kill()

	serial := machine.Serial()
	if serial == nil {
		fmt.Fprintf(os.Stderr, "Error: serial console not available\n")
		os.Exit(1)
	}

	// Scan serial output for pass/fail markers
	if err := serial.ScanForMarker(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("PASS")
}
