package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_BuiltInDefault(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SHINZO_DATA_DIR", dir)
	t.Setenv("SHINZO_KEY_PASSPHRASE", "x")

	cfg, err := Load("")
	require.NoError(t, err)
	assert.Equal(t, "<built-in default>", cfg.Source)
	assert.Equal(t, DefaultNetwork, cfg.Network)
	assert.NotEmpty(t, cfg.Shinzo.HubBaseURL, "preset applied")
	assert.Equal(t, dir, cfg.DefraDB.Store.Path)
	assert.Equal(t, filepath.Join(dir, "lens"), cfg.HostConfig.LensRegistryPath)
}

func TestLoad_DataDirDefaultsToHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("SHINZO_DATA_DIR", "")
	t.Setenv("SHINZO_KEY_PASSPHRASE", "x")

	cfg, err := Load("")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".shinzo", "host"), cfg.DefraDB.Store.Path)
}

func TestLoad_ConfigPathWinsOverDefaultDataDir(t *testing.T) {
	t.Setenv("SHINZO_DATA_DIR", "")
	t.Setenv("SHINZO_KEY_PASSPHRASE", "x")
	p := writeConfig(t, "network: custom\ndefradb:\n  store:\n    path: /srv/shinzo\n")

	cfg, err := Load(p)
	require.NoError(t, err)
	assert.Equal(t, "/srv/shinzo", cfg.DefraDB.Store.Path)
	assert.Equal(t, "/srv/shinzo/lens", cfg.HostConfig.LensRegistryPath)
}

func TestLoad_PassphraseGeneratedThenReused(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SHINZO_DATA_DIR", dir)
	t.Setenv("SHINZO_KEY_PASSPHRASE", "")

	first, err := Load("")
	require.NoError(t, err)
	assert.True(t, first.PassphraseGenerated)
	assert.Len(t, first.DefraDB.KeyringSecret, 64)
	info, err := os.Stat(filepath.Join(dir, PassphraseFileName))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	second, err := Load("")
	require.NoError(t, err)
	assert.False(t, second.PassphraseGenerated)
	assert.Equal(t, first.DefraDB.KeyringSecret, second.DefraDB.KeyringSecret)
}

func TestLoad_ExplicitPassphraseWinsAndWritesNothing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SHINZO_DATA_DIR", dir)
	t.Setenv("SHINZO_KEY_PASSPHRASE", "explicit")

	cfg, err := Load("")
	require.NoError(t, err)
	assert.Equal(t, "explicit", cfg.DefraDB.KeyringSecret)
	_, err = os.Stat(filepath.Join(dir, PassphraseFileName))
	assert.True(t, os.IsNotExist(err))
}
