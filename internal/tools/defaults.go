package tools

import (
	"path/filepath"
	"strings"

	"morph/internal/config"
)

// NewDefaultRegistry creates a registry with the standard filesystem tools
// rooted at repoRoot. Used by the Haven client and the Loopgate server.
func NewDefaultRegistry(repoRoot string, policy config.Policy) (*Registry, error) {
	fsCfg := policy.Tools.Filesystem
	return newRegistryWithRoot(repoRoot, fsCfg.AllowedRoots, fsCfg.DeniedPaths, policy, false)
}

// NewSandboxRegistry creates a registry with filesystem tools rooted in a
// sandbox home directory. Used by Haven so Morph operates inside its own
// virtual filesystem instead of the user's real filesystem.
func NewSandboxRegistry(repoRoot string, sandboxHome string, policy config.Policy) (*Registry, error) {
	// Override allowed roots to be the sandbox home itself.
	// Denied paths protect sandbox internals.
	allowedRoots := []string{"."}
	deniedPaths := []string{"agents", "logs"}
	registry, err := newRegistryWithRoot(sandboxHome, allowedRoots, deniedPaths, policy, true)
	if err != nil {
		return nil, err
	}

	if err := registry.TryRegister(WrapTrustedSandboxLocal(&JournalList{
		Root: sandboxHome,
	})); err != nil {
		return nil, err
	}
	if err := registry.TryRegister(WrapTrustedSandboxLocal(&JournalRead{
		Root: sandboxHome,
	})); err != nil {
		return nil, err
	}
	if err := registry.TryRegister(WrapTrustedSandboxLocal(&JournalWrite{
		Root: sandboxHome,
	})); err != nil {
		return nil, err
	}
	if err := registry.TryRegister(WrapTrustedSandboxLocal(&HavenOperatorContextTool{})); err != nil {
		return nil, err
	}
	if err := registry.TryRegister(WrapTrustedSandboxLocal(&NotesList{
		Root: sandboxHome,
	})); err != nil {
		return nil, err
	}
	if err := registry.TryRegister(WrapTrustedSandboxLocal(&NotesRead{
		Root: sandboxHome,
	})); err != nil {
		return nil, err
	}
	if err := registry.TryRegister(WrapTrustedSandboxLocal(&NotesWrite{
		Root: sandboxHome,
	})); err != nil {
		return nil, err
	}
	if err := registry.TryRegister(WrapTrustedSandboxLocal(&PaintList{
		Root: sandboxHome,
	})); err != nil {
		return nil, err
	}
	if err := registry.TryRegister(WrapTrustedSandboxLocal(&PaintSave{
		Root: sandboxHome,
	})); err != nil {
		return nil, err
	}
	if err := registry.TryRegister(WrapTrustedSandboxLocal(&TodoAdd{})); err != nil {
		return nil, err
	}
	if err := registry.TryRegister(WrapTrustedSandboxLocal(&TodoComplete{})); err != nil {
		return nil, err
	}
	if err := registry.TryRegister(WrapTrustedSandboxLocal(&TodoList{})); err != nil {
		return nil, err
	}
	if strings.TrimSpace(repoRoot) != "" {
		stateDir := filepath.Join(repoRoot, "runtime", "state")
		if err := registry.TryRegister(WrapTrustedSandboxLocal(&NoteCreate{
			StateDir: stateDir,
		})); err != nil {
			return nil, err
		}
		if err := registry.TryRegister(WrapTrustedSandboxLocal(&DesktopOrganize{
			StateDir: stateDir,
		})); err != nil {
			return nil, err
		}
	}

	return registry, nil
}

func newRegistryWithRoot(root string, allowedRoots []string, deniedPaths []string, policy config.Policy, trustedSandboxLocal bool) (*Registry, error) {
	reg := NewRegistry()
	registerTool := func(tool Tool) error {
		if trustedSandboxLocal {
			return reg.TryRegister(WrapTrustedSandboxLocal(tool))
		}
		return reg.TryRegister(tool)
	}

	if err := registerTool(&FSRead{
		RepoRoot:     root,
		AllowedRoots: allowedRoots,
		DeniedPaths:  deniedPaths,
	}); err != nil {
		return nil, err
	}

	if err := registerTool(&FSWrite{
		RepoRoot:     root,
		AllowedRoots: allowedRoots,
		DeniedPaths:  deniedPaths,
	}); err != nil {
		return nil, err
	}

	if err := registerTool(&FSList{
		RepoRoot:     root,
		AllowedRoots: allowedRoots,
		DeniedPaths:  deniedPaths,
	}); err != nil {
		return nil, err
	}

	if err := registerTool(&FSMkdir{
		RepoRoot:     root,
		AllowedRoots: allowedRoots,
		DeniedPaths:  deniedPaths,
	}); err != nil {
		return nil, err
	}

	if err := registerTool(&MemoryRemember{}); err != nil {
		return nil, err
	}

	// Host folder capabilities are Loopgate-executed; never mark as trusted-sandbox-local.
	if err := reg.TryRegister(&HostFolderList{}); err != nil {
		return nil, err
	}
	if err := reg.TryRegister(&HostFolderRead{}); err != nil {
		return nil, err
	}
	if err := reg.TryRegister(&HostOrganizePlan{}); err != nil {
		return nil, err
	}
	if err := reg.TryRegister(&HostPlanApply{}); err != nil {
		return nil, err
	}

	if policy.Tools.Shell.Enabled {
		if err := reg.TryRegister(&ShellExec{
			WorkDir: root,
		}); err != nil {
			return nil, err
		}
	}

	if err := reg.TryRegister(&InvokeCapability{}); err != nil {
		return nil, err
	}

	return reg, nil
}
