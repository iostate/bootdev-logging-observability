package main

import (
	"context"
	"crypto/rand"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var httpRequestsTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Total number of HTTP requests",
	},
	[]string{"method", "path", "status"},
)

type LogContext struct {
	Username  string
	RequestID string
	Error     error
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

			attrs := []any{
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.String("client_ip", redactIP(r.RemoteAddr)),
				slog.Duration("duration", time.Since(start)),
				slog.Int("request_body_bytes", spyReader.bytesRead),
				slog.Int("response_status", spyWriter.statusCode),
				slog.Int("response_body_bytes", spyWriter.bytesWritten),
			}
			if logContext.RequestID != "" {
				attrs = append(attrs, slog.String("request_id", logContext.RequestID))
			}
			if logContext.Username != "" {
				attrs = append(attrs, slog.String("user", logContext.Username))
			}
			if logContext.Error != nil {
				attrs = append(attrs, slog.Any("error", logContext.Error))
			}
			logger.Info("Served request", attrs...)
		})
	}
}

func requestIdMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-Id")
		if requestID == "" {
			requestID = rand.Text()
		}
		w.Header().Set("X-Request-Id", requestID)
		if logCtx, ok := r.Context().Value(LogContextKey).(*LogContext); ok {
			logCtx.RequestID = requestID
		}
		next.ServeHTTP(w, r)
	})
}

func httpError(ctx context.Context, w http.ResponseWriter, status int, err error) {
	if logCtx, ok := ctx.Value(LogContextKey).(*LogContext); ok {
		logCtx.Error = err
	}
	if status == 401 {
		http.Error(w, http.StatusText(401), 401)
		return
	}
	if status == 403 {
		http.Error(w, http.StatusText(403), 403)
		return
	}
	if status == 500 {
		http.Error(w, http.StatusText(500), 500)
		return
	}

	http.Error(w, err.Error(), status)
}

func isNonIPv4(ipStr string) bool {
	addr, err := netip.ParseAddr(ipStr)
	if err != nil {
		// The string is not a valid IP address at all
		return true
	}
	// Returns true if it is a valid IPv6 address
	return addr.Is6()
}

func redactIP(address string) string {

	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return address
	}

	addr := net.ParseIP(host)

	splits := strings.Split(addr.String(), ".")
	splits[len(splits)-1] = "x"
	result := strings.Join(splits, ".")
	return result
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func metricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{
			ResponseWriter: w,
			status:         http.StatusOK,
		}

		next.ServeHTTP(rec, r)

		path := r.URL.Path
		method := r.Method
		status := strconv.Itoa(rec.status)

		httpRequestsTotal.
			WithLabelValues(method, path, status).
			Inc()
	})
}
