package loopgate

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"runtime/debug"
	"time"
)

const (
	loopgateRequestIDHeader                   = "X-Loopgate-Request-ID"
	requestIDContextKey     requestContextKey = "loopgate_request_id"
)

type responseStatusRecorder struct {
	http.ResponseWriter
	statusCode  int
	wroteHeader bool
}

func (recorder *responseStatusRecorder) WriteHeader(statusCode int) {
	recorder.statusCode = statusCode
	recorder.wroteHeader = true
	recorder.ResponseWriter.WriteHeader(statusCode)
}

func (recorder *responseStatusRecorder) Write(payload []byte) (int, error) {
	if !recorder.wroteHeader {
		recorder.WriteHeader(http.StatusOK)
	}
	return recorder.ResponseWriter.Write(payload)
}

func (recorder *responseStatusRecorder) Flush() {
	if flusher, ok := recorder.ResponseWriter.(http.Flusher); ok {
		if !recorder.wroteHeader {
			recorder.WriteHeader(http.StatusOK)
		}
		flusher.Flush()
	}
}

func (recorder *responseStatusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := recorder.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("response writer does not support hijacking")
	}
	return hijacker.Hijack()
}

func (recorder *responseStatusRecorder) Push(target string, options *http.PushOptions) error {
	pusher, ok := recorder.ResponseWriter.(http.Pusher)
	if !ok {
		return http.ErrNotSupported
	}
	return pusher.Push(target, options)
}

func (recorder *responseStatusRecorder) ReadFrom(reader io.Reader) (int64, error) {
	readFrom, ok := recorder.ResponseWriter.(io.ReaderFrom)
	if !ok {
		return io.Copy(recorder.ResponseWriter, reader)
	}
	if !recorder.wroteHeader {
		recorder.WriteHeader(http.StatusOK)
	}
	return readFrom.ReadFrom(reader)
}

func (recorder *responseStatusRecorder) effectiveStatusCode() int {
	if recorder.statusCode == 0 {
		return http.StatusOK
	}
	return recorder.statusCode
}

func requestIDFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	requestID, ok := ctx.Value(requestIDContextKey).(string)
	return requestID, ok
}

func (server *Server) wrapHTTPHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestID := server.allocateHTTPRequestID()
		request = request.WithContext(context.WithValue(request.Context(), requestIDContextKey, requestID))

		recorder := &responseStatusRecorder{ResponseWriter: writer}
		recorder.Header().Set(loopgateRequestIDHeader, requestID)

		startedAt := time.Now()
		if server.diagnostic != nil && server.diagnostic.Client != nil {
			server.diagnostic.Client.Debug("http_request_started",
				"request_id", requestID,
				"method", request.Method,
				"path", request.URL.Path,
				"remote", request.RemoteAddr,
			)
		}

		defer func() {
			durationMillis := time.Since(startedAt).Milliseconds()
			if recovered := recover(); recovered != nil {
				panicText := fmt.Sprint(recovered)
				fmt.Fprintf(os.Stderr, "ERROR: panic_recovered request_id=%s method=%s path=%s panic=%s\n", requestID, request.Method, request.URL.Path, panicText)
				if server.diagnostic != nil && server.diagnostic.Server != nil {
					server.diagnostic.Server.Error("panic_recovered",
						"request_id", requestID,
						"method", request.Method,
						"path", request.URL.Path,
						"remote", request.RemoteAddr,
						"duration_ms", durationMillis,
						"panic", panicText,
						"stack", string(debug.Stack()),
					)
				}
				if !recorder.wroteHeader {
					http.Error(recorder, "internal server error", http.StatusInternalServerError)
				}
				return
			}
			if server.diagnostic != nil && server.diagnostic.Client != nil {
				server.diagnostic.Client.Debug("http_request_finished",
					"request_id", requestID,
					"method", request.Method,
					"path", request.URL.Path,
					"remote", request.RemoteAddr,
					"status", recorder.effectiveStatusCode(),
					"duration_ms", durationMillis,
				)
			}
		}()

		next.ServeHTTP(recorder, request)
	})
}

func (server *Server) allocateHTTPRequestID() string {
	requestIDSuffix, err := randomHex(8)
	if err == nil {
		return "http_" + requestIDSuffix
	}
	if server.diagnostic != nil && server.diagnostic.Server != nil {
		server.diagnostic.Server.Warn("request_id_fallback",
			"kind", "http",
			"operator_error_class", "random_unavailable",
		)
	}
	return fmt.Sprintf("http_fallback_%d", time.Now().UTC().UnixNano())
}
