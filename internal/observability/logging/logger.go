package logging

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

func NewJSONLogger(nodeID, level string) *slog.Logger {
	opts := &slog.HandlerOptions{Level: parseLevel(level)}
	handler := slog.NewJSONHandler(output(), opts)
	return slog.New(handler).With("node_id", nodeID)
}

func parseLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func output() io.Writer {
	return os.Stdout
}
