package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
	return p
}

func TestNetworkPreset_DefaultIsTestnet(t *testing.T) {
	cfg, err := LoadConfig(writeConfig(t, "defradb:\n  url: localhost:9181\n"))
	require.NoError(t, err)

	preset, _ := LookupNetwork("testnet")
	assert.Equal(t, "testnet", cfg.Network)
	assert.Equal(t, preset.HubBaseURL, cfg.Shinzo.HubBaseURL)
	assert.Equal(t, preset.BootstrapPeers, cfg.DefraDB.P2P.BootstrapPeers)
}

func TestNetworkPreset_ConfigPeersAppendedAndDeduped(t *testing.T) {
	preset, _ := LookupNetwork("testnet")
	body := "network: testnet\ndefradb:\n  p2p:\n    bootstrap_peers:\n      - 'https://node.example'\n      - '" + preset.BootstrapPeers[0] + "'\n"
	cfg, err := LoadConfig(writeConfig(t, body))
	require.NoError(t, err)

	assert.Len(t, cfg.DefraDB.P2P.BootstrapPeers, len(preset.BootstrapPeers)+1)
	assert.Equal(t, "https://node.example", cfg.DefraDB.P2P.BootstrapPeers[len(preset.BootstrapPeers)])
}

func TestNetworkPreset_HubFromConfigWins(t *testing.T) {
	cfg, err := LoadConfig(writeConfig(t, "network: testnet\nshinzo:\n  hub_base_url: my.hub\n"))
	require.NoError(t, err)
	assert.Equal(t, "my.hub", cfg.Shinzo.HubBaseURL)
}

func TestNetworkPreset_Custom(t *testing.T) {
	cfg, err := LoadConfig(writeConfig(t, "network: custom\nshinzo:\n  hub_base_url: host.docker.internal\n"))
	require.NoError(t, err)
	assert.Equal(t, "host.docker.internal", cfg.Shinzo.HubBaseURL)
	assert.Empty(t, cfg.DefraDB.P2P.BootstrapPeers)
}

func TestNetworkPreset_Unknown(t *testing.T) {
	_, err := LoadConfig(writeConfig(t, "network: moonnet\n"))
	assert.ErrorIs(t, err, ErrUnknownNetwork)
}

func TestNetworkPreset_EnvOverrides(t *testing.T) {
	t.Setenv("SHINZO_NETWORK", "custom")
	t.Setenv("ALLOWED_ORIGINS", "https://a.example, http://localhost:3000")
	cfg, err := LoadConfig(writeConfig(t, "network: testnet\n"))
	require.NoError(t, err)
	assert.Equal(t, "custom", cfg.Network)
	assert.Equal(t, []string{"https://a.example", "http://localhost:3000"}, cfg.HostConfig.HTTP.AllowedOrigins)
}

func TestNetworkPreset_OriginsAdditive(t *testing.T) {
	t.Setenv("ALLOWED_ORIGINS", "http://localhost:3000")
	cfg, err := LoadConfig(writeConfig(t, "network: testnet\nhost:\n  http:\n    allowed_origins: [\"https://my.app\"]\n"))
	require.NoError(t, err)
	assert.Equal(t, []string{"https://*.shinzo.network", "https://my.app", "http://localhost:3000"}, cfg.HostConfig.HTTP.AllowedOrigins)
}
