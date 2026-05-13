package unifi

// Resource CRUD endpoints for declarative state (networks, WLANs,
// firewall rules, port forwards).
//
// Endpoint provenance (verified):
//
//   - /proxy/network/api/s/{site}/rest/networkconf
//   - /proxy/network/api/s/{site}/rest/wlanconf
//   - /proxy/network/api/s/{site}/rest/firewallrule
//   - /proxy/network/api/s/{site}/rest/portforward
//
//   All four follow the legacy /proxy/network/api/s/{site}/rest/<obj>
//   pattern that PR 1 already uses for /rest/user. They support the
//   standard CRUD verbs: GET to list, POST to create, PUT
//   /rest/<obj>/{_id} to update, DELETE /rest/<obj>/{_id} to delete.
//
//   The Official Integration API does not yet expose these resources;
//   the legacy /api endpoint is the load-bearing surface for declarative
//   config and is documented across the Ubiquiti community wiki
//   (ubntwiki.com) and the long-running Art-of-WiFi PHP client
//   (github.com/Art-of-WiFi/UniFi-API-client). The latter is the de
//   facto reference for which fields the controller accepts; unictl
//   models only the fields it needs and ignores the rest.

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// UniCtlMarker is the value unictl writes into the `note` field of any
// resource it creates. Pruning uses this to distinguish managed from
// unmanaged objects without operator bookkeeping.
const UniCtlMarker = "app=unictl"

// Network is the subset of /rest/networkconf fields unictl reads/writes.
// The controller returns extra fields; they are ignored on decode.
type Network struct {
	ID          string `json:"_id,omitempty"`
	Name        string `json:"name"`
	Purpose     string `json:"purpose,omitempty"`
	Subnet      string `json:"ip_subnet,omitempty"`
	VLAN        int    `json:"vlan,omitempty"`
	DHCPDEnable *bool  `json:"dhcpd_enabled,omitempty"`
	DHCPDStart  string `json:"dhcpd_start,omitempty"`
	DHCPDStop   string `json:"dhcpd_stop,omitempty"`
	Enabled     *bool  `json:"enabled,omitempty"`
	Note        string `json:"note,omitempty"`
}

// WlanConf is the subset of /rest/wlanconf fields unictl reads/writes.
type WlanConf struct {
	ID         string `json:"_id,omitempty"`
	Name       string `json:"name"`               // SSID
	NetworkID  string `json:"networkconf_id,omitempty"`
	Security   string `json:"security,omitempty"` // "open" | "wpapsk" | ...
	XPassphrase string `json:"x_passphrase,omitempty"`
	Band       string `json:"wlan_band,omitempty"` // "2g" | "5g" | "both"
	HideSSID   *bool  `json:"hide_ssid,omitempty"`
	Enabled    *bool  `json:"enabled,omitempty"`
	Note       string `json:"note,omitempty"`
}

// FirewallRule is the subset of /rest/firewallrule fields unictl
// reads/writes.
type FirewallRule struct {
	ID                string `json:"_id,omitempty"`
	Name              string `json:"name"`
	Action            string `json:"action,omitempty"` // "accept" | "drop" | "reject"
	Ruleset           string `json:"ruleset,omitempty"`
	Protocol          string `json:"protocol,omitempty"`
	SrcAddress        string `json:"src_address,omitempty"`
	SrcPort           string `json:"src_port,omitempty"`
	DstAddress        string `json:"dst_address,omitempty"`
	DstPort           string `json:"dst_port,omitempty"`
	Enabled           *bool  `json:"enabled,omitempty"`
	Note              string `json:"note,omitempty"`
}

// PortForward is the subset of /rest/portforward fields unictl
// reads/writes. Field names map to the controller's wire names: `src`
// for source, `fwd` for the destination IP, `proto` for protocol.
type PortForward struct {
	ID         string `json:"_id,omitempty"`
	Name       string `json:"name"`
	Src        string `json:"src,omitempty"` // "any" or CIDR
	SrcPort    string `json:"src_port,omitempty"`
	DstAddress string `json:"fwd,omitempty"`
	DstPort    string `json:"fwd_port,omitempty"`
	Protocol   string `json:"proto,omitempty"`
	Enabled    *bool  `json:"enabled,omitempty"`
	Note       string `json:"note,omitempty"`
}

// listResponse is the standard /rest/* envelope.
type listResponse[T any] struct {
	Data []T `json:"data"`
}

func (c *Client) restPath(resource string) string {
	return fmt.Sprintf("/proxy/network/api/s/%s/rest/%s", url.PathEscape(c.cfg.Site), resource)
}

// ---- Networks -------------------------------------------------------------

func (c *Client) ListNetworks(ctx context.Context) ([]Network, error) {
	var resp listResponse[Network]
	if err := c.do(ctx, http.MethodGet, c.restPath("networkconf"), nil, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

func (c *Client) CreateNetwork(ctx context.Context, n Network) (Network, error) {
	var resp listResponse[Network]
	if err := c.do(ctx, http.MethodPost, c.restPath("networkconf"), n, &resp); err != nil {
		return Network{}, err
	}
	if len(resp.Data) > 0 {
		return resp.Data[0], nil
	}
	return n, nil
}

func (c *Client) UpdateNetwork(ctx context.Context, n Network) error {
	if n.ID == "" {
		return fmt.Errorf("unifi: UpdateNetwork requires _id")
	}
	return c.do(ctx, http.MethodPut, c.restPath("networkconf")+"/"+url.PathEscape(n.ID), n, nil)
}

func (c *Client) DeleteNetwork(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("unifi: DeleteNetwork requires id")
	}
	return c.do(ctx, http.MethodDelete, c.restPath("networkconf")+"/"+url.PathEscape(id), nil, nil)
}

// ---- WLANs ----------------------------------------------------------------

func (c *Client) ListWlans(ctx context.Context) ([]WlanConf, error) {
	var resp listResponse[WlanConf]
	if err := c.do(ctx, http.MethodGet, c.restPath("wlanconf"), nil, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

func (c *Client) CreateWlan(ctx context.Context, w WlanConf) (WlanConf, error) {
	var resp listResponse[WlanConf]
	if err := c.do(ctx, http.MethodPost, c.restPath("wlanconf"), w, &resp); err != nil {
		return WlanConf{}, err
	}
	if len(resp.Data) > 0 {
		return resp.Data[0], nil
	}
	return w, nil
}

func (c *Client) UpdateWlan(ctx context.Context, w WlanConf) error {
	if w.ID == "" {
		return fmt.Errorf("unifi: UpdateWlan requires _id")
	}
	return c.do(ctx, http.MethodPut, c.restPath("wlanconf")+"/"+url.PathEscape(w.ID), w, nil)
}

func (c *Client) DeleteWlan(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("unifi: DeleteWlan requires id")
	}
	return c.do(ctx, http.MethodDelete, c.restPath("wlanconf")+"/"+url.PathEscape(id), nil, nil)
}

// ---- Firewall rules -------------------------------------------------------

func (c *Client) ListFirewallRules(ctx context.Context) ([]FirewallRule, error) {
	var resp listResponse[FirewallRule]
	if err := c.do(ctx, http.MethodGet, c.restPath("firewallrule"), nil, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

func (c *Client) CreateFirewallRule(ctx context.Context, r FirewallRule) (FirewallRule, error) {
	var resp listResponse[FirewallRule]
	if err := c.do(ctx, http.MethodPost, c.restPath("firewallrule"), r, &resp); err != nil {
		return FirewallRule{}, err
	}
	if len(resp.Data) > 0 {
		return resp.Data[0], nil
	}
	return r, nil
}

func (c *Client) UpdateFirewallRule(ctx context.Context, r FirewallRule) error {
	if r.ID == "" {
		return fmt.Errorf("unifi: UpdateFirewallRule requires _id")
	}
	return c.do(ctx, http.MethodPut, c.restPath("firewallrule")+"/"+url.PathEscape(r.ID), r, nil)
}

func (c *Client) DeleteFirewallRule(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("unifi: DeleteFirewallRule requires id")
	}
	return c.do(ctx, http.MethodDelete, c.restPath("firewallrule")+"/"+url.PathEscape(id), nil, nil)
}

// ---- Port forwards --------------------------------------------------------

func (c *Client) ListPortForwards(ctx context.Context) ([]PortForward, error) {
	var resp listResponse[PortForward]
	if err := c.do(ctx, http.MethodGet, c.restPath("portforward"), nil, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

func (c *Client) CreatePortForward(ctx context.Context, p PortForward) (PortForward, error) {
	var resp listResponse[PortForward]
	if err := c.do(ctx, http.MethodPost, c.restPath("portforward"), p, &resp); err != nil {
		return PortForward{}, err
	}
	if len(resp.Data) > 0 {
		return resp.Data[0], nil
	}
	return p, nil
}

func (c *Client) UpdatePortForward(ctx context.Context, p PortForward) error {
	if p.ID == "" {
		return fmt.Errorf("unifi: UpdatePortForward requires _id")
	}
	return c.do(ctx, http.MethodPut, c.restPath("portforward")+"/"+url.PathEscape(p.ID), p, nil)
}

func (c *Client) DeletePortForward(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("unifi: DeletePortForward requires id")
	}
	return c.do(ctx, http.MethodDelete, c.restPath("portforward")+"/"+url.PathEscape(id), nil, nil)
}
