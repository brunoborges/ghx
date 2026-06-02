package main

import (
	"fmt"
	"log"
	"os"

	"github.com/brunoborges/ghx/src/internal/config"
	"github.com/brunoborges/ghx/src/internal/daemon"
	"github.com/brunoborges/ghx/src/internal/ghcli"
)

var version = "dev"

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Printf("warning: config load: %v (using defaults)", err)
	}

	// Setup logging
	log.SetPrefix("ghxd: ")
	log.SetFlags(log.Ldate | log.Ltime | log.Lmsgprefix)

	if cfg.LogFile != "" {
		f, err := os.OpenFile(cfg.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			log.Printf("warning: cannot open log file %s: %v (logging to stderr)", cfg.LogFile, err)
		} else {
			log.SetOutput(f)
			defer f.Close()
		}
	}

	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Printf("ghxd version %s\n", version)
		os.Exit(0)
	}

	if len(os.Args) > 1 && (os.Args[1] == "--help" || os.Args[1] == "-h") {
		fmt.Println("ghxd — GitHub CLI Cache Proxy Daemon")
		fmt.Printf("Version: %s\n", version)
		fmt.Println()
		fmt.Println("Usage: ghxd [--help | --version]")
		fmt.Println()
		fmt.Println("Options:")
		fmt.Println("  --help, -h       Show this help")
		fmt.Println("  --version, -v    Show version")
		fmt.Println()
		fmt.Printf("  Socket:    %s\n", cfg.SocketPath)
		fmt.Printf("  Dashboard: http://127.0.0.1:%d/\n", cfg.DashboardPort)
		fmt.Printf("  PID file:  %s\n", cfg.PIDFile)
		fmt.Printf("  Log file:  %s\n", cfg.LogFile)
		fmt.Println()
		fmt.Println("The daemon is typically managed via: ghx xdaemon start|stop|status|restart")
		os.Exit(0)
	}

	// Resolve real gh binary before starting the server
	resolved, err := ghcli.ResolveGHPath(cfg.GHPath)
	if err != nil {
		log.Fatalf("fatal: cannot find GitHub CLI (gh): %v", err)
	}

	srv := daemon.NewServer(cfg, version, resolved)
	if err := srv.Run(); err != nil {
		log.Fatalf("fatal: %v", err)
	}
}
