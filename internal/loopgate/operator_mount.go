package loopgate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"morph/internal/safety"
	toolspkg "morph/internal/tools"
)

const (
	maxOperatorMountPathsPerSession = 16
	maxOperatorMountPathBytes       = 4096
	operatorMountWriteGrantTTL      = 8 * time.Hour
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

func normalizePrimaryOperatorMountPathForSession(actor string, rawPrimary string, normalizedMounts []string) (string, error) {
	rawPrimary = strings.TrimSpace(rawPrimary)
	if rawPrimary == "" {
		return "", nil
	}
	if defaultLabel(actor, "client") != "haven" {
		return "", fmt.Errorf("primary_operator_mount_path is only accepted for actor haven")
	}
	if len(normalizedMounts) == 0 {
		return "", fmt.Errorf("primary_operator_mount_path requires operator_mount_paths")
	}
	canon, err := canonicalizeOperatorMountPath(rawPrimary)
	if err != nil {
		return "", fmt.Errorf("primary_operator_mount_path: %w", err)
	}
	for _, mountPath := range normalizedMounts {
		if mountPath == canon {
			return canon, nil
		}
	}
	return "", fmt.Errorf("primary_operator_mount_path must match one of operator_mount_paths")
}

type operatorMountSessionBinding struct {
	primary string
	paths   []string
}

type operatorMountWriteGrant struct {
	root      string
	expiresAt time.Time
}

func operatorMountBindingFromContext(server *Server, ctx context.Context) (operatorMountSessionBinding, error) {
	sid, _ := ctx.Value(operatorMountCtxKey{}).(string)
	sid = strings.TrimSpace(sid)
	if sid == "" {
		return operatorMountSessionBinding{}, fmt.Errorf("missing control session binding for operator mount tool")
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	sess, ok := server.sessions[sid]
	if !ok {
		return operatorMountSessionBinding{}, fmt.Errorf("unknown control session")
	}
	return operatorMountSessionBinding{
		primary: strings.TrimSpace(sess.PrimaryOperatorMountPath),
		paths:   append([]string(nil), sess.OperatorMountPaths...),
	}, nil
}

func operatorMountToolRoots(server *Server, ctx context.Context) (repoRoot string, allowedRoots []string, err error) {
	binding, err := operatorMountBindingFromContext(server, ctx)
	if err != nil {
		return "", nil, err
	}
	if len(binding.paths) == 0 {
		return "", nil, fmt.Errorf("no operator directory grants for this session; allow read in Haven or use /adir")
	}
	repoRoot = strings.TrimSpace(binding.primary)
	if repoRoot == "" {
		repoRoot = binding.paths[0]
	}
	return repoRoot, binding.paths, nil
}

func operatorMountBindingForControlSession(server *Server, controlSessionID string) (operatorMountSessionBinding, error) {
	controlSessionID = strings.TrimSpace(controlSessionID)
	if controlSessionID == "" {
		return operatorMountSessionBinding{}, fmt.Errorf("missing control session binding for operator mount tool")
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	controlSession, found := server.sessions[controlSessionID]
	if !found {
		return operatorMountSessionBinding{}, fmt.Errorf("unknown control session")
	}
	return operatorMountSessionBinding{
		primary: strings.TrimSpace(controlSession.PrimaryOperatorMountPath),
		paths:   append([]string(nil), controlSession.OperatorMountPaths...),
	}, nil
}

func operatorMountWriteGrantForRequest(server *Server, controlSessionID string, capabilityRequest CapabilityRequest) (operatorMountWriteGrant, bool, error) {
	if capabilityRequest.Capability != "operator_mount.fs_write" && capabilityRequest.Capability != "operator_mount.fs_mkdir" {
		return operatorMountWriteGrant{}, false, nil
	}
	binding, err := operatorMountBindingForControlSession(server, controlSessionID)
	if err != nil {
		return operatorMountWriteGrant{}, false, err
	}
	if len(binding.paths) == 0 {
		return operatorMountWriteGrant{}, false, fmt.Errorf("no operator directory grants for this session")
	}
	repoRoot := strings.TrimSpace(binding.primary)
	if repoRoot == "" {
		repoRoot = binding.paths[0]
	}
	pathValue := strings.TrimSpace(capabilityRequest.Arguments["path"])
	if pathValue == "" {
		return operatorMountWriteGrant{}, false, fmt.Errorf("missing path argument")
	}
	safePathExplanation, err := safety.ExplainSafePath(repoRoot, binding.paths, nil, pathValue)
	if err != nil {
		return operatorMountWriteGrant{}, false, err
	}
	matchedRoot := strings.TrimSpace(safePathExplanation.AllowedMatch)
	if matchedRoot == "" {
		return operatorMountWriteGrant{}, false, fmt.Errorf("matched root not found")
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	controlSession, found := server.sessions[controlSessionID]
	if !found {
		return operatorMountWriteGrant{}, false, fmt.Errorf("unknown control session")
	}
	if controlSession.OperatorMountWriteGrants == nil {
		return operatorMountWriteGrant{root: matchedRoot}, false, nil
	}
	expiresAt, granted := controlSession.OperatorMountWriteGrants[matchedRoot]
	if !granted {
		return operatorMountWriteGrant{root: matchedRoot}, false, nil
	}
	if !expiresAt.IsZero() && server.now().UTC().After(expiresAt) {
		delete(controlSession.OperatorMountWriteGrants, matchedRoot)
		server.sessions[controlSessionID] = controlSession
		return operatorMountWriteGrant{root: matchedRoot}, false, nil
	}
	return operatorMountWriteGrant{root: matchedRoot, expiresAt: expiresAt}, true, nil
}

type operatorMountFSRead struct{ server *Server }

func (operatorMountFSRead) Name() string      { return "operator_mount.fs_read" }
func (operatorMountFSRead) Category() string  { return "filesystem" }
func (operatorMountFSRead) Operation() string { return toolspkg.OpRead }
func (t operatorMountFSRead) Schema() toolspkg.Schema {
	return (&toolspkg.FSRead{}).Schema()
}
func (t operatorMountFSRead) Execute(ctx context.Context, args map[string]string) (string, error) {
	repoRoot, mounts, err := operatorMountToolRoots(t.server, ctx)
	if err != nil {
		return "", err
	}
	fr := &toolspkg.FSRead{RepoRoot: repoRoot, AllowedRoots: mounts, DeniedPaths: nil}
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
	repoRoot, mounts, err := operatorMountToolRoots(t.server, ctx)
	if err != nil {
		return "", err
	}
	fl := &toolspkg.FSList{RepoRoot: repoRoot, AllowedRoots: mounts, DeniedPaths: nil}
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
	repoRoot, mounts, err := operatorMountToolRoots(t.server, ctx)
	if err != nil {
		return "", err
	}
	fw := &toolspkg.FSWrite{RepoRoot: repoRoot, AllowedRoots: mounts, DeniedPaths: nil}
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
	repoRoot, mounts, err := operatorMountToolRoots(t.server, ctx)
	if err != nil {
		return "", err
	}
	fm := &toolspkg.FSMkdir{RepoRoot: repoRoot, AllowedRoots: mounts, DeniedPaths: nil}
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
