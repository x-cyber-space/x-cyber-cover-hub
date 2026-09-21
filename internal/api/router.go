// Package api implements the HTTP surface: routing, middleware, and the cover
// endpoint.
//
// The shape deliberately mirrors the sibling lyrics service — same middleware
// order, same error envelope — so the two can be read and operated together.
package api

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/x-cyber-space/x-cyber-cover-hub/internal/cache"
	"github.com/x-cyber-space/x-cyber-cover-hub/internal/provider"
)

// NewRouter sets up the HTTP router and middleware.
func NewRouter(store cache.Store, dispatcher *provider.Dispatcher, defaultSize int, version string) http.Handler {
	mux := http.NewServeMux()

	coverHandler := CoverHandler(store, dispatcher, defaultSize)

	mux.HandleFunc("/api/cover", getOnly(coverHandler))

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{
			"service": "x-cyber-cover-hub",
			"status":  "running",
			"version": version,
		})
	})

	return recoveryMiddleware(corsMiddleware(loggingMiddleware(mux)))
}

func getOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		next(w, r)
	}
}

// writeJSON writes a compact JSON body with no trailing newline and HTML
// escaping disabled, matching the sibling service byte for byte.
func writeJSON(w http.ResponseWriter, status int, payload any) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(payload); err != nil {
		slog.Error("json encode failed", "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(bytes.TrimSuffix(buf.Bytes(), []byte("\n")))
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		// The image endpoint sets an ETag, and a browser client cannot read the
		// response headers of an image it embeds, so expose them explicitly for
		// fetch()-based clients.
		w.Header().Set("Access-Control-Expose-Headers", "ETag, Content-Type, X-Cover-Source")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		slog.Info("request",
			"method", r.Method,
			"path", r.URL.RequestURI(),
			"status", rec.status,
			"duration", time.Since(start).Round(time.Microsecond).String(),
		)
	})
}

// statusRecorder captures the status and the body size so the access log
// carries both. Cover responses are images, so the byte count is the
// interesting part.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	written     int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(status int) {
	if !r.wroteHeader {
		r.status = status
		r.wroteHeader = true
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	r.wroteHeader = true
	n, err := r.ResponseWriter.Write(b)
	r.written += n
	return n, err
}

func recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic recovered",
					"panic", rec,
					"path", r.URL.RequestURI(),
					"stack", string(debug.Stack()),
				)
				writeJSON(w, http.StatusInternalServerError, ErrorResponse{
					Message:    "Internal server error",
					Name:       "InternalServerError",
					StatusCode: http.StatusInternalServerError,
				})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// writeBytes sends an image body with the validators a client needs to avoid
// re-downloading it.
func writeBytes(w http.ResponseWriter, status int, contentType string, body []byte, headers map[string]string) {
	if contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	for k, v := range headers {
		w.Header().Set(k, v)
	}
	w.WriteHeader(status)
	_, _ = io.Copy(w, bytes.NewReader(body))
}
