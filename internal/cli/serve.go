package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/HudoGriz/cephilis/internal/config"
	"github.com/HudoGriz/cephilis/internal/mount"
)

// scanner runs periodic space scans and keeps the last good result. Each scan
// runs in a child process: if CephFS hangs, the child blocks in D state and
// cannot be killed, so no new scan is started while one is still running and
// the stuck time is exported instead.
type scanner struct {
	run      func(ctx context.Context) ([]byte, error)
	timeout  time.Duration
	mu       sync.Mutex
	last     []byte
	lastOK   time.Time
	failures int
	running  time.Time // zero when idle
}

func (s *scanner) tick() {
	s.mu.Lock()
	if !s.running.IsZero() {
		s.mu.Unlock()
		return
	}
	s.running = time.Now()
	s.mu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
		out, err := s.run(ctx)
		cancel()
		s.mu.Lock()
		defer s.mu.Unlock()
		s.running = time.Time{}
		if err != nil {
			s.failures++
			slog.Error("space scan failed", "err", err)
			return
		}
		s.last, s.lastOK = out, time.Now()
	}()
}

func (s *scanner) write(w io.Writer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w.Write(s.last) //nolint:errcheck // client went away
	p := config.MetricsPrefix
	running := 0.0
	if !s.running.IsZero() {
		running = time.Since(s.running).Seconds()
	}
	last := int64(0)
	if !s.lastOK.IsZero() {
		last = s.lastOK.Unix()
	}
	fmt.Fprintf(w, "# HELP %s_serve_last_success_timestamp_seconds Time of the last successful space scan.\n# TYPE %s_serve_last_success_timestamp_seconds gauge\n%s_serve_last_success_timestamp_seconds %d\n", p, p, p, last)
	fmt.Fprintf(w, "# HELP %s_serve_scan_running_seconds How long the current scan has been running; grows without bound if CephFS hangs.\n# TYPE %s_serve_scan_running_seconds gauge\n%s_serve_scan_running_seconds %.3f\n", p, p, p, running)
	fmt.Fprintf(w, "# HELP %s_serve_scan_failures_total Space scans that failed or timed out.\n# TYPE %s_serve_scan_failures_total counter\n%s_serve_scan_failures_total %d\n", p, p, p, s.failures)
}

func runServe(args []string, stdout, stderr io.Writer) error {
	if hasHelpArg(args) {
		fmt.Fprintln(stdout, "Usage: cephilis serve [--listen ADDR] [--interval DUR] [--timeout DUR] [--config-dir DIR] [--health=false]")
		return nil
	}
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	listen := fs.String("listen", ":9966", "HTTP listen address")
	interval := fs.Duration("interval", time.Hour, "Time between space scans")
	timeout := fs.Duration("timeout", 15*time.Minute, "Kill a space scan after this long")
	configDir := fs.String("config-dir", "", "Config directory")
	withHealth := fs.Bool("health", true, "Also export client health (mounts.yaml) on every scrape")
	if err := fs.Parse(args); err != nil {
		return err
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	// Fail early on a broken config instead of serving empty metrics.
	if _, err := loadSpace("", *configDir); err != nil {
		return err
	}
	var mounts []config.Mount
	if *withHealth {
		mcfg, err := loadMounts("", *configDir)
		if err != nil {
			return fmt.Errorf("--health needs mounts.yaml (or pass --health=false): %w", err)
		}
		mounts = mcfg.Mounts
	}

	scanArgs := []string{"space", "--format", "prom"}
	if *configDir != "" {
		scanArgs = append(scanArgs, "--config-dir", *configDir)
	}
	s := &scanner{timeout: *timeout, run: func(ctx context.Context) ([]byte, error) {
		cmd := exec.CommandContext(ctx, exe, scanArgs...)
		cmd.Stderr = stderr
		return cmd.Output()
	}}
	go func() {
		s.tick()
		for range time.Tick(*interval) {
			s.tick()
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		s.write(w)
		if len(mounts) > 0 {
			// D-safe: reads mountinfo and debugfs only, never the mount.
			io.WriteString(w, healthProm(mount.CheckHealthAll(mounts))) //nolint:errcheck
		}
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, "cephilis "+Version+" — metrics at /metrics\n") //nolint:errcheck
	})
	slog.Info("serving", "listen", *listen, "interval", *interval, "health", *withHealth, "version", Version)
	srv := &http.Server{Addr: *listen, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	return srv.ListenAndServe()
}
