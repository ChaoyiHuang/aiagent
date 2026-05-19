package openclaw

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// CopyDir copies a directory recursively, handling symlinks.
func CopyDir(src string, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}

	if !info.IsDir() {
		return CopyFile(src, dst)
	}

	if err := os.MkdirAll(dst, info.Mode()); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.Type()&os.ModeSymlink != 0 {
			log.Printf("Skipping symlink: %s", srcPath)
			continue
		}

		if entry.IsDir() {
			if err := CopyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err := CopyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}

	return nil
}

// CopyFile copies a single file.
func CopyFile(src string, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}

	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}

	if err := os.WriteFile(dst, data, info.Mode()); err != nil {
		return err
	}

	return nil
}

// RewriteRegistryPaths rewrites absolute paths in the OpenClaw plugin registry.
// The registry (installs.json) has paths like /openclaw-state/... that need to be
// rewritten to the actual runtime location.
func RewriteRegistryPaths(registryPath string, oldPrefix string, newPrefix string) error {
	if _, err := os.Stat(registryPath); err != nil {
		return fmt.Errorf("registry not found: %w", err)
	}

	data, err := os.ReadFile(registryPath)
	if err != nil {
		return fmt.Errorf("failed to read registry: %w", err)
	}

	content := string(data)
	newContent := content
	oldPrefixSlash := oldPrefix + "/"
	newPrefixSlash := newPrefix + "/"
	newContent = strings.ReplaceAll(newContent, oldPrefixSlash, newPrefixSlash)

	if strings.Contains(newContent, "\""+oldPrefix+"\"") {
		newContent = strings.ReplaceAll(newContent, "\""+oldPrefix+"\"", "\""+newPrefix+"\"")
	}

	if err := os.WriteFile(registryPath, []byte(newContent), 0644); err != nil {
		return fmt.Errorf("failed to write registry: %w", err)
	}

	return nil
}
