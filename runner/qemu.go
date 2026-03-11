package runner

import (
	"fmt"
	"runtime"
	"strings"
)

// defaultMachineType returns the machine string for the runner package.
// Includes smm=on for UEFI Secure Boot support (required by bulldozer).
// The vm package has its own defaults without smm=on — callers needing
// Secure Boot should use vm.WithMachine("q35,accel=kvm,smm=on") explicitly.
// Uses host-aware defaults for non-Bazel usage; Bazel rules set MachineType
// explicitly from the toolchain.
func defaultMachineType() string {
	switch runtime.GOOS {
	case "darwin":
		if runtime.GOARCH == "arm64" {
			return "virt,accel=hvf"
		}
		return "q35,accel=hvf"
	default: // linux
		if runtime.GOARCH == "arm64" {
			return "virt,accel=kvm"
		}
		return "q35,accel=kvm,smm=on"
	}
}

// buildQEMUArgs constructs the QEMU command arguments
func (r *Runner) buildQEMUArgs(ovmfCode, varsPath, diskPath, isoPath, tpmSocket string, bootFromISO bool, tapName, secondTapName string, directMode bool) []string {
	memory := r.config.Memory
	if memory == "" {
		memory = "6G"
	}

	cpus := r.config.CPUs
	if cpus == 0 {
		cpus = 2
	}

	machineType := r.config.MachineType
	if machineType == "" {
		machineType = defaultMachineType()
	}

	// Use "max" for TCG to enable all emulated CPU features; "host" is only
	// valid for hardware-assisted acceleration (KVM/HVF).
	cpuModel := "host"
	if strings.Contains(machineType, "accel=tcg") {
		cpuModel = "max"
	}

	args := []string{
		"-name", r.config.ServerName,
		"-machine", machineType,
		"-cpu", cpuModel,
		"-m", memory,
		"-smp", fmt.Sprintf("%d", cpus),
		"-device", "virtio-rng-pci",
	}

	// Network - use tap device connected to bridge
	netdevArg := fmt.Sprintf("tap,id=net0,ifname=%s,script=no,downscript=no", tapName)
	deviceArg := "virtio-net-pci,netdev=net0"
	if r.config.MacAddress != "" {
		deviceArg = fmt.Sprintf("virtio-net-pci,netdev=net0,mac=%s", r.config.MacAddress)
	}
	args = append(args,
		"-netdev", netdevArg,
		"-device", deviceArg,
	)

	// Add second network interface if specified
	if secondTapName != "" {
		netdevArg2 := fmt.Sprintf("tap,id=net1,ifname=%s,script=no,downscript=no", secondTapName)
		deviceArg2 := "virtio-net-pci,netdev=net1,addr=0x6"
		args = append(args,
			"-netdev", netdevArg2,
			"-device", deviceArg2,
		)
	}

	// TPM
	args = append(args,
		"-chardev", fmt.Sprintf("socket,id=char0,path=%s", tpmSocket),
		"-tpmdev", "emulator,id=tpm0,chardev=char0",
		"-device", "tpm-tis,tpmdev=tpm0",
	)

	// UEFI firmware
	args = append(args,
		"-global", "driver=cfi.pflash01,property=secure,value=on",
		"-drive", fmt.Sprintf("if=pflash,format=raw,unit=0,file=%s,readonly=on", ovmfCode),
		"-drive", fmt.Sprintf("if=pflash,format=raw,unit=1,file=%s", varsPath),
	)

	// Storage devices
	if bootFromISO && isoPath != "" {
		args = append(args,
			"-device", "virtio-scsi-pci,id=scsi0,addr=0x4",
			"-drive", fmt.Sprintf("file=%s,format=raw,cache=none,media=cdrom,if=none,id=cd0", isoPath),
			"-device", "scsi-cd,bus=scsi0.0,drive=cd0,bootindex=0",
			"-drive", fmt.Sprintf("file=%s,format=raw,if=none,id=disk", diskPath),
			"-device", "virtio-blk-pci,drive=disk,addr=0x5,bootindex=1",
		)
	} else {
		args = append(args,
			"-drive", fmt.Sprintf("file=%s,format=raw,if=none,id=disk", diskPath),
			"-device", "virtio-blk-pci,drive=disk,addr=0x5,bootindex=0",
		)
	}

	// Console output
	if directMode {
		args = append(args, "-nographic")
	} else {
		args = append(args, "-serial", "stdio", "-display", "none")
	}

	return args
}
