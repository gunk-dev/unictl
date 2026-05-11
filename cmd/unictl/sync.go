package main

import (
	"errors"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/gunk-dev/unictl/internal/events"
	"github.com/gunk-dev/unictl/internal/output"
	syncpkg "github.com/gunk-dev/unictl/internal/sync"
	"github.com/gunk-dev/unictl/internal/unifi"
)

func newSyncCmd() *cobra.Command {
	var (
		apply    bool
		insecure bool
		site     string
	)
	cmd := &cobra.Command{
		Use:   "sync <event-log-path>",
		Short: "Fold an event log into desired state and converge the controller toward it",
		Long: "Loads events from the given file or directory, folds them into a desired " +
			"state snapshot, diffs that against the live controller, and emits a structured " +
			"plan.\n\n" +
			"Defaults to --dry-run. Pass --apply to mutate the controller.\n\n" +
			"Env:\n" +
			"  UNIFI_HOST       Controller base URL (e.g. https://10.0.0.1)\n" +
			"  UNIFI_API_KEY    Local API key (Settings → Control Plane → Integrations)\n" +
			"  UNIFI_INSECURE   Set to 1 to skip TLS verification (same as --insecure)",
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			path := args[0]

			host := os.Getenv("UNIFI_HOST")
			apiKey := os.Getenv("UNIFI_API_KEY")
			if host == "" {
				fail(output.CodeValidation,
					"UNIFI_HOST is not set",
					"Set UNIFI_HOST to your controller URL (e.g. https://10.0.0.1)")
			}
			if apiKey == "" {
				fail(output.CodeAuth,
					"UNIFI_API_KEY is not set",
					"Mint a local API key in the controller UI under Settings → Control Plane → Integrations, then export UNIFI_API_KEY")
			}
			if os.Getenv("UNIFI_INSECURE") == "1" {
				insecure = true
			}

			log, err := events.LoadPath(path)
			if err != nil {
				failf(output.CodeValidation, "Check the file syntax against `unictl schema`", "%v", err)
			}
			desired, err := events.Fold(log, time.Now().UTC())
			if err != nil {
				failf(output.CodeValidation, "Inspect the event log for unsupported event types", "%v", err)
			}

			client, err := unifi.New(unifi.Config{
				Host:     host,
				APIKey:   apiKey,
				Site:     site,
				Insecure: insecure,
			})
			if err != nil {
				failf(output.CodeValidation, "", "%v", err)
			}

			plan, err := syncpkg.Build(c.Context(), client, desired)
			if err != nil {
				handleControllerErr(err)
			}

			if !apply {
				if err := output.WriteJSON(os.Stdout, plan); err != nil {
					failf(output.CodeInternal, "", "write plan: %v", err)
				}
				return nil
			}

			syncpkg.Apply(c.Context(), client, &plan)
			if err := output.WriteJSON(os.Stdout, plan); err != nil {
				failf(output.CodeInternal, "", "write plan: %v", err)
			}
			if plan.HasFailures() {
				if plan.HasSuccesses() {
					os.Exit(output.ExitPartialSuccess)
				}
				os.Exit(output.ExitSystemError)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&apply, "apply", false, "Execute the plan against the controller (default: dry-run only)")
	cmd.Flags().BoolVar(&insecure, "insecure", false, "Skip TLS verification (also honors UNIFI_INSECURE=1)")
	cmd.Flags().StringVar(&site, "site", "default", "UniFi site short-name (legacy API path component)")
	return cmd
}

func handleControllerErr(err error) {
	switch {
	case errors.Is(err, unifi.ErrAuth):
		failf(output.CodeAuth,
			"Verify UNIFI_API_KEY is correct and not expired",
			"%v", err)
	case errors.Is(err, unifi.ErrNetwork):
		failf(output.CodeNetwork,
			"Check UNIFI_HOST reachability and TLS settings (most home UDMs need --insecure)",
			"%v", err)
	case errors.Is(err, unifi.ErrController):
		failf(output.CodeController,
			"The controller rejected the request; inspect the message for details",
			"%v", err)
	default:
		failf(output.CodeInternal, "", "%v", err)
	}
}
