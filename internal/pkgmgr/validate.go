package pkgmgr

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ExtensionValidation describes static checks performed on a package candidate.
// Extension code is parsed by Node.js but never imported or executed.
type ExtensionValidation struct {
	PackageName       string
	PackageVersion    string
	CommandTools      []string
	JavaScriptModules []string
	TypeScriptModules []string
}

// ValidateExtensionPackage validates the manifest and JavaScript syntax for a
// local extension package candidate. It does not install the package, run
// command tools, import extension modules, or execute candidate code.
func ValidateExtensionPackage(ctx context.Context, packageRoot string) (ExtensionValidation, error) {
	var result ExtensionValidation
	absoluteRoot, err := filepath.Abs(packageRoot)
	if err != nil {
		return result, fmt.Errorf("resolve package candidate: %w", err)
	}
	info, err := os.Stat(absoluteRoot)
	if err != nil {
		return result, fmt.Errorf("read package candidate: %w", err)
	}
	if !info.IsDir() {
		return result, fmt.Errorf("package candidate must be a directory")
	}
	if _, err := os.Stat(filepath.Join(absoluteRoot, "package.json")); err != nil {
		return result, fmt.Errorf("package candidate must contain package.json: %w", err)
	}

	manifest, err := ReadManifest(absoluteRoot)
	if err != nil {
		return result, fmt.Errorf("validate package manifest: %w", err)
	}
	result.PackageName, result.PackageVersion, err = ReadPackageInfo(absoluteRoot)
	if err != nil {
		return result, fmt.Errorf("read package metadata: %w", err)
	}
	if result.PackageName == "" {
		result.PackageName = filepath.Base(absoluteRoot)
	}
	for _, tool := range manifest.CommandTools {
		result.CommandTools = append(result.CommandTools, tool.Name)
	}

	pack := &Package{Root: absoluteRoot, Manifest: manifest}
	modules, err := pack.ResourcePaths(ResourceExtensions)
	if err != nil {
		return result, fmt.Errorf("discover extension modules: %w", err)
	}
	var javascript []string
	for _, module := range modules {
		relative, relErr := filepath.Rel(absoluteRoot, module)
		if relErr != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
			return result, fmt.Errorf("extension module resolves outside the candidate package")
		}
		switch strings.ToLower(filepath.Ext(module)) {
		case ".js", ".mjs", ".cjs":
			javascript = append(javascript, module)
			result.JavaScriptModules = append(result.JavaScriptModules, displayPath(absoluteRoot, module))
		case ".ts", ".tsx":
			result.TypeScriptModules = append(result.TypeScriptModules, displayPath(absoluteRoot, module))
		}
	}
	if len(javascript) == 0 {
		return result, nil
	}
	node, err := exec.LookPath("node")
	if err != nil {
		return result, fmt.Errorf("Node.js is required to syntax-check JavaScript extension modules")
	}
	for _, module := range javascript {
		checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		command := exec.CommandContext(checkCtx, node, "--check", module)
		command.Dir = absoluteRoot
		output, checkErr := command.CombinedOutput()
		cancel()
		if checkErr != nil {
			message := strings.TrimSpace(string(output))
			if message == "" {
				message = checkErr.Error()
			}
			return result, fmt.Errorf("JavaScript syntax check failed for %s: %s", displayPath(absoluteRoot, module), message)
		}
	}
	return result, nil
}

func displayPath(root, file string) string {
	relative, err := filepath.Rel(root, file)
	if err != nil {
		return file
	}
	return filepath.ToSlash(relative)
}
