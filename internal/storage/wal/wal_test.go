package wal

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLogAppendReopenAndReplay(t *testing.T) {
	dir := t.TempDir()
	log, err := Open(Config{Dir: dir, SyncMode: SyncAlways})
	if err != nil {
		t.Fatalf("open WAL: %v", err)
	}
	for _, payload := range [][]byte{[]byte(`{"value":1}`), []byte(`{"value":2}`)} {
		if _, err := log.Append("update", payload); err != nil {
			t.Fatalf("append WAL: %v", err)
		}
	}
	if err := log.Close(); err != nil {
		t.Fatalf("close WAL: %v", err)
	}

	reopened, err := Open(Config{Dir: dir, SyncMode: SyncAlways})
	if err != nil {
		t.Fatalf("reopen WAL: %v", err)
	}
	defer reopened.Close()
	if reopened.LastIndex() != 2 {
		t.Fatalf("expected last index 2, got %d", reopened.LastIndex())
	}
	records, err := reopened.RecordsAfter(1)
	if err != nil {
		t.Fatalf("records after checkpoint: %v", err)
	}
	if len(records) != 1 || records[0].Index != 2 || string(records[0].Payload) != `{"value":2}` {
		t.Fatalf("unexpected replay records: %+v", records)
	}
}

func TestLogIgnoresTruncatedCrashTail(t *testing.T) {
	dir := t.TempDir()
	log, err := Open(Config{Dir: dir, SyncMode: SyncAlways})
	if err != nil {
		t.Fatalf("open WAL: %v", err)
	}
	if _, err := log.Append("update", []byte(`{"value":1}`)); err != nil {
		t.Fatalf("append WAL: %v", err)
	}
	if err := log.Close(); err != nil {
		t.Fatalf("close WAL: %v", err)
	}
	file, err := os.OpenFile(filepath.Join(dir, fileName), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open WAL tail: %v", err)
	}
	if _, err := file.WriteString(`{"index":2,"type":"update"`); err != nil {
		t.Fatalf("write truncated tail: %v", err)
	}
	_ = file.Close()

	reopened, err := Open(Config{Dir: dir, SyncMode: SyncAlways})
	if err != nil {
		t.Fatalf("reopen with truncated tail: %v", err)
	}
	defer reopened.Close()
	if reopened.LastIndex() != 1 {
		t.Fatalf("expected valid prefix index 1, got %d", reopened.LastIndex())
	}
	if _, err := reopened.Append("update", []byte(`{"value":2}`)); err != nil {
		t.Fatalf("append after truncated-tail recovery: %v", err)
	}
	records, err := reopened.RecordsAfter(0)
	if err != nil || len(records) != 2 {
		t.Fatalf("expected two valid records after recovery, count=%d err=%v", len(records), err)
	}
}

func TestLogRejectsCorruptCompleteRecord(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, fileName), []byte("not-json\n"), 0o600); err != nil {
		t.Fatalf("write corrupt WAL: %v", err)
	}
	if _, err := Open(Config{Dir: dir, SyncMode: SyncAlways}); !errors.Is(err, ErrCorruptRecord) {
		t.Fatalf("expected corrupt record error, got %v", err)
	}
}

func TestLogBatchModeFlushesAtConfiguredSize(t *testing.T) {
	log, err := Open(Config{Dir: t.TempDir(), SyncMode: SyncBatch, BatchSize: 2})
	if err != nil {
		t.Fatalf("open WAL: %v", err)
	}
	defer log.Close()
	if _, err := log.Append("update", []byte(`{"value":1}`)); err != nil {
		t.Fatalf("append first record: %v", err)
	}
	if log.pendingSync != 1 {
		t.Fatalf("expected one pending record, got %d", log.pendingSync)
	}
	if _, err := log.Append("update", []byte(`{"value":2}`)); err != nil {
		t.Fatalf("append second record: %v", err)
	}
	if log.pendingSync != 0 {
		t.Fatalf("expected batch sync, pending=%d", log.pendingSync)
	}
}
