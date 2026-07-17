package wal

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

const fileName = "updates.wal"

type SyncMode string

const (
	SyncAlways SyncMode = "always"
	SyncBatch  SyncMode = "batch"
	SyncNone   SyncMode = "none"
)

var (
	ErrInvalidConfig = errors.New("wal: invalid config")
	ErrCorruptRecord = errors.New("wal: corrupt record")
	ErrClosed        = errors.New("wal: closed")
)

type Config struct {
	Dir       string
	SyncMode  SyncMode
	BatchSize int
}

type Record struct {
	Index     uint64          `json:"index"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"created_at"`
	Checksum  string          `json:"checksum"`
}

type Log struct {
	mu          sync.Mutex
	file        *os.File
	mode        SyncMode
	batchSize   int
	pendingSync int
	lastIndex   uint64
	closed      bool
}

func Open(config Config) (*Log, error) {
	if config.Dir == "" {
		return nil, ErrInvalidConfig
	}
	if config.SyncMode == "" {
		config.SyncMode = SyncAlways
	}
	if config.SyncMode != SyncAlways && config.SyncMode != SyncBatch && config.SyncMode != SyncNone {
		return nil, ErrInvalidConfig
	}
	if config.BatchSize <= 0 {
		config.BatchSize = 32
	}
	if err := os.MkdirAll(config.Dir, 0o750); err != nil {
		return nil, fmt.Errorf("create WAL directory: %w", err)
	}
	path := filepath.Join(config.Dir, fileName)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open WAL: %w", err)
	}
	records, validSize, err := readRecords(file)
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("stat WAL: %w", err)
	}
	if info.Size() > validSize {
		if err := file.Truncate(validSize); err != nil {
			_ = file.Close()
			return nil, fmt.Errorf("truncate incomplete WAL tail: %w", err)
		}
	}
	if _, err := file.Seek(0, io.SeekEnd); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("seek WAL end: %w", err)
	}
	log := &Log{file: file, mode: config.SyncMode, batchSize: config.BatchSize}
	if len(records) > 0 {
		log.lastIndex = records[len(records)-1].Index
	}
	return log, nil
}

func (l *Log) Append(recordType string, payload []byte) (uint64, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return 0, ErrClosed
	}
	if recordType == "" || len(payload) == 0 {
		return 0, ErrInvalidConfig
	}
	record := Record{
		Index:     l.lastIndex + 1,
		Type:      recordType,
		Payload:   append([]byte(nil), payload...),
		CreatedAt: time.Now().UTC(),
	}
	record.Checksum = checksum(record)
	raw, err := json.Marshal(record)
	if err != nil {
		return 0, fmt.Errorf("encode WAL record: %w", err)
	}
	raw = append(raw, '\n')
	if _, err := l.file.Seek(0, io.SeekEnd); err != nil {
		return 0, fmt.Errorf("seek WAL before append: %w", err)
	}
	if _, err := l.file.Write(raw); err != nil {
		return 0, fmt.Errorf("append WAL record: %w", err)
	}
	l.lastIndex = record.Index
	l.pendingSync++
	if l.mode == SyncAlways || (l.mode == SyncBatch && l.pendingSync >= l.batchSize) {
		if err := l.syncLocked(); err != nil {
			return 0, err
		}
	}
	return record.Index, nil
}

func (l *Log) RecordsAfter(index uint64) ([]Record, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil, ErrClosed
	}
	if err := l.file.Sync(); err != nil {
		return nil, fmt.Errorf("sync WAL before replay: %w", err)
	}
	records, _, err := readRecords(l.file)
	if err != nil {
		return nil, err
	}
	out := make([]Record, 0, len(records))
	for _, record := range records {
		if record.Index > index {
			out = append(out, record)
		}
	}
	return out, nil
}

func (l *Log) LastIndex() uint64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.lastIndex
}

func (l *Log) Sync() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return ErrClosed
	}
	return l.syncLocked()
}

func (l *Log) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	if err := l.file.Sync(); err != nil {
		_ = l.file.Close()
		return fmt.Errorf("sync WAL on close: %w", err)
	}
	return l.file.Close()
}

func (l *Log) syncLocked() error {
	if err := l.file.Sync(); err != nil {
		return fmt.Errorf("sync WAL: %w", err)
	}
	l.pendingSync = 0
	return nil
}

func readRecords(file *os.File) ([]Record, int64, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, 0, fmt.Errorf("seek WAL: %w", err)
	}
	reader := bufio.NewReader(file)
	var records []Record
	var expected uint64 = 1
	var validSize int64
	for {
		line, err := reader.ReadBytes('\n')
		if errors.Is(err, io.EOF) && len(bytes.TrimSpace(line)) > 0 {
			break
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, validSize, fmt.Errorf("read WAL: %w", err)
		}
		lineSize := len(line)
		line = bytes.TrimSpace(line)
		if len(line) > 0 {
			var record Record
			if decodeErr := json.Unmarshal(line, &record); decodeErr != nil {
				return nil, validSize, fmt.Errorf("%w at index %d: %v", ErrCorruptRecord, expected, decodeErr)
			}
			if record.Index != expected || record.Type == "" || len(record.Payload) == 0 || record.CreatedAt.IsZero() || record.Checksum != checksum(record) {
				return nil, validSize, fmt.Errorf("%w at index %d", ErrCorruptRecord, expected)
			}
			records = append(records, record)
			expected++
			validSize += int64(lineSize)
		}
		if errors.Is(err, io.EOF) {
			break
		}
	}
	if _, err := file.Seek(0, io.SeekEnd); err != nil {
		return nil, validSize, fmt.Errorf("seek WAL end: %w", err)
	}
	return records, validSize, nil
}

func checksum(record Record) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte(strconv.FormatUint(record.Index, 10)))
	_, _ = hash.Write([]byte{'\n'})
	_, _ = hash.Write([]byte(record.Type))
	_, _ = hash.Write([]byte{'\n'})
	_, _ = hash.Write(record.Payload)
	return hex.EncodeToString(hash.Sum(nil))
}
