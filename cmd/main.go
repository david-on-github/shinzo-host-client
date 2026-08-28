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

	// Run until SIGINT/SIGTERM (Ctrl-C, `docker stop`, systemd stop), then close
	// cleanly so the database flushes and the P2P host leaves the swarm.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	stop()

	fmt.Println("Shutting down...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := myHost.Close(shutdownCtx); err != nil {
		fmt.Fprintf(os.Stderr, "shutdown error: %v\n", err)
		os.Exit(1)
	}
}

// shutdownTimeout bounds a graceful stop. Docker gives a container 10s
// before SIGKILL by default; staying under that means a clean close always
// wins without any compose configuration.
const shutdownTimeout = 8 * time.Second
