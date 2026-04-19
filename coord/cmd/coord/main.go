// Package main is the entry point for the Canopy coordination server.
// It runs an HTTP API server, a STUN server, and a TURN relay.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/canopy-dev/coord/internal/api"
	"github.com/canopy-dev/coord/internal/store"
	"github.com/canopy-dev/coord/internal/stun"
	"github.com/canopy-dev/coord/internal/turn"
)

// storePairingChecker adapts the store to the turn.PairingChecker interface.
type storePairingChecker struct {
	store *store.Store
}

func (c *storePairingChecker) CanRelay(deviceKey, peerKey string) bool {
	return c.store.CanLookup(deviceKey, peerKey)
}

func main() {
	logger, err := zap.NewProduction()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	httpAddr := envOrDefault("HTTP_ADDR", ":8080")
	stunAddr := envOrDefault("STUN_ADDR", ":3478")
	turnAddr := envOrDefault("TURN_ADDR", ":3479")

	st := store.New()

	// Root context for background workers, cancelled on shutdown.
	rootCtx, stopRoot := context.WithCancel(context.Background())
	defer stopRoot()

	// Periodic cleanup of stale devices and expired pairing sessions. Pairing
	// sessions have a 5-minute TTL, so we sweep them on the same cadence. Both
	// maps would otherwise grow unbounded under repeated pairing attempts.
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-rootCtx.Done():
				return
			case <-ticker.C:
				if removed := st.Cleanup(10 * time.Minute); removed > 0 {
					logger.Info("cleaned stale devices", zap.Int("removed", removed))
				}
				if removed := st.CleanupPairingSessions(); removed > 0 {
					logger.Info("cleaned expired pairing sessions", zap.Int("removed", removed))
				}
			}
		}
	}()

	// Start STUN server.
	stunSrv := stun.New(logger)
	if err := stunSrv.ListenAndServe(stunAddr); err != nil {
		logger.Fatal("stun server failed", zap.Error(err))
	}
	logger.Info("stun server listening", zap.String("addr", stunAddr))

	// Start TURN relay.
	checker := &storePairingChecker{store: st}
	turnRelay := turn.New(checker, logger)
	if err := turnRelay.ListenAndServe(turnAddr); err != nil {
		logger.Fatal("turn relay failed", zap.Error(err))
	}
	logger.Info("turn relay listening", zap.String("addr", turnAddr))

	// Start HTTP API server.
	apiSrv := api.New(st, logger)
	httpSrv := &http.Server{
		Addr:         httpAddr,
		Handler:      apiSrv.Handler(),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		logger.Info("http server listening", zap.String("addr", httpAddr))
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("http server failed", zap.Error(err))
		}
	}()

	// Wait for shutdown signal.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	logger.Info("shutting down...")

	// Cancel background workers (device/pairing cleanup).
	stopRoot()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := httpSrv.Shutdown(ctx); err != nil {
		logger.Error("http shutdown error", zap.Error(err))
	}

	if err := stunSrv.Close(); err != nil {
		logger.Error("stun shutdown error", zap.Error(err))
	}

	if err := turnRelay.Close(); err != nil {
		logger.Error("turn shutdown error", zap.Error(err))
	}

	stats := turnRelay.Stats()
	logger.Info("turn relay stats",
		zap.Int64("total_allocations", stats.TotalAllocations),
		zap.Int64("total_bytes_relayed", stats.TotalBytesRelayed),
	)

	logger.Info("shutdown complete")
}

func envOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
