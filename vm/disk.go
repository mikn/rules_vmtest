package vm

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// CreateDisk creates a new raw disk image of the given size.
func CreateDisk(qemuImg, path, size string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create disk directory: %w", err)
	}
	cmd := exec.Command(qemuImg, "create", "-f", "raw", path, size)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("qemu-img create failed: %w\nOutput: %s", err, string(output))
	}
	return nil
}

// CopyFile copies a file from src to dst, creating parent directories.
func CopyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}

// CopyDir recursively copies a directory.
func CopyDir(src, dst string) error {
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}
	return exec.Command("cp", "-r", src+"/.", dst).Run()
}

// FileExists checks if a path exists and is a regular file.
func FileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
