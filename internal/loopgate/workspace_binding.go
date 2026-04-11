package loopgate

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
)

func (server *Server) deriveWorkspaceIDFromRepoRoot() string {
	absoluteRepoRoot, err := filepath.Abs(server.repoRoot)
	if err != nil {
		absoluteRepoRoot = server.repoRoot
	}
	workspaceHash := sha256.Sum256([]byte(absoluteRepoRoot))
	return hex.EncodeToString(workspaceHash[:])
}
