package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sync"

	"gossipdataaggregation-sdcc/internal/gossip/protocol"
	"gossipdataaggregation-sdcc/internal/storage/snapshot"
	"gossipdataaggregation-sdcc/internal/storage/wal"
)

const (
	recordDelta    = "state_delta"
	recordSnapshot = "state_snapshot"
)

var ErrInvalidRecoveryRecord = errors.New("storage: invalid recovery record")

type Config struct {
	DataDir      string
	WALSyncMode  wal.SyncMode
	WALBatchSize int
}

type RecoveredMutation struct {
	Index    uint64
	Delta    *protocol.StateDelta
	Snapshot *protocol.SnapshotResp
}

type Recovery struct {
	Checkpoint *protocol.SnapshotResp
	Mutations  []RecoveredMutation
	WALIndex   uint64
}

type Store struct {
	mu        sync.Mutex
	wal       *wal.Log
	snapshots *snapshot.Store
	closed    bool
}

func Open(config Config) (*Store, error) {
	log, err := wal.Open(wal.Config{
		Dir:       filepath.Join(config.DataDir, "wal"),
		SyncMode:  config.WALSyncMode,
		BatchSize: config.WALBatchSize,
	})
	if err != nil {
		return nil, err
	}
	snapshots, err := snapshot.NewStore(filepath.Join(config.DataDir, "snapshots"))
	if err != nil {
		_ = log.Close()
		return nil, err
	}
	return &Store{wal: log, snapshots: snapshots}, nil
}

func (s *Store) AppendDelta(delta protocol.StateDelta) error {
	raw, err := json.Marshal(delta)
	if err != nil {
		return fmt.Errorf("encode delta mutation: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return wal.ErrClosed
	}
	_, err = s.wal.Append(recordDelta, raw)
	return err
}

func (s *Store) AppendSnapshot(state protocol.SnapshotResp) error {
	raw, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("encode snapshot mutation: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return wal.ErrClosed
	}
	_, err = s.wal.Append(recordSnapshot, raw)
	return err
}

func (s *Store) SaveCheckpoint(state protocol.SnapshotResp) error {
	raw, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("encode checkpoint: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return wal.ErrClosed
	}
	if err := s.wal.Sync(); err != nil {
		return err
	}
	_, err = s.snapshots.Save(s.wal.LastIndex(), raw)
	return err
}

func (s *Store) Recover() (Recovery, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Recovery{}, wal.ErrClosed
	}

	var recovery Recovery
	checkpoint, found, err := s.snapshots.LoadLatest()
	if err != nil {
		return Recovery{}, err
	}
	if found {
		var state protocol.SnapshotResp
		if err := json.Unmarshal(checkpoint.Payload, &state); err != nil {
			return Recovery{}, fmt.Errorf("decode checkpoint: %w", err)
		}
		if _, err := protocol.DecodeSnapshotResp(checkpoint.Payload); err != nil {
			return Recovery{}, fmt.Errorf("validate checkpoint: %w", err)
		}
		if checkpoint.Metadata.WALIndex > s.wal.LastIndex() {
			return Recovery{}, ErrInvalidRecoveryRecord
		}
		recovery.Checkpoint = &state
		recovery.WALIndex = checkpoint.Metadata.WALIndex
	}

	records, err := s.wal.RecordsAfter(recovery.WALIndex)
	if err != nil {
		return Recovery{}, err
	}
	for _, record := range records {
		mutation := RecoveredMutation{Index: record.Index}
		switch record.Type {
		case recordDelta:
			var delta protocol.StateDelta
			if err := json.Unmarshal(record.Payload, &delta); err != nil {
				return Recovery{}, fmt.Errorf("%w at WAL index %d: %v", ErrInvalidRecoveryRecord, record.Index, err)
			}
			mutation.Delta = &delta
		case recordSnapshot:
			var state protocol.SnapshotResp
			if err := json.Unmarshal(record.Payload, &state); err != nil {
				return Recovery{}, fmt.Errorf("%w at WAL index %d: %v", ErrInvalidRecoveryRecord, record.Index, err)
			}
			mutation.Snapshot = &state
		default:
			return Recovery{}, fmt.Errorf("%w at WAL index %d: unknown type %q", ErrInvalidRecoveryRecord, record.Index, record.Type)
		}
		recovery.Mutations = append(recovery.Mutations, mutation)
		recovery.WALIndex = record.Index
	}
	return recovery, nil
}

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	return s.wal.Close()
}
