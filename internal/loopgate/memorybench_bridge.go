package loopgate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"morph/internal/config"
	tclpkg "morph/internal/tcl"
)

type ProjectedNodeDiscoverBackend interface {
	DiscoverProjectedNodes(ctx context.Context, rawRequest ProjectedNodeDiscoverRequest) ([]ProjectedNodeDiscoverItem, error)
}

type MemoryCandidateGovernanceBackend interface {
	EvaluateMemoryCandidate(ctx context.Context, rawRequest BenchmarkMemoryCandidateRequest) (BenchmarkMemoryCandidateDecision, error)
}

type BenchmarkMemoryCandidateRequest struct {
	FactKey         string
	FactValue       string
	SourceText      string
	CandidateSource string
	SourceChannel   string
}

type BenchmarkMemoryCandidateDecision struct {
	PersistenceDisposition string
	ShouldPersist          bool
	HardDeny               bool
	ReasonCode             string
	RiskMotifs             []string
}

type BenchmarkRememberedFactSeed struct {
	FactKey       string
	FactValue     string
	SourceText    string
	SourceChannel string
	Scope         string
}

type productionParityMaterializedFactDebugRecord struct {
	Scope          string
	InspectionID   string
	DistillateID   string
	FactKey        string
	FactValue      string
	AnchorTupleKey string
	LineageStatus  string
	SourceRef      string
}

func OpenContinuityTCLProjectedNodeDiscoverBackend(repoRoot string) (ProjectedNodeDiscoverBackend, error) {
	benchmarkServer, err := NewServer(repoRoot, filepath.Join(repoRoot, "runtime", "memorybench-loopgate.sock"))
	if err != nil {
		return nil, fmt.Errorf("open loopgate server for benchmark discovery: %w", err)
	}
	benchmarkServer.memoryMu.Lock()
	defaultPartition, partitionErr := benchmarkServer.ensureMemoryPartitionLocked("")
	benchmarkServer.memoryMu.Unlock()
	if partitionErr != nil {
		return nil, fmt.Errorf("benchmark default memory partition: %w", partitionErr)
	}
	if defaultPartition == nil || defaultPartition.backend == nil {
		return nil, fmt.Errorf("benchmark memory backend is not configured")
	}
	continuityBackend, ok := defaultPartition.backend.(*continuityTCLMemoryBackend)
	if !ok {
		return nil, fmt.Errorf("benchmark discovery backend %T does not support projected-node discovery", defaultPartition.backend)
	}
	return continuityBackend, nil
}

func OpenContinuityTCLFixtureProjectedNodeDiscoverBackend(repoRoot string, seedNodes []BenchmarkProjectedNodeSeed) (ProjectedNodeDiscoverBackend, error) {
	if len(seedNodes) == 0 {
		return nil, fmt.Errorf("at least one benchmark projected node seed is required")
	}
	sqliteStore, err := openBenchmarkContinuitySQLiteStore(repoRoot, "continuity-fixture-*.sqlite")
	if err != nil {
		return nil, fmt.Errorf("open benchmark continuity sqlite store: %w", err)
	}
	if err := sqliteStore.replaceBenchmarkProjectedNodes(seedNodes); err != nil {
		return nil, fmt.Errorf("seed benchmark continuity sqlite store: %w", err)
	}
	return &continuityTCLMemoryBackend{
		server:                          nil,
		partition:                       nil,
		store:                           sqliteStore,
		productionParityMaterialization: nil,
	}, nil
}

func OpenContinuityTCLProductionParityProjectedNodeDiscoverBackend(repoRoot string, rememberedFactSeeds []BenchmarkRememberedFactSeed, fixtureSeedNodes []BenchmarkProjectedNodeSeed) (ProjectedNodeDiscoverBackend, error) {
	if len(rememberedFactSeeds) == 0 && len(fixtureSeedNodes) == 0 {
		return nil, fmt.Errorf("at least one production-parity continuity seed is required")
	}
	mergedAuthoritativeState := continuityMemoryState{
		SchemaVersion: continuityMemorySchemaVersion,
		Inspections:   make(map[string]continuityInspectionRecord),
		Distillates:   make(map[string]continuityDistillateRecord),
		ResonateKeys:  make(map[string]continuityResonateKeyRecord),
	}
	scopeOrder := make([]string, 0, len(rememberedFactSeeds))
	rememberedFactSeedsByScope := make(map[string][]BenchmarkRememberedFactSeed, len(rememberedFactSeeds))
	for _, rememberedFactSeed := range rememberedFactSeeds {
		validatedRememberScope := strings.TrimSpace(rememberedFactSeed.Scope)
		if validatedRememberScope == "" {
			return nil, fmt.Errorf("benchmark remembered fact seed %q requires a non-empty scope", strings.TrimSpace(rememberedFactSeed.FactKey))
		}
		if _, found := rememberedFactSeedsByScope[validatedRememberScope]; !found {
			scopeOrder = append(scopeOrder, validatedRememberScope)
		}
		rememberedFactSeedsByScope[validatedRememberScope] = append(rememberedFactSeedsByScope[validatedRememberScope], rememberedFactSeed)
	}
	for _, benchmarkScope := range scopeOrder {
		seededState, err := seedBenchmarkRememberedFactsOverControlPlane(repoRoot, rememberedFactSeedsByScope[benchmarkScope])
		if err != nil {
			return nil, err
		}
		func() {
			defer seededState.cleanup()
			rewrittenState := rewriteContinuityMemoryStateScope(seededState.authoritativeState, benchmarkScope)
			err = mergeContinuityMemoryStateRecords(&mergedAuthoritativeState, rewrittenState)
		}()
		if err != nil {
			return nil, fmt.Errorf("merge route-seeded production-parity state for scope %q: %w", benchmarkScope, err)
		}
	}
	materializedFactDebugRecords := buildProductionParityMaterializedFactDebugRecords(mergedAuthoritativeState)

	sqliteStore, err := openBenchmarkContinuitySQLiteStore(repoRoot, "continuity-production-parity-*.sqlite")
	if err != nil {
		return nil, fmt.Errorf("open benchmark continuity sqlite store: %w", err)
	}
	if err := sqliteStore.replaceProjectedNodes(mergedAuthoritativeState); err != nil {
		return nil, fmt.Errorf("seed benchmark continuity authoritative projected nodes: %w", err)
	}
	if len(fixtureSeedNodes) > 0 {
		if err := sqliteStore.replaceBenchmarkFixtureProjectedNodes(fixtureSeedNodes); err != nil {
			return nil, fmt.Errorf("seed benchmark continuity fixture-ingest sqlite store: %w", err)
		}
	}
	return &continuityTCLMemoryBackend{
		server:                          nil,
		partition:                       nil,
		store:                           sqliteStore,
		productionParityMaterialization: materializedFactDebugRecords,
	}, nil
}

type benchmarkProductionParitySeedState struct {
	isolatedRepoRoot   string
	controlSessionID   string
	authoritativeState continuityMemoryState
	cleanup            func()
}

func openBenchmarkContinuitySQLiteStore(repoRoot string, filenamePattern string) (*continuitySQLiteStore, error) {
	benchmarkStateDir := filepath.Join(repoRoot, "runtime", "memorybench")
	if err := os.MkdirAll(benchmarkStateDir, 0o700); err != nil {
		return nil, fmt.Errorf("ensure benchmark continuity state dir: %w", err)
	}
	temporaryStoreFile, err := os.CreateTemp(benchmarkStateDir, filenamePattern)
	if err != nil {
		return nil, fmt.Errorf("create benchmark continuity sqlite path: %w", err)
	}
	temporaryStorePath := temporaryStoreFile.Name()
	if err := temporaryStoreFile.Close(); err != nil {
		return nil, fmt.Errorf("close benchmark continuity sqlite placeholder: %w", err)
	}
	sqliteStore, err := openContinuitySQLiteStore(temporaryStorePath)
	if err != nil {
		return nil, err
	}
	return sqliteStore, nil
}

func prepareIsolatedBenchmarkServerRepoRoot(repoRoot string) (string, error) {
	benchmarkStateDir := filepath.Join(repoRoot, "runtime", "memorybench")
	if err := os.MkdirAll(benchmarkStateDir, 0o700); err != nil {
		return "", fmt.Errorf("ensure benchmark continuity state dir: %w", err)
	}
	temporaryRepoRoot, err := os.MkdirTemp(benchmarkStateDir, "continuity-production-repo-*")
	if err != nil {
		return "", fmt.Errorf("create temporary benchmark repo root: %w", err)
	}
	if err := linkBenchmarkRepoSubtree(repoRoot, temporaryRepoRoot, "core", true); err != nil {
		return "", err
	}
	if err := linkBenchmarkRepoSubtree(repoRoot, temporaryRepoRoot, "config", false); err != nil {
		return "", err
	}
	if err := linkBenchmarkRepoSubtree(repoRoot, temporaryRepoRoot, "persona", false); err != nil {
		return "", err
	}
	return temporaryRepoRoot, nil
}

func linkBenchmarkRepoSubtree(sourceRepoRoot string, benchmarkRepoRoot string, relativePath string, required bool) error {
	sourcePath := filepath.Join(sourceRepoRoot, relativePath)
	if _, err := os.Stat(sourcePath); err != nil {
		if os.IsNotExist(err) && !required {
			return nil
		}
		if os.IsNotExist(err) {
			return fmt.Errorf("required benchmark repo subtree %q not found under %q", relativePath, sourceRepoRoot)
		}
		return fmt.Errorf("stat benchmark repo subtree %q: %w", relativePath, err)
	}
	targetPath := filepath.Join(benchmarkRepoRoot, relativePath)
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o700); err != nil {
		return fmt.Errorf("create benchmark repo subtree dir %q: %w", relativePath, err)
	}
	if err := os.Symlink(sourcePath, targetPath); err != nil {
		return fmt.Errorf("symlink benchmark repo subtree %q: %w", relativePath, err)
	}
	return nil
}

func seedBenchmarkRememberedFactsOverControlPlane(repoRoot string, rememberedFactSeeds []BenchmarkRememberedFactSeed) (benchmarkProductionParitySeedState, error) {
	isolatedBenchmarkRepoRoot, err := prepareIsolatedBenchmarkServerRepoRoot(repoRoot)
	if err != nil {
		return benchmarkProductionParitySeedState{}, fmt.Errorf("prepare isolated benchmark repo root: %w", err)
	}

	benchmarkServer, benchmarkClient, stopBenchmarkServer, err := startBenchmarkControlPlaneServer(isolatedBenchmarkRepoRoot)
	if err != nil {
		_ = os.RemoveAll(isolatedBenchmarkRepoRoot)
		return benchmarkProductionParitySeedState{}, fmt.Errorf("open loopgate server for benchmark production-parity discovery: %w", err)
	}
	cleanup := func() {
		stopBenchmarkServer()
		_ = os.RemoveAll(isolatedBenchmarkRepoRoot)
	}

	// Keep relative ordering deterministic while staying inside the real signed-request
	// skew window. Route-authenticated benchmark seeding must obey the same integrity
	// binding as product traffic, so seeding cannot freeze the control-plane clock far
	// away from wall time.
	baseRememberedSeedTimeUTC := time.Now().UTC().Truncate(time.Second)
	for rememberedFactSeedIndex, rememberedFactSeed := range rememberedFactSeeds {
		rememberedSeedTimeUTC := baseRememberedSeedTimeUTC.Add(time.Duration(rememberedFactSeedIndex) * time.Second)
		benchmarkServer.SetNowForTest(func() time.Time {
			return rememberedSeedTimeUTC
		})
		validatedRememberRequest := MemoryRememberRequest{
			Scope:           memoryScopeGlobal,
			FactKey:         strings.TrimSpace(rememberedFactSeed.FactKey),
			FactValue:       strings.TrimSpace(rememberedFactSeed.FactValue),
			SourceText:      strings.TrimSpace(rememberedFactSeed.SourceText),
			CandidateSource: memoryCandidateSourceExplicitFact,
			SourceChannel:   strings.TrimSpace(rememberedFactSeed.SourceChannel),
		}
		if validatedRememberRequest.SourceChannel == "" {
			validatedRememberRequest.SourceChannel = memorySourceChannelUnknown
		}
		if _, err := benchmarkClient.RememberMemoryFact(context.Background(), validatedRememberRequest); err != nil {
			cleanup()
			return benchmarkProductionParitySeedState{}, fmt.Errorf("seed benchmark remembered fact %q through control plane: %w", validatedRememberRequest.FactKey, err)
		}
	}

	authoritativeState, err := cloneBenchmarkPartitionState(benchmarkServer, "")
	if err != nil {
		cleanup()
		return benchmarkProductionParitySeedState{}, fmt.Errorf("clone production-parity authoritative state: %w", err)
	}
	return benchmarkProductionParitySeedState{
		isolatedRepoRoot:   isolatedBenchmarkRepoRoot,
		controlSessionID:   benchmarkClient.controlSessionID,
		authoritativeState: authoritativeState,
		cleanup:            cleanup,
	}, nil
}

func startBenchmarkControlPlaneServer(repoRoot string) (*Server, *Client, func(), error) {
	socketFile, err := os.CreateTemp("", "memorybench-loopgate-*.sock")
	if err != nil {
		return nil, nil, nil, fmt.Errorf("create benchmark socket path: %w", err)
	}
	socketPath := socketFile.Name()
	if err := socketFile.Close(); err != nil {
		_ = os.Remove(socketPath)
		return nil, nil, nil, fmt.Errorf("close benchmark socket placeholder: %w", err)
	}
	_ = os.Remove(socketPath)

	benchmarkServer, err := NewServer(repoRoot, socketPath)
	if err != nil {
		return nil, nil, nil, err
	}
	benchmarkServer.sessionOpenMinInterval = 0
	benchmarkServer.maxActiveSessionsPerUID = 64
	benchmarkServer.expirySweepMaxInterval = 0

	serverContext, cancelServer := context.WithCancel(context.Background())
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		_ = benchmarkServer.Serve(serverContext)
	}()

	benchmarkClient := NewClient(socketPath)
	deadline := time.Now().Add(2 * time.Second)
	for {
		_, healthErr := benchmarkClient.Health(context.Background())
		if healthErr == nil {
			break
		}
		if time.Now().After(deadline) {
			cancelServer()
			<-serverDone
			return nil, nil, nil, fmt.Errorf("wait for benchmark loopgate health: %w", healthErr)
		}
		time.Sleep(25 * time.Millisecond)
	}

	benchmarkClient.ConfigureSession("memorybench", "memorybench-seed", []string{controlCapabilityMemoryWrite})
	if _, err := benchmarkClient.ensureCapabilityToken(context.Background()); err != nil {
		cancelServer()
		<-serverDone
		return nil, nil, nil, fmt.Errorf("open benchmark control session: %w", err)
	}

	return benchmarkServer, benchmarkClient, func() {
		cancelServer()
		<-serverDone
		_ = os.Remove(socketPath)
	}, nil
}

func cloneBenchmarkPartitionState(benchmarkServer *Server, tenantID string) (continuityMemoryState, error) {
	benchmarkServer.memoryMu.Lock()
	defer benchmarkServer.memoryMu.Unlock()
	partition, err := benchmarkServer.ensureMemoryPartitionLocked(tenantID)
	if err != nil {
		return continuityMemoryState{}, fmt.Errorf("benchmark memory partition %q: %w", tenantID, err)
	}
	return cloneContinuityMemoryState(partition.state), nil
}

func mergeContinuityMemoryStateRecords(mergedState *continuityMemoryState, partitionState continuityMemoryState) error {
	for inspectionID, inspectionRecord := range partitionState.Inspections {
		if _, exists := mergedState.Inspections[inspectionID]; exists {
			return fmt.Errorf("duplicate benchmark inspection id %q while merging production-parity state", inspectionID)
		}
		mergedState.Inspections[inspectionID] = cloneContinuityInspectionRecord(inspectionRecord)
	}
	for distillateID, distillateRecord := range partitionState.Distillates {
		if _, exists := mergedState.Distillates[distillateID]; exists {
			return fmt.Errorf("duplicate benchmark distillate id %q while merging production-parity state", distillateID)
		}
		mergedState.Distillates[distillateID] = cloneContinuityDistillateRecord(distillateRecord)
	}
	for keyID, resonateKeyRecord := range partitionState.ResonateKeys {
		if _, exists := mergedState.ResonateKeys[keyID]; exists {
			return fmt.Errorf("duplicate benchmark resonate key id %q while merging production-parity state", keyID)
		}
		mergedState.ResonateKeys[keyID] = cloneContinuityResonateKeyRecord(resonateKeyRecord)
	}
	return nil
}

func rewriteContinuityMemoryStateScope(partitionState continuityMemoryState, benchmarkScope string) continuityMemoryState {
	rewrittenState := cloneContinuityMemoryState(partitionState)
	for inspectionID, inspectionRecord := range rewrittenState.Inspections {
		inspectionRecord.Scope = benchmarkScope
		rewrittenState.Inspections[inspectionID] = inspectionRecord
	}
	for distillateID, distillateRecord := range rewrittenState.Distillates {
		distillateRecord.Scope = benchmarkScope
		rewrittenState.Distillates[distillateID] = distillateRecord
	}
	for keyID, resonateKeyRecord := range rewrittenState.ResonateKeys {
		resonateKeyRecord.Scope = benchmarkScope
		rewrittenState.ResonateKeys[keyID] = resonateKeyRecord
	}
	return rewrittenState
}

func buildProductionParityMaterializedFactDebugRecords(authoritativeState continuityMemoryState) []productionParityMaterializedFactDebugRecord {
	explicitDistillates := explicitRememberedFactDistillates(authoritativeState)
	debugRecords := make([]productionParityMaterializedFactDebugRecord, 0, len(explicitDistillates))
	for _, distillateRecord := range explicitDistillates {
		explicitFactRecord, found := explicitProfileFactFromDistillate(authoritativeState, distillateRecord)
		if !found {
			continue
		}
		inspectionRecord, found := authoritativeState.Inspections[distillateRecord.InspectionID]
		if !found {
			continue
		}
		sourceRef := ""
		if len(distillateRecord.Facts) > 0 {
			sourceRef = strings.TrimSpace(distillateRecord.Facts[0].SourceRef)
		}
		debugRecords = append(debugRecords, productionParityMaterializedFactDebugRecord{
			Scope:          strings.TrimSpace(distillateRecord.Scope),
			InspectionID:   explicitFactRecord.InspectionID,
			DistillateID:   explicitFactRecord.DistillateID,
			FactKey:        explicitFactRecord.FactKey,
			FactValue:      explicitFactRecord.FactValue,
			AnchorTupleKey: explicitFactRecord.AnchorTupleKey,
			LineageStatus:  normalizeContinuityInspectionRecordMust(inspectionRecord).Lineage.Status,
			SourceRef:      sourceRef,
		})
	}
	return debugRecords
}

func OpenContinuityTCLMemoryCandidateGovernanceBackend() (MemoryCandidateGovernanceBackend, error) {
	return continuityTCLMemoryCandidateGovernanceBackend{
		runtimeConfig: config.DefaultRuntimeConfig(),
	}, nil
}

type continuityTCLMemoryCandidateGovernanceBackend struct {
	runtimeConfig config.RuntimeConfig
}

func (backend continuityTCLMemoryCandidateGovernanceBackend) EvaluateMemoryCandidate(ctx context.Context, rawRequest BenchmarkMemoryCandidateRequest) (BenchmarkMemoryCandidateDecision, error) {
	_ = ctx
	validatedRequest, err := backend.normalizeBenchmarkMemoryCandidateRequest(rawRequest)
	if err != nil {
		return BenchmarkMemoryCandidateDecision{
			PersistenceDisposition: "invalid",
			ShouldPersist:          false,
			HardDeny:               true,
			ReasonCode:             DenialCodeMemoryCandidateInvalid,
		}, nil
	}

	validatedCandidateResult, err := buildValidatedMemoryRememberCandidate(validatedRequest)
	if err != nil {
		return BenchmarkMemoryCandidateDecision{
			PersistenceDisposition: "invalid",
			ShouldPersist:          false,
			HardDeny:               true,
			ReasonCode:             DenialCodeMemoryCandidateInvalid,
		}, nil
	}
	denialCode, _, shouldPersist := memoryRememberGovernanceDecision(validatedCandidateResult.ValidatedCandidate.Decision)
	return BenchmarkMemoryCandidateDecision{
		PersistenceDisposition: benchmarkPersistenceDisposition(validatedCandidateResult.ValidatedCandidate.Decision, shouldPersist),
		ShouldPersist:          shouldPersist,
		HardDeny:               validatedCandidateResult.ValidatedCandidate.Decision.HardDeny,
		ReasonCode:             strings.TrimSpace(denialCode),
		RiskMotifs:             riskMotifStrings(validatedCandidateResult.ValidatedCandidate.Signatures.RiskMotifs),
	}, nil
}

func (backend continuityTCLMemoryCandidateGovernanceBackend) normalizeBenchmarkMemoryCandidateRequest(rawRequest BenchmarkMemoryCandidateRequest) (MemoryRememberRequest, error) {
	validatedRequest := MemoryRememberRequest{
		Scope:           memoryScopeGlobal,
		FactKey:         strings.TrimSpace(rawRequest.FactKey),
		FactValue:       strings.TrimSpace(rawRequest.FactValue),
		SourceText:      strings.TrimSpace(rawRequest.SourceText),
		CandidateSource: strings.TrimSpace(rawRequest.CandidateSource),
		SourceChannel:   strings.TrimSpace(rawRequest.SourceChannel),
	}
	if validatedRequest.CandidateSource == "" {
		validatedRequest.CandidateSource = memoryCandidateSourceExplicitFact
	}
	if validatedRequest.SourceChannel == "" {
		validatedRequest.SourceChannel = memorySourceChannelUnknown
	}
	if err := validatedRequest.Validate(); err != nil {
		return MemoryRememberRequest{}, err
	}
	if len([]byte(validatedRequest.FactValue)) > backend.runtimeConfig.Memory.ExplicitFactWrites.MaxValueBytes {
		return MemoryRememberRequest{}, fmt.Errorf("fact_value exceeds max_value_bytes")
	}
	return validatedRequest, nil
}

func benchmarkPersistenceDisposition(validatedDecision tclpkg.ValidatedMemoryDecision, shouldPersist bool) string {
	if shouldPersist {
		return "persist"
	}
	switch validatedDecision.Disposition {
	case tclpkg.DispositionQuarantine:
		return "quarantine"
	case tclpkg.DispositionReview, tclpkg.DispositionFlag:
		return "review"
	case tclpkg.DispositionDrop:
		return "deny"
	default:
		return "deny"
	}
}
