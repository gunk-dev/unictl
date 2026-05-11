package schema

// #BlockedClient is the per-MAC entry in the derived desired state.
#BlockedClient: {
	mac:         #MAC
	reason?:     string
	expires_at?: #Timestamp
}

// #DesiredState is the snapshot produced by folding an #EventLog at a
// particular time. It describes what the controller should look like, not
// what it currently does — the reconciler diffs this against the live
// controller state to produce a plan.
#DesiredState: {
	blocked: [...#BlockedClient]
}
