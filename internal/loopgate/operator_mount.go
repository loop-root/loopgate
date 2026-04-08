package loopgate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	toolspkg "morph/internal/tools"
)

const (
	maxOperatorMountPathsPerSession = 16
	maxOperatorMountPathBytes       = 4096
)

type operatorMountCtxKey struct{}

func withOperatorMountControlSession(ctx context.Context, controlSessionID string) context.Context {
	return context.WithValue(ctx, operatorMountCtxKey{}, strings.TrimSpace(controlSessionID))
}

// isDangerousOperatorMountPath mirrors Haven's havenpath host deny list: system roots
// that must not be granted as operator read roots.
func isDangerousOperatorMountPath(abs string) bool {
	clean := filepath.Clean(abs)
	if clean == "/" {
		return true
	}
	denyPrefixes := []string{
		"/etc", "/usr", "/bin", "/sbin", "/var/db", "/private/var/db",
		"/System", "/Library", "/private/etc",
	}
	for _, p := range denyPrefixes {
		if clean == p || strings.HasPrefix(clean, p+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// canonicalizeOperatorMountPath resolves, requires an existing directory, and rejects unsafe roots.
func canonicalizeOperatorMountPath(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("empty path")
	}
	if len(raw) > maxOperatorMountPathBytes {
		return "", errors.New("path too long")
	}
	abs, err := filepath.Abs(raw)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	if isDangerousOperatorMountPath(resolved) {
		return "", errors.New("path is not permitted for operator mount")
	}
	fi, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !fi.IsDir() {
		return "", errors.New("path is not a directory")
	}
	return filepath.Clean(resolved), nil
}

// normalizeOperatorMountPathsForSession validates and deduplicates paths for actor "haven" only.
func normalizeOperatorMountPathsForSession(actor string, rawPaths []string) ([]string, error) {
	if len(rawPaths) == 0 {
		return nil, nil
	}
	if defaultLabel(actor, "client") != "haven" {
		return nil, fmt.Errorf("operator_mount_paths is only accepted for actor haven")
	}
	if len(rawPaths) > maxOperatorMountPathsPerSession {
		return nil, fmt.Errorf("operator_mount_paths: at most %d entries", maxOperatorMountPathsPerSession)
	}
	seen := make(map[string]struct{}, len(rawPaths))
	var out []string
	for _, p := range rawPaths {
		canon, err := canonicalizeOperatorMountPath(p)
		if err != nil {
			return nil, fmt.Errorf("operator_mount_paths: %w", err)
		}
		if _, ok := seen[canon]; ok {
			continue
		}
		seen[canon] = struct{}{}
		out = append(out, canon)
	}
	return out, nil
}

func operatorMountPathsFromContext(server *Server, ctx context.Context) ([]string, error) {
	sid, _ := ctx.Value(operatorMountCtxKey{}).(string)
	sid = strings.TrimSpace(sid)
	if sid == "" {
		return nil, fmt.Errorf("missing control session binding for operator mount tool")
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	sess, ok := server.sessions[sid]
	if !ok {
		return nil, fmt.Errorf("unknown control session")
	}
	out := append([]string(nil), sess.OperatorMountPaths...)
	return out, nil
}

type operatorMountFSRead struct{ server *Server }

func (operatorMountFSRead) Name() string      { return "operator_mount.fs_read" }
func (operatorMountFSRead) Category() string  { return "filesystem" }
func (operatorMountFSRead) Operation() string { return toolspkg.OpRead }
func (t operatorMountFSRead) Schema() toolspkg.Schema {
	return (&toolspkg.FSRead{}).Schema()
}
func (t operatorMountFSRead) Execute(ctx context.Context, args map[string]string) (string, error) {
	mounts, err := operatorMountPathsFromContext(t.server, ctx)
	if err != nil {
		return "", err
	}
	if len(mounts) == 0 {
		return "", fmt.Errorf("no operator directory grants for this session; allow read in Haven or use /adir")
	}
	fr := &toolspkg.FSRead{RepoRoot: mounts[0], AllowedRoots: mounts, DeniedPaths: nil}
	return fr.Execute(ctx, args)
}

type operatorMountFSList struct{ server *Server }

func (operatorMountFSList) Name() string      { return "operator_mount.fs_list" }
func (operatorMountFSList) Category() string  { return "filesystem" }
func (operatorMountFSList) Operation() string { return toolspkg.OpRead }
func (t operatorMountFSList) Schema() toolspkg.Schema {
	return (&toolspkg.FSList{}).Schema()
}
func (t operatorMountFSList) Execute(ctx context.Context, args map[string]string) (string, error) {
	mounts, err := operatorMountPathsFromContext(t.server, ctx)
	if err != nil {
		return "", err
	}
	if len(mounts) == 0 {
		return "", fmt.Errorf("no operator directory grants for this session; allow read in Haven or use /adir")
	}
	fl := &toolspkg.FSList{RepoRoot: mounts[0], AllowedRoots: mounts, DeniedPaths: nil}
	return fl.Execute(ctx, args)
}

type operatorMountFSWrite struct{ server *Server }

func (operatorMountFSWrite) Name() string      { return "operator_mount.fs_write" }
func (operatorMountFSWrite) Category() string  { return "filesystem" }
func (operatorMountFSWrite) Operation() string { return toolspkg.OpWrite }
func (t operatorMountFSWrite) Schema() toolspkg.Schema {
	return (&toolspkg.FSWrite{}).Schema()
}
func (t operatorMountFSWrite) Execute(ctx context.Context, args map[string]string) (string, error) {
	mounts, err := operatorMountPathsFromContext(t.server, ctx)
	if err != nil {
		return "", err
	}
	if len(mounts) == 0 {
		return "", fmt.Errorf("no operator directory grants for this session; allow read in Haven or use /adir")
	}
	fw := &toolspkg.FSWrite{RepoRoot: mounts[0], AllowedRoots: mounts, DeniedPaths: nil}
	return fw.Execute(ctx, args)
}

type operatorMountFSMkdir struct{ server *Server }

func (operatorMountFSMkdir) Name() string      { return "operator_mount.fs_mkdir" }
func (operatorMountFSMkdir) Category() string  { return "filesystem" }
func (operatorMountFSMkdir) Operation() string { return toolspkg.OpWrite }
func (t operatorMountFSMkdir) Schema() toolspkg.Schema {
	return (&toolspkg.FSMkdir{}).Schema()
}
func (t operatorMountFSMkdir) Execute(ctx context.Context, args map[string]string) (string, error) {
	mounts, err := operatorMountPathsFromContext(t.server, ctx)
	if err != nil {
		return "", err
	}
	if len(mounts) == 0 {
		return "", fmt.Errorf("no operator directory grants for this session; allow read in Haven or use /adir")
	}
	fm := &toolspkg.FSMkdir{RepoRoot: mounts[0], AllowedRoots: mounts, DeniedPaths: nil}
	return fm.Execute(ctx, args)
}

func registerOperatorMountTools(server *Server) error {
	tools := []toolspkg.Tool{
		operatorMountFSRead{server: server},
		operatorMountFSList{server: server},
		operatorMountFSWrite{server: server},
		operatorMountFSMkdir{server: server},
	}
	for _, t := range tools {
		if err := server.registry.TryRegister(t); err != nil {
			return fmt.Errorf("register %s: %w", t.Name(), err)
		}
	}
	return nil
}
