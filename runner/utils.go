package runner

import (
	"os"

	"github.com/mikn/rules_qemu/vm"
)

func fileExists(path string) bool {
	return vm.FileExists(path)
}

func isNewer(src, dst string) bool {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return false
	}

	dstInfo, err := os.Stat(dst)
	if err != nil {
		return true
	}

	return srcInfo.ModTime().After(dstInfo.ModTime())
}

func copyFile(src, dst string) error {
	return vm.CopyFile(src, dst)
}

func copyDir(src, dst string) error {
	return vm.CopyDir(src, dst)
}
