package vm

import "fmt"

// buildArgs constructs the QEMU command-line arguments from the resolved config.
func buildArgs(c *Config) []string {
	args := []string{
		"-name", c.name,
		"-machine", c.machine,
		"-cpu", "host",
		"-m", c.memory,
		"-smp", fmt.Sprintf("%d", c.cpus),
		"-device", "virtio-rng-pci",
	}

	args = appendDebugConsoleArgs(args, c)
	args = appendBootOrderArgs(args, c)
	// Firmware and storage come before network so that OVMF discovers boot
	// devices on lower PCI addresses first, avoiding unnecessary PXE ROM
	// loading and TPM measurement overhead for NICs.
	args = appendFirmwareArgs(args, c)
	args = appendStorageArgs(args, c)
	args = appendTPMArgs(args, c)
	args = appendNetworkArgs(args, c)
	args = append9PArgs(args, c)
	args = appendAgentArgs(args, c)
	args = appendSerialArgs(args, c)
	args = appendQMPArgs(args, c)

	return args
}

func appendDebugConsoleArgs(args []string, c *Config) []string {
	if c.debugConsolePath == "" {
		return args
	}
	return append(args,
		"-debugcon", "file:"+c.debugConsolePath,
		"-global", "isa-debugcon.iobase=0x402",
	)
}

func appendBootOrderArgs(args []string, c *Config) []string {
	if c.bootOrder == "" {
		return args
	}
	return append(args, "-boot", "order="+c.bootOrder)
}

func appendNetworkArgs(args []string, c *Config) []string {
	switch c.networkMode {
	case networkUser:
		netdev := "user,id=net0"
		if c.userNetOptions != "" {
			netdev += "," + c.userNetOptions
		}
		args = append(args,
			"-netdev", netdev,
			"-device", primaryNetDevice(c),
		)
	case networkTap:
		netdev := fmt.Sprintf("tap,id=net0,ifname=%s,script=no,downscript=no", c.tapDevice)
		args = append(args,
			"-netdev", netdev,
			"-device", primaryNetDevice(c),
		)
		if c.secondTap != "" {
			netdev2 := fmt.Sprintf("tap,id=net1,ifname=%s,script=no,downscript=no", c.secondTap)
			args = append(args,
				"-netdev", netdev2,
				"-device", "virtio-net-pci,netdev=net1,addr=0x6",
			)
		}
	case networkNone:
		args = append(args, "-nic", "none")
	}
	return args
}

func primaryNetDevice(c *Config) string {
	dev := "virtio-net-pci,netdev=net0"
	if c.macAddress != "" {
		dev += fmt.Sprintf(",mac=%s", c.macAddress)
	}
	return dev
}

func appendTPMArgs(args []string, c *Config) []string {
	if c.tpmSocketPath == "" {
		return args
	}
	return append(args,
		"-chardev", fmt.Sprintf("socket,id=char0,path=%s", c.tpmSocketPath),
		"-tpmdev", "emulator,id=tpm0,chardev=char0",
		"-device", "tpm-tis,tpmdev=tpm0",
	)
}

func appendFirmwareArgs(args []string, c *Config) []string {
	// Direct kernel boot
	if c.kernel != "" {
		args = append(args, "-kernel", c.kernel)
		if c.initrd != "" {
			args = append(args, "-initrd", c.initrd)
		}
		if c.cmdline != "" {
			args = append(args, "-append", c.cmdline)
		}
		return args
	}

	// UEFI boot
	if c.ovmfCode != "" {
		// Only enable pflash secure mode when the machine has SMM (smm=on).
		// Secure pflash requires SMM — without it, nothing can write UEFI variables.
		if c.smmEnabled {
			args = append(args,
				"-global", "driver=cfi.pflash01,property=secure,value=on",
			)
		}
		args = append(args,
			"-drive", fmt.Sprintf("if=pflash,format=raw,unit=0,file=%s,readonly=on", c.ovmfCode),
		)
		if c.resolvedVarsPath != "" {
			args = append(args,
				"-drive", fmt.Sprintf("if=pflash,format=raw,unit=1,file=%s", c.resolvedVarsPath),
			)
		}
	}

	return args
}

func appendStorageArgs(args []string, c *Config) []string {
	diskPath := c.resolvedDiskPath
	if diskPath == "" {
		return args
	}

	bootFromISO := c.iso != ""
	if bootFromISO {
		args = append(args,
			"-device", "virtio-scsi-pci,id=scsi0,addr=0x4",
			"-drive", fmt.Sprintf("file=%s,format=raw,cache=none,media=cdrom,if=none,id=cd0", c.iso),
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

	return args
}

func append9PArgs(args []string, c *Config) []string {
	for i, s := range c.shares {
		id := fmt.Sprintf("shared%d", i)
		args = append(args,
			"-fsdev", fmt.Sprintf("local,id=%s,path=%s,security_model=mapped-xattr", id, s.hostPath),
			"-device", fmt.Sprintf("virtio-9p-pci,fsdev=%s,mount_tag=%s", id, s.tag),
		)
	}
	return args
}

func appendAgentArgs(args []string, c *Config) []string {
	if !c.enableAgent || c.agentSockPath == "" {
		return args
	}
	return append(args,
		"-device", "virtio-serial-pci,id=vmtest-vserial",
		"-chardev", fmt.Sprintf("socket,id=vmtest-agent,path=%s,server=on,wait=off", c.agentSockPath),
		"-device", "virtserialport,chardev=vmtest-agent,name=org.vmtest.agent",
	)
}

func appendSerialArgs(args []string, c *Config) []string {
	switch c.serialMode {
	case serialSocket:
		// Serial output goes to stdout, captured via cmd.StdoutPipe().
		// Use the simple -serial stdio form (matching the proven old runner)
		// rather than chardev indirection. Works across guest reboots since
		// the pipe stays connected to the QEMU process.
		args = append(args,
			"-serial", "stdio",
			"-display", "none",
		)
	case serialStdio:
		args = append(args, "-nographic")
	default:
		args = append(args, "-display", "none")
	}
	return args
}

func appendQMPArgs(args []string, c *Config) []string {
	if !c.enableQMP {
		return args
	}
	return append(args,
		"-qmp", fmt.Sprintf("unix:%s,server=on,wait=off", c.qmpSocketPath),
	)
}
