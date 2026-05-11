package main

import (
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/gunk-dev/unictl/internal/output"
)

type helpAllFlag struct {
	Name        string `json:"name"`
	Shorthand   string `json:"shorthand,omitempty"`
	Default     string `json:"default,omitempty"`
	Description string `json:"description"`
	Type        string `json:"type"`
}

type helpAllCommand struct {
	Name        string           `json:"name"`
	Path        string           `json:"path"`
	Short       string           `json:"short"`
	Long        string           `json:"long,omitempty"`
	Use         string           `json:"use"`
	Flags       []helpAllFlag    `json:"flags,omitempty"`
	Subcommands []helpAllCommand `json:"subcommands,omitempty"`
}

type helpAllPayload struct {
	Schema     string         `json:"schema_version"`
	OutputType string         `json:"output_type"`
	Errors     errorsSchema   `json:"errors"`
	ExitCodes  []exitCodeDoc  `json:"exit_codes"`
	Root       helpAllCommand `json:"root"`
}

type errorsSchema struct {
	Envelope string   `json:"envelope"`
	Codes    []string `json:"codes"`
}

type exitCodeDoc struct {
	Code        int    `json:"code"`
	Description string `json:"description"`
}

// runHelpAll dumps the entire command tree, flags, exit-code matrix, and
// error envelope in a single JSON document. The goal is one-shot
// discovery for agents: read this once, learn the surface.
func runHelpAll(c *cobra.Command) error {
	root := c.Root()
	payload := helpAllPayload{
		Schema:     output.SchemaVersion,
		OutputType: "json-envelope",
		Errors: errorsSchema{
			Envelope: `{"error": {"code": "...", "message": "...", "hint": "..."}}`,
			Codes: []string{
				output.CodeAuth,
				output.CodeNetwork,
				output.CodeValidation,
				output.CodeController,
				output.CodeInternal,
			},
		},
		ExitCodes: []exitCodeDoc{
			{Code: output.ExitOK, Description: "success"},
			{Code: output.ExitUserError, Description: "user error (bad input, auth)"},
			{Code: output.ExitSystemError, Description: "system error (network, controller unreachable)"},
			{Code: output.ExitPartialSuccess, Description: "partial success (some sync ops succeeded, some failed)"},
		},
		Root: describe(root),
	}
	return output.WriteJSON(os.Stdout, payload)
}

func describe(c *cobra.Command) helpAllCommand {
	out := helpAllCommand{
		Name:  c.Name(),
		Path:  c.CommandPath(),
		Short: c.Short,
		Long:  c.Long,
		Use:   c.Use,
	}
	c.LocalFlags().VisitAll(func(f *pflag.Flag) {
		out.Flags = append(out.Flags, helpAllFlag{
			Name:        f.Name,
			Shorthand:   f.Shorthand,
			Default:     f.DefValue,
			Description: f.Usage,
			Type:        f.Value.Type(),
		})
	})
	for _, sub := range c.Commands() {
		if sub.Hidden {
			continue
		}
		out.Subcommands = append(out.Subcommands, describe(sub))
	}
	return out
}
