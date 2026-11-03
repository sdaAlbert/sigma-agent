//go:build windows

package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func readSecret(prompt string) (string, error) {
	if prompt != "" {
		fmt.Fprintln(os.Stderr, prompt)
	}
	script := "$secure = Read-Host -Prompt 'API key (hidden)' -AsSecureString; " +
		"$pointer = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($secure); " +
		"try { [Console]::Out.Write([Runtime.InteropServices.Marshal]::PtrToStringBSTR($pointer)) } " +
		"finally { [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($pointer) }"
	command := exec.Command("powershell.exe", "-NoLogo", "-NoProfile", "-Command", script)
	command.Stdin = os.Stdin
	command.Stderr = os.Stderr
	var output bytes.Buffer
	command.Stdout = &output
	if err := command.Run(); err != nil {
		return "", fmt.Errorf("read hidden API key from the Windows console: %w", err)
	}
	return strings.TrimSpace(output.String()), nil
}
