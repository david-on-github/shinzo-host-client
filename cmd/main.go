package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/shinzonetwork/shinzo-host-client/config"
	"github.com/shinzonetwork/shinzo-host-client/pkg/host"
)

// version is set by the build (-ldflags "-X main.version=vX.Y.Z").
var version = "dev" //nolint:gochecknoglobals

const (
	exitUsage       = 2 // conventional exit code for bad command-line usage
	defaultHTTPPort = 8080
	shutdownTimeout = 8 * time.Second // under Docker's default 10s stop grace period
	clientTimeout   = 5 * time.Second
)

const usage = `Shinzo host — serves Views to the Shinzo network.

Usage:
  shinzo-host [run] [flags]   start the node (default)
  shinzo-host health [--port] exit 0 if a local node reports healthy
  shinzo-host id     [--port] print the local node's peer ID and connection string
  shinzo-host version

Flags for run (each mirrors an environment variable):
  --config      path     config file            (CONFIG_PATH; default: built-in)
  --data-dir    path     all node state         (SHINZO_DATA_DIR; default: ~/.shinzo/host)
  --network     name     testnet | custom       (SHINZO_NETWORK)
  --passphrase  string   key passphrase         (SHINZO_KEY_PASSPHRASE; default: generated on first run)

Other environment variables: BOOTSTRAP_PEERS, ALLOWED_ORIGINS, LOG_LEVEL, LOG_DIR, SOURCE_CHAIN_ID.
`

func main() {
	args := os.Args[1:]
	cmd := "run"
	if len(args) > 0 && args[0] != "" && args[0][0] != '-' {
		cmd, args = args[0], args[1:]
	}
	switch cmd {
	case "run":
		os.Exit(runNode(args))
	case "health":
		os.Exit(healthCmd(args))
	case "id":
		os.Exit(idCmd(args))
	case "version":
		fmt.Println(version)
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", cmd, usage)
		os.Exit(exitUsage)
	}
}

// runNode starts the node and blocks until SIGINT/SIGTERM.
func runNode(args []string) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.Usage = func() { fmt.Print(usage) }
	cfgPath := fs.String("config", "", "config file")
	dataDir := fs.String("data-dir", "", "data directory")
	network := fs.String("network", "", "network preset")
	passphrase := fs.String("passphrase", "", "key passphrase")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	// Flags are sugar over the environment so there is exactly one config path.
	for k, v := range map[string]string{"CONFIG_PATH": *cfgPath, "SHINZO_DATA_DIR": *dataDir, "SHINZO_NETWORK": *network, "SHINZO_KEY_PASSPHRASE": *passphrase} {
		if v != "" {
			_ = os.Setenv(k, v)
		}
	}

	cfg, err := config.Load(findConfigFile())
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to load config: %v\n", err)
		return 1
	}

	myHost, err := host.StartHosting(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to start hosting: %v\n", err)
		return 1
	}
	printBanner(cfg, myHost)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	<-ctx.Done()
	stop() // restore default signal handling: a second Ctrl-C kills immediately

	fmt.Println("Shutting down...")
	if err := closeWithTimeout(myHost); err != nil {
		fmt.Fprintf(os.Stderr, "shutdown error: %v\n", err)
		return 1
	}
	return 0
}

// findConfigFile returns an explicit or discovered config file, or "" for the built-in default.
func findConfigFile() string {
	if p := os.Getenv("CONFIG_PATH"); p != "" {
		return p
	}
	for _, path := range []string{
		"./config/config.local.yaml", // developer overrides (gitignored)
		"./config/config.yaml",       // repo checkout
		"./config.local.yaml",        // container: mounted developer overrides
		"./config.yaml",              // container: baked-in defaults
	} {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

func closeWithTimeout(h *host.Host) error {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	return h.Close(ctx)
}

// printBanner tells the operator what they need after startup: where the node
// is, who it is, where its state lives, and how to register it.
func printBanner(cfg *config.Config, h *host.Host) {
	port := cfg.HostConfig.HealthServerPort
	if port == 0 {
		port = defaultHTTPPort
	}
	scheme := "http"
	if cfg.HostConfig.HTTP.TLS.CertFile != "" {
		scheme = "https"
	}
	base := fmt.Sprintf("%s://localhost:%d", scheme, port)
	peerID := "(not available yet)"
	if info, err := h.GetPeerInfo(); err == nil && info != nil && info.Self != nil && info.Self.ID != "" {
		peerID = info.Self.ID
	}
	fmt.Printf("\n"+
		"  Shinzo host %s is running (network: %s, config: %s)\n"+
		"  API + health : %s   (health: /health, GraphQL: /api/v0/graphql)\n"+
		"  Peer ID      : %s\n"+
		"  Data         : %s\n"+
		"  Register     : %s/registration-app\n",
		version, cfg.Network, cfg.Source, base, peerID, cfg.DefraDB.Store.Path, base)
	if cfg.PassphraseGenerated {
		fmt.Printf("  Passphrase   : generated and saved to %s — back it up; it unlocks this node's identity.\n", cfg.PassphraseFile)
	}
	fmt.Println()
}

// healthCmd probes a local node; used by the container HEALTHCHECK and systemd.
func healthCmd(args []string) int {
	fs := flag.NewFlagSet("health", flag.ContinueOnError)
	port := fs.Int("port", defaultHTTPPort, "HTTP port of the local node")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	body, code, err := getJSON(fmt.Sprintf("http://127.0.0.1:%d/health", *port))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	status, _ := body["status"].(string)
	fmt.Printf("%s (HTTP %d)\n", status, code)
	if code != http.StatusOK {
		return 1
	}
	return 0
}

// idCmd prints the local node's identity in the forms other people need.
func idCmd(args []string) int {
	fs := flag.NewFlagSet("id", flag.ContinueOnError)
	port := fs.Int("port", defaultHTTPPort, "HTTP port of the local node")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	body, _, err := getJSON(fmt.Sprintf("http://127.0.0.1:%d/registration", *port))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	p2p, _ := body["p2p"].(map[string]any)
	self, _ := p2p["self"].(map[string]any)
	reg, _ := body["registration"].(map[string]any)
	fmt.Printf("peer id           : %v\n", self["id"])
	fmt.Printf("connection string : %v\n", reg["connection_string"])
	fmt.Printf("endpoint          : %v\n", reg["endpoint_address"])
	return 0
}

func getJSON(url string) (map[string]any, int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), clientTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("no node answering at %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("decode %s: %w", url, err)
	}
	return body, resp.StatusCode, nil
}
