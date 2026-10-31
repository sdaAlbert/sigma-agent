//go:build !windows

package fsx

import "os"

func ReplaceFile(source, destination string) error {
	return os.Rename(source, destination)
}
