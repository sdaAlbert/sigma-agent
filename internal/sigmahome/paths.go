// Package sigmahome centralizes Sigma's working-directory state layout.
package sigmahome

import "path/filepath"

const DirectoryName = ".sigma"

func Path(root string, parts ...string) string {
	return filepath.Join(append([]string{root, DirectoryName}, parts...)...)
}

func Sessions(root string) string { return Path(root, "sessions") }
func Runs(root string) string     { return Path(root, "runs") }
func Server(root string) string   { return Path(root, "server") }
func Memory(root string) string   { return Path(root, "MEMORY.md") }
