// Package loopdiag provides optional, non-authoritative text logs for Loopgate operators.
// These files complement (and must not replace) the hash-chained JSONL audit ledger.
//
// Security: diagnostic files must not record access tokens, API keys, or other secrets.
// Loopgate emits only fixed vocabulary fields and numeric metadata (see secrets.LoopgateOperatorErrorClass
// and server_diagnostic_logging.go). HTTP middleware omits Authorization and bodies; request paths must
// not be used to carry secrets.
package loopdiag

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"loopgate/internal/config"
)

const slogTraceLevel = slog.Level(-8)

// Manager holds per-channel slog loggers writing to separate files (audit, server, client, socket, memory, ledger, model).
type Manager struct {
	Audit  *slog.Logger
	Server *slog.Logger
	Client *slog.Logger
	Socket *slog.Logger
	Memory *slog.Logger
	Ledger *slog.Logger
	Model  *slog.Logger

	files []*os.File
	mu    sync.Mutex
}

// Open creates diagnostic log files under repoRoot/cfg.Directory when cfg.Enabled.
func Open(repoRoot string, cfg config.DiagnosticLogging) (*Manager, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	dir := filepath.Join(repoRoot, filepath.Clean(cfg.ResolvedDirectory()))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create diagnostic log dir: %w", err)
	}

	m := &Manager{}
	channels := []struct {
		name string
		file string
		dest **slog.Logger
	}{
		{"audit", cfg.Files.Audit, &m.Audit},
		{"server", cfg.Files.Server, &m.Server},
		{"client", cfg.Files.Client, &m.Client},
		{"socket", cfg.Files.Socket, &m.Socket},
		{"memory", cfg.Files.Memory, &m.Memory},
		{"ledger", cfg.Files.Ledger, &m.Ledger},
		{"model", cfg.Files.Model, &m.Model},
	}

	defaultBasenames := map[string]string{
		"audit": "audit.log", "server": "server.log", "client": "client.log",
		"socket": "socket.log", "memory": "memory.log", "ledger": "ledger.log", "model": "model.log",
	}
	for _, ch := range channels {
		level := parseDiagLevel(cfg.LevelForChannel(ch.name))
		baseName := filepath.Base(strings.TrimSpace(ch.file))
		if baseName == "." || baseName == "" {
			baseName = defaultBasenames[ch.name]
		}
		path := filepath.Join(dir, baseName)
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			m.Close()
			return nil, fmt.Errorf("open diagnostic log %s: %w", path, err)
		}
		m.files = append(m.files, f)
		handler := slog.NewTextHandler(f, &slog.HandlerOptions{
			Level: level,
		})
		*ch.dest = slog.New(handler).With("channel", ch.name)
	}
	return m, nil
}

func parseDiagLevel(s string) slog.Leveler {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "error":
		return slog.LevelError
	case "warn", "warning":
		return slog.LevelWarn
	case "info":
		return slog.LevelInfo
	case "debug":
		return slog.LevelDebug
	case "trace":
		return slogTraceLevel
	default:
		return slog.LevelInfo
	}
}

// Close flushes and closes all log files.
func (m *Manager) Close() error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var firstErr error
	for _, f := range m.files {
		if f == nil {
			continue
		}
		if err := f.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	m.files = nil
	return firstErr
}

// HTTPMiddleware logs one Debug line per HTTP request (no Authorization header, no body).
func HTTPMiddleware(client *slog.Logger, next http.Handler) http.Handler {
	if client == nil {
		return next
	}
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		client.Debug("http_request",
			"method", request.Method,
			"path", request.URL.Path,
			"remote", request.RemoteAddr,
		)
		next.ServeHTTP(writer, request)
	})
}
