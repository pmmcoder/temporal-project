package obs

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

func NewLogger(level string) *slog.Logger {
	return NewLoggerWithWriter(level, os.Stdout)
}

func NewLoggerWithWriter(level string, writer io.Writer) *slog.Logger {
	handlerOptions := &slog.HandlerOptions{
		Level: parseLevel(level),
		ReplaceAttr: func(_ []string, attribute slog.Attr) slog.Attr {
			if isSensitiveKey(attribute.Key) {
				return slog.String(attribute.Key, "<REDACTED>")
			}
			return attribute
		},
	}
	return slog.New(slog.NewJSONHandler(writer, handlerOptions))
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

func isSensitiveKey(key string) bool {
	key = strings.ToLower(key)
	for _, fragment := range []string{
		"authorization",
		"cookie",
		"password",
		"secret",
		"token",
		"api_key",
	} {
		if strings.Contains(key, fragment) {
			return true
		}
	}
	return false
}
