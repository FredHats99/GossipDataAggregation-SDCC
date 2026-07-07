package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"gossipdataaggregation-sdcc/internal/aggregation/pipeline"
	"gossipdataaggregation-sdcc/internal/api"
	"gossipdataaggregation-sdcc/internal/config"
	deltagossip "gossipdataaggregation-sdcc/internal/gossip/delta"
	"gossipdataaggregation-sdcc/internal/gossip/transport"
	"gossipdataaggregation-sdcc/internal/membership"
	"gossipdataaggregation-sdcc/internal/observability/logging"
)

var ErrServerClosed = errors.New("http server closed")

type App struct {
	cfg            config.Config
	logger         *slog.Logger
	server         *http.Server
	health         *api.HealthHandler
	bootstrapper   *membership.Bootstrapper
	aggregation    *pipeline.Manager
	deltaRuntime   *deltagossip.Runtime
	deltaTransport *transport.UDPFrameTransport
}

func New(cfg config.Config) (*App, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	logger := logging.NewJSONLogger(cfg.NodeID, cfg.LogLevel)
	aggregation, err := pipeline.New(pipeline.Config{
		NodeID:            cfg.NodeID,
		TopKMax:           cfg.TopKMax,
		OutboundQueueSize: cfg.OutboundQueue,
	})
	if err != nil {
		return nil, fmt.Errorf("create aggregation pipeline: %w", err)
	}

	codec := transport.NewJSONCodec()
	deltaTransport, err := transport.NewUDPFrameTransport(":0", transport.DefaultMaxFrameSize)
	if err != nil {
		return nil, fmt.Errorf("create state delta UDP transport: %w", err)
	}
	deltaSender, err := transport.NewEncodingSender(deltaTransport, codec)
	if err != nil {
		_ = deltaTransport.Close()
		return nil, fmt.Errorf("create state delta sender: %w", err)
	}
	retryingSender, err := transport.NewRetryingSender(deltaSender, transport.DefaultRetryConfig())
	if err != nil {
		_ = deltaTransport.Close()
		return nil, fmt.Errorf("create retrying state delta sender: %w", err)
	}

	bootstrapper := membership.NewBootstrapper(
		cfg.NodeID,
		cfg.BindAddr,
		cfg.SeedNodeList(),
		cfg.GossipIntervalDuration(),
		cfg.Fanout,
		logger,
	)
	deltaRuntime, err := deltagossip.NewRuntime(deltagossip.Config{
		NodeID:       cfg.NodeID,
		SelfEndpoint: cfg.BindAddr,
		Peers:        cfg.SeedNodeList(),
		Fanout:       cfg.Fanout,
		SendTimeout:  2 * time.Second,
		Logger:       logger,
	}, aggregation, retryingSender)
	if err != nil {
		_ = deltaTransport.Close()
		return nil, fmt.Errorf("create state delta runtime: %w", err)
	}
	bootstrapper.SetEnvelopeHandler(deltaRuntime.HandleEnvelope)

	mux := http.NewServeMux()
	health := api.NewHealthHandler()
	health.Register(mux)
	members := api.NewMembersHandler(bootstrapper.MembersSnapshot)
	members.Register(mux)
	aggregates := api.NewAggregationHandler(aggregation)
	aggregates.Register(mux)

	server := &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: mux,
	}

	return &App{
		cfg:            cfg,
		logger:         logger,
		server:         server,
		health:         health,
		bootstrapper:   bootstrapper,
		aggregation:    aggregation,
		deltaRuntime:   deltaRuntime,
		deltaTransport: deltaTransport,
	}, nil
}

func (a *App) Run(ctx context.Context) error {
	a.logger.Info(
		"starting application",
		"http_addr", a.cfg.HTTPAddr,
		"bind_addr", a.cfg.BindAddr,
		"seed_nodes", a.cfg.SeedNodeList(),
		"gossip_interval_ms", a.cfg.GossipInterval,
		"fanout", a.cfg.Fanout,
	)

	if err := a.bootstrapper.StartJoinListener(ctx); err != nil {
		return fmt.Errorf("membership listener failed: %w", err)
	}

	a.deltaRuntime.Start(ctx)
	errCh := make(chan error, 1)
	go func() {
		if err := a.server.ListenAndServe(); err != nil {
			errCh <- err
		}
	}()
	go a.bootstrapper.JoinSeeds(ctx)
	go a.bootstrapper.StartGossipLoop(ctx)

	select {
	case <-ctx.Done():
		a.logger.Info("shutdown signal received")
		if err := a.shutdown(); err != nil {
			return fmt.Errorf("shutdown failed: %w", err)
		}
		a.logger.Info("application stopped")
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return ErrServerClosed
		}
		class := ClassifyError(err)
		a.logger.Error("server terminated", "error", err.Error(), "error_class", class)
		if class == ErrorRecoverable {
			return nil
		}
		return fmt.Errorf("server failed: %w", err)
	}
}

func (a *App) shutdown() error {
	a.health.SetReady(false)
	a.aggregation.Close()
	if a.deltaTransport != nil {
		_ = a.deltaTransport.Close()
	}
	ctx, cancel := context.WithTimeout(context.Background(), a.cfg.ShutdownDuration())
	defer cancel()
	return a.server.Shutdown(ctx)
}
