package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gunk-dev/unictl/internal/events"
	"github.com/gunk-dev/unictl/internal/output"
	"github.com/gunk-dev/unictl/internal/resources"
	"github.com/gunk-dev/unictl/internal/resourcesync"
	syncpkg "github.com/gunk-dev/unictl/internal/sync"
	"github.com/gunk-dev/unictl/internal/unifi"
)

// combinedPlan is the data payload emitted by `unictl sync`. Either or
// both sub-envelopes may be populated depending on what the input
// declared. Consumers should treat absent fields as "nothing to plan
// for that axis".
type combinedPlan struct {
	Events    *syncpkg.Plan      `json:"events,omitempty"`
	Resources *resourcesync.Plan `json:"resources,omitempty"`
}

func (p combinedPlan) hasFailures() bool {
	if p.Events != nil && p.Events.HasFailures() {
		return true
	}
	if p.Resources != nil && p.Resources.HasFailures() {
		return true
	}
	return false
}

func (p combinedPlan) hasSuccesses() bool {
	if p.Events != nil && p.Events.HasSuccesses() {
		return true
	}
	if p.Resources != nil && p.Resources.HasSuccesses() {
		return true
	}
	return false
}

func newSyncCmd() *cobra.Command {
	var (
		apply    bool
		insecure bool
		site     string
		prune    bool
	)
	cmd := &cobra.Command{
		Use:   "sync <path>",
		Short: "Plan and apply events + declarative resources against the controller",
		Long: "Loads events (events*.cue) and/or declarative resources (resources*.cue) " +
			"from the given file or directory, diffs them against the live controller, " +
			"and emits a structured plan.\n\n" +
			"Events fold into a desired blocked-clients snapshot. Resources " +
			"(networks/VLANs, WLANs, firewall rules, port forwards) are matched by name " +
			"and reconciled via create/update/delete ops.\n\n" +
			"Defaults to --dry-run. Pass --apply to mutate the controller. Pass --prune " +
			"to allow deletion of controller-side resources marked managed (`app=unictl`) " +
			"but absent from the declared config. Unmanaged resources are never deleted.\n\n" +
			"WLAN PSKs are referenced via a `password_env` field naming an env var. " +
			"Dry-runs render the var name as `@env:NAME`; --apply resolves and fails loud " +
			"if the var is unset.\n\n" +
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

			evLog, resCfg, err := classifyAndLoad(path)
			if err != nil {
				failf(output.CodeValidation, "Check the file syntax against `unictl schema`", "%v", err)
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

			var combined combinedPlan
			if len(evLog) > 0 {
				desired, err := events.Fold(evLog, time.Now().UTC())
				if err != nil {
					failf(output.CodeValidation, "Inspect the event log for unsupported event types", "%v", err)
				}
				plan, err := syncpkg.Build(c.Context(), client, desired)
				if err != nil {
					handleControllerErr(err)
				}
				combined.Events = &plan
			}
			if !resCfg.IsEmpty() {
				plan, err := resourcesync.Build(c.Context(), client, resCfg, prune)
				if err != nil {
					handleControllerErr(err)
				}
				combined.Resources = &plan
			}

			if combined.Events == nil && combined.Resources == nil {
				failf(output.CodeValidation,
					"Provide a file or directory containing events*.cue or resources*.cue",
					"no events or resources found at %s", path)
			}

			if !apply {
				if err := output.WriteJSON(os.Stdout, combined); err != nil {
					failf(output.CodeInternal, "", "write plan: %v", err)
				}
				return nil
			}

			if combined.Events != nil {
				syncpkg.Apply(c.Context(), client, combined.Events)
			}
			if combined.Resources != nil {
				resourcesync.Apply(c.Context(), client, combined.Resources)
			}
			if err := output.WriteJSON(os.Stdout, combined); err != nil {
				failf(output.CodeInternal, "", "write plan: %v", err)
			}
			if combined.hasFailures() {
				if combined.hasSuccesses() {
					os.Exit(output.ExitPartialSuccess)
				}
				os.Exit(output.ExitSystemError)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&apply, "apply", false, "Execute the plan against the controller (default: dry-run only)")
	cmd.Flags().BoolVar(&prune, "prune", false, "Delete controller resources marked managed (app=unictl) but absent from config")
	cmd.Flags().BoolVar(&insecure, "insecure", false, "Skip TLS verification (also honors UNIFI_INSECURE=1)")
	cmd.Flags().StringVar(&site, "site", "default", "UniFi site short-name (legacy API path component)")
	return cmd
}

// classifyAndLoad routes the user's path argument to the right loader.
// A directory is walked once and `.cue` files split by basename prefix
// (events*.cue → events loader, resources*.cue → resources loader).
// A single file is classified by basename first, then by content if
// the basename is ambiguous.
func classifyAndLoad(path string) ([]events.Event, resources.NetworkConfig, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, resources.NetworkConfig{}, fmt.Errorf("stat %s: %w", path, err)
	}
	if !info.IsDir() {
		return classifyAndLoadFile(path)
	}

	var eventFiles, resourceFiles []string
	err = fs.WalkDir(os.DirFS(path), ".", func(p string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if d.IsDir() {
			return nil
		}
		base := filepath.Base(p)
		if !strings.HasSuffix(base, ".cue") {
			return nil
		}
		full := filepath.Join(path, p)
		switch {
		case strings.HasPrefix(base, "events"):
			eventFiles = append(eventFiles, full)
		case strings.HasPrefix(base, "resources"):
			resourceFiles = append(resourceFiles, full)
		}
		return nil
	})
	if err != nil {
		return nil, resources.NetworkConfig{}, fmt.Errorf("walk %s: %w", path, err)
	}
	sort.Strings(eventFiles)
	sort.Strings(resourceFiles)

	var evLog []events.Event
	for _, f := range eventFiles {
		l, err := events.LoadPath(f)
		if err != nil {
			return nil, resources.NetworkConfig{}, err
		}
		evLog = append(evLog, l...)
	}
	var cfg resources.NetworkConfig
	for _, f := range resourceFiles {
		c, err := resources.Load(f)
		if err != nil {
			return nil, resources.NetworkConfig{}, err
		}
		cfg.Networks = append(cfg.Networks, c.Networks...)
		cfg.Wlans = append(cfg.Wlans, c.Wlans...)
		cfg.FirewallRules = append(cfg.FirewallRules, c.FirewallRules...)
		cfg.PortForwards = append(cfg.PortForwards, c.PortForwards...)
	}
	return evLog, cfg, nil
}

func classifyAndLoadFile(path string) ([]events.Event, resources.NetworkConfig, error) {
	base := filepath.Base(path)
	switch {
	case strings.HasPrefix(base, "events"):
		l, err := events.LoadPath(path)
		return l, resources.NetworkConfig{}, err
	case strings.HasPrefix(base, "resources"):
		c, err := resources.Load(path)
		return nil, c, err
	}
	// Ambiguous filename: try events first to preserve the PR 1 surface
	// for arbitrary filenames, then fall back to resources.
	if l, err := events.LoadPath(path); err == nil {
		return l, resources.NetworkConfig{}, nil
	}
	c, err := resources.Load(path)
	if err != nil {
		return nil, resources.NetworkConfig{}, fmt.Errorf("could not load %s as events or resources: %w", path, err)
	}
	return nil, c, nil
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

