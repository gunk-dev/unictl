// Package resources loads declarative resource configurations (networks,
// WLANs, firewall rules, port forwards) from CUE files.
//
// The loader mirrors internal/events: a single Load entry point that
// accepts either a file or a directory, validates against the embedded
// schema, and returns typed Go structs the sync engine can plan against.
//
// Resources are matched by user-stable `name`, never by controller
// internal IDs. The reconciler maintains an `app=unictl` marker on
// objects it creates so pruning can distinguish managed from unmanaged.
//
// Secrets: WLAN PSKs are referenced via `password_env` (an env-var name).
// The loader does NOT resolve env vars — that happens at apply time so
// dry-runs don't require secrets to be set.
package resources

// Network mirrors schema/#Network.
type Network struct {
	Name         string     `json:"name"`
	Purpose      string     `json:"purpose"`
	Subnet       string     `json:"subnet,omitempty"`
	VLAN         int        `json:"vlan,omitempty"`
	DHCPEnabled  *bool      `json:"dhcp_enabled,omitempty"`
	DHCPRange    *DHCPRange `json:"dhcp_range,omitempty"`
	Enabled      *bool      `json:"enabled,omitempty"`
}

// DHCPRange is the inclusive start/end of a DHCP pool.
type DHCPRange struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

// Wlan mirrors schema/#Wlan. PasswordEnv names an environment variable
// holding the PSK; it is resolved at apply time, not at load time.
type Wlan struct {
	Name        string `json:"name"`
	Network     string `json:"network"`
	Security    string `json:"security"`
	PasswordEnv string `json:"password_env,omitempty"`
	Band        string `json:"band"`
	HideSSID    *bool  `json:"hide_ssid,omitempty"`
	Enabled     *bool  `json:"enabled,omitempty"`
}

// FirewallRule mirrors schema/#FirewallRule.
type FirewallRule struct {
	Name        string `json:"name"`
	Action      string `json:"action"`
	Ruleset     string `json:"ruleset"`
	Protocol    string `json:"protocol,omitempty"`
	SrcAddress  string `json:"src_address,omitempty"`
	SrcPort     string `json:"src_port,omitempty"`
	DstAddress  string `json:"dst_address,omitempty"`
	DstPort     string `json:"dst_port,omitempty"`
	Enabled     *bool  `json:"enabled,omitempty"`
}

// PortForward mirrors schema/#PortForward.
type PortForward struct {
	Name       string `json:"name"`
	Source     string `json:"source,omitempty"`
	SrcPort    string `json:"src_port"`
	DstAddress string `json:"dst_address"`
	DstPort    string `json:"dst_port"`
	Protocol   string `json:"protocol"`
	Enabled    *bool  `json:"enabled,omitempty"`
}

// NetworkConfig is the top-level declarative bundle.
type NetworkConfig struct {
	Networks      []Network      `json:"networks,omitempty"`
	Wlans         []Wlan         `json:"wlans,omitempty"`
	FirewallRules []FirewallRule `json:"firewall_rules,omitempty"`
	PortForwards  []PortForward  `json:"port_forwards,omitempty"`
}

// IsEmpty reports whether the config declares nothing. Used by the sync
// path to short-circuit when only event-shaped state is present.
func (c NetworkConfig) IsEmpty() bool {
	return len(c.Networks) == 0 &&
		len(c.Wlans) == 0 &&
		len(c.FirewallRules) == 0 &&
		len(c.PortForwards) == 0
}

