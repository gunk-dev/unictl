package events

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"

	"github.com/gunk-dev/unictl/internal/embedschema"
)

// schemaInstance compiles the bundled events.cue schema into a reusable
// CUE value. Built once per call rather than at package init to keep
// initialization cost off binaries that don't touch the loader (e.g.
// `unictl version`).
func schemaInstance(ctx *cue.Context) (cue.Value, error) {
	v := ctx.CompileBytes(embedschema.EventsCUE, cue.Filename("events.cue"))
	if err := v.Err(); err != nil {
		return cue.Value{}, fmt.Errorf("compile embedded schema: %w", err)
	}
	return v, nil
}

// LoadPath reads an event log from `path`. If `path` is a directory, every
// `.cue` file in it is loaded and concatenated; if it is a file, that one
// file is loaded. Either form may export a single #Event or a top-level
// list (#EventLog).
func LoadPath(path string) ([]Event, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("events: stat %s: %w", path, err)
	}
	var files []string
	if info.IsDir() {
		err := fs.WalkDir(os.DirFS(path), ".", func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if strings.HasSuffix(p, ".cue") {
				files = append(files, filepath.Join(path, p))
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("events: walk %s: %w", path, err)
		}
		sort.Strings(files)
	} else {
		files = []string{path}
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("events: no .cue files found at %s", path)
	}

	ctx := cuecontext.New()
	schemaVal, err := schemaInstance(ctx)
	if err != nil {
		return nil, err
	}
	eventDef := schemaVal.LookupPath(cue.ParsePath("#Event"))
	if err := eventDef.Err(); err != nil {
		return nil, fmt.Errorf("events: lookup #Event: %w", err)
	}
	logDef := schemaVal.LookupPath(cue.ParsePath("#EventLog"))
	if err := logDef.Err(); err != nil {
		return nil, fmt.Errorf("events: lookup #EventLog: %w", err)
	}

	var out []Event
	for _, f := range files {
		evs, err := loadFile(ctx, eventDef, logDef, f)
		if err != nil {
			return nil, err
		}
		out = append(out, evs...)
	}
	return out, nil
}

func loadFile(ctx *cue.Context, eventDef, logDef cue.Value, path string) ([]Event, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("events: read %s: %w", path, err)
	}
	val := ctx.CompileBytes(data, cue.Filename(path))
	if err := val.Err(); err != nil {
		return nil, fmt.Errorf("events: compile %s: %w", path, err)
	}
	// Event-log files in this repo declare `package schema` to share types
	// with the canonical schema. When that's the case, the events live as a
	// top-level field (we accept either `events: [...]` or the file being a
	// bare list/event); pull the `events` field if it exists.
	if v := val.LookupPath(cue.ParsePath("events")); v.Exists() {
		val = v
	}

	// Try matching as a list (#EventLog) first, then as a single #Event.
	if err := val.Unify(logDef).Validate(cue.Concrete(true)); err == nil {
		return decodeList(val)
	}
	if err := val.Unify(eventDef).Validate(cue.Concrete(true)); err == nil {
		ev, err := decodeOne(val)
		if err != nil {
			return nil, fmt.Errorf("events: %s: %w", path, err)
		}
		return []Event{ev}, nil
	}
	// Neither shape validated — surface the list-shape error since lists
	// are the conventional form.
	return nil, fmt.Errorf("events: %s does not match #EventLog or #Event: %v", path, val.Unify(logDef).Validate())
}

func decodeList(val cue.Value) ([]Event, error) {
	it, err := val.List()
	if err != nil {
		return nil, fmt.Errorf("expected a list of events: %w", err)
	}
	var out []Event
	for it.Next() {
		ev, err := decodeOne(it.Value())
		if err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, nil
}

func decodeOne(v cue.Value) (Event, error) {
	var raw struct {
		At        string `json:"at"`
		Type      string `json:"type"`
		MAC       string `json:"mac"`
		Reason    string `json:"reason"`
		ExpiresAt string `json:"expires_at"`
	}
	if err := v.Decode(&raw); err != nil {
		return Event{}, fmt.Errorf("decode event: %w", err)
	}
	at, err := time.Parse(time.RFC3339, raw.At)
	if err != nil {
		return Event{}, fmt.Errorf("event %s: bad timestamp %q: %w", raw.Type, raw.At, err)
	}
	var exp time.Time
	if raw.ExpiresAt != "" {
		exp, err = time.Parse(time.RFC3339, raw.ExpiresAt)
		if err != nil {
			return Event{}, fmt.Errorf("event %s: bad expires_at %q: %w", raw.Type, raw.ExpiresAt, err)
		}
	}
	return Event{
		At:        at,
		Type:      raw.Type,
		MAC:       raw.MAC,
		Reason:    raw.Reason,
		ExpiresAt: exp,
	}, nil
}
