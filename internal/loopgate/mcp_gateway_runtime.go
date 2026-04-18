package loopgate

import (
	"bufio"
	"context"
	"fmt"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"loopgate/internal/secrets"
)

const (
	mcpGatewayServerStateStarting = "starting"
	mcpGatewayServerStateLaunched = "launched"
)

type mcpGatewayLaunchedServer struct {
	ServerID                   string
	Transport                  string
	LaunchState                string
	LaunchAttemptID            string
	PID                        int
	StartedAt                  time.Time
	WorkingDirectory           string
	CommandPath                string
	CommandArgs                []string
	AllowedEnvironment         []string
	SecretEnvironmentVariables []string
	StderrPath                 string
	StdinWriter                *os.File
	StdoutReader               *os.File
	StdoutBufferedReader       *bufio.Reader
	Initialized                bool
	// ioMu serializes stdio access for a single launched MCP server process.
	//
	// Why this lock exists:
	//   - MCP stdio is a single ordered byte stream
	//   - concurrent writes or read/write interleaving would corrupt framing and
	//     make launch/execute debugging impossible
	//
	// Sequencing rule:
	//   - ioMu is process-local; never hold it while touching Server mutexes
	ioMu sync.Mutex
}

func buildMCPGatewayEnsureLaunchResponse(launchedServer *mcpGatewayLaunchedServer, reused bool) controlapipkg.MCPGatewayEnsureLaunchResponse {
	return controlapipkg.MCPGatewayEnsureLaunchResponse{
		ServerID:                   launchedServer.ServerID,
		Transport:                  launchedServer.Transport,
		LaunchState:                launchedServer.LaunchState,
		PID:                        launchedServer.PID,
		Reused:                     reused,
		WorkingDirectory:           launchedServer.WorkingDirectory,
		CommandPath:                launchedServer.CommandPath,
		CommandArgs:                append([]string(nil), launchedServer.CommandArgs...),
		AllowedEnvironment:         append([]string(nil), launchedServer.AllowedEnvironment...),
		SecretEnvironmentVariables: append([]string(nil), launchedServer.SecretEnvironmentVariables...),
		StderrPath:                 launchedServer.StderrPath,
	}
}

func buildMCPGatewayStopResponse(launchedServer *mcpGatewayLaunchedServer, stopped bool, serverID string) controlapipkg.MCPGatewayStopResponse {
	stopResponse := controlapipkg.MCPGatewayStopResponse{
		ServerID: strings.TrimSpace(serverID),
		Stopped:  stopped,
	}
	if launchedServer == nil {
		return stopResponse
	}
	stopResponse.ServerID = launchedServer.ServerID
	stopResponse.Transport = launchedServer.Transport
	stopResponse.PreviousLaunchState = launchedServer.LaunchState
	stopResponse.PID = launchedServer.PID
	return stopResponse
}

func buildMCPGatewayServerRuntimeView(serverManifest mcpGatewayServerManifest, launchedServer *mcpGatewayLaunchedServer) controlapipkg.MCPGatewayServerRuntimeView {
	runtimeView := controlapipkg.MCPGatewayServerRuntimeView{
		ServerID:        serverManifest.ServerID,
		DeclaredEnabled: serverManifest.Enabled,
		Transport:       serverManifest.Transport,
		RuntimeState:    "absent",
	}
	if launchedServer == nil {
		return runtimeView
	}

	runtimeView.ServerID = launchedServer.ServerID
	runtimeView.Transport = launchedServer.Transport
	runtimeView.RuntimeState = launchedServer.LaunchState
	runtimeView.PID = launchedServer.PID
	runtimeView.Initialized = launchedServer.Initialized
	runtimeView.WorkingDirectory = launchedServer.WorkingDirectory
	runtimeView.CommandPath = launchedServer.CommandPath
	runtimeView.StderrPath = launchedServer.StderrPath
	runtimeView.LastKnownLaunchID = launchedServer.LaunchAttemptID
	if !launchedServer.StartedAt.IsZero() {
		runtimeView.StartedAtUTC = launchedServer.StartedAt.UTC().Format(time.RFC3339Nano)
	}
	return runtimeView
}

func buildMCPGatewayServerLaunchedAuditData(tokenClaims capabilityToken, launchedServer *mcpGatewayLaunchedServer) map[string]interface{} {
	return map[string]interface{}{
		"control_session_id":       tokenClaims.ControlSessionID,
		"actor_label":              tokenClaims.ActorLabel,
		"client_session_label":     tokenClaims.ClientSessionLabel,
		"tenant_id":                tokenClaims.TenantID,
		"user_id":                  tokenClaims.UserID,
		"server_id":                launchedServer.ServerID,
		"transport":                launchedServer.Transport,
		"launch_state":             launchedServer.LaunchState,
		"pid":                      launchedServer.PID,
		"working_directory":        launchedServer.WorkingDirectory,
		"command_path":             launchedServer.CommandPath,
		"command_args":             append([]string(nil), launchedServer.CommandArgs...),
		"allowed_environment":      append([]string(nil), launchedServer.AllowedEnvironment...),
		"secret_environment_names": append([]string(nil), launchedServer.SecretEnvironmentVariables...),
		"stderr_path":              launchedServer.StderrPath,
	}
}

func buildMCPGatewayServerStoppedAuditData(tokenClaims capabilityToken, launchedServer *mcpGatewayLaunchedServer) map[string]interface{} {
	return map[string]interface{}{
		"control_session_id":       tokenClaims.ControlSessionID,
		"actor_label":              tokenClaims.ActorLabel,
		"client_session_label":     tokenClaims.ClientSessionLabel,
		"tenant_id":                tokenClaims.TenantID,
		"user_id":                  tokenClaims.UserID,
		"server_id":                launchedServer.ServerID,
		"transport":                launchedServer.Transport,
		"previous_launch_state":    launchedServer.LaunchState,
		"pid":                      launchedServer.PID,
		"working_directory":        launchedServer.WorkingDirectory,
		"command_path":             launchedServer.CommandPath,
		"command_args":             append([]string(nil), launchedServer.CommandArgs...),
		"allowed_environment":      append([]string(nil), launchedServer.AllowedEnvironment...),
		"secret_environment_names": append([]string(nil), launchedServer.SecretEnvironmentVariables...),
		"stderr_path":              launchedServer.StderrPath,
	}
}

func resolveMCPGatewayWorkingDirectory(repoRoot string, workingDirectory string) string {
	trimmedWorkingDirectory := strings.TrimSpace(workingDirectory)
	if trimmedWorkingDirectory == "" || trimmedWorkingDirectory == "." {
		return filepath.Clean(repoRoot)
	}
	if filepath.IsAbs(trimmedWorkingDirectory) {
		return filepath.Clean(trimmedWorkingDirectory)
	}
	return filepath.Clean(filepath.Join(repoRoot, trimmedWorkingDirectory))
}

func sortedSecretEnvironmentVariableNames(secretEnvironment map[string]secrets.SecretRef) []string {
	environmentVariableNames := make([]string, 0, len(secretEnvironment))
	for environmentVariableName := range secretEnvironment {
		environmentVariableNames = append(environmentVariableNames, environmentVariableName)
	}
	slices.Sort(environmentVariableNames)
	return environmentVariableNames
}

func (server *Server) buildMCPGatewayLaunchEnvironment(ctx context.Context, serverManifest mcpGatewayServerManifest) ([]string, error) {
	launchEnvironment := make([]string, 0, len(serverManifest.AllowedEnvironment)+len(serverManifest.SecretEnvironment))
	for _, environmentVariableName := range serverManifest.AllowedEnvironment {
		if rawEnvironmentValue, found := os.LookupEnv(environmentVariableName); found {
			launchEnvironment = append(launchEnvironment, environmentVariableName+"="+rawEnvironmentValue)
		}
	}

	secretEnvironmentVariableNames := sortedSecretEnvironmentVariableNames(serverManifest.SecretEnvironment)
	for _, environmentVariableName := range secretEnvironmentVariableNames {
		secretRef := serverManifest.SecretEnvironment[environmentVariableName]
		if err := secretRef.Validate(); err != nil {
			return nil, fmt.Errorf("validate secret environment %q: %w", environmentVariableName, err)
		}
		secretStore, err := server.secretStoreForRef(secretRef)
		if err != nil {
			return nil, fmt.Errorf("resolve secret environment %q: %w", environmentVariableName, err)
		}
		rawSecretBytes, _, err := secretStore.Get(ctx, secretRef)
		if err != nil {
			return nil, fmt.Errorf("load secret environment %q: %w", environmentVariableName, err)
		}
		launchEnvironment = append(launchEnvironment, environmentVariableName+"="+string(rawSecretBytes))
		clear(rawSecretBytes)
	}

	return launchEnvironment, nil
}

func closeMCPGatewayPipe(fileHandle *os.File) {
	if fileHandle == nil {
		return
	}
	_ = fileHandle.Close()
}

func closeMCPGatewayLaunchedServerPipes(launchedServer *mcpGatewayLaunchedServer) {
	if launchedServer == nil {
		return
	}
	closeMCPGatewayPipe(launchedServer.StdinWriter)
	closeMCPGatewayPipe(launchedServer.StdoutReader)
}

func killMCPGatewayProcessByPID(pid int) {
	if pid <= 0 {
		return
	}
	processHandle, err := os.FindProcess(pid)
	if err != nil {
		return
	}
	_ = processHandle.Kill()
}

func (server *Server) cleanupDeadMCPGatewayServerIfNeeded(serverID string) {
	server.mu.Lock()
	launchedServer, found := server.mcpGatewayLaunchedServers[serverID]
	if !found || launchedServer == nil || launchedServer.LaunchState != mcpGatewayServerStateLaunched || launchedServer.PID <= 0 {
		server.mu.Unlock()
		return
	}
	launchedServerPID := launchedServer.PID
	server.mu.Unlock()

	processStillAlive, err := server.processExists(launchedServerPID)
	if err != nil || processStillAlive {
		return
	}

	server.mu.Lock()
	currentLaunchedServer, found := server.mcpGatewayLaunchedServers[serverID]
	if !found || currentLaunchedServer != launchedServer || currentLaunchedServer.LaunchState != mcpGatewayServerStateLaunched || currentLaunchedServer.PID != launchedServerPID {
		server.mu.Unlock()
		return
	}
	delete(server.mcpGatewayLaunchedServers, serverID)
	server.mu.Unlock()
	closeMCPGatewayLaunchedServerPipes(launchedServer)
}

func (server *Server) ensureMCPGatewayServerLaunched(ctx context.Context, tokenClaims capabilityToken, ensureLaunchRequest controlapipkg.MCPGatewayEnsureLaunchRequest) (controlapipkg.MCPGatewayEnsureLaunchResponse, controlapipkg.CapabilityResponse, bool) {
	serverManifest, err := server.resolveMCPGatewayServerManifest(ensureLaunchRequest.ServerID)
	if err != nil {
		return controlapipkg.MCPGatewayEnsureLaunchResponse{}, controlapipkg.CapabilityResponse{
			RequestID:    strings.TrimSpace(ensureLaunchRequest.ServerID),
			Status:       controlapipkg.ResponseStatusDenied,
			DenialReason: err.Error(),
			DenialCode:   mcpGatewayDecisionDenialCode(err),
		}, false
	}
	if serverManifest.Transport != "stdio" {
		return controlapipkg.MCPGatewayEnsureLaunchResponse{}, controlapipkg.CapabilityResponse{
			RequestID:    serverManifest.ServerID,
			Status:       controlapipkg.ResponseStatusDenied,
			DenialReason: "only stdio MCP transports are supported in the current broker launch slice",
			DenialCode:   controlapipkg.DenialCodeExecutionFailed,
		}, false
	}

	server.cleanupDeadMCPGatewayServerIfNeeded(serverManifest.ServerID)

	server.mu.Lock()
	if launchedServer, found := server.mcpGatewayLaunchedServers[serverManifest.ServerID]; found {
		switch launchedServer.LaunchState {
		case mcpGatewayServerStateLaunched:
			server.mu.Unlock()
			return buildMCPGatewayEnsureLaunchResponse(launchedServer, true), controlapipkg.CapabilityResponse{}, true
		case mcpGatewayServerStateStarting:
			server.mu.Unlock()
			return controlapipkg.MCPGatewayEnsureLaunchResponse{}, controlapipkg.CapabilityResponse{
				RequestID:    serverManifest.ServerID,
				Status:       controlapipkg.ResponseStatusError,
				DenialReason: "mcp gateway server launch is already in progress",
				DenialCode:   controlapipkg.DenialCodeExecutionFailed,
			}, false
		}
	}

	launchAttemptID, err := randomHex(12)
	if err != nil {
		server.mu.Unlock()
		return controlapipkg.MCPGatewayEnsureLaunchResponse{}, controlapipkg.CapabilityResponse{
			RequestID:    serverManifest.ServerID,
			Status:       controlapipkg.ResponseStatusError,
			DenialReason: "failed to allocate mcp gateway launch attempt id",
			DenialCode:   controlapipkg.DenialCodeExecutionFailed,
		}, false
	}
	startingRecord := mcpGatewayLaunchedServer{
		ServerID:        serverManifest.ServerID,
		Transport:       serverManifest.Transport,
		LaunchState:     mcpGatewayServerStateStarting,
		LaunchAttemptID: launchAttemptID,
		StartedAt:       server.now().UTC(),
	}
	server.mcpGatewayLaunchedServers[serverManifest.ServerID] = &startingRecord
	server.mu.Unlock()

	workingDirectory := resolveMCPGatewayWorkingDirectory(server.repoRoot, serverManifest.WorkingDirectory)
	commandPath := strings.TrimSpace(serverManifest.LaunchCommand)
	if !filepath.IsAbs(commandPath) {
		resolvedCommandPath, lookPathErr := exec.LookPath(commandPath)
		if lookPathErr != nil {
			server.mu.Lock()
			currentRecord, found := server.mcpGatewayLaunchedServers[serverManifest.ServerID]
			if found && currentRecord.LaunchAttemptID == launchAttemptID && currentRecord.LaunchState == mcpGatewayServerStateStarting {
				delete(server.mcpGatewayLaunchedServers, serverManifest.ServerID)
			}
			server.mu.Unlock()
			return controlapipkg.MCPGatewayEnsureLaunchResponse{}, controlapipkg.CapabilityResponse{
				RequestID:    serverManifest.ServerID,
				Status:       controlapipkg.ResponseStatusError,
				DenialReason: "failed to resolve mcp gateway launch command",
				DenialCode:   controlapipkg.DenialCodeExecutionFailed,
				Redacted:     true,
			}, false
		}
		commandPath = resolvedCommandPath
	}

	launchEnvironment, err := server.buildMCPGatewayLaunchEnvironment(ctx, serverManifest)
	if err != nil {
		server.mu.Lock()
		currentRecord, found := server.mcpGatewayLaunchedServers[serverManifest.ServerID]
		if found && currentRecord.LaunchAttemptID == launchAttemptID && currentRecord.LaunchState == mcpGatewayServerStateStarting {
			delete(server.mcpGatewayLaunchedServers, serverManifest.ServerID)
		}
		server.mu.Unlock()
		return controlapipkg.MCPGatewayEnsureLaunchResponse{}, controlapipkg.CapabilityResponse{
			RequestID:    serverManifest.ServerID,
			Status:       controlapipkg.ResponseStatusError,
			DenialReason: "failed to resolve mcp gateway launch environment",
			DenialCode:   controlapipkg.DenialCodeExecutionFailed,
			Redacted:     true,
		}, false
	}

	if err := os.MkdirAll(filepath.Join(server.repoRoot, "runtime", "logs", "mcp_gateway"), 0o700); err != nil {
		server.mu.Lock()
		currentRecord, found := server.mcpGatewayLaunchedServers[serverManifest.ServerID]
		if found && currentRecord.LaunchAttemptID == launchAttemptID && currentRecord.LaunchState == mcpGatewayServerStateStarting {
			delete(server.mcpGatewayLaunchedServers, serverManifest.ServerID)
		}
		server.mu.Unlock()
		return controlapipkg.MCPGatewayEnsureLaunchResponse{}, controlapipkg.CapabilityResponse{
			RequestID:    serverManifest.ServerID,
			Status:       controlapipkg.ResponseStatusError,
			DenialReason: "failed to create mcp gateway log directory",
			DenialCode:   controlapipkg.DenialCodeExecutionFailed,
		}, false
	}
	stderrPath := filepath.Join(server.repoRoot, "runtime", "logs", "mcp_gateway", serverManifest.ServerID+".stderr.log")
	stderrFile, err := os.OpenFile(stderrPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		server.mu.Lock()
		currentRecord, found := server.mcpGatewayLaunchedServers[serverManifest.ServerID]
		if found && currentRecord.LaunchAttemptID == launchAttemptID && currentRecord.LaunchState == mcpGatewayServerStateStarting {
			delete(server.mcpGatewayLaunchedServers, serverManifest.ServerID)
		}
		server.mu.Unlock()
		return controlapipkg.MCPGatewayEnsureLaunchResponse{}, controlapipkg.CapabilityResponse{
			RequestID:    serverManifest.ServerID,
			Status:       controlapipkg.ResponseStatusError,
			DenialReason: "failed to open mcp gateway stderr log",
			DenialCode:   controlapipkg.DenialCodeExecutionFailed,
		}, false
	}

	parentReadStdout, childWriteStdout, err := os.Pipe()
	if err != nil {
		_ = stderrFile.Close()
		server.mu.Lock()
		currentRecord, found := server.mcpGatewayLaunchedServers[serverManifest.ServerID]
		if found && currentRecord.LaunchAttemptID == launchAttemptID && currentRecord.LaunchState == mcpGatewayServerStateStarting {
			delete(server.mcpGatewayLaunchedServers, serverManifest.ServerID)
		}
		server.mu.Unlock()
		return controlapipkg.MCPGatewayEnsureLaunchResponse{}, controlapipkg.CapabilityResponse{
			RequestID:    serverManifest.ServerID,
			Status:       controlapipkg.ResponseStatusError,
			DenialReason: "failed to create mcp gateway stdout pipe",
			DenialCode:   controlapipkg.DenialCodeExecutionFailed,
		}, false
	}
	childReadStdin, parentWriteStdin, err := os.Pipe()
	if err != nil {
		_ = childWriteStdout.Close()
		_ = parentReadStdout.Close()
		_ = stderrFile.Close()
		server.mu.Lock()
		currentRecord, found := server.mcpGatewayLaunchedServers[serverManifest.ServerID]
		if found && currentRecord.LaunchAttemptID == launchAttemptID && currentRecord.LaunchState == mcpGatewayServerStateStarting {
			delete(server.mcpGatewayLaunchedServers, serverManifest.ServerID)
		}
		server.mu.Unlock()
		return controlapipkg.MCPGatewayEnsureLaunchResponse{}, controlapipkg.CapabilityResponse{
			RequestID:    serverManifest.ServerID,
			Status:       controlapipkg.ResponseStatusError,
			DenialReason: "failed to create mcp gateway stdin pipe",
			DenialCode:   controlapipkg.DenialCodeExecutionFailed,
		}, false
	}

	argv := append([]string{commandPath}, serverManifest.LaunchArgs...)
	process, err := os.StartProcess(commandPath, argv, &os.ProcAttr{
		Dir: workingDirectory,
		Env: launchEnvironment,
		Files: []*os.File{
			childReadStdin,
			childWriteStdout,
			stderrFile,
		},
	})

	_ = childReadStdin.Close()
	_ = childWriteStdout.Close()
	_ = stderrFile.Close()

	if err != nil {
		_ = parentWriteStdin.Close()
		_ = parentReadStdout.Close()
		server.mu.Lock()
		currentRecord, found := server.mcpGatewayLaunchedServers[serverManifest.ServerID]
		if found && currentRecord.LaunchAttemptID == launchAttemptID && currentRecord.LaunchState == mcpGatewayServerStateStarting {
			delete(server.mcpGatewayLaunchedServers, serverManifest.ServerID)
		}
		server.mu.Unlock()
		return controlapipkg.MCPGatewayEnsureLaunchResponse{}, controlapipkg.CapabilityResponse{
			RequestID:    serverManifest.ServerID,
			Status:       controlapipkg.ResponseStatusError,
			DenialReason: "failed to launch MCP gateway server",
			DenialCode:   controlapipkg.DenialCodeExecutionFailed,
			Redacted:     true,
		}, false
	}

	launchedServer := mcpGatewayLaunchedServer{
		ServerID:                   serverManifest.ServerID,
		Transport:                  serverManifest.Transport,
		LaunchState:                mcpGatewayServerStateLaunched,
		LaunchAttemptID:            launchAttemptID,
		PID:                        process.Pid,
		StartedAt:                  server.now().UTC(),
		WorkingDirectory:           workingDirectory,
		CommandPath:                commandPath,
		CommandArgs:                append([]string(nil), serverManifest.LaunchArgs...),
		AllowedEnvironment:         append([]string(nil), serverManifest.AllowedEnvironment...),
		SecretEnvironmentVariables: sortedSecretEnvironmentVariableNames(serverManifest.SecretEnvironment),
		StderrPath:                 stderrPath,
		StdinWriter:                parentWriteStdin,
		StdoutReader:               parentReadStdout,
		StdoutBufferedReader:       bufio.NewReader(parentReadStdout),
	}

	if releaseErr := process.Release(); releaseErr != nil {
		closeMCPGatewayLaunchedServerPipes(&launchedServer)
		killMCPGatewayProcessByPID(process.Pid)
		server.mu.Lock()
		currentRecord, found := server.mcpGatewayLaunchedServers[serverManifest.ServerID]
		if found && currentRecord.LaunchAttemptID == launchAttemptID && currentRecord.LaunchState == mcpGatewayServerStateStarting {
			delete(server.mcpGatewayLaunchedServers, serverManifest.ServerID)
		}
		server.mu.Unlock()
		return controlapipkg.MCPGatewayEnsureLaunchResponse{}, controlapipkg.CapabilityResponse{
			RequestID:    serverManifest.ServerID,
			Status:       controlapipkg.ResponseStatusError,
			DenialReason: "failed to detach MCP gateway process handle",
			DenialCode:   controlapipkg.DenialCodeExecutionFailed,
		}, false
	}

	if err := server.logEvent("mcp_gateway.server_launched", tokenClaims.ControlSessionID, buildMCPGatewayServerLaunchedAuditData(tokenClaims, &launchedServer)); err != nil {
		closeMCPGatewayLaunchedServerPipes(&launchedServer)
		killMCPGatewayProcessByPID(launchedServer.PID)
		server.mu.Lock()
		currentRecord, found := server.mcpGatewayLaunchedServers[serverManifest.ServerID]
		if found && currentRecord.LaunchAttemptID == launchAttemptID && currentRecord.LaunchState == mcpGatewayServerStateStarting {
			delete(server.mcpGatewayLaunchedServers, serverManifest.ServerID)
		}
		server.mu.Unlock()
		return controlapipkg.MCPGatewayEnsureLaunchResponse{}, controlapipkg.CapabilityResponse{
			RequestID:    serverManifest.ServerID,
			Status:       controlapipkg.ResponseStatusError,
			DenialReason: "control-plane audit is unavailable",
			DenialCode:   controlapipkg.DenialCodeAuditUnavailable,
			Redacted:     true,
		}, false
	}

	server.mu.Lock()
	currentRecord, found := server.mcpGatewayLaunchedServers[serverManifest.ServerID]
	if !found || currentRecord == nil || currentRecord.LaunchAttemptID != launchAttemptID || currentRecord.LaunchState != mcpGatewayServerStateStarting {
		server.mu.Unlock()
		closeMCPGatewayLaunchedServerPipes(&launchedServer)
		killMCPGatewayProcessByPID(launchedServer.PID)
		return controlapipkg.MCPGatewayEnsureLaunchResponse{}, controlapipkg.CapabilityResponse{
			RequestID:    serverManifest.ServerID,
			Status:       controlapipkg.ResponseStatusError,
			DenialReason: "mcp gateway launch state changed unexpectedly",
			DenialCode:   controlapipkg.DenialCodeExecutionFailed,
		}, false
	}
	server.mcpGatewayLaunchedServers[serverManifest.ServerID] = &launchedServer
	server.mu.Unlock()

	return buildMCPGatewayEnsureLaunchResponse(&launchedServer, false), controlapipkg.CapabilityResponse{}, true
}

func (server *Server) stopMCPGatewayServer(_ context.Context, tokenClaims capabilityToken, stopRequest controlapipkg.MCPGatewayStopRequest) (controlapipkg.MCPGatewayStopResponse, controlapipkg.CapabilityResponse, bool) {
	serverID := strings.TrimSpace(stopRequest.ServerID)
	server.cleanupDeadMCPGatewayServerIfNeeded(serverID)

	server.mu.Lock()
	launchedServer, found := server.mcpGatewayLaunchedServers[serverID]
	if found {
		delete(server.mcpGatewayLaunchedServers, serverID)
	}
	server.mu.Unlock()

	if !found || launchedServer == nil {
		return buildMCPGatewayStopResponse(nil, false, serverID), controlapipkg.CapabilityResponse{}, true
	}

	launchedServer.ioMu.Lock()
	closeMCPGatewayLaunchedServerPipes(launchedServer)
	killMCPGatewayProcessByPID(launchedServer.PID)
	launchedServer.ioMu.Unlock()

	if err := server.logEvent("mcp_gateway.server_stopped", tokenClaims.ControlSessionID, buildMCPGatewayServerStoppedAuditData(tokenClaims, launchedServer)); err != nil {
		return controlapipkg.MCPGatewayStopResponse{}, controlapipkg.CapabilityResponse{
			RequestID:    launchedServer.ServerID,
			Status:       controlapipkg.ResponseStatusError,
			DenialReason: "control-plane audit is unavailable",
			DenialCode:   controlapipkg.DenialCodeAuditUnavailable,
			Redacted:     true,
		}, false
	}

	return buildMCPGatewayStopResponse(launchedServer, true, serverID), controlapipkg.CapabilityResponse{}, true
}

func (server *Server) buildMCPGatewayServerStatusResponse() controlapipkg.MCPGatewayServerStatusResponse {
	policyRuntime := server.currentPolicyRuntime()
	serverIDs := make([]string, 0, len(policyRuntime.mcpGatewayManifests))
	for serverID := range policyRuntime.mcpGatewayManifests {
		serverIDs = append(serverIDs, serverID)
	}
	slices.Sort(serverIDs)
	for _, serverID := range serverIDs {
		server.cleanupDeadMCPGatewayServerIfNeeded(serverID)
	}

	server.mu.Lock()
	defer server.mu.Unlock()

	response := controlapipkg.MCPGatewayServerStatusResponse{
		Servers: make([]controlapipkg.MCPGatewayServerRuntimeView, 0, len(serverIDs)),
	}
	for _, serverID := range serverIDs {
		serverManifest := policyRuntime.mcpGatewayManifests[serverID]
		response.Servers = append(response.Servers, buildMCPGatewayServerRuntimeView(serverManifest, server.mcpGatewayLaunchedServers[serverID]))
	}
	return response
}
