package unifi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// resourceServer is a tiny stand-in for the controller's /rest/* CRUD
// surface. It records the last request seen so tests can assert path
// shape and verb without coupling to any real controller. Endpoints
// covered match the package-level provenance comments in resources.go.
type resourceServer struct {
	srv     *httptest.Server
	lastMethod string
	lastPath   string
	lastBody   []byte
	respData   any
}

func newResourceServer(t *testing.T) *resourceServer {
	t.Helper()
	rs := &resourceServer{}
	rs.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-KEY") != "test-key" {
			http.Error(w, "no key", http.StatusForbidden)
			return
		}
		rs.lastMethod = r.Method
		rs.lastPath = r.URL.Path
		rs.lastBody, _ = io.ReadAll(r.Body)
		if rs.respData != nil {
			_ = json.NewEncoder(w).Encode(map[string]any{"data": rs.respData})
			return
		}
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	t.Cleanup(rs.srv.Close)
	return rs
}

func clientFor(t *testing.T, rs *resourceServer) *Client {
	t.Helper()
	c, err := New(Config{Host: rs.srv.URL, APIKey: "test-key", Site: "default"})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestListNetworksHitsCitedEndpoint(t *testing.T) {
	rs := newResourceServer(t)
	rs.respData = []Network{{ID: "n1", Name: "iot", Purpose: "corporate"}}
	c := clientFor(t, rs)
	got, err := c.ListNetworks(context.Background())
	if err != nil {
		t.Fatalf("ListNetworks: %v", err)
	}
	if rs.lastMethod != http.MethodGet {
		t.Fatalf("method=%q", rs.lastMethod)
	}
	if !strings.HasSuffix(rs.lastPath, "/proxy/network/api/s/default/rest/networkconf") {
		t.Fatalf("path=%q", rs.lastPath)
	}
	if len(got) != 1 || got[0].Name != "iot" {
		t.Fatalf("decoded: %+v", got)
	}
}

func TestCRUDPathShapesForAllResources(t *testing.T) {
	rs := newResourceServer(t)
	c := clientFor(t, rs)
	ctx := context.Background()

	checks := []struct {
		name string
		do   func() error
		want string
		verb string
	}{
		{"list-wlans", func() error { _, err := c.ListWlans(ctx); return err }, "/rest/wlanconf", http.MethodGet},
		{"create-wlan", func() error { _, err := c.CreateWlan(ctx, WlanConf{Name: "x"}); return err }, "/rest/wlanconf", http.MethodPost},
		{"update-wlan", func() error { return c.UpdateWlan(ctx, WlanConf{ID: "w1", Name: "x"}) }, "/rest/wlanconf/w1", http.MethodPut},
		{"delete-wlan", func() error { return c.DeleteWlan(ctx, "w1") }, "/rest/wlanconf/w1", http.MethodDelete},

		{"list-firewall", func() error { _, err := c.ListFirewallRules(ctx); return err }, "/rest/firewallrule", http.MethodGet},
		{"create-firewall", func() error { _, err := c.CreateFirewallRule(ctx, FirewallRule{Name: "x"}); return err }, "/rest/firewallrule", http.MethodPost},
		{"update-firewall", func() error { return c.UpdateFirewallRule(ctx, FirewallRule{ID: "r1", Name: "x"}) }, "/rest/firewallrule/r1", http.MethodPut},
		{"delete-firewall", func() error { return c.DeleteFirewallRule(ctx, "r1") }, "/rest/firewallrule/r1", http.MethodDelete},

		{"list-portforward", func() error { _, err := c.ListPortForwards(ctx); return err }, "/rest/portforward", http.MethodGet},
		{"create-portforward", func() error { _, err := c.CreatePortForward(ctx, PortForward{Name: "x"}); return err }, "/rest/portforward", http.MethodPost},
		{"update-portforward", func() error { return c.UpdatePortForward(ctx, PortForward{ID: "p1", Name: "x"}) }, "/rest/portforward/p1", http.MethodPut},
		{"delete-portforward", func() error { return c.DeletePortForward(ctx, "p1") }, "/rest/portforward/p1", http.MethodDelete},
	}
	for _, tc := range checks {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.do(); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if rs.lastMethod != tc.verb {
				t.Fatalf("%s: method=%q want %q", tc.name, rs.lastMethod, tc.verb)
			}
			if !strings.HasSuffix(rs.lastPath, tc.want) {
				t.Fatalf("%s: path=%q want suffix %q", tc.name, rs.lastPath, tc.want)
			}
		})
	}
}

func TestUpdateNetworkRequiresID(t *testing.T) {
	rs := newResourceServer(t)
	c := clientFor(t, rs)
	if err := c.UpdateNetwork(context.Background(), Network{Name: "x"}); err == nil {
		t.Fatal("expected error when _id is empty")
	}
}
