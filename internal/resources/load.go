package resources

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"

	"github.com/gunk-dev/unictl/internal/embedschema"
)

// topLevelKeys is the set of CUE field names the loader projects out of
// each resources file. Derived once from the JSON tags on NetworkConfig
// so the Go struct is the single source of truth — adding a new resource
// type requires adding a struct field, not editing this loader.
var topLevelKeys = networkConfigJSONKeys()

func networkConfigJSONKeys() []string {
	t := reflect.TypeOf(NetworkConfig{})
	keys := make([]string, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		tag := t.Field(i).Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name := strings.SplitN(tag, ",", 2)[0]
		if name == "" {
			continue
		}
		keys = append(keys, name)
	}
	return keys
}

// schemaInstance compiles the bundled resources.cue schema into a
// reusable CUE value. Built lazily per Load call to keep startup cost
// off binaries that don't touch the loader.
func schemaInstance(ctx *cue.Context) (cue.Value, error) {
	v := ctx.CompileBytes(embedschema.ResourcesCUE, cue.Filename("resources.cue"))
	if err := v.Err(); err != nil {
		return cue.Value{}, fmt.Errorf("compile embedded schema: %w", err)
	}
	return v, nil
}

// Load reads a declarative resource config from `path`. If `path` is a
// directory, every `.cue` file whose basename matches `resources*.cue`
// is loaded and merged in lexical order. If `path` is a file, that one
// file is loaded.
//
// Files declare `package schema` to share types with the canonical
// schema; the loader pulls the top-level fields (`networks`, `wlans`,
// `firewall_rules`, `port_forwards`) from each and concatenates them.
//
// `password_env` is captured verbatim — env vars are NOT resolved here.
// The caller resolves them at apply time so dry-run plans can be
// produced without the secrets being set.
func Load(path string) (NetworkConfig, error) {
	files, err := discover(path)
	if err != nil {
		return NetworkConfig{}, err
	}
	if len(files) == 0 {
		return NetworkConfig{}, fmt.Errorf("resources: no resource .cue files found at %s", path)
	}

	ctx := cuecontext.New()
	schemaVal, err := schemaInstance(ctx)
	if err != nil {
		return NetworkConfig{}, err
	}
	configDef := schemaVal.LookupPath(cue.ParsePath("#NetworkConfig"))
	if err := configDef.Err(); err != nil {
		return NetworkConfig{}, fmt.Errorf("resources: lookup #NetworkConfig: %w", err)
	}

	var out NetworkConfig
	for _, f := range files {
		cfg, err := loadFile(ctx, configDef, f)
		if err != nil {
			return NetworkConfig{}, err
		}
		out.Networks = append(out.Networks, cfg.Networks...)
		out.Wlans = append(out.Wlans, cfg.Wlans...)
		out.FirewallRules = append(out.FirewallRules, cfg.FirewallRules...)
		out.PortForwards = append(out.PortForwards, cfg.PortForwards...)
	}
	return out, nil
}

// isResourceFile reports whether a `.cue` filename looks like a resource
// declaration. Used internally to classify mixed directories.
func isResourceFile(name string) bool {
	base := filepath.Base(name)
	if !strings.HasSuffix(base, ".cue") {
		return false
	}
	return strings.HasPrefix(base, "resources")
}

func discover(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("resources: stat %s: %w", path, err)
	}
	if !info.IsDir() {
		return []string{path}, nil
	}
	var files []string
	err = fs.WalkDir(os.DirFS(path), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if isResourceFile(p) {
			files = append(files, filepath.Join(path, p))
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("resources: walk %s: %w", path, err)
	}
	sort.Strings(files)
	return files, nil
}

func loadFile(ctx *cue.Context, configDef cue.Value, path string) (NetworkConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return NetworkConfig{}, fmt.Errorf("resources: read %s: %w", path, err)
	}
	val := ctx.CompileBytes(data, cue.Filename(path))
	if err := val.Err(); err != nil {
		return NetworkConfig{}, fmt.Errorf("resources: compile %s: %w", path, err)
	}

	// Files in this repo declare `package schema`, so the actual config
	// payload sits as top-level fields. Project just the resource fields
	// (derived from NetworkConfig's JSON tags) and unify against the
	// schema; this keeps unrelated package metadata from leaking into
	// validation while not hardcoding the field list in the loader.
	view := ctx.CompileString("{}")
	for _, name := range topLevelKeys {
		v := val.LookupPath(cue.ParsePath(name))
		if v.Exists() {
			view = view.FillPath(cue.ParsePath(name), v)
		}
	}

	unified := view.Unify(configDef)
	if err := unified.Validate(cue.Concrete(true)); err != nil {
		return NetworkConfig{}, fmt.Errorf("resources: %s does not match #NetworkConfig: %w", path, err)
	}

	var raw NetworkConfig
	if err := unified.Decode(&raw); err != nil {
		return NetworkConfig{}, fmt.Errorf("resources: decode %s: %w", path, err)
	}
	return raw, nil
}
