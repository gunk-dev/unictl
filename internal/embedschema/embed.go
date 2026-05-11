// Package embedschema embeds the top-level CUE schema files at build time
// so commands like `unictl schema` and the event-log loader can ship a
// single binary.
//
// The canonical source of truth is the repo's top-level schema/ directory;
// that's what CI's `cue vet` runs against. The files below are byte-for-byte
// copies maintained by `go generate ./internal/embedschema` (see gen.go).
package embedschema

import _ "embed"

//go:embed events.cue
var EventsCUE []byte

//go:embed state.cue
var StateCUE []byte

// Files is the set of CUE schema files keyed by basename, for tools that
// want to iterate (e.g. `unictl schema --raw`).
var Files = map[string][]byte{
	"events.cue": EventsCUE,
	"state.cue":  StateCUE,
}
