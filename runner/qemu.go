package runner

import (
	"fmt"
)

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

	args := []string{
		"-name", r.config.ServerName,
		"-machine", "q35,accel=kvm,smm=on",
		"-cpu", "host",
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
