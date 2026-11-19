package workspace

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
)

func hostShell(ctx context.Context, command string) (*exec.Cmd, string, error) {
	if runtime.GOOS == "windows" {
		for _, name := range []string{"pwsh.exe", "powershell.exe"} {
			path, err := exec.LookPath(name)
			if err == nil {
				return exec.CommandContext(ctx, path, "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", command), name, nil
			}
		}
		return nil, "", errors.New("host command mode requires PowerShell (pwsh.exe or powershell.exe) on PATH")
	}
	for _, name := range []string{"bash", "sh"} {
		path, err := exec.LookPath(name)
		if err == nil {
			return exec.CommandContext(ctx, path, "-lc", command), name, nil
		}
	}
	return nil, "", errors.New("host command mode requires bash or sh on PATH")
}

func hostSearchGuidance() string {
	if _, err := exec.LookPath("rg"); err == nil {
		return "Ripgrep (rg) is available on this host: use `rg --files` to list files and `rg -n` to search code. Keep paths workspace-relative; rg honors ignore rules by default."
	}
	if runtime.GOOS == "windows" {
		return "Ripgrep (rg) is not on PATH. Use PowerShell Get-ChildItem -Recurse -File for listing and Select-String for text search; Windows find.exe searches text and is not the Unix file-finding command."
	}
	findAvailable := "find is unavailable on PATH"
	if _, err := exec.LookPath("find"); err == nil {
		findAvailable = "find is available: use `find . -type f` for listing"
	}
	grepAvailable := "grep is unavailable on PATH"
	if _, err := exec.LookPath("grep"); err == nil {
		grepAvailable = "grep is available: use `grep -RIn` for text search"
	}
	return fmt.Sprintf("Ripgrep (rg) is not on PATH. %s; %s. Exclude dependency/build directories and keep paths workspace-relative.", findAvailable, grepAvailable)
}

func executeHostCommand(ctx context.Context, command, workingDirectory string, outputLimit int) (string, string, int, error) {
	process, shell, err := hostShell(ctx, command)
	if err != nil {
		return "", "", -1, err
	}
	process.Dir = workingDirectory
	output := &limitedCommandBuffer{limit: outputLimit}
	process.Stdout, process.Stderr = output, output
	err = process.Run()
	exitCode := 0
	if err != nil {
		exitCode = -1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
	}
	return output.String(), shell, exitCode, err
}
