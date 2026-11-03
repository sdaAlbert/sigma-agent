//go:build !windows

package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func readSecret(prompt string) (string, error) {
	modeCommand := exec.Command("stty", "-g")
	modeCommand.Stdin = os.Stdin
	mode, err := modeCommand.Output()
	if err != nil {
		return "", fmt.Errorf("hidden API-key input requires an interactive terminal: %w", err)
	}
	restore := exec.Command("stty", strings.TrimSpace(string(mode)))
	restore.Stdin = os.Stdin
	defer func() { _ = restore.Run() }()
	noEcho := exec.Command("stty", "-echo")
	noEcho.Stdin = os.Stdin
	if err := noEcho.Run(); err != nil {
		return "", fmt.Errorf("disable terminal echo for API-key input: %w", err)
	}
	if prompt != "" {
		fmt.Fprint(os.Stderr, prompt)
	}
	key, err := bufio.NewReader(os.Stdin).ReadString('\n')
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("read API key: %w", err)
	}
	return strings.TrimSpace(key), nil
}
