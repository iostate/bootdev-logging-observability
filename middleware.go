package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"time"
)

type LogContext struct {
	Username string
}

type spyReadCloser struct {
	io.ReadCloser
	bytesRead  int
	statusCode int
}

func (r *spyReadCloser) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	r.bytesRead += n
	return n, err
}

type spyResponseWriter struct {
	http.ResponseWriter
	bytesWritten int
	statusCode   int
}

func (w *spyResponseWriter) Write(p []byte) (int, error) {
	if w.statusCode == 0 {
		w.statusCode = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(p)
	w.bytesWritten += n
	return n, err
}

func (w *spyResponseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func requestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			spyReader := &spyReadCloser{ReadCloser: r.Body}
			r.Body = spyReader

			spyWriter := &spyResponseWriter{ResponseWriter: w}

			logContext := &LogContext{}
			newCtx := context.WithValue(r.Context(), LogContextKey, logContext)
			r = r.WithContext(newCtx)

			next.ServeHTTP(spyWriter, r)

			if logContext.Username != "" {

				logger.Info("Served request",
					slog.String("method", r.Method),
					slog.String("path", r.URL.Path),
					slog.String("client_ip", r.RemoteAddr),
					slog.Duration("duration", time.Since(start)),
					slog.Int("request_body_bytes", spyReader.bytesRead),
					slog.Int("response_status", spyWriter.statusCode),
					slog.Int("response_body_bytes", spyWriter.bytesWritten),
					slog.String("user", logContext.Username),
				)
			} else {
				logger.Info("Served request",
					slog.String("method", r.Method),
					slog.String("path", r.URL.Path),
					slog.String("client_ip", r.RemoteAddr),
					slog.Duration("duration", time.Since(start)),
					slog.Int("request_body_bytes", spyReader.bytesRead),
					slog.Int("response_status", spyWriter.statusCode),
					slog.Int("response_body_bytes", spyWriter.bytesWritten))
			}
		})
	}
}
