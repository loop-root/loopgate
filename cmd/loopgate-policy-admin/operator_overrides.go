package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"loopgate/internal/config"
	"loopgate/internal/loopgate"
)

func runOverrides(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) < 1 {
		fmt.Fprintln(stderr, "ERROR: overrides subcommand is required")
		printUsage(stderr)
		return 2
	}
	switch args[0] {
	case "list":
		return runOverrideList(args[1:], stdout, stderr)
	case "grant":
		return runOverrideGrant(args[1:], stdout, stderr)
	case "grant-edit-path":
		return runOverrideGrantEditPath(args[1:], stdout, stderr)
	case "revoke":
		return runOverrideRevoke(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "ERROR: unknown overrides subcommand %q\n", args[0])
		printUsage(stderr)
		return 2
	}
}

func runGrants(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) < 1 {
		fmt.Fprintln(stderr, "ERROR: grants subcommand is required")
		printUsage(stderr)
		return 2
	}
	switch args[0] {
	case "list":
		return runOverrideList(args[1:], stdout, stderr)
	case "add", "grant":
		return runOverrideGrant(args[1:], stdout, stderr)
	case "revoke", "remove":
		return runOverrideRevoke(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "ERROR: unknown grants subcommand %q\n", args[0])
		printUsage(stderr)
		return 2
	}
}

func runOverrideList(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("overrides list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repoRootFlag := fs.String("repo", "", "repository root used to resolve the operator override document")
	allFlag := fs.Bool("all", false, "include revoked operator grants")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "ERROR: unexpected positional arguments: %s\n", strings.Join(fs.Args(), " "))
		return 2
	}

	baseRoot, err := resolveBaseRoot(*repoRootFlag)
	if err != nil {
		fmt.Fprintln(stderr, "ERROR:", err)
		return 1
	}
	loadResult, err := config.LoadOperatorOverrideDocumentWithHash(baseRoot)
	if err != nil {
		fmt.Fprintln(stderr, "ERROR: load operator override document:", err)
		return 1
	}

	fmt.Fprintf(stdout, "operator_override_path: %s\n", config.OperatorOverrideDocumentPath(baseRoot))
	fmt.Fprintf(stdout, "operator_override_signature_path: %s\n", config.OperatorOverrideSignaturePath(baseRoot))
	fmt.Fprintf(stdout, "operator_override_present: %t\n", loadResult.Present)
	if !loadResult.Present {
		fmt.Fprintln(stdout, "active_grants: (none)")
		return 0
	}
	fmt.Fprintf(stdout, "operator_override_sha256: %s\n", loadResult.ContentSHA256)
	fmt.Fprintf(stdout, "signature_key_id: %s\n", loadResult.SignatureKeyID)

	activeGrantCount := 0
	revokedGrantCount := 0
	for _, grant := range loadResult.Document.Grants {
		switch grant.State {
		case "active":
			activeGrantCount++
		case "revoked":
			revokedGrantCount++
		}
	}
	fmt.Fprintf(stdout, "active_grant_count: %d\n", activeGrantCount)
	fmt.Fprintf(stdout, "revoked_grant_count: %d\n", revokedGrantCount)
	if activeGrantCount == 0 {
		fmt.Fprintln(stdout, "active_grants: (none)")
		if !*allFlag {
			return 0
		}
	}
	printedGrantCount := 0
	for _, grant := range loadResult.Document.Grants {
		if grant.State != "active" && !*allFlag {
			continue
		}
		printedGrantCount++
		fmt.Fprintf(stdout, "grant.id: %s\n", grant.ID)
		fmt.Fprintf(stdout, "grant.class: %s\n", grant.Class)
		fmt.Fprintf(stdout, "grant.state: %s\n", grant.State)
		fmt.Fprintln(stdout, "grant.scope: permanent")
		fmt.Fprintf(stdout, "grant.path_prefixes: %s\n", strings.Join(grant.PathPrefixes, ", "))
		fmt.Fprintf(stdout, "grant.created_at_utc: %s\n", grant.CreatedAtUTC)
		if strings.TrimSpace(grant.RevokedAtUTC) != "" {
			fmt.Fprintf(stdout, "grant.revoked_at_utc: %s\n", grant.RevokedAtUTC)
		}
	}
	if printedGrantCount == 0 && *allFlag {
		fmt.Fprintln(stdout, "grants: (none)")
	}
	return 0
}

func runOverrideGrantEditPath(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("overrides grant-edit-path", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repoRootFlag := fs.String("repo", "", "repository root used to resolve the operator override document")
	socketPathFlag := fs.String("socket", "", "Unix socket path (default: LOOPGATE_SOCKET or <repo>/runtime/state/loopgate.sock)")
	pathFlag := fs.String("path", "", "repo-relative or absolute subtree path to allow for Edit and MultiEdit")
	privateKeyPathFlag := fs.String("private-key-file", "", "path to a PKCS#8 PEM-encoded Ed25519 private key")
	keyIDFlag := fs.String("key-id", "", "trusted signing key identifier (defaults to the current signed policy key_id)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "ERROR: unexpected positional arguments: %s\n", strings.Join(fs.Args(), " "))
		return 2
	}
	return applyOverrideGrantPath(overrideGrantPathRequest{
		ClassName:          config.OperatorOverrideClassRepoEditSafe,
		RawPath:            *pathFlag,
		RepoRootFlag:       *repoRootFlag,
		SocketPathFlag:     *socketPathFlag,
		PrivateKeyPathFlag: *privateKeyPathFlag,
		KeyIDFlag:          *keyIDFlag,
	}, stdout, stderr)
}

func runOverrideGrant(args []string, stdout io.Writer, stderr io.Writer) int {
	overrideClass := ""
	parseArgs := args
	if len(args) > 0 && !strings.HasPrefix(strings.TrimSpace(args[0]), "-") {
		overrideClass = strings.TrimSpace(args[0])
		parseArgs = args[1:]
	}
	fs := flag.NewFlagSet("overrides grant", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repoRootFlag := fs.String("repo", "", "repository root used to resolve the operator override document")
	socketPathFlag := fs.String("socket", "", "Unix socket path (default: LOOPGATE_SOCKET or <repo>/runtime/state/loopgate.sock)")
	pathFlag := fs.String("path", "", "repo-relative or absolute subtree path to grant")
	privateKeyPathFlag := fs.String("private-key-file", "", "path to a PKCS#8 PEM-encoded Ed25519 private key")
	keyIDFlag := fs.String("key-id", "", "trusted signing key identifier (defaults to the current signed policy key_id)")
	dryRunFlag := fs.Bool("dry-run", false, "preview the grant without writing or reloading operator overrides")
	if err := fs.Parse(parseArgs); err != nil {
		return 2
	}
	if overrideClass == "" {
		if fs.NArg() != 1 {
			fmt.Fprintln(stderr, "ERROR: overrides grant requires exactly one class name")
			return 2
		}
		overrideClass = strings.TrimSpace(fs.Arg(0))
	} else if fs.NArg() != 0 {
		fmt.Fprintln(stderr, "ERROR: overrides grant requires exactly one class name")
		return 2
	}

	return applyOverrideGrantPath(overrideGrantPathRequest{
		ClassName:          overrideClass,
		RawPath:            *pathFlag,
		RepoRootFlag:       *repoRootFlag,
		SocketPathFlag:     *socketPathFlag,
		PrivateKeyPathFlag: *privateKeyPathFlag,
		KeyIDFlag:          *keyIDFlag,
		DryRun:             *dryRunFlag,
	}, stdout, stderr)
}

type overrideGrantPathRequest struct {
	ClassName          string
	RawPath            string
	RepoRootFlag       string
	SocketPathFlag     string
	PrivateKeyPathFlag string
	KeyIDFlag          string
	DryRun             bool
}

func applyOverrideGrantPath(request overrideGrantPathRequest, stdout io.Writer, stderr io.Writer) int {
	overrideClass := strings.TrimSpace(request.ClassName)
	if !isPathScopedOperatorOverrideClass(overrideClass) {
		fmt.Fprintf(stderr, "ERROR: unsupported path-scoped operator override class %q (supported: %s)\n", overrideClass, strings.Join(pathScopedOperatorOverrideClasses(), ", "))
		return 2
	}

	baseRoot, err := resolveBaseRoot(request.RepoRootFlag)
	if err != nil {
		fmt.Fprintln(stderr, "ERROR:", err)
		return 1
	}
	loadedPolicy, err := loadPolicyDocument(baseRoot, "", "")
	if err != nil {
		fmt.Fprintln(stderr, "ERROR: load signed policy:", err)
		return 1
	}
	if got := loadedPolicy.Policy.OperatorOverrideMaxDelegation(overrideClass); got != config.OperatorOverrideDelegationPersistent {
		fmt.Fprintf(stderr, "ERROR: parent policy %s max_delegation=%s does not allow permanent operator grants\n", overrideClass, got)
		return 1
	}

	normalizedPath, err := normalizePathScopedOperatorOverridePath(loadedPolicy.Policy, baseRoot, overrideClass, request.RawPath)
	if err != nil {
		fmt.Fprintln(stderr, "ERROR:", err)
		return 2
	}
	loadResult, err := config.LoadOperatorOverrideDocumentWithHash(baseRoot)
	if err != nil {
		fmt.Fprintln(stderr, "ERROR: load current operator override document:", err)
		return 1
	}
	for _, grant := range config.ActiveOperatorOverrideGrants(loadResult.Document, overrideClass) {
		if len(grant.PathPrefixes) == 1 && grant.PathPrefixes[0] == normalizedPath {
			fmt.Fprintf(stdout, "operator grant already present id=%s path=%s scope=permanent\n", grant.ID, normalizedPath)
			return 0
		}
	}

	keyID, err := resolveOperatorOverrideKeyID(baseRoot, request.KeyIDFlag)
	if err != nil {
		fmt.Fprintln(stderr, "ERROR:", err)
		return 1
	}
	if request.DryRun {
		fmt.Fprintln(stdout, "operator grant preview")
		fmt.Fprintf(stdout, "grant_class: %s\n", overrideClass)
		fmt.Fprintln(stdout, "grant_scope: permanent")
		fmt.Fprintf(stdout, "path_prefix: %s\n", normalizedPath)
		fmt.Fprintf(stdout, "key_id: %s\n", keyID)
		fmt.Fprintln(stdout, "would_write: false")
		return 0
	}

	privateKeyPath, _, err := resolvePolicySigningPrivateKeyPath(strings.TrimSpace(request.PrivateKeyPathFlag), strings.TrimSpace(os.Getenv(policySigningPrivateKeyFileEnv)), keyID)
	if err != nil {
		fmt.Fprintln(stderr, "ERROR:", err)
		return 2
	}
	privateKey, err := loadPolicySigningPrivateKey(privateKeyPath)
	if err != nil {
		fmt.Fprintln(stderr, "ERROR:", err)
		return 1
	}

	nextDocument := loadResult.Document
	grantID, err := newOperatorOverrideGrantID()
	if err != nil {
		fmt.Fprintln(stderr, "ERROR: generate operator override id:", err)
		return 1
	}
	nextDocument.Grants = append(nextDocument.Grants, config.OperatorOverrideGrant{
		ID:           grantID,
		Class:        overrideClass,
		State:        "active",
		PathPrefixes: []string{normalizedPath},
		CreatedAtUTC: time.Now().UTC().Format(time.RFC3339),
	})

	if err := writeAndApplyOperatorOverrideDocument(baseRoot, resolveSocketPath(baseRoot, request.SocketPathFlag), nextDocument, privateKey, keyID); err != nil {
		fmt.Fprintln(stderr, "ERROR:", err)
		return 1
	}

	fmt.Fprintln(stdout, "operator grant applied")
	fmt.Fprintf(stdout, "grant_id: %s\n", grantID)
	fmt.Fprintf(stdout, "grant_class: %s\n", overrideClass)
	fmt.Fprintln(stdout, "grant_scope: permanent")
	fmt.Fprintf(stdout, "path_prefix: %s\n", normalizedPath)
	return 0
}

func runOverrideRevoke(args []string, stdout io.Writer, stderr io.Writer) int {
	overrideID := ""
	parseArgs := args
	if len(args) > 0 && !strings.HasPrefix(strings.TrimSpace(args[0]), "-") {
		overrideID = strings.TrimSpace(args[0])
		parseArgs = args[1:]
	}
	fs := flag.NewFlagSet("overrides revoke", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repoRootFlag := fs.String("repo", "", "repository root used to resolve the operator override document")
	socketPathFlag := fs.String("socket", "", "Unix socket path (default: LOOPGATE_SOCKET or <repo>/runtime/state/loopgate.sock)")
	privateKeyPathFlag := fs.String("private-key-file", "", "path to a PKCS#8 PEM-encoded Ed25519 private key")
	keyIDFlag := fs.String("key-id", "", "trusted signing key identifier (defaults to the current signed policy key_id)")
	dryRunFlag := fs.Bool("dry-run", false, "preview the revocation without writing or reloading operator grants")
	if err := fs.Parse(parseArgs); err != nil {
		return 2
	}
	if overrideID == "" {
		if fs.NArg() != 1 {
			fmt.Fprintln(stderr, "ERROR: overrides revoke requires exactly one override id")
			return 2
		}
		overrideID = strings.TrimSpace(fs.Arg(0))
	} else if fs.NArg() != 0 {
		fmt.Fprintln(stderr, "ERROR: overrides revoke requires exactly one override id")
		return 2
	}

	baseRoot, err := resolveBaseRoot(*repoRootFlag)
	if err != nil {
		fmt.Fprintln(stderr, "ERROR:", err)
		return 1
	}
	loadResult, err := config.LoadOperatorOverrideDocumentWithHash(baseRoot)
	if err != nil {
		fmt.Fprintln(stderr, "ERROR: load current operator override document:", err)
		return 1
	}
	if !loadResult.Present {
		fmt.Fprintln(stderr, "ERROR: no operator override document is present")
		return 1
	}

	nextDocument := loadResult.Document
	found := false
	for index := range nextDocument.Grants {
		if nextDocument.Grants[index].ID != overrideID {
			continue
		}
		found = true
		if nextDocument.Grants[index].State == "revoked" {
			fmt.Fprintf(stdout, "operator grant %s already revoked\n", overrideID)
			return 0
		}
		nextDocument.Grants[index].State = "revoked"
		nextDocument.Grants[index].RevokedAtUTC = time.Now().UTC().Format(time.RFC3339)
	}
	if !found {
		fmt.Fprintf(stderr, "ERROR: operator grant %q not found\n", overrideID)
		return 1
	}

	if *dryRunFlag {
		fmt.Fprintln(stdout, "operator grant revoke preview")
		fmt.Fprintf(stdout, "grant_id: %s\n", overrideID)
		fmt.Fprintln(stdout, "would_write: false")
		return 0
	}

	keyID, err := resolveOperatorOverrideKeyID(baseRoot, *keyIDFlag)
	if err != nil {
		fmt.Fprintln(stderr, "ERROR:", err)
		return 1
	}
	privateKeyPath, _, err := resolvePolicySigningPrivateKeyPath(strings.TrimSpace(*privateKeyPathFlag), strings.TrimSpace(os.Getenv(policySigningPrivateKeyFileEnv)), keyID)
	if err != nil {
		fmt.Fprintln(stderr, "ERROR:", err)
		return 2
	}
	privateKey, err := loadPolicySigningPrivateKey(privateKeyPath)
	if err != nil {
		fmt.Fprintln(stderr, "ERROR:", err)
		return 1
	}
	if err := writeAndApplyOperatorOverrideDocument(baseRoot, resolveSocketPath(baseRoot, *socketPathFlag), nextDocument, privateKey, keyID); err != nil {
		fmt.Fprintln(stderr, "ERROR:", err)
		return 1
	}

	fmt.Fprintf(stdout, "operator grant %s revoked\n", overrideID)
	return 0
}

func writeAndApplyOperatorOverrideDocument(repoRoot string, socketPath string, document config.OperatorOverrideDocument, privateKey []byte, keyID string) error {
	backup, err := captureOperatorOverrideDiskBackup(repoRoot)
	if err != nil {
		return err
	}

	documentBytes, err := config.MarshalOperatorOverrideDocumentYAML(document)
	if err != nil {
		return fmt.Errorf("marshal operator override document: %w", err)
	}
	signatureFile, err := config.SignOperatorOverrideDocument(documentBytes, keyID, privateKey)
	if err != nil {
		return fmt.Errorf("sign operator override document: %w", err)
	}
	if err := config.WriteOperatorOverrideDocumentYAML(repoRoot, document); err != nil {
		return fmt.Errorf("write operator override document: %w", err)
	}
	if err := config.WriteOperatorOverrideSignatureYAML(repoRoot, signatureFile); err != nil {
		restoreErr := restoreOperatorOverrideDiskBackup(repoRoot, backup)
		return formatOperatorOverrideWriteFailure("write operator override signature", err, restoreErr)
	}

	client := loopgate.NewClient(socketPath)
	client.ConfigureSession("loopgate-policy-admin", defaultPolicyAdminSessionID("overrides-apply"), []string{"config.read", "config.write"})
	if _, err := client.ReloadOperatorOverridesFromDisk(context.Background()); err != nil {
		restoreErr := restoreOperatorOverrideDiskBackup(repoRoot, backup)
		return formatOperatorOverrideWriteFailure("reload operator overrides", err, restoreErr)
	}
	return nil
}

func resolveOperatorOverrideKeyID(repoRoot string, flagValue string) (string, error) {
	if trimmedKeyID := strings.TrimSpace(flagValue); trimmedKeyID != "" {
		return trimmedKeyID, nil
	}
	signatureFile, err := config.LoadPolicySignatureFile(repoRoot)
	if err != nil {
		return "", fmt.Errorf("load current signed policy key_id: %w", err)
	}
	return strings.TrimSpace(signatureFile.KeyID), nil
}

func loadPolicySigningPrivateKey(privateKeyPath string) ([]byte, error) {
	rawPrivateKeyBytes, err := os.ReadFile(privateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("read private key %s: %w", privateKeyPath, err)
	}
	privateKey, err := config.ParsePolicySigningPrivateKeyPEM(rawPrivateKeyBytes)
	if err != nil {
		return nil, fmt.Errorf("parse private key %s: %w", privateKeyPath, err)
	}
	return privateKey, nil
}

type operatorOverrideDiskBackup struct {
	documentBytes    []byte
	signatureBytes   []byte
	documentPresent  bool
	signaturePresent bool
}

func captureOperatorOverrideDiskBackup(repoRoot string) (operatorOverrideDiskBackup, error) {
	documentPath := config.OperatorOverrideDocumentPath(repoRoot)
	signaturePath := config.OperatorOverrideSignaturePath(repoRoot)
	backup := operatorOverrideDiskBackup{}
	if rawDocumentBytes, err := os.ReadFile(documentPath); err == nil {
		backup.documentBytes = append([]byte(nil), rawDocumentBytes...)
		backup.documentPresent = true
	} else if !os.IsNotExist(err) {
		return operatorOverrideDiskBackup{}, fmt.Errorf("read operator override document backup: %w", err)
	}
	if rawSignatureBytes, err := os.ReadFile(signaturePath); err == nil {
		backup.signatureBytes = append([]byte(nil), rawSignatureBytes...)
		backup.signaturePresent = true
	} else if !os.IsNotExist(err) {
		return operatorOverrideDiskBackup{}, fmt.Errorf("read operator override signature backup: %w", err)
	}
	return backup, nil
}

func restoreOperatorOverrideDiskBackup(repoRoot string, backup operatorOverrideDiskBackup) error {
	documentPath := config.OperatorOverrideDocumentPath(repoRoot)
	signaturePath := config.OperatorOverrideSignaturePath(repoRoot)
	if backup.documentPresent {
		if err := os.WriteFile(documentPath, backup.documentBytes, 0o600); err != nil {
			return fmt.Errorf("restore operator override document: %w", err)
		}
	} else if err := os.Remove(documentPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove operator override document during rollback: %w", err)
	}
	if backup.signaturePresent {
		if err := os.WriteFile(signaturePath, backup.signatureBytes, 0o600); err != nil {
			return fmt.Errorf("restore operator override signature: %w", err)
		}
	} else if err := os.Remove(signaturePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove operator override signature during rollback: %w", err)
	}
	return nil
}

func formatOperatorOverrideWriteFailure(action string, err error, restoreErr error) error {
	if restoreErr == nil {
		return fmt.Errorf("%s: %w", action, err)
	}
	return fmt.Errorf("%s: %v (rollback failed: %v)", action, err, restoreErr)
}

func isPathScopedOperatorOverrideClass(className string) bool {
	for _, supportedClass := range pathScopedOperatorOverrideClasses() {
		if strings.TrimSpace(className) == supportedClass {
			return true
		}
	}
	return false
}

func pathScopedOperatorOverrideClasses() []string {
	return []string{
		config.OperatorOverrideClassRepoReadSearch,
		config.OperatorOverrideClassRepoEditSafe,
		config.OperatorOverrideClassRepoWriteSafe,
		config.OperatorOverrideClassRepoBashSafe,
	}
}

func normalizePathScopedOperatorOverridePath(policy config.Policy, repoRoot string, overrideClass string, rawPath string) (string, error) {
	trimmedPath := strings.TrimSpace(rawPath)
	if trimmedPath == "" {
		return "", fmt.Errorf("override path is required")
	}
	resolvedPath := trimmedPath
	if !filepath.IsAbs(resolvedPath) {
		resolvedPath = filepath.Join(repoRoot, resolvedPath)
	}
	resolvedPath = filepath.Clean(resolvedPath)
	if !localPathWithinRoot(resolvedPath, repoRoot) {
		return "", fmt.Errorf("override path %q must stay within the repository root", resolvedPath)
	}

	for _, toolName := range pathScopedOperatorOverrideTools(overrideClass) {
		allowedRoots, deniedPaths := effectiveClaudeCodePathPolicy(policy, toolName)
		if len(allowedRoots) > 0 && !localPathMatchesAnyPolicyRoot(resolvedPath, allowedRoots, repoRoot) {
			return "", fmt.Errorf("override path %q is outside %s allowed_roots", resolvedPath, toolName)
		}
		if localPathMatchesAnyPolicyRoot(resolvedPath, deniedPaths, repoRoot) {
			return "", fmt.Errorf("override path %q matches %s denied_paths", resolvedPath, toolName)
		}
	}

	relativePath, err := filepath.Rel(repoRoot, resolvedPath)
	if err != nil {
		return "", fmt.Errorf("derive repo-relative override path: %w", err)
	}
	if relativePath == "." {
		return ".", nil
	}
	return filepath.Clean(relativePath), nil
}

func pathScopedOperatorOverrideTools(overrideClass string) []string {
	switch overrideClass {
	case config.OperatorOverrideClassRepoReadSearch:
		return []string{"Read", "Glob", "Grep"}
	case config.OperatorOverrideClassRepoEditSafe:
		return []string{"Edit", "MultiEdit"}
	case config.OperatorOverrideClassRepoWriteSafe:
		return []string{"Write"}
	default:
		return nil
	}
}

func effectiveClaudeCodePathPolicy(policy config.Policy, toolName string) ([]string, []string) {
	allowedRoots := append([]string(nil), policy.Tools.Filesystem.AllowedRoots...)
	deniedPaths := append([]string(nil), policy.Tools.Filesystem.DeniedPaths...)
	if toolPolicy, configured := policy.ClaudeCodeToolPolicy(toolName); configured {
		if len(toolPolicy.AllowedRoots) > 0 {
			allowedRoots = append([]string(nil), toolPolicy.AllowedRoots...)
		}
		if len(toolPolicy.DeniedPaths) > 0 {
			deniedPaths = append([]string(nil), toolPolicy.DeniedPaths...)
		}
	}
	return allowedRoots, deniedPaths
}

func localPathMatchesAnyPolicyRoot(targetPath string, configuredPaths []string, repoRoot string) bool {
	for _, configuredPath := range configuredPaths {
		resolvedConfiguredPath := localResolveConfiguredPath(configuredPath, repoRoot)
		if resolvedConfiguredPath == "" {
			continue
		}
		if localPathWithinRoot(targetPath, resolvedConfiguredPath) {
			return true
		}
	}
	return false
}

func localResolveConfiguredPath(configuredPath string, repoRoot string) string {
	trimmedConfiguredPath := strings.TrimSpace(configuredPath)
	if trimmedConfiguredPath == "" {
		return ""
	}
	if filepath.IsAbs(trimmedConfiguredPath) {
		return filepath.Clean(trimmedConfiguredPath)
	}
	return filepath.Clean(filepath.Join(repoRoot, trimmedConfiguredPath))
}

func localPathWithinRoot(targetPath string, rootPath string) bool {
	relativePath, err := filepath.Rel(rootPath, targetPath)
	if err != nil {
		return false
	}
	if relativePath == "." {
		return true
	}
	return relativePath != ".." && !strings.HasPrefix(relativePath, ".."+string(filepath.Separator))
}

func newOperatorOverrideGrantID() (string, error) {
	randomBytes := make([]byte, 6)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}
	return "override-" + time.Now().UTC().Format("20060102150405") + "-" + hex.EncodeToString(randomBytes), nil
}
