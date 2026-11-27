// Package version holds the build version of git-attributed.
//
// A release build injects the version string with linker flags:
//
//	-ldflags "-X github.com/amichne/attributed/internal/version.Version=v0.1.0"
//
// Builds without injection report dev.
package version

// Version is the git-attributed version. Release builds inject the validated
// git tag, with its v prefix; local and CI builds keep the dev default.
var Version = "dev"
