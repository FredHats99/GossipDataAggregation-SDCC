package transport

import "errors"

var (
	ErrNilSender        = errors.New("transport: nil sender")
	ErrNilReceiver      = errors.New("transport: nil receiver")
	ErrNilFrameSender   = errors.New("transport: nil frame sender")
	ErrNilFrameReceiver = errors.New("transport: nil frame receiver")
	ErrNilCodec         = errors.New("transport: nil codec")
	ErrEmptyType        = errors.New("transport: empty envelope type")
	ErrEmptyFrom        = errors.New("transport: empty envelope from")
	ErrInvalidFrom      = errors.New("transport: invalid envelope from")
	ErrInvalidTimestamp = errors.New("transport: invalid envelope timestamp")
	ErrEmptyPayload     = errors.New("transport: empty envelope payload")
	ErrEmptyChecksum    = errors.New("transport: empty envelope checksum")
	ErrInvalidChecksum  = errors.New("transport: invalid envelope checksum")
	ErrChecksumMismatch = errors.New("transport: envelope checksum mismatch")
	ErrUnsupportedType  = errors.New("transport: unsupported envelope type")
	ErrFrameTooLarge    = errors.New("transport: frame too large")
	ErrDuplicateMessage = errors.New("transport: duplicate message")
	ErrStaleSequence    = errors.New("transport: stale or non-monotonic sequence")
)
