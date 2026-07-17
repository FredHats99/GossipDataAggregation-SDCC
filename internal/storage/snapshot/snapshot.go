package snapshot

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const schemaVersion = 1

var ErrCorruptSnapshot = errors.New("snapshot: corrupt snapshot")

type Metadata struct {
	SchemaVersion int       `json:"schema_version"`
	WALIndex      uint64    `json:"wal_index"`
	CreatedAt     time.Time `json:"created_at"`
	Checksum      string    `json:"checksum"`
}

type File struct {
	Metadata Metadata        `json:"metadata"`
	Payload  json.RawMessage `json:"payload"`
}

type Store struct {
	dir string
}

func NewStore(dir string) (*Store, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("snapshot: empty directory")
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("create snapshot directory: %w", err)
	}
	return &Store{dir: dir}, nil
}

func (s *Store) Save(walIndex uint64, payload []byte) (Metadata, error) {
	if len(payload) == 0 {
		return Metadata{}, ErrCorruptSnapshot
	}
	metadata := Metadata{
		SchemaVersion: schemaVersion,
		WALIndex:      walIndex,
		CreatedAt:     time.Now().UTC(),
	}
	metadata.Checksum = checksum(metadata, payload)
	file := File{Metadata: metadata, Payload: append([]byte(nil), payload...)}
	raw, err := json.Marshal(file)
	if err != nil {
		return Metadata{}, fmt.Errorf("encode snapshot: %w", err)
	}

	temp, err := os.CreateTemp(s.dir, ".snapshot-*.tmp")
	if err != nil {
		return Metadata{}, fmt.Errorf("create temporary snapshot: %w", err)
	}
	tempPath := temp.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tempPath)
		}
	}()
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return Metadata{}, fmt.Errorf("chmod temporary snapshot: %w", err)
	}
	if _, err := temp.Write(raw); err != nil {
		_ = temp.Close()
		return Metadata{}, fmt.Errorf("write temporary snapshot: %w", err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return Metadata{}, fmt.Errorf("sync temporary snapshot: %w", err)
	}
	if err := temp.Close(); err != nil {
		return Metadata{}, fmt.Errorf("close temporary snapshot: %w", err)
	}

	finalPath := filepath.Join(s.dir, fmt.Sprintf("snapshot-%020d.json", walIndex))
	if err := os.Rename(tempPath, finalPath); err != nil {
		return Metadata{}, fmt.Errorf("publish snapshot: %w", err)
	}
	removeTemp = false
	if dir, err := os.Open(s.dir); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return metadata, nil
}

func (s *Store) LoadLatest() (File, bool, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return File{}, false, fmt.Errorf("list snapshots: %w", err)
	}
	type candidate struct {
		name  string
		index uint64
	}
	var candidates []candidate
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "snapshot-") || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		rawIndex := strings.TrimSuffix(strings.TrimPrefix(entry.Name(), "snapshot-"), ".json")
		index, err := strconv.ParseUint(rawIndex, 10, 64)
		if err != nil {
			continue
		}
		candidates = append(candidates, candidate{name: entry.Name(), index: index})
	}
	if len(candidates) == 0 {
		return File{}, false, nil
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].index > candidates[j].index })
	raw, err := os.ReadFile(filepath.Join(s.dir, candidates[0].name))
	if err != nil {
		return File{}, false, fmt.Errorf("read latest snapshot: %w", err)
	}
	var file File
	if err := json.Unmarshal(raw, &file); err != nil {
		return File{}, false, fmt.Errorf("%w: %v", ErrCorruptSnapshot, err)
	}
	if file.Metadata.SchemaVersion != schemaVersion ||
		file.Metadata.WALIndex != candidates[0].index ||
		file.Metadata.CreatedAt.IsZero() ||
		len(file.Payload) == 0 ||
		file.Metadata.Checksum != checksum(file.Metadata, file.Payload) {
		return File{}, false, ErrCorruptSnapshot
	}
	return file, true, nil
}

func checksum(metadata Metadata, payload []byte) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte(strconv.Itoa(metadata.SchemaVersion)))
	_, _ = hash.Write([]byte{'\n'})
	_, _ = hash.Write([]byte(strconv.FormatUint(metadata.WALIndex, 10)))
	_, _ = hash.Write([]byte{'\n'})
	_, _ = hash.Write(payload)
	return hex.EncodeToString(hash.Sum(nil))
}
