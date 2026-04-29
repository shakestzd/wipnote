package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/shakestzd/htmlgraph/internal/otel"
	"github.com/shakestzd/htmlgraph/internal/otel/adapter"
	"github.com/shakestzd/htmlgraph/internal/otel/sink/ndjson"
	"github.com/spf13/cobra"
)

const defaultIdleTimeout = 5 * time.Minute

func otelCollectCmd() *cobra.Command {
	var (
		sessionID  string
		projectDir string
		listen     string
	)
	cmd := &cobra.Command{
		Use:    "otel-collect",
		Hidden: true,
		Short:  "Internal: per-session OTel collector (do not invoke directly)",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runOtelCollect(sessionID, projectDir, listen)
		},
	}
	cmd.Flags().StringVar(&sessionID, "session-id", "", "Session ULID (required)")
	cmd.Flags().StringVar(&projectDir, "project-dir", "", "Project root (required)")
	cmd.Flags().StringVar(&listen, "listen", "127.0.0.1:0", "Listen address (default ephemeral)")
	_ = cmd.MarkFlagRequired("session-id")
	_ = cmd.MarkFlagRequired("project-dir")
	return cmd
}

func runOtelCollect(sessionID, projectDir, listenAddr string) error {
	sessDir := filepath.Join(projectDir, ".htmlgraph", "sessions", sessionID)
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		return fmt.Errorf("create session dir: %w", err)
	}

	snk, err := ndjson.New(projectDir, sessionID)
	if err != nil {
		return fmt.Errorf("create ndjson sink: %w", err)
	}
	defer snk.Close()

	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", listenAddr, err)
	}
	port := ln.Addr().(*net.TCPAddr).Port

	lastActivity := &atomic.Int64{}
	lastActivity.Store(time.Now().UnixMilli())

	mux := buildCollectorMux(snk, lastActivity)
	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	if _, err := fmt.Fprintf(os.Stdout, "htmlgraph-otel-ready port=%d\n", port); err != nil {
		return fmt.Errorf("write handshake: %w", err)
	}
	_ = os.Stdout.Sync()

	if err := writeCollectorStartEvent(snk, sessionID, port); err != nil {
		fmt.Fprintf(os.Stderr, "collector_start event: %v\n", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "otel-collect serve: %v\n", err)
		}
	}()

	return awaitShutdown(ctx, cancel, srv, snk, lastActivity)
}

// buildCollectorMux creates the OTLP HTTP mux with activity tracking.
func buildCollectorMux(snk *ndjson.Sink, lastActivity *atomic.Int64) *http.ServeMux {
	reg := adapter.NewRegistry()
	reg.Register(adapter.NewClaudeAdapter())
	reg.Register(adapter.NewCodexAdapter())
	reg.Register(adapter.NewGeminiAdapter())

	handler := &collectorHandler{
		registry:     reg,
		sink:         snk,
		lastActivity: lastActivity,
	}

	mux := http.NewServeMux()
	mux.Handle("/v1/traces", handler)
	mux.Handle("/v1/metrics", handler)
	mux.Handle("/v1/logs", handler)
	return mux
}

// writeCollectorStartEvent writes the collector_start NDJSON line.
func writeCollectorStartEvent(snk *ndjson.Sink, sessionID string, port int) error {
	attrs := map[string]any{
		"htmlgraph_sid": sessionID,
		"pid":           os.Getpid(),
		"port":          port,
	}

	sig := otel.UnifiedSignal{
		Harness:       "htmlgraph",
		SignalID:      "collector-start-" + sessionID,
		Kind:          "collector_start",
		CanonicalName: "collector_start",
		NativeName:    "collector_start",
		Timestamp:     time.Now().UTC(),
		SessionID:     sessionID,
		RawAttrs:      attrs,
	}
	return snk.WriteBatch(context.Background(), "htmlgraph", nil, []otel.UnifiedSignal{sig})
}

// awaitShutdown blocks until SIGTERM or idle timeout, then gracefully shuts down.
func awaitShutdown(ctx context.Context, cancel context.CancelFunc, srv *http.Server, snk *ndjson.Sink, lastActivity *atomic.Int64) error {
	idleTimeout := parseIdleTimeout()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-sigCh:
			cancel()
			return gracefulShutdown(srv, snk)
		case <-ticker.C:
			elapsed := time.Since(time.UnixMilli(lastActivity.Load()))
			if elapsed >= idleTimeout {
				cancel()
				return gracefulShutdown(srv, snk)
			}
		case <-ctx.Done():
			return gracefulShutdown(srv, snk)
		}
	}
}

func gracefulShutdown(srv *http.Server, snk *ndjson.Sink) error {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	return snk.Close()
}

func parseIdleTimeout() time.Duration {
	if s := os.Getenv("HTMLGRAPH_OTEL_IDLE_TIMEOUT"); s != "" {
		if d, err := time.ParseDuration(s); err == nil && d > 0 {
			return d
		}
		if ms, err := strconv.Atoi(s); err == nil && ms > 0 {
			return time.Duration(ms) * time.Millisecond
		}
	}
	return defaultIdleTimeout
}
