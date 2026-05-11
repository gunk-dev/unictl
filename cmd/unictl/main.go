// Command unictl is the declarative reconciler + observability CLI for
// UniFi Network controllers.
//
// CLI surface is JSON-first by design: every successful response is a
// `{"$schema": "unifi-v1", "data": ...}` envelope, every failure is a
// structured `{"error": {"code": ..., "message": ..., "hint": ...}}`
// document written to stderr.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/gunk-dev/unictl/internal/output"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	root := newRootCmd()
	if err := root.ExecuteContext(ctx); err != nil {
		// Cobra-level errors (bad flag, etc.) are user errors. Subcommands
		// that need to map errors to the structured exit-code matrix
		// should call os.Exit themselves.
		output.WriteError(os.Stderr, output.CodeValidation, err.Error(), "")
		os.Exit(output.ExitUserError)
	}
}

func newRootCmd() *cobra.Command {
	var helpAll bool
	cmd := &cobra.Command{
		Use:   "unictl",
		Short: "Declarative reconciler + observability CLI for UniFi Network controllers",
		Long: "unictl folds an append-only event log into desired controller state " +
			"and converges a UniFi Network controller toward it.\n\n" +
			"Default output is JSON wrapped in a {\"$schema\": \"unifi-v1\", \"data\": ...} envelope. " +
			"Pass --human for pretty output.",
		SilenceUsage:  true,
		SilenceErrors: true,
		// When --help-all is set, intercept before any subcommand runs.
		RunE: func(c *cobra.Command, args []string) error {
			if helpAll {
				return runHelpAll(c)
			}
			return c.Help()
		},
	}
	cmd.PersistentFlags().Bool("human", false, "Render output for humans instead of JSON")
	cmd.Flags().BoolVar(&helpAll, "help-all", false, "Print the full command tree + flags + schemas as a single JSON document")
	cmd.AddCommand(
		newVersionCmd(),
		newSchemaCmd(),
		newSyncCmd(),
	)
	return cmd
}

// fail writes a structured error and exits with the appropriate code.
// Subcommands invoke this in place of returning errors so we keep tight
// control over the exit-code contract.
func fail(code, message, hint string) {
	os.Exit(output.Fail(code, message, hint))
}

func failf(code, hint, format string, args ...any) {
	fail(code, fmt.Sprintf(format, args...), hint)
}
