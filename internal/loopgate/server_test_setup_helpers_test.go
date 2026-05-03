package loopgate

import (
	"context"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"loopgate/internal/config"
	"loopgate/internal/testutil"
)

func newShortLoopgateTestRepoRoot(t *testing.T) string {
	t.Helper()

	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	workspaceRoot := filepath.Clean(filepath.Join(workingDirectory, "..", ".."))
	baseDir := filepath.Join(workspaceRoot, ".tmp-loopgate-tests")
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		t.Fatalf("mkdir short test base dir: %v", err)
	}
	repoRoot, err := os.MkdirTemp(baseDir, "rt-")
	if err != nil {
		t.Fatalf("mkdir short test repo root: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(repoRoot) })
	return repoRoot
}

func newShortLoopgateSocketPath(t *testing.T) string {
	t.Helper()

	socketFile, err := os.CreateTemp(os.TempDir(), "loopgate-*.sock")
	if err != nil {
		t.Fatalf("create short socket file: %v", err)
	}
	socketPath := socketFile.Name()
	_ = socketFile.Close()
	_ = os.Remove(socketPath)
	t.Cleanup(func() { _ = os.Remove(socketPath) })
	return socketPath
}

func startLoopgateServer(t *testing.T, repoRoot string, policyYAML string) (*Client, controlapipkg.StatusResponse, *Server) {
	return startLoopgateServerWithRuntime(t, repoRoot, policyYAML, nil, true)
}

func writeSignedTestPolicyYAML(t *testing.T, repoRoot string, policyYAML string) {
	t.Helper()

	policySigner, err := testutil.NewPolicyTestSigner()
	if err != nil {
		t.Fatalf("new test policy signer: %v", err)
	}
	policySigner.ConfigureEnv(t.Setenv)
	if err := policySigner.WriteSignedPolicyYAML(repoRoot, policyYAML); err != nil {
		t.Fatalf("write signed policy: %v", err)
	}
}

func writeSignedTestOperatorOverrideDocument(t *testing.T, repoRoot string, policySigner *testutil.PolicyTestSigner, document config.OperatorOverrideDocument) {
	t.Helper()

	policySigner.ConfigureEnv(t.Setenv)
	documentBytes, err := config.MarshalOperatorOverrideDocumentYAML(document)
	if err != nil {
		t.Fatalf("marshal operator override document: %v", err)
	}
	signatureFile, err := config.SignOperatorOverrideDocument(documentBytes, policySigner.KeyID, policySigner.PrivateKey)
	if err != nil {
		t.Fatalf("sign operator override document: %v", err)
	}
	if err := config.WriteOperatorOverrideDocumentYAML(repoRoot, document); err != nil {
		t.Fatalf("write operator override document: %v", err)
	}
	if err := config.WriteOperatorOverrideSignatureYAML(repoRoot, signatureFile); err != nil {
		t.Fatalf("write operator override signature: %v", err)
	}
}

func pinTestProcessAsExpectedClient(t *testing.T, server *Server) {
	t.Helper()

	testExecutablePath, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test executable: %v", err)
	}
	normalizedExecutablePath := normalizeSessionExecutablePinPath(testExecutablePath)
	if strings.TrimSpace(normalizedExecutablePath) == "" {
		t.Fatal("expected normalized test executable path")
	}
	server.expectedClientPath = normalizedExecutablePath
}

// startLoopgateServerWithRuntime starts Loopgate in a temp repo. When runSessionBootstrap is false,
// the server is healthy but no control session is opened (for tests where session open must fail).

func startLoopgateServerWithRuntime(t *testing.T, repoRoot string, policyYAML string, runtimeCfg *config.RuntimeConfig, runSessionBootstrap bool) (*Client, controlapipkg.StatusResponse, *Server) {
	t.Helper()

	policySigner, err := testutil.NewPolicyTestSigner()
	if err != nil {
		t.Fatalf("new test policy signer: %v", err)
	}
	return startLoopgateServerWithSignerAndRuntime(t, repoRoot, policyYAML, policySigner, runtimeCfg, runSessionBootstrap)
}

func startLoopgateServerWithSignerAndRuntime(t *testing.T, repoRoot string, policyYAML string, policySigner *testutil.PolicyTestSigner, runtimeCfg *config.RuntimeConfig, runSessionBootstrap bool) (*Client, controlapipkg.StatusResponse, *Server) {
	t.Helper()

	policySigner.ConfigureEnv(t.Setenv)
	if err := policySigner.WriteSignedPolicyYAML(repoRoot, policyYAML); err != nil {
		t.Fatalf("write signed policy: %v", err)
	}
	if runtimeCfg != nil {
		if err := config.WriteRuntimeConfigYAML(repoRoot, *runtimeCfg); err != nil {
			t.Fatalf("write runtime config: %v", err)
		}
	}

	socketPath := newShortLoopgateSocketPath(t)
	server, err := NewServer(repoRoot, socketPath)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	server.sessionOpenMinInterval = 0
	server.maxActiveSessionsPerUID = 64
	server.expirySweepMaxInterval = 0

	serverContext, cancel := context.WithCancel(context.Background())
	serverDone := make(chan struct{})
	serveErrCh := make(chan error, 1)
	go func() {
		defer close(serverDone)
		serveErrCh <- server.Serve(serverContext)
	}()
	t.Cleanup(func() {
		cancel()
		<-serverDone
	})

	client := NewClient(socketPath)
	deadline := time.Now().Add(2 * time.Second)
	for {
		_, err = client.Health(context.Background())
		if err == nil {
			break
		}
		select {
		case serveErr := <-serveErrCh:
			t.Fatalf("loopgate serve exited before health check: %v", serveErr)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("wait for loopgate health: %v", err)
		}
		time.Sleep(25 * time.Millisecond)
	}

	if !runSessionBootstrap {
		return client, controlapipkg.StatusResponse{}, server
	}

	client.ConfigureSession("test-actor", "test-session", []string{"fs_list"})
	status, err := client.Status(context.Background())
	if err != nil {
		t.Fatalf("bootstrap status after session: %v", err)
	}
	client.ConfigureSession("test-actor", "test-session", advertisedSessionCapabilityNames(status))
	status, err = client.Status(context.Background())
	if err != nil {
		t.Fatalf("final status after advertised session bootstrap: %v", err)
	}
	server.mu.Lock()
	server.sessionState.openByUID = make(map[uint32]time.Time)
	server.mu.Unlock()
	return client, status, server
}
