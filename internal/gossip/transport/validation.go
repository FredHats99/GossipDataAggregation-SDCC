package transport

import (
	"encoding/hex"
	"errors"
	"regexp"
	"time"
)

var (
	nodeIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,30}$`)
	allowedTypes  = map[string]struct{}{
		"Ping":         {},
		"StateDigest":  {},
		"StateDelta":   {},
		"Ack":          {},
		"SnapshotReq":  {},
		"SnapshotResp": {},
	}
)

func validateEnvelope(message Envelope) error {
	if message.Type == "" {
		return ErrEmptyType
	}
	if _, ok := allowedTypes[message.Type]; !ok {
		return ErrUnsupportedType
	}
	if message.From == "" {
		return ErrEmptyFrom
	}
	if !nodeIDPattern.MatchString(message.From) {
		return ErrInvalidFrom
	}
	if message.Timestamp == "" {
		return ErrInvalidTimestamp
	}
	if _, err := time.Parse(time.RFC3339Nano, message.Timestamp); err != nil {
		return ErrInvalidTimestamp
	}
	if len(message.Payload) == 0 {
		return ErrEmptyPayload
	}
	if message.Checksum == "" {
		return ErrEmptyChecksum
	}
	if len(message.Checksum) != 64 {
		return ErrInvalidChecksum
	}
	if _, err := hex.DecodeString(message.Checksum); err != nil {
		return ErrInvalidChecksum
	}
	return nil
}

func isValidationError(err error) bool {
	return errors.Is(err, ErrEmptyType) ||
		errors.Is(err, ErrUnsupportedType) ||
		errors.Is(err, ErrEmptyFrom) ||
		errors.Is(err, ErrInvalidFrom) ||
		errors.Is(err, ErrInvalidTimestamp) ||
		errors.Is(err, ErrEmptyPayload) ||
		errors.Is(err, ErrEmptyChecksum) ||
		errors.Is(err, ErrInvalidChecksum) ||
		errors.Is(err, ErrChecksumMismatch)
}
