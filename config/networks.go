package config

import (
	"fmt"
	"sort"
	"strings"
)

// NetworkPreset holds the constants that define a Shinzo network: where
// ShinzoHub lives and which peers a fresh node should dial to join.
//
// These are properties of the network itself (like a chain's bootnodes), not
// of any particular deployment, which is why they live in code rather than in
// the example config. Operators pick a network by name; anything they put in
// config.yaml is layered on top.
type NetworkPreset struct {
	HubBaseURL     string
	BootstrapPeers []string
	// AllowedOrigins are the network's own web apps (explorer, studio, …),
	// allowed to read any node on the network from a browser.
	AllowedOrigins []string
}

// DefaultNetwork is used when neither config.yaml nor SHINZO_NETWORK names one.
const DefaultNetwork = "testnet"

// NetworkCustom disables presets entirely; the operator supplies everything.
const NetworkCustom = "custom"

var networks = map[string]NetworkPreset{ //nolint:gochecknoglobals
	"testnet": {
		HubBaseURL:     "testnet.shinzo.network",
		AllowedOrigins: []string{"https://*.shinzo.network"},
		BootstrapPeers: []string{
			"/ip4/35.254.135.221/tcp/9171/p2p/12D3KooWDUdHSCXBM5Wb7te6ZdWMgqddw7tJ7npWSzXK5tQgBsbT",
			"/ip4/34.57.239.57/tcp/9171/p2p/12D3KooWBAgCEJHYqzuCFEXzjsw2CnV9JqvqMgTKYDww58aCxwW5",
			"/ip4/34.134.119.63/tcp/9171/p2p/12D3KooWQQTuSQaz4HfuvnJHakkQy3PhWbKBBbS3RkmBw4ZsFkyT",
		},
	},
}

// Networks returns the known network names, sorted.
func Networks() []string {
	names := make([]string, 0, len(networks)+1)
	for n := range networks {
		names = append(names, n)
	}
	sort.Strings(names)
	return append(names, NetworkCustom)
}

// LookupNetwork returns the preset for a network name (case-insensitive).
func LookupNetwork(name string) (NetworkPreset, bool) {
	p, ok := networks[strings.ToLower(strings.TrimSpace(name))]
	return p, ok
}

// applyNetworkPreset fills in hub and bootstrap peers from the named network.
// Config values win over the preset for the hub; configured bootstrap peers are
// appended to the preset's (deduplicated), so operators can add peers without
// losing the network's.
func (c *Config) applyNetworkPreset() error {
	name := strings.ToLower(strings.TrimSpace(c.Network))
	if name == "" {
		name = DefaultNetwork
	}
	c.Network = name
	if name == NetworkCustom {
		return nil
	}

	preset, ok := LookupNetwork(name)
	if !ok {
		return fmt.Errorf("%w: %q (known: %s)", ErrUnknownNetwork, name, strings.Join(Networks(), ", "))
	}

	if c.Shinzo.HubBaseURL == "" {
		c.Shinzo.HubBaseURL = preset.HubBaseURL
	}

	seen := make(map[string]struct{}, len(preset.BootstrapPeers)+len(c.DefraDB.P2P.BootstrapPeers))
	merged := make([]string, 0, len(seen))
	for _, p := range append(append([]string{}, preset.BootstrapPeers...), c.DefraDB.P2P.BootstrapPeers...) {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		merged = append(merged, p)
	}
	c.DefraDB.P2P.BootstrapPeers = merged
	c.HostConfig.HTTP.AllowedOrigins = mergeUnique(preset.AllowedOrigins, c.HostConfig.HTTP.AllowedOrigins)
	return nil
}

// mergeUnique concatenates lists in order, dropping blanks and duplicates.
func mergeUnique(lists ...[]string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, l := range lists {
		for _, v := range l {
			v = strings.TrimSpace(v)
			if v == "" {
				continue
			}
			if _, dup := seen[v]; dup {
				continue
			}
			seen[v] = struct{}{}
			out = append(out, v)
		}
	}
	return out
}
