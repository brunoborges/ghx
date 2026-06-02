package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/brunoborges/ghx/src/internal/client"
	"github.com/brunoborges/ghx/src/internal/config"
	execctx "github.com/brunoborges/ghx/src/internal/context"
	"github.com/brunoborges/ghx/src/internal/ghcli"
	"github.com/brunoborges/ghx/src/internal/protocol"
)

var version = "dev"

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ghx: warning: config load: %v\n", err)
	}

	args := os.Args[1:]
	if len(args) == 0 {
		mustResolveGH(cfg)
		execDirect(cfg.GHPath, nil)
		return
	}

	// Handle ghx-specific subcommands (x-prefixed to avoid conflicts with gh)
	if handled := handleSubcommand(cfg, args); handled {
		return
	}

	// Parse ghx flags (before the gh args)
	ghArgs, noCache, ttlOverride := parseGHXFlags(args)

	if len(ghArgs) == 0 {
		mustResolveGH(cfg)
		execDirect(cfg.GHPath, nil)
		return
	}

	// Resolve real gh binary (lazy — only on execution path)
	mustResolveGH(cfg)

	// Resolve execution context
	ctx := execctx.Resolve(cfg.GHPath)

	// Connect to daemon, auto-starting if needed
	cl := client.New(cfg.SocketPath)
	if ready, err := ensureDaemon(cfg, cl); !ready {
		if err != nil {
			fmt.Fprintf(os.Stderr, "ghx: %v (falling back to direct gh)\n", err)
		}
		execDirect(cfg.GHPath, ghArgs)
		return
	}

	// Send request to daemon
	workDir, _ := os.Getwd()
	req := &protocol.Request{
		Type:        protocol.TypeExec,
		Args:        ghArgs,
		Context:     ctx,
		WorkDir:     workDir,
		NoCache:     noCache,
		TTLOverride: ttlOverride,
	}

	resp, err := cl.Send(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ghx: daemon error: %v (falling back to direct gh)\n", err)
		execDirect(cfg.GHPath, ghArgs)
		return
	}

	if len(resp.Stdout) > 0 {
		os.Stdout.Write(resp.Stdout)
	}
	if len(resp.Stderr) > 0 {
		os.Stderr.Write(resp.Stderr)
	}
	os.Exit(resp.ExitCode)
}

// handleSubcommand handles ghx-specific subcommands. Returns true if handled.
func handleSubcommand(cfg *config.Config, args []string) bool {
	switch args[0] {
	case "xversion":
		fmt.Printf("ghx version %s\n", version)
	case "xhelp":
		printHelp(cfg)
	case "xdaemon":
		handleDaemon(cfg, args[1:])
	case "xcache":
		handleCache(cfg, args[1:])
	case "ghcli":
		handleGH(cfg, args[1:])
	default:
		return false
	}
	return true
}

func printHelp(cfg *config.Config) {
	fmt.Println("ghx — GitHub CLI Cache Proxy")
	fmt.Printf("Version: %s\n", version)
	fmt.Println()
	fmt.Println("Usage: ghx [flags] <gh command> [args...]")
	fmt.Println()
	fmt.Println("Flags:")
	fmt.Println("  --no-cache    Bypass cache for this request")
	fmt.Println("  --ttl <sec>   Override TTL for this request")
	fmt.Println()
	fmt.Println("Commands (x-prefixed to avoid conflicts with gh):")
	fmt.Println("  xversion      Show ghx version")
	fmt.Println("  xhelp         Show this help")
	fmt.Println("  xdaemon       Manage the ghxd daemon (start|stop|status|restart)")
	fmt.Println("  xcache        Manage the cache (stats|flush|keys)")
	fmt.Println("  ghcli         Manage the GitHub CLI binary (status|upgrade)")
	fmt.Println()
	fmt.Println("All other arguments are forwarded to gh via the caching daemon.")
	fmt.Printf("Config: ~/.ghx/\n")
	fmt.Printf("Dashboard: http://127.0.0.1:%d/\n", cfg.DashboardPort)
}

// parseGHXFlags extracts ghx-specific flags from the argument list.
// It stops at the first unrecognized argument, treating the rest as gh args.
func parseGHXFlags(args []string) (ghArgs []string, noCache bool, ttlOverride int) {
	noCache = os.Getenv("GHX_NO_CACHE") == "1"

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--no-cache":
			noCache = true
		case "--ttl":
			if i+1 < len(args) {
				i++
				if v, err := strconv.Atoi(args[i]); err == nil {
					ttlOverride = v
				}
			}
		default:
			ghArgs = append(ghArgs, args[i:]...)
			return
		}
	}
	return
}

// ensureDaemon makes sure the daemon is running, auto-starting it if configured.
// Returns (true, nil) if daemon is ready, (false, nil) if auto-start is disabled,
// or (false, err) if auto-start failed.
func ensureDaemon(cfg *config.Config, cl *client.Client) (bool, error) {
	if cl.IsRunning() {
		return true, nil
	}
	if !cfg.AutoStart {
		return false, nil
	}

	if err := startDaemon(cfg); err != nil {
		return false, fmt.Errorf("daemon auto-start failed: %v", err)
	}

	// Wait for daemon to be ready
	for i := 0; i < 20; i++ {
		if cl.IsRunning() {
			return true, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false, fmt.Errorf("daemon failed to start in time")
}

func handleDaemon(cfg *config.Config, args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: ghx xdaemon <start|stop|status|restart>")
		os.Exit(1)
	}

	switch args[0] {
	case "start":
		handleDaemonStart(cfg, args[1:])

	case "stop":
		cl := client.New(cfg.SocketPath)
		if !cl.IsRunning() {
			fmt.Println("ghx: daemon is not running")
			return
		}
		resp, err := cl.Send(&protocol.Request{Type: protocol.TypeShutdown})
		if err != nil {
			fmt.Fprintf(os.Stderr, "ghx: stop failed: %v\n", err)
			os.Exit(1)
		}
		os.Stdout.Write(resp.Stdout)

	case "status":
		cl := client.New(cfg.SocketPath)
		if !cl.IsRunning() {
			fmt.Println("ghx: daemon is not running")
			os.Exit(1)
		}
		resp, err := cl.Send(&protocol.Request{Type: protocol.TypeStats})
		if err != nil {
			fmt.Fprintf(os.Stderr, "ghx: %v\n", err)
			os.Exit(1)
		}
		printFormattedStats(resp.Stdout)

	case "restart":
		handleDaemon(cfg, []string{"stop"})
		time.Sleep(500 * time.Millisecond)
		handleDaemon(cfg, []string{"start", "-d"})

	default:
		fmt.Fprintf(os.Stderr, "ghx: unknown daemon command: %s\n", args[0])
		os.Exit(1)
	}
}

func handleDaemonStart(cfg *config.Config, args []string) {
	if isDetachMode(args) {
		if err := startDaemon(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "ghx: failed to start daemon: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("ghx: daemon started (socket: %s, dashboard: http://127.0.0.1:%d/)\n", cfg.SocketPath, cfg.DashboardPort)
		return
	}
	// Foreground mode — exec the daemon directly
	ghxdPath, err := findGHXD()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ghx: %v\n", err)
		os.Exit(1)
	}
	execReplace(ghxdPath, []string{"ghxd"}, os.Environ())
}

// isDetachMode returns true if args contain a detach flag.
func isDetachMode(args []string) bool {
	for _, a := range args {
		if a == "-d" || a == "--detach" {
			return true
		}
	}
	return false
}

func handleCache(cfg *config.Config, args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: ghx xcache <stats|flush|keys>")
		os.Exit(1)
	}

	cl := client.New(cfg.SocketPath)
	if !cl.IsRunning() {
		fmt.Println("ghx: daemon is not running")
		os.Exit(1)
	}

	switch args[0] {
	case "stats":
		resp, err := cl.Send(&protocol.Request{Type: protocol.TypeStats})
		if err != nil {
			fmt.Fprintf(os.Stderr, "ghx: %v\n", err)
			os.Exit(1)
		}
		printFormattedStats(resp.Stdout)

	case "flush":
		resp, err := cl.Send(&protocol.Request{Type: protocol.TypeFlush})
		if err != nil {
			fmt.Fprintf(os.Stderr, "ghx: %v\n", err)
			os.Exit(1)
		}
		os.Stdout.Write(resp.Stdout)

	case "keys":
		resp, err := cl.Send(&protocol.Request{Type: protocol.TypeKeys})
		if err != nil {
			fmt.Fprintf(os.Stderr, "ghx: %v\n", err)
			os.Exit(1)
		}
		var keys []string
		json.Unmarshal(resp.Stdout, &keys)
		for _, k := range keys {
			fmt.Println(k)
		}

	default:
		fmt.Fprintf(os.Stderr, "ghx: unknown cache command: %s\n", args[0])
		os.Exit(1)
	}
}

func printFormattedStats(data []byte) {
	var stats struct {
		Uptime       string                     `json:"uptime"`
		Total        int64                      `json:"total"`
		Hits         int64                      `json:"hits"`
		Misses       int64                      `json:"misses"`
		Passthrough  int64                      `json:"passthrough"`
		Coalesced    int64                      `json:"coalesced"`
		HitRate      float64                    `json:"hit_rate"`
		CacheSize    int                        `json:"cache_size"`
		MaxCacheSize int                        `json:"max_cache_size"`
		Commands     map[string]json.RawMessage `json:"commands"`
	}
	if err := json.Unmarshal(data, &stats); err != nil {
		os.Stdout.Write(data)
		return
	}

	fmt.Printf("Uptime:          %s\n", stats.Uptime)
	fmt.Printf("Total Requests:  %d\n", stats.Total)
	fmt.Printf("Cache Hits:      %d (%.1f%%)\n", stats.Hits, stats.HitRate)
	fmt.Printf("Cache Misses:    %d\n", stats.Misses)
	fmt.Printf("Passthrough:     %d\n", stats.Passthrough)
	fmt.Printf("Coalesced:       %d\n", stats.Coalesced)
	fmt.Printf("Cache Size:      %d / %d entries\n", stats.CacheSize, stats.MaxCacheSize)

	if len(stats.Commands) > 0 {
		fmt.Println("\nTop Commands:")
		for name, raw := range stats.Commands {
			var cmd struct {
				Hits   int64 `json:"hits"`
				Misses int64 `json:"misses"`
			}
			json.Unmarshal(raw, &cmd)
			total := cmd.Hits + cmd.Misses
			rate := 0.0
			if total > 0 {
				rate = float64(cmd.Hits) / float64(total) * 100
			}
			fmt.Printf("  %-24s %d hits / %d misses  (%.1f%%)\n",
				strings.ReplaceAll(name, "_", " "), cmd.Hits, cmd.Misses, rate)
		}
	}
}

// findGHXD locates the ghxd binary.
func findGHXD() (string, error) {
	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}

	// Check next to the ghx binary
	exe, err := os.Executable()
	if err == nil {
		dir := filepath.Dir(exe)
		candidate := filepath.Join(dir, "ghxd"+suffix)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}

	// Check PATH
	path, err := exec.LookPath("ghxd" + suffix)
	if err != nil {
		return "", fmt.Errorf("ghxd not found (install it next to ghx or in PATH)")
	}
	return path, nil
}

// mustResolveGH resolves the real gh binary path, updating cfg.GHPath.
// Exits with an error if gh cannot be found or downloaded.
// Also checks staleness for managed binaries.
func mustResolveGH(cfg *config.Config) {
	resolved, err := ghcli.ResolveGHPath(cfg.GHPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ghx: %v\n", err)
		os.Exit(1)
	}
	cfg.GHPath = resolved
	ghcli.CheckStaleness(resolved)
}

func handleGH(cfg *config.Config, args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: ghx ghcli <status|upgrade>")
		fmt.Println()
		fmt.Println("  status    Show which gh binary is in use and its version")
		fmt.Println("  upgrade   Download the latest GitHub CLI to ~/.ghx/bin/gh")
		os.Exit(1)
	}

	switch args[0] {
	case "status":
		handleGHStatus(cfg)
	case "upgrade":
		handleGHUpgrade()
	default:
		fmt.Fprintf(os.Stderr, "ghx: unknown ghcli command: %s\n", args[0])
		os.Exit(1)
	}
}

func handleGHStatus(cfg *config.Config) {
	resolved, err := ghcli.ResolveGHPath(cfg.GHPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ghx: %v\n", err)
		os.Exit(1)
	}

	managed := ghcli.IsManagedGH(resolved)
	source := "PATH"
	if cfg.GHPath != "" && cfg.GHPath != "gh" {
		source = "config (gh_path)"
	} else if managed {
		source = "managed (~/.ghx/bin/gh)"
	}

	fmt.Printf("gh binary:  %s\n", resolved)
	fmt.Printf("source:     %s\n", source)

	ver, err := ghcli.InstalledVersion(resolved)
	if err != nil {
		fmt.Printf("version:    unknown (%v)\n", err)
	} else {
		fmt.Printf("version:    %s\n", ver)
	}

	if managed {
		info, err := os.Stat(resolved)
		if err == nil {
			age := time.Since(info.ModTime())
			days := int(age.Hours() / 24)
			fmt.Printf("installed:  %d days ago\n", days)
		}
	}
}

func handleGHUpgrade() {
	managed := ghcli.ManagedGHPath()
	if managed == "" {
		fmt.Fprintf(os.Stderr, "ghx: cannot determine managed gh path\n")
		os.Exit(1)
	}

	ver, err := ghcli.Upgrade(managed)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ghx: upgrade failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("ghx: GitHub CLI upgraded to v%s\n", ver)
}
