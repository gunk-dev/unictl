package main

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/gunk-dev/unictl/internal/embedschema"
	"github.com/gunk-dev/unictl/internal/output"
)

type schemaDump struct {
	Schema string            `json:"schema_version"`
	Files  map[string]string `json:"files"`
}

func newSchemaCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "schema",
		Short: "Dump the canonical CUE schemas for events and state",
		Long: "Prints the embedded CUE schema files keyed by basename. " +
			"Agents can consume this to validate event logs without bundling unictl.",
		RunE: func(c *cobra.Command, args []string) error {
			files := map[string]string{}
			for name, data := range embedschema.Files {
				files[name] = string(data)
			}
			return output.WriteJSON(os.Stdout, schemaDump{
				Schema: output.SchemaVersion,
				Files:  files,
			})
		},
	}
}
