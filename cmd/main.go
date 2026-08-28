package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/shinzonetwork/shinzo-host-client/config"
	"github.com/shinzonetwork/shinzo-host-client/pkg/host"
)

func findConfigFile() string {
	if p := os.Getenv("CONFIG_PATH"); p != "" {
		return p
	}

	possiblePaths := []string{
		"./config/config.local.yaml", // Developer overrides (gitignored)
		"./config/config.yaml",       // From project root
		"./config.local.yaml",        // Docker / mounted developer overrides
		"./config.yaml",              // Docker / baked-in defaults
		"../config.yaml",             // From bin/ directory
	}

	for _, path := range possiblePaths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	return "config.yaml"
}

func main() {
	configPath := findConfigFile()
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		panic(fmt.Errorf("unable to load config %q: %w", configPath, err))
	}
	fmt.Printf("Loaded config from %s (network: %s)\n", configPath, cfg.Network)

	myHost, err := host.StartHosting(cfg)
	if err != nil {
		panic(fmt.Errorf("failed to start hosting: %w", err))
	}
	printBanner(cfg, myHost)

	// Run until SIGINT/SIGTERM (Ctrl-C, `docker stop`, systemd stop), then close
	// cleanly so the database flushes and the P2P host leaves the swarm.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	<-ctx.Done()
	stop() // restore default signal handling: a second Ctrl-C kills immediately

	fmt.Println("Shutting down...")
	if err := closeWithTimeout(myHost); err != nil {
		fmt.Fprintf(os.Stderr, "shutdown error: %v\n", err)
		os.Exit(1)
	}
}

func closeWithTimeout(h *host.Host) error {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	return h.Close(ctx)
}

// printBanner tells the operator the three things they need after startup:
// where the node is, who it is, and how to register it.
func printBanner(cfg *config.Config, h *host.Host) {
	port := cfg.HostConfig.HealthServerPort
	if port == 0 {
		port = 8080
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
		"  Shinzo host is running (network: %s)\n"+
		"  API + health : %s   (health: /health, GraphQL: /api/v0/graphql)\n"+
		"  Peer ID      : %s\n"+
		"  Register     : %s/registration-app\n\n", cfg.Network, base, peerID, base)
}

// shutdownTimeout bounds a graceful stop. Docker gives a container 10s
// before SIGKILL by default; staying under that means a clean close always
// wins without any compose configuration.
const shutdownTimeout = 8 * time.Second
