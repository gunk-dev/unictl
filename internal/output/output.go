// Package output centralizes how unictl writes JSON to stdout/stderr.
//
// Every successful response is wrapped in an envelope:
//
//	{"$schema": "unifi-v1", "data": <payload>}
//
// Every failure is a structured error written to stderr:
//
//	{"error": {"code": "...", "message": "...", "hint": "..."}}
//
// Exit codes follow a stable contract:
//
//	0 — success
//	1 — user error (bad input, auth)
//	2 — system error (network, controller unreachable)
//	3 — partial success (some sync ops succeeded, some failed)
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// SchemaVersion is the discriminator embedded in every successful response.
// Bump only when an incompatible change is made; appending fields does not
// require a bump.
const SchemaVersion = "unifi-v1"

// Error codes. Stable; only add, never rename.
const (
	CodeAuth       = "AUTH"
	CodeNetwork    = "NETWORK"
	CodeValidation = "VALIDATION"
	CodeController = "CONTROLLER"
	CodeInternal   = "INTERNAL"
)

// Exit codes.
const (
	ExitOK             = 0
	ExitUserError      = 1
	ExitSystemError    = 2
	ExitPartialSuccess = 3
)

// Envelope is the standard success wrapper.
type Envelope struct {
	Schema string `json:"$schema"`
	Data   any    `json:"data"`
}

// ErrorBody is the structured error written to stderr.
type ErrorBody struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail captures the user-actionable information about a failure.
type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
}

// WriteJSON writes a single JSON document wrapped in the envelope.
func WriteJSON(w io.Writer, data any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(Envelope{Schema: SchemaVersion, Data: data})
}

// WriteNDJSON writes one JSON object per line, each wrapped in the envelope.
// Use for stream-shaped commands.
func WriteNDJSON(w io.Writer, item any) error {
	enc := json.NewEncoder(w)
	return enc.Encode(Envelope{Schema: SchemaVersion, Data: item})
}

// WriteError writes a structured error to stderr.
func WriteError(w io.Writer, code, message, hint string) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(ErrorBody{Error: ErrorDetail{Code: code, Message: message, Hint: hint}})
}

// Fail writes a structured error to stderr and returns the exit code that
// best matches `code`. Callers exit with the returned value.
func Fail(code, message, hint string) int {
	WriteError(os.Stderr, code, message, hint)
	switch code {
	case CodeNetwork, CodeController:
		return ExitSystemError
	case CodeInternal:
		return ExitSystemError
	default:
		return ExitUserError
	}
}

// Failf is a convenience wrapper around Fail with fmt.Sprintf message.
func Failf(code, hint, format string, args ...any) int {
	return Fail(code, fmt.Sprintf(format, args...), hint)
}
