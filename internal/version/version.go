// Package version exposes the binary's release identifiers.
//
// GitSHA is injected at link time:
//
//	go build -ldflags "-X github.com/gunk-dev/unictl/internal/version.GitSHA=$(git rev-parse --short HEAD)"
//
// The Nix flake sets it via ldflags. Unset builds fall back to "dev".
package version

const Version = "0.1.0"

var GitSHA = "dev"
