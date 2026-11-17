// Package buildinfo provides version metadata embedded into release binaries.
package buildinfo

// Version defaults to dev for local builds. Release workflows replace it with
// the version derived from the Git tag using the Go linker's -X option.
var Version = "dev"
