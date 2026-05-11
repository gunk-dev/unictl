package main

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/gunk-dev/unictl/internal/output"
	"github.com/gunk-dev/unictl/internal/version"
)

type versionInfo struct {
	Version string `json:"version"`
	GitSHA  string `json:"git_sha"`
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version + git SHA",
		RunE: func(c *cobra.Command, args []string) error {
			info := versionInfo{Version: version.Version, GitSHA: version.GitSHA}
			return output.WriteJSON(os.Stdout, info)
		},
	}
}
