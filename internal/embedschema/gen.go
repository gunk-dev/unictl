// Code generation hook for syncing the embedded copies of the top-level
// schema/*.cue files into this package.
//
// Run `go generate ./internal/embedschema/...` after editing schema/*.cue.
package embedschema

//go:generate cp ../../schema/events.cue events.cue
//go:generate cp ../../schema/state.cue state.cue
