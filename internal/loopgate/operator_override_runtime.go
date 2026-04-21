package loopgate

import (
	"fmt"

	"loopgate/internal/config"
)

type serverOperatorOverrideRuntime struct {
	document       config.OperatorOverrideDocument
	contentSHA256  string
	signatureKeyID string
	present        bool
}

func (server *Server) currentOperatorOverrideRuntime() serverOperatorOverrideRuntime {
	server.operatorOverrideRuntimeMu.RLock()
	runtime := server.operatorOverrideRuntime
	server.operatorOverrideRuntimeMu.RUnlock()
	return runtime
}

func (server *Server) storeOperatorOverrideRuntime(runtime serverOperatorOverrideRuntime) {
	server.operatorOverrideRuntimeMu.Lock()
	server.operatorOverrideRuntime = runtime
	server.operatorOverrideRuntimeMu.Unlock()
}

func (server *Server) reloadOperatorOverrideRuntimeFromDisk() (serverOperatorOverrideRuntime, error) {
	loadResult, err := config.LoadOperatorOverrideDocumentWithHash(server.repoRoot)
	if err != nil {
		return serverOperatorOverrideRuntime{}, fmt.Errorf("load operator override document: %w", err)
	}
	return serverOperatorOverrideRuntime{
		document:       loadResult.Document,
		contentSHA256:  loadResult.ContentSHA256,
		signatureKeyID: loadResult.SignatureKeyID,
		present:        loadResult.Present,
	}, nil
}
