package logger

import (
	"log/slog"
	"os"
)

var (
	// Default logger
	defaultLogger *slog.Logger
)

func init() {
	// Create default logger
	defaultLogger = slog.New(
		slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		}),
	)
}

// SetDefault sets the default logger
func SetDefault(l *slog.Logger) {
	defaultLogger = l
}

// Default returns the default logger
func Default() *slog.Logger {
	return defaultLogger
}

// With returns a logger with additional attributes
func With(args ...any) *slog.Logger {
	return defaultLogger.With(args...)
}

// Info logs an info message
func Info(msg string, args ...any) {
	defaultLogger.Info(msg, args...)
}

// Error logs an error message
func Error(msg string, args ...any) {
	defaultLogger.Error(msg, args...)
}

// Debug logs a debug message
func Debug(msg string, args ...any) {
	defaultLogger.Debug(msg, args...)
}

// Warn logs a warning message
func Warn(msg string, args ...any) {
	defaultLogger.Warn(msg, args...)
}
