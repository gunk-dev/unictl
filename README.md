# unictl

Declarative reconciler + observability CLI for [UniFi Network](https://ui.com/) controllers (UDM Pro and similar). Folds an append-only event log into desired controller state and converges the controller toward it.

`unictl` is built to be useful to anyone running a UniFi Network controller. It is JSON-first and agent-friendly by default: every successful response is wrapped as `{"$schema": "unifi-v1", "data": ...}`, every failure is a structured error on stderr with stable exit codes. A single `unictl --help-all` call dumps the full command tree, flag list, exit-code matrix, and error envelope so an agent can learn the whole surface in one shot.

## What's in this PR (v0.1.0)

- `unictl sync` — folds an event log, plans block / unblock operations against the controller, and (with `--apply`) executes them.
- `unictl schema` — dumps the CUE schemas for events and derived state.
- `unictl version`, `unictl --help-all` — release identifiers + machine-readable discovery.
- Local API-key auth (`X-API-KEY`) for UniFi Network 8.x+.

Read commands (`list`, `get`) and ephemeral writes (`do kick`, `do reboot-port`) are intentionally out of scope here; they follow in later PRs.

## Quick start

### Install

Go:

```
go install github.com/gunk-dev/unictl/cmd/unictl@latest
```

Nix flake:

```
nix run github:gunk-dev/unictl -- version
nix profile install github:gunk-dev/unictl
```

A dev shell with Go, golangci-lint, and cue is available via `nix develop`.

### Mint a local API key

In the UniFi OS console (UDM, UDR, Cloud Key, UniFi OS Server, etc.):

1. Open **Settings → Control Plane → Integrations**.
2. Click **Create API Key**, name it, copy it.

Older UniFi Network versions surfaced this under **Settings → Admins & Users → API Keys**; either path lands at the same key. Local API keys require Network 8.x+.

### Set environment

```
export UNIFI_HOST="https://10.0.0.1"   # your controller URL
export UNIFI_API_KEY="...key..."
# Most home UDMs ship a self-signed cert; you'll likely need this:
export UNIFI_INSECURE=1                 # or pass --insecure
```

This is the same TLS posture you've already accepted for `kubectl` and `argocd` against self-signed clusters: trust the host you can reach on your LAN, opt out of verification, or pin the cert yourself.

### Dry-run a sync

```
unictl sync examples/events.cue --dry-run
```

Sample output:

```
{
  "$schema": "unifi-v1",
  "data": {
    "plan": [
      {
        "op": "block",
        "mac": "aa:bb:cc:dd:ee:01",
        "reason": "Unpatched IoT camera, isolate until firmware update lands",
        "before": "unblocked",
        "after": "blocked",
        "status": "planned"
      }
    ],
    "dry_run": true,
    "applied": false,
    "site": "default"
  }
}
```

Happy with the plan? Run it for real:

```
unictl sync examples/events.cue --apply
```

## Concepts

### Events

State changes are modeled as timestamped, declarative events:

```cue
events: [
  {at: "2026-01-01T00:00:00Z", type: "block",   mac: "aa:bb:cc:dd:ee:01", reason: "..."},
  {at: "2026-01-02T00:00:00Z", type: "unblock", mac: "aa:bb:cc:dd:ee:01"},
]
```

Events are append-only — they live in the log forever. The log is your audit trail by construction.

### Fold

To answer "what should the controller look like right now?", `unictl` sorts the log by `at`, replays it event-by-event, and produces a `DesiredState` — currently a list of MACs that should be blocked, with reasons and expiries. Expired blocks lift themselves without needing a counter-event.

### Converge

`unictl sync` diffs the folded desired state against the live controller and emits a plan. With `--apply`, the plan runs.

## CLI reference

| Command | Description |
| --- | --- |
| `unictl version` | Version + git SHA as JSON. |
| `unictl schema` | Dump the embedded CUE schemas. |
| `unictl --help-all` | Full command tree + flags + schemas + exit-code matrix as one JSON document. |
| `unictl sync <path>` | Fold the event log at `<path>` and emit a plan. Defaults to `--dry-run`; pass `--apply` to mutate the controller. |

Flags common to `sync`:

- `--apply` — execute the plan (default: dry-run).
- `--insecure` — skip TLS verification (or set `UNIFI_INSECURE=1`).
- `--site` — UniFi site short-name. Default `default`.

### Exit codes

| Code | Meaning |
| --- | --- |
| `0` | Success |
| `1` | User error (bad input, auth) |
| `2` | System error (network, controller unreachable) |
| `3` | Partial success (some sync ops succeeded, some failed) |

### Error envelope

Errors are JSON on stderr:

```
{
  "error": {
    "code": "NETWORK",
    "message": "unifi: controller unreachable: ...",
    "hint": "Check UNIFI_HOST reachability and TLS settings (most home UDMs need --insecure)"
  }
}
```

Codes: `AUTH`, `NETWORK`, `VALIDATION`, `CONTROLLER`, `INTERNAL`.

## Stability

- The `$schema` discriminator on every successful response is the contract. Today it's `"unifi-v1"`.
- New fields may be **appended** to existing payloads. They will not be reordered or silently renamed.
- New error codes may be **added**; existing codes will not be repurposed.
- Exit code meanings are stable.
- A bump from `unifi-v1` to `unifi-v2` will only happen for incompatible changes and will be called out in the release notes.

## Contributing

- `go test ./...` to run the test suite.
- `go build ./...` to verify the binary compiles.
- `cue vet ./schema/...` to validate the schemas.
- If you edit `schema/*.cue`, run `go generate ./internal/embedschema/...` to refresh the embedded copies.

## License

Apache License 2.0. See [LICENSE](./LICENSE) and [NOTICE](./NOTICE).
