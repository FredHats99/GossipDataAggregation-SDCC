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
	"gossipdataaggregation-sdcc/internal/storage"
	"gossipdataaggregation-sdcc/internal/storage/wal"
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
	persistence    *storage.Store
	snapshotDone   chan struct{}
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
	persistence, err := storage.Open(storage.Config{
		DataDir:      cfg.DataDir,
		WALSyncMode:  wal.SyncMode(cfg.WALFsyncMode),
		WALBatchSize: cfg.WALFsyncBatch,
	})
	if err != nil {
		return nil, fmt.Errorf("open persistence store: %w", err)
	}
	if err := recoverAggregation(aggregation, persistence); err != nil {
		_ = persistence.Close()
		return nil, fmt.Errorf("recover aggregation state: %w", err)
	}
	aggregation.SetJournal(persistence)

	codec := transport.NewJSONCodec()
	deltaTransport, err := transport.NewUDPFrameTransport(":0", transport.DefaultMaxFrameSize)
	if err != nil {
		_ = persistence.Close()
		return nil, fmt.Errorf("create state delta UDP transport: %w", err)
	}
	deltaSender, err := transport.NewEncodingSender(deltaTransport, codec)
	if err != nil {
		_ = deltaTransport.Close()
		_ = persistence.Close()
		return nil, fmt.Errorf("create state delta sender: %w", err)
	}
	retryingSender, err := transport.NewRetryingSender(deltaSender, transport.DefaultRetryConfig())
	if err != nil {
		_ = deltaTransport.Close()
		_ = persistence.Close()
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
		NodeID:              cfg.NodeID,
		SelfEndpoint:        cfg.BindAddr,
		Peers:               cfg.SeedNodeList(),
		PeerProvider:        bootstrapper.AlivePeerEndpoints,
		Fanout:              cfg.Fanout,
		SendTimeout:         2 * time.Second,
		AntiEntropyInterval: cfg.AntiEntropyIntervalDuration(),
		DeltaHistorySize:    cfg.DeltaHistory,
		Logger:              logger,
	}, aggregation, retryingSender)
	if err != nil {
		_ = deltaTransport.Close()
		_ = persistence.Close()
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
		persistence:    persistence,
		snapshotDone:   make(chan struct{}),
	}, nil
}

func (a *App) Run(ctx context.Context) error {
	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()
	a.logger.Info(
		"starting application",
		"http_addr", a.cfg.HTTPAddr,
		"bind_addr", a.cfg.BindAddr,
		"seed_nodes", a.cfg.SeedNodeList(),
		"gossip_interval_ms", a.cfg.GossipInterval,
		"data_dir", a.cfg.DataDir,
		"wal_fsync_mode", a.cfg.WALFsyncMode,
		"snapshot_interval_seconds", a.cfg.SnapshotInterval,
		"fanout", a.cfg.Fanout,
	)

	if err := a.bootstrapper.StartJoinListener(runCtx); err != nil {
		_ = a.persistence.Close()
		return fmt.Errorf("membership listener failed: %w", err)
	}

	a.deltaRuntime.Start(runCtx)
	go a.runSnapshotLoop(runCtx)
	errCh := make(chan error, 1)
	go func() {
		if err := a.server.ListenAndServe(); err != nil {
			errCh <- err
		}
	}()
	go a.bootstrapper.JoinSeeds(runCtx)
	go a.bootstrapper.StartGossipLoop(runCtx)

	select {
	case <-ctx.Done():
		a.logger.Info("shutdown signal received")
		cancelRun()
		<-a.snapshotDone
		if err := a.shutdown(); err != nil {
			return fmt.Errorf("shutdown failed: %w", err)
		}
		a.logger.Info("application stopped")
		return nil
	case err := <-errCh:
		cancelRun()
		<-a.snapshotDone
		shutdownErr := a.shutdown()
		if errors.Is(err, http.ErrServerClosed) {
			if shutdownErr != nil {
				return fmt.Errorf("shutdown after server close: %w", shutdownErr)
			}
			return ErrServerClosed
		}
		class := ClassifyError(err)
		a.logger.Error("server terminated", "error", err.Error(), "error_class", class)
		if shutdownErr != nil {
			return errors.Join(fmt.Errorf("server failed: %w", err), fmt.Errorf("shutdown failed: %w", shutdownErr))
		}
		if class == ErrorRecoverable {
			return nil
		}
		return fmt.Errorf("server failed: %w", err)
	}
}

func (a *App) shutdown() error {
	a.health.SetReady(false)
	ctx, cancel := context.WithTimeout(context.Background(), a.cfg.ShutdownDuration())
	defer cancel()
	if err := a.server.Shutdown(ctx); err != nil {
		return err
	}
	if a.deltaTransport != nil {
		_ = a.deltaTransport.Close()
	}
	if a.persistence != nil {
		if err := a.aggregation.Checkpoint(a.persistence); err != nil {
			return err
		}
		if err := a.persistence.Close(); err != nil {
			return err
		}
	}
	a.aggregation.Close()
	return nil
}

func (a *App) runSnapshotLoop(ctx context.Context) {
	defer close(a.snapshotDone)
	ticker := time.NewTicker(a.cfg.SnapshotIntervalDuration())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := a.aggregation.Checkpoint(a.persistence); err != nil {
				a.logger.Error("periodic snapshot failed", "error", err.Error())
				continue
			}
			a.logger.Debug("periodic snapshot created")
		}
	}
}

func recoverAggregation(manager *pipeline.Manager, store *storage.Store) error {
	recovery, err := store.Recover()
	if err != nil {
		return err
	}
	if recovery.Checkpoint != nil {
		if err := manager.RestoreCheckpoint(*recovery.Checkpoint); err != nil {
			return err
		}
	}
	for _, mutation := range recovery.Mutations {
		if mutation.Delta != nil {
			if err := manager.ReplayDelta(*mutation.Delta); err != nil {
				return fmt.Errorf("replay WAL delta %d: %w", mutation.Index, err)
			}
			continue
		}
		if mutation.Snapshot != nil {
			if err := manager.ReplaySnapshot(*mutation.Snapshot); err != nil {
				return fmt.Errorf("replay WAL snapshot %d: %w", mutation.Index, err)
			}
		}
	}
	return nil
}
