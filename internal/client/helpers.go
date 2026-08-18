package client

import (
	"context"
	"errors"
	"net/http"
	"time"

	"zwidy/internal/config"
	"zwidy/internal/logging"
	"zwidy/internal/metrics"
)

func serverStartMetricsFunc(ctx context.Context, cfg *config.Config, reg *metrics.Registry, logger *logging.Logger) *http.Server {
	if !cfg.Metrics.Enabled {
		return nil
	}
	srv := &http.Server{Addr: cfg.Metrics.Address, Handler: reg.Handler()}
	go func() {
		logger.Info("metrics server started", map[string]any{"component": "metrics", "address": cfg.Metrics.Address})
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Warn("metrics server stopped", map[string]any{"component": "metrics", "error": err.Error()})
		}
	}()
	go func() {
		<-ctx.Done()
		serverShutdownHTTPFunc(srv)
	}()
	return srv
	}

func serverShutdownHTTPFunc(srv *http.Server) {
	if srv == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	}
