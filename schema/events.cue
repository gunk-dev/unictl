// Package schema defines the declarative event types unictl folds into
// desired controller state.
//
// Events are append-only. Past events stay in the log forever; the actuator
// derives current desired state by folding events in timestamp order. New
// event variants (port forwards, VLAN membership, etc.) can be added to the
// #Event disjunction without breaking existing logs.
package schema

// #Timestamp is an RFC3339 timestamp. unictl rejects logs containing
// timestamps it cannot parse.
#Timestamp: string

// #MAC is a colon-separated lowercase MAC address (e.g. "aa:bb:cc:dd:ee:ff").
// The fold normalizes MACs before comparison; the schema only sketches the
// shape.
#MAC: =~"^[0-9a-fA-F]{2}(:[0-9a-fA-F]{2}){5}$"

// #BlockEvent declares that a client (by MAC) should be blocked from the
// network starting at `at`. If `expires_at` is set, the block automatically
// lifts after that timestamp without needing a counter-event.
#BlockEvent: {
	at:          #Timestamp
	type:        "block"
	mac:         #MAC
	reason?:     string
	expires_at?: #Timestamp
}

// #UnblockEvent declares that a previously-blocked client should be
// unblocked starting at `at`. Use this to lift an open-ended block early.
// Unblock events without a corresponding earlier block are valid and act as
// no-ops during the fold.
#UnblockEvent: {
	at:      #Timestamp
	type:    "unblock"
	mac:     #MAC
	reason?: string
}

// #Event is the discriminated union of all event types. New variants get
// appended here; the `type` field is the discriminator.
//
// Extension point: future PRs will add #PortForwardOpenEvent /
// #PortForwardCloseEvent etc. Keep variants `type`-discriminated and
// timestamped so the fold remains deterministic.
#Event: #BlockEvent | #UnblockEvent

// #EventLog is the top-level shape of a log file: a list of events. Files
// may also export a single #Event; the loader accepts both.
#EventLog: [...#Event]
