package tools

import "loopgate/internal/config"

// NewDefaultRegistry creates a registry with the standard filesystem tools
// rooted at repoRoot. Used by local operator clients and the Loopgate server.
func NewDefaultRegistry(repoRoot string, policy config.Policy) (*Registry, error) {
	fsCfg := policy.Tools.Filesystem
	return newRegistryWithRoot(repoRoot, fsCfg.AllowedRoots, fsCfg.DeniedPaths, policy)
}

// NewSandboxRegistry creates a registry with filesystem tools rooted in a
// sandbox home directory.
func NewSandboxRegistry(sandboxHome string, policy config.Policy) (*Registry, error) {
	// Override allowed roots to be the sandbox home itself.
	// Denied paths protect sandbox internals.
	allowedRoots := []string{"."}
	deniedPaths := []string{"agents", "logs"}
	registry, err := newRegistryWithRoot(sandboxHome, allowedRoots, deniedPaths, policy)
	if err != nil {
		return nil, err
	}

	return registry, nil
}

func newRegistryWithRoot(root string, allowedRoots []string, deniedPaths []string, policy config.Policy) (*Registry, error) {
	reg := NewRegistry()

	if err := reg.TryRegister(&FSRead{
		RepoRoot:     root,
		AllowedRoots: allowedRoots,
		DeniedPaths:  deniedPaths,
	}); err != nil {
		return nil, err
	}

	if err := reg.TryRegister(&FSWrite{
		RepoRoot:     root,
		AllowedRoots: allowedRoots,
		DeniedPaths:  deniedPaths,
	}); err != nil {
		return nil, err
	}

	if err := reg.TryRegister(&FSList{
		RepoRoot:     root,
		AllowedRoots: allowedRoots,
		DeniedPaths:  deniedPaths,
	}); err != nil {
		return nil, err
	}

	if err := reg.TryRegister(&FSMkdir{
		RepoRoot:     root,
		AllowedRoots: allowedRoots,
		DeniedPaths:  deniedPaths,
	}); err != nil {
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
			WorkDir:         root,
			AllowedCommands: append([]string(nil), policy.Tools.Shell.AllowedCommands...),
		}); err != nil {
			return nil, err
		}
	}

	if err := reg.TryRegister(&InvokeCapability{}); err != nil {
		return nil, err
	}

	return reg, nil
}
