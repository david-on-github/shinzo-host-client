package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/shinzonetwork/shinzo-host-client/pkg/defradb"
	"github.com/shinzonetwork/shinzo-host-client/pkg/pruner"
	"gopkg.in/yaml.v3"
)

// CollectionName is the name of the collection where we store Shinzo-specific documents in DefraDB.
const CollectionName = "shinzo"

// Default configuration values for schema fetching.
const (
	DefaultIndexerSchemaEndpoint   = "/api/v1/schema"
	DefaultSchemaHTTPClientTimeout = 30
	MaxSchemaHTTPClientTimeout     = 300
)

// ErrNegativeSchemaTimeout is returned when the schema HTTP client timeout is negative.
var ErrNegativeSchemaTimeout = fmt.Errorf("schema.http_client_timeout_secs must be non-negative")

// firstEnv returns the value of the first environment variable that is set.
func firstEnv(names ...string) string {
	for _, n := range names {
		if v := os.Getenv(n); v != "" {
			return v
		}
	}
	return ""
}

// DefaultSourceChainID is Ethereum mainnet.
const DefaultSourceChainID = 1

// ErrUnknownNetwork is returned when config.network / SHINZO_NETWORK names no known preset.
var ErrUnknownNetwork = fmt.Errorf("unknown network")

// ErrExcessiveSchemaTimeout is returned when the schema HTTP client timeout exceeds the maximum.
var ErrExcessiveSchemaTimeout = fmt.Errorf("schema.http_client_timeout_secs must not exceed %d", MaxSchemaHTTPClientTimeout)

// DefraDBP2PConfig represents P2P configuration for DefraDB.
type DefraDBP2PConfig struct {
	Enabled                bool     `yaml:"enabled"`
	BootstrapPeers         []string `yaml:"bootstrap_peers"`
	ListenAddr             string   `yaml:"listen_addr"`
	MaxRetries             int      `yaml:"max_retries"`
	RetryBaseDelayMs       int      `yaml:"retry_base_delay_ms"`
	ReconnectIntervalMs    int      `yaml:"reconnect_interval_ms"`
	EnableAutoReconnect    bool     `yaml:"enable_auto_reconnect"`
	PeerDiscoveryTimeoutMs int      `yaml:"peer_discovery_timeout_ms"` // Timeout for auto-discovering peer IDs (default: 10000)
	// BootstrapFromHub asks ShinzoHub for registered indexers on startup and
	// peers with a capped selection of them (see MaxIndexerPeers). Off by
	// default: every indexer peer streams a full copy of every block, so which
	// indexers a host listens to is a deliberate choice — the network preset
	// and bootstrap_peers — not something derived from who has registered.
	// Overridable with BOOTSTRAP_FROM_HUB=true.
	BootstrapFromHub bool `yaml:"bootstrap_from_hub"`
	// MaxIndexerPeers caps how many indexers this host replicates from.
	// Every indexer peer sends a full copy of every block, so load scales
	// linearly with this number. 0 = minimum_attestations + 1.
	MaxIndexerPeers int `yaml:"max_indexer_peers"`
}

// DefraDBStoreConfig represents store configuration for DefraDB.
type DefraDBStoreConfig struct {
	Path string `yaml:"path"`
	// Badger memory configuration
	BlockCacheMB int64 `yaml:"block_cache_mb"`
	MemTableMB   int64 `yaml:"memtable_mb"`
	IndexCacheMB int64 `yaml:"index_cache_mb"`
	// Badger compaction configuration
	NumCompactors           int `yaml:"num_compactors"`
	NumLevelZeroTables      int `yaml:"num_level_zero_tables"`
	NumLevelZeroTablesStall int `yaml:"num_level_zero_tables_stall"`
	// Badger value log configuration
	ValueLogFileSizeMB int64 `yaml:"value_log_file_size_mb"`
}

// DefraDBConfig represents DefraDB configuration.
type DefraDBConfig struct {
	URL           string             `yaml:"url"`
	KeyringSecret string             `yaml:"keyring_secret"`
	P2P           DefraDBP2PConfig   `yaml:"p2p"`
	Store         DefraDBStoreConfig `yaml:"store"`
}

// LoggerConfig represents logger configuration.
type LoggerConfig struct {
	Development bool `yaml:"development"`
}

// Config represents the overall configuration for the Shinzo host application, including DefraDB, Shinzo-specific settings, logging, hosting, and pruning.
type Config struct {
	// Network selects a built-in preset (hub + bootstrap peers). See Networks().
	// "custom" disables presets. Overridable with SHINZO_NETWORK.
	Network    string        `yaml:"network"`
	DefraDB    DefraDBConfig `yaml:"defradb"`
	Shinzo     ShinzoConfig  `yaml:"shinzo"`
	Schema     SchemaConfig  `yaml:"schema"`
	Logger     LoggerConfig  `yaml:"logger"`
	HostConfig HostConfig    `yaml:"host"`
	Pruner     pruner.Config `yaml:"pruner"`
}

// SchemaConfig represents configuration for dynamic schema fetching.
type SchemaConfig struct {
	IndexerSchemaEndpoint string `yaml:"indexer_schema_endpoint"`
	HTTPClientTimeoutSecs int    `yaml:"http_client_timeout_secs"`
	// AuthToken is the bearer token used to authenticate schema fetch requests
	// against indexers with SCHEMA_AUTH_MODE=token (the default).
	//
	// IMPORTANT: This field uses yaml:"-", which means YAML configuration is
	// SILENTLY IGNORED. The token MUST be provided via the INDEXER_SCHEMA_ENDPOINT_AUTH_TOKEN
	// environment variable. Setting this field in config.yaml will NOT work —
	// the host will start with an empty token, causing schema fetches to
	// receive 401/503 and fall back to the embedded schema (fail-closed).
	AuthToken string `yaml:"-"`
}

// ShinzoConfig represents configuration specific to the Shinzo host application.
type ShinzoConfig struct {
	MinimumAttestations int `yaml:"minimum_attestations"`
	// SourceChainID is the EVM chain this host consumes primitives for
	// (1 = Ethereum mainnet). Only indexers registered for this chain on the
	// hub are considered as peers. Overridable with SOURCE_CHAIN_ID.
	SourceChainID uint64 `yaml:"source_chain_id"`
	HubBaseURL    string `yaml:"hub_base_url"` // ShinzoHub hostname only — no scheme, no port (e.g. "testnet.shinzo.network")
	StartHeight   uint64 `yaml:"start_height"`

	// P2P Control Settings
	P2PEnabled bool `yaml:"p2p_enabled"`

	// View Management Settings
	ViewInactivityTimeout string `yaml:"view_inactivity_timeout"` // Stop updating after inactivity (default: 24h)
	ViewCleanupInterval   string `yaml:"view_cleanup_interval"`   // Check for inactive views (default: 1h)
	ViewWorkerCount       int    `yaml:"view_worker_count"`       // Workers for lens transformations (default: 2)
	ViewQueueSize         int    `yaml:"view_queue_size"`         // Queue size for view processing jobs (default: 1000)

	// Queue Settings
	CacheQueueSize int `yaml:"cache_queue_size"` // Size of job queue for document processing

	// Batch Attestation Processing Settings
	BatchWriterCount           int  `yaml:"batch_writer_count"`           // Number of batch writers
	BatchSize                  int  `yaml:"batch_size"`                   // Max attestations per batch
	BatchFlushInterval         int  `yaml:"batch_flush_interval"`         // Flush interval in milliseconds
	MaxConcurrentVerifications int  `yaml:"max_concurrent_verifications"` // Max concurrent signature verifications
	UseBlockSignatures         bool `yaml:"use_block_signatures"`         // Use block signatures for attestations
	DocWorkerCount             int  `yaml:"doc_worker_count"`             // Number of document processing workers
	DocQueueSize               int  `yaml:"doc_queue_size"`               // Queue size for document event notifications

	// Event Filtering
	EventFilter EventFilterConfig `yaml:"event_filter"` // Configure filtering of P2P events
}

// EventFilterConfig configures content-based filtering of P2P events.
type EventFilterConfig struct {
	Enabled        bool              `yaml:"enabled"`         // Master switch for filtering
	Mode           string            `yaml:"mode"`            // "allowlist" (default) or "blocklist"
	CascadeFilters bool              `yaml:"cascade_filters"` // If true, filtering a tx also filters its logs/ALEs
	BlockRange     *BlockRangeFilter `yaml:"block_range"`     // Optional block number range filter
	Groups         []FilterGroup     `yaml:"groups"`          // Named filter groups combined with OR logic
}

// FilterGroup is a named set of contract and topic filters that can be toggled independently.
type FilterGroup struct {
	Name      string           `yaml:"name"`      // Human-readable name (e.g., "uniswap-v3")
	Enabled   bool             `yaml:"enabled"`   // Toggle this group on/off
	Contracts []ContractFilter `yaml:"contracts"` // Contract address filters
	Topics    []TopicFilter    `yaml:"topics"`    // Event topic filters
}

// ContractFilter matches events by contract address.
type ContractFilter struct {
	Address string   `yaml:"address"` // Contract address (0x...)
	Name    string   `yaml:"name"`    // Human-readable name for logging
	Types   []string `yaml:"types"`   // Collection types to apply to: "transaction", "log", "accessListEntry"
}

// TopicFilter matches log events by topic values.
type TopicFilter struct {
	Topic0 string `yaml:"topic0"` // Event signature hash (required)
	Topic1 string `yaml:"topic1"` // Optional indexed parameter 1
	Topic2 string `yaml:"topic2"` // Optional indexed parameter 2
	Topic3 string `yaml:"topic3"` // Optional indexed parameter 3
	Name   string `yaml:"name"`   // Human-readable name (e.g., "Swap", "Transfer")
}

// BlockRangeFilter restricts processing to a range of block numbers.
type BlockRangeFilter struct {
	MinBlock uint64 `yaml:"min_block"` // Minimum block number (inclusive)
	MaxBlock uint64 `yaml:"max_block"` // Maximum block number (inclusive), 0 = no upper limit
}

// HostConfig represents configuration specific to the Shinzo host application.
type HostConfig struct {
	LensRegistryPath   string         `yaml:"lens_registry_path"`    // At this path, we will store the lens' wasm files
	HealthServerPort   int            `yaml:"health_server_port"`    // Port for the health server (default: 8080)
	OpenBrowserOnStart bool           `yaml:"open_browser_on_start"` // Auto-open metrics page in browser on startup (default: false)
	HTTP               HTTPConfig     `yaml:"http"`                  // Public HTTP surface options (CORS, TLS)
	Snapshot           SnapshotConfig `yaml:"snapshot"`              // Snapshot bootstrap configuration
}

// HTTPConfig configures the node's public HTTP surface (the health server).
type HTTPConfig struct {
	// AllowedOrigins lists browser origins permitted to call this node
	// (e.g. "https://explorer.shinzo.network"). "*" allows any origin.
	// Empty disables CORS entirely.
	AllowedOrigins []string `yaml:"allowed_origins"`
	// TLS, when both files are set, serves HTTPS directly from the node.
	TLS TLSConfig `yaml:"tls"`
}

// TLSConfig points at a PEM certificate/key pair.
type TLSConfig struct {
	CertFile string `yaml:"cert_file"`
	KeyFile  string `yaml:"key_file"`
}

// SnapshotConfig configures historical snapshot download and import on startup.
type SnapshotConfig struct {
	// Enabled controls whether snapshot bootstrap runs on startup.
	Enabled bool `yaml:"enabled"`
	// IndexerURL is the HTTP base URL of the indexer serving snapshots.
	IndexerURL string `yaml:"indexer_url"`
	// HistoricalRanges specifies block ranges the host needs for bootstrap.
	HistoricalRanges []BlockRange `yaml:"historical_ranges"`
}

// BlockRange represents an inclusive block number range.
type BlockRange struct {
	Start int64 `yaml:"start"`
	End   int64 `yaml:"end"`
}

// LoadConfig loads configuration from a YAML file.
func LoadConfig(path string) (*Config, error) {
	// Load YAML config
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	if err := applyEnvOverrides(&cfg); err != nil {
		return nil, err
	}
	if err := applySchemaConfig(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// applyEnvOverrides layers operator inputs from the environment over the
// file, then resolves the network preset. Environment wins over the file.
func applyEnvOverrides(cfg *Config) error {
	// Apply environment variable overrides
	if v := os.Getenv("START_HEIGHT"); v != "" {
		height, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid START_HEIGHT value %q: %w", v, err)
		}
		cfg.Shinzo.StartHeight = height
	}

	if v := os.Getenv("BOOTSTRAP_PEERS"); v != "" {
		cfg.DefraDB.P2P.BootstrapPeers = strings.Split(v, ",")
	}

	// SHINZO_DATA_DIR relocates all node state (database, keys, lens registry)
	// in one go, for running the binary outside the repo / container.
	if v := os.Getenv("SHINZO_DATA_DIR"); v != "" {
		cfg.DefraDB.Store.Path = v
		cfg.HostConfig.LensRegistryPath = filepath.Join(v, "lens")
	}

	if v := os.Getenv("BOOTSTRAP_FROM_HUB"); v != "" {
		cfg.DefraDB.P2P.BootstrapFromHub = strings.EqualFold(v, "true") || v == "1"
	}

	if v := os.Getenv("SOURCE_CHAIN_ID"); v != "" {
		id, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid SOURCE_CHAIN_ID value %q: %w", v, err)
		}
		cfg.Shinzo.SourceChainID = id
	}
	if cfg.Shinzo.SourceChainID == 0 {
		cfg.Shinzo.SourceChainID = DefaultSourceChainID
	}

	if v := os.Getenv("SHINZO_NETWORK"); v != "" {
		cfg.Network = v
	}
	if v := os.Getenv("SHINZO_HUB_BASE_URL"); v != "" {
		cfg.Shinzo.HubBaseURL = v
	}
	if err := cfg.applyNetworkPreset(); err != nil {
		return err
	}

	// ALLOWED_ORIGINS is a comma-separated list of browser origins, added to
	// the network preset's and the config file's.
	if v := os.Getenv("ALLOWED_ORIGINS"); v != "" {
		cfg.HostConfig.HTTP.AllowedOrigins = mergeUnique(cfg.HostConfig.HTTP.AllowedOrigins, strings.Split(v, ","))
	}

	// DEFRA_URL overrides defradb.url at runtime, so deployment artifacts
	// can pick a different bind address (e.g. 0.0.0.0:9181 instead of the
	// loopback-only default) without editing the YAML.
	if v := os.Getenv("DEFRA_URL"); v != "" {
		cfg.DefraDB.URL = v
	}

	// SHINZO_KEY_PASSPHRASE encrypts the node's identity keys on disk.
	// DEFRA_KEYRING_SECRET / DEFRADB_KEYRING_SECRET are accepted as legacy aliases.
	if v := firstEnv("SHINZO_KEY_PASSPHRASE", "DEFRA_KEYRING_SECRET", "DEFRADB_KEYRING_SECRET"); v != "" {
		cfg.DefraDB.KeyringSecret = v
	} else if f := os.Getenv("SHINZO_KEY_PASSPHRASE_FILE"); f != "" {
		// Docker/Kubernetes secrets are mounted as files; read the passphrase from one.
		b, err := os.ReadFile(filepath.Clean(f))
		if err != nil {
			return fmt.Errorf("read SHINZO_KEY_PASSPHRASE_FILE: %w", err)
		}
		cfg.DefraDB.KeyringSecret = strings.TrimSpace(string(b))
	}
	return nil
}

// applySchemaConfig resolves schema-fetch settings and validates the timeout.
func applySchemaConfig(cfg *Config) error {
	if v := os.Getenv("INDEXER_SCHEMA_ENDPOINT"); v != "" {
		cfg.Schema.IndexerSchemaEndpoint = v
	}

	if v := os.Getenv("INDEXER_SCHEMA_ENDPOINT_AUTH_TOKEN"); v != "" {
		cfg.Schema.AuthToken = v
	}
	if cfg.Schema.IndexerSchemaEndpoint == "" {
		cfg.Schema.IndexerSchemaEndpoint = DefaultIndexerSchemaEndpoint
	}
	switch {
	case cfg.Schema.HTTPClientTimeoutSecs < 0:
		return fmt.Errorf("%w: got %d", ErrNegativeSchemaTimeout, cfg.Schema.HTTPClientTimeoutSecs)
	case cfg.Schema.HTTPClientTimeoutSecs > MaxSchemaHTTPClientTimeout:
		return fmt.Errorf("%w: got %d", ErrExcessiveSchemaTimeout, cfg.Schema.HTTPClientTimeoutSecs)
	case cfg.Schema.HTTPClientTimeoutSecs == 0:
		cfg.Schema.HTTPClientTimeoutSecs = DefaultSchemaHTTPClientTimeout
	}
	return nil
}

// ToInternalConfig converts the host config to the pkg/defradb internal config
// shape consumed by StartDefraInstance and signer helpers.
func (c *Config) ToInternalConfig() *defradb.Config {
	if c == nil {
		return nil
	}

	return &defradb.Config{
		DefraDB: defradb.DefraDBConfig{
			URL:           c.DefraDB.URL,
			KeyringSecret: c.DefraDB.KeyringSecret,
			P2P: defradb.DefraP2PConfig{
				Enabled:             c.DefraDB.P2P.Enabled,
				BootstrapPeers:      []string{}, // Empty - peers added after ViewManager init
				ListenAddr:          c.DefraDB.P2P.ListenAddr,
				MaxRetries:          c.DefraDB.P2P.MaxRetries,
				RetryBaseDelayMs:    c.DefraDB.P2P.RetryBaseDelayMs,
				ReconnectIntervalMs: c.DefraDB.P2P.ReconnectIntervalMs,
				EnableAutoReconnect: c.DefraDB.P2P.EnableAutoReconnect,
			},
			Store: defradb.DefraStoreConfig{
				Path:                    c.DefraDB.Store.Path,
				BlockCacheMB:            c.DefraDB.Store.BlockCacheMB,
				MemTableMB:              c.DefraDB.Store.MemTableMB,
				IndexCacheMB:            c.DefraDB.Store.IndexCacheMB,
				NumCompactors:           c.DefraDB.Store.NumCompactors,
				NumLevelZeroTables:      c.DefraDB.Store.NumLevelZeroTables,
				NumLevelZeroTablesStall: c.DefraDB.Store.NumLevelZeroTablesStall,
				ValueLogFileSizeMB:      c.DefraDB.Store.ValueLogFileSizeMB,
			},
		},
		Logger: defradb.LoggerConfig{
			Development: c.Logger.Development,
		},
	}
}
