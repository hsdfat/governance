package logger

import (
	"github.com/hsdfat/go-zlog/logger"
	"go.uber.org/zap"
)

// Log is the global logger instance for the governance project
var Log logger.LoggerI = logger.NewLogger()

func init() {
	Log.(*logger.Logger).SugaredLogger = Log.(*logger.Logger).SugaredLogger.WithOptions(zap.AddCallerSkip(1))
}

// SetLevel sets the global log level
// Valid levels: "debug", "info", "warn", "error", "fatal"
func SetLevel(level string) {
	logger.SetLevel(level)
}

// WithFields creates a new logger with contextual fields
// Example: logger.WithFields("conn_id", "abc123", "state", "OPEN")
func WithFields(args ...any) logger.LoggerI {
	return Log.With(args...).(logger.LoggerI)
}

// Logger is an alias for the underlying logger interface
type Logger = logger.LoggerI

// New creates a new logger with a name and level
func New(name, level string) Logger {
	if level != "" {
		// Set level if provided
		logger.SetLevel(level)
	}
	return Log.With("component", name).(logger.LoggerI)
}

// Legacy compatibility functions for existing code using zap.Field

// Debug logs a debug message with zap fields
func Debug(msg string, fields ...zap.Field) {
	Log.(*logger.Logger).SugaredLogger.Desugar().Debug(msg, fields...)
}

// Info logs an info message with zap fields
func Info(msg string, fields ...zap.Field) {
	Log.(*logger.Logger).SugaredLogger.Desugar().Info(msg, fields...)
}

// Warn logs a warning message with zap fields
func Warn(msg string, fields ...zap.Field) {
	Log.(*logger.Logger).SugaredLogger.Desugar().Warn(msg, fields...)
}

// Error logs an error message with zap fields
func Error(msg string, fields ...zap.Field) {
	Log.(*logger.Logger).SugaredLogger.Desugar().Error(msg, fields...)
}

// Sync flushes any buffered log entries
func Sync() error {
	return Log.(*logger.Logger).SugaredLogger.Sync()
}
