package snapshot

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestStoreSavesAndLoadsLatestValidSnapshot(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("new snapshot store: %v", err)
	}
	if _, err := store.Save(2, []byte(`{"value":2}`)); err != nil {
		t.Fatalf("save snapshot 2: %v", err)
	}
	if _, err := store.Save(5, []byte(`{"value":5}`)); err != nil {
		t.Fatalf("save snapshot 5: %v", err)
	}

	latest, found, err := store.LoadLatest()
	if err != nil {
		t.Fatalf("load latest snapshot: %v", err)
	}
	if !found || latest.Metadata.WALIndex != 5 || string(latest.Payload) != `{"value":5}` {
		t.Fatalf("unexpected latest snapshot: found=%v file=%+v", found, latest)
	}
}

func TestStoreDetectsSnapshotChecksumMismatch(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("new snapshot store: %v", err)
	}
	if _, err := store.Save(3, []byte(`{"value":3}`)); err != nil {
		t.Fatalf("save snapshot: %v", err)
	}
	paths, err := filepath.Glob(filepath.Join(dir, "snapshot-*.json"))
	if err != nil || len(paths) != 1 {
		t.Fatalf("find snapshot: paths=%v err=%v", paths, err)
	}
	raw, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	var file File
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	file.Payload = json.RawMessage(`{"value":999}`)
	raw, err = json.Marshal(file)
	if err != nil {
		t.Fatalf("encode tampered snapshot: %v", err)
	}
	if err := os.WriteFile(paths[0], raw, 0o600); err != nil {
		t.Fatalf("write tampered snapshot: %v", err)
	}

	if _, _, err := store.LoadLatest(); !errors.Is(err, ErrCorruptSnapshot) {
		t.Fatalf("expected corrupt snapshot error, got %v", err)
	}
}
