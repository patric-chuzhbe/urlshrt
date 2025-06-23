// Package logger provides structured logging functionality
// using the Uber zap logging library. It supports log levels and output customization.
package logger

import (
	"errors"
	"net/http"
	"os"
	"time"

	"go.uber.org/zap"
)

type responseData struct {
	status int
	size   int
}

type loggingResponseWriter struct {
	http.ResponseWriter
	responseData *responseData
}

// Log is a global SugaredLogger instance from the zap logging library.
// It provides a structured and leveled logging API with a simpler interface
// for common use cases like formatted output and key-value logging.
// Log should be initialized via Init().
var Log *zap.SugaredLogger

// Write implements the io.Writer interface for logger middleware.
// It writes log data to the underlying logger, capturing response size.
func (r *loggingResponseWriter) Write(b []byte) (int, error) {
	size, err := r.ResponseWriter.Write(b)
	r.responseData.size += size
	return size, err
}

// WriteHeader writes the HTTP status code to the response and logs it.
// It should be used in custom response writer implementations that support logging.
func (r *loggingResponseWriter) WriteHeader(statusCode int) {
	r.ResponseWriter.WriteHeader(statusCode)
	r.responseData.status = statusCode
}

// Init initializes the global logger configuration.
// It sets the output destination and global log level.
func Init(level string) error {
	lvl, err := zap.ParseAtomicLevel(level)
	if err != nil {
		return err
	}

	cfg := zap.NewDevelopmentConfig()
	cfg.Level = lvl
	zl, err := cfg.Build()
	if err != nil {
		return err
	}
	Log = zl.Sugar()

	return nil
}

// Sync flushes any buffered log entries to the output.
// It should be called when shutting down to ensure all logs are written.
func Sync() error {
	if err := Log.Sync(); err != nil && !errors.Is(err, os.ErrInvalid) {
		return err
	}

	return nil
}

// WithLoggingHTTPMiddleware wraps an http.Handler with structured logging capabilities.
// It injects request-scoped loggers and logs method, URL, and response status.
func WithLoggingHTTPMiddleware(h http.Handler) http.Handler {
	logFn := func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		responseData := &responseData{
			status: 0,
			size:   0,
		}
		lw := loggingResponseWriter{
			ResponseWriter: w,
			responseData:   responseData,
		}
		h.ServeHTTP(&lw, r)

		duration := time.Since(start)

		Log.Infoln(
			"uri", r.RequestURI,
			"method", r.Method,
			"status", responseData.status,
			"duration", duration,
			"size", responseData.size,
		)
	}

	return http.HandlerFunc(logFn)
}
