package workspace

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/sdaAlbert/sigma-agent/internal/sigmahome"
)

func localMemoryPath(root string) string {
	return sigmahome.Memory(root)
}

func existingLocalMemoryPath(root string) (string, error) {
	current := localMemoryPath(root)
	if _, err := os.Lstat(current); err == nil {
		return current, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	legacy := filepath.Join(root, ".sigma-agent", "MEMORY.md")
	if _, err := os.Lstat(legacy); err == nil {
		return legacy, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	return current, nil
}

func migrateLegacyMemory(root string) error {
	current := localMemoryPath(root)
	if _, err := os.Lstat(current); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	legacy := filepath.Join(root, ".sigma-agent", "MEMORY.md")
	info, err := os.Lstat(legacy)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() > maxMemoryBytes {
		return errors.New("legacy project memory must be a regular file within the configured size limit")
	}
	data, err := readProjectFile(root, legacy, maxMemoryBytes)
	if err != nil {
		return err
	}
	return writeProjectMemoryFile(root, data)
}
