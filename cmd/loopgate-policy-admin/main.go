// Command loopgate-policy-admin validates and explains signed or unsigned
// Loopgate policy YAML, and renders starter admin policy templates.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"

	"loopgate/internal/config"
	"loopgate/internal/loopgate"
	controlapipkg "loopgate/internal/loopgate/controlapi"
)

const policySigningPrivateKeyFileEnv = "LOOPGATE_POLICY_SIGNING_PRIVATE_KEY_FILE"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) < 1 {
		printUsage(stderr)
		return 2
	}
	switch args[0] {
	case "validate":
		return runValidate(args[1:], stdout, stderr)
	case "explain":
		return runExplain(args[1:], stdout, stderr)
	case "diff":
		return runDiff(args[1:], stdout, stderr)
	case "render-template":
		return runRenderTemplate(args[1:], stdout, stderr)
	case "apply":
		return runApply(args[1:], stdout, stderr)
	case "approvals":
		return runApprovals(args[1:], stdout, stderr)
	case "help", "-h", "--help":
		printUsage(stderr)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown subcommand %q\n", args[0])
		printUsage(stderr)
		return 2
	}
}

func printUsage(w io.Writer) {
	fmt.Fprintf(w, `Usage:
  loopgate-policy-admin validate        [-repo DIR] [-policy-file PATH] [-signature-file PATH]
  loopgate-policy-admin explain         [-repo DIR] [-policy-file PATH] [-signature-file PATH] [-tool NAME]
  loopgate-policy-admin diff            [-repo DIR] [-left-policy-file PATH] [-left-signature-file PATH] -right-policy-file PATH [-right-signature-file PATH]
  loopgate-policy-admin render-template [-preset strict|balanced|read-only|developer]
  loopgate-policy-admin apply           [-repo DIR] [-socket PATH] [-verify-setup] [-private-key-file PATH] [-key-id ID]
  loopgate-policy-admin approvals list  [-repo DIR] [-socket PATH]
  loopgate-policy-admin approvals approve <id> [-repo DIR] [-socket PATH] [-reason TEXT]
  loopgate-policy-admin approvals deny <id>    [-repo DIR] [-socket PATH] [-reason TEXT]

Defaults:
  -repo defaults to the current working directory.
  If -policy-file is omitted, validate/explain use the signed repository policy at
  core/policy/policy.yaml plus core/policy/policy.yaml.sig.
  If a policy file is provided explicitly, detached signature verification is optional and only
  runs when -signature-file is also provided.
  diff compares normalized effective policy state after strict parsing and defaults.
  It is not a literal line-by-line source diff of YAML comments, ordering, or formatting.
`)
}

type loadedPolicyDocument struct {
	Policy            config.Policy
	PolicyPath        string
	SignaturePath     string
	ContentSHA256     string
	SignatureVerified bool
}

func runApply(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("apply", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repoRootFlag := fs.String("repo", "", "repository root used to resolve the signed policy path")
	socketPathFlag := fs.String("socket", "", "Unix socket path (default: LOOPGATE_SOCKET or <repo>/runtime/state/loopgate.sock)")
	verifySetupFlag := fs.Bool("verify-setup", false, "verify the local policy signing setup before hot-applying")
	privateKeyPathFlag := fs.String("private-key-file", "", "path to a PKCS#8 PEM-encoded Ed25519 private key used with -verify-setup")
	keyIDFlag := fs.String("key-id", "", "trusted signing key identifier used with -verify-setup (defaults to the current signed policy key_id)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	loadedPolicy, err := loadPolicyDocument(*repoRootFlag, "", "")
	if err != nil {
		fmt.Fprintln(stderr, "ERROR: load signed policy:", err)
		return 1
	}

	baseRoot, err := resolveBaseRoot(*repoRootFlag)
	if err != nil {
		fmt.Fprintln(stderr, "ERROR:", err)
		return 1
	}

	if *verifySetupFlag {
		effectiveKeyID := strings.TrimSpace(*keyIDFlag)
		if effectiveKeyID == "" {
			signatureFile, err := config.LoadPolicySignatureFile(baseRoot)
			if err != nil {
				fmt.Fprintln(stderr, "ERROR: load signed policy key_id for verify-setup:", err)
				return 1
			}
			effectiveKeyID = strings.TrimSpace(signatureFile.KeyID)
		}
		privateKeyPath, privateKeyPathSource, err := resolvePolicySigningPrivateKeyPath(strings.TrimSpace(*privateKeyPathFlag), strings.TrimSpace(os.Getenv(policySigningPrivateKeyFileEnv)), effectiveKeyID)
		if err != nil {
			fmt.Fprintln(stderr, "ERROR:", err)
			return 2
		}
		verificationResult, err := config.VerifyPolicySigningSetup(baseRoot, privateKeyPath, effectiveKeyID)
		if err != nil {
			fmt.Fprintln(stderr, "ERROR: verify policy signing setup:", err)
			return 1
		}
		fmt.Fprintln(stdout, "policy signing setup OK")
		fmt.Fprintf(stdout, "key_id: %s\n", verificationResult.ExpectedKeyID)
		fmt.Fprintf(stdout, "policy_signature: %s\n", verificationResult.SignaturePath)
		fmt.Fprintf(stdout, "policy_signature_key_id: %s\n", verificationResult.SignatureKeyID)
		fmt.Fprintf(stdout, "signer_key_path: %s\n", verificationResult.SignerKeyPath)
		fmt.Fprintf(stdout, "signer_key_source: %s\n", privateKeyPathSource)
		fmt.Fprintf(stdout, "signer_key_permissions: %04o\n", verificationResult.SignerKeyPermissions)
	}

	socketPath := resolveSocketPath(baseRoot, *socketPathFlag)
	client := loopgate.NewClient(socketPath)
	client.ConfigureSession("loopgate-policy-admin", defaultPolicyAdminSessionID("apply"), []string{"config.read", "config.write"})

	runningPolicy, err := client.LoadPolicyConfig(context.Background())
	if err != nil {
		fmt.Fprintln(stderr, "ERROR: load running policy:", err)
		return 1
	}
	diffLines := diffPolicyValues(reflect.ValueOf(runningPolicy), reflect.ValueOf(loadedPolicy.Policy), "")

	reloadResponse, err := client.ReloadPolicyFromDisk(context.Background())
	if err != nil {
		fmt.Fprintln(stderr, "ERROR: apply signed policy:", err)
		return 1
	}
	if strings.TrimSpace(reloadResponse.PolicySHA256) != loadedPolicy.ContentSHA256 {
		fmt.Fprintf(stderr, "ERROR: reloaded policy sha mismatch: local=%s server=%s\n", loadedPolicy.ContentSHA256, strings.TrimSpace(reloadResponse.PolicySHA256))
		return 1
	}

	fmt.Fprintln(stdout, "policy hot-apply OK")
	fmt.Fprintf(stdout, "policy_path: %s\n", loadedPolicy.PolicyPath)
	fmt.Fprintf(stdout, "signature_path: %s\n", loadedPolicy.SignaturePath)
	fmt.Fprintf(stdout, "socket_path: %s\n", socketPath)
	if len(diffLines) == 0 {
		fmt.Fprintln(stdout, "normalized_running_policy_diff: (none)")
	} else {
		fmt.Fprintln(stdout, "normalized_running_policy_diff:")
		for _, diffLine := range diffLines {
			fmt.Fprintf(stdout, "  %s\n", diffLine)
		}
	}
	fmt.Fprintf(stdout, "previous_policy_sha256: %s\n", reloadResponse.PreviousPolicySHA256)
	fmt.Fprintf(stdout, "policy_sha256: %s\n", reloadResponse.PolicySHA256)
	fmt.Fprintf(stdout, "policy_changed: %t\n", reloadResponse.PolicyChanged)
	return 0
}

func runApprovals(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) < 1 {
		fmt.Fprintln(stderr, "ERROR: approvals subcommand is required")
		printUsage(stderr)
		return 2
	}
	switch args[0] {
	case "list":
		return runApprovalsList(args[1:], stdout, stderr)
	case "approve":
		return runApprovalDecision(args[1:], true, stdout, stderr)
	case "deny":
		return runApprovalDecision(args[1:], false, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "ERROR: unknown approvals subcommand %q\n", args[0])
		printUsage(stderr)
		return 2
	}
}

func runApprovalsList(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("approvals list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repoRootFlag := fs.String("repo", "", "repository root used to resolve the default socket path")
	socketPathFlag := fs.String("socket", "", "Unix socket path (default: LOOPGATE_SOCKET or <repo>/runtime/state/loopgate.sock)")
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
	client := newPolicyAdminClient(resolveSocketPath(baseRoot, *socketPathFlag), "approvals-list")

	approvalResponse, err := client.ListPendingApprovals(context.Background())
	if err != nil {
		fmt.Fprintln(stderr, "ERROR: list approvals:", err)
		return 1
	}
	if len(approvalResponse.Approvals) == 0 {
		fmt.Fprintln(stdout, "no pending approvals")
		return 0
	}

	writer := tabwriter.NewWriter(stdout, 0, 8, 2, ' ', 0)
	fmt.Fprintln(writer, "APPROVAL ID\tSESSION\tREQUESTER\tCAPABILITY\tREQUESTED AT")
	for _, approval := range approvalResponse.Approvals {
		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\n",
			approval.ApprovalRequestID,
			approval.ControlSessionID,
			approval.Requester,
			approval.Capability,
			approval.CreatedAtUTC,
		)
	}
	_ = writer.Flush()
	return 0
}

func runApprovalDecision(args []string, approved bool, stdout io.Writer, stderr io.Writer) int {
	commandName := "approvals deny"
	if approved {
		commandName = "approvals approve"
	}
	approvalID := ""
	parseArgs := args
	if len(args) > 0 && !strings.HasPrefix(strings.TrimSpace(args[0]), "-") {
		approvalID = strings.TrimSpace(args[0])
		parseArgs = args[1:]
	}
	fs := flag.NewFlagSet(commandName, flag.ContinueOnError)
	fs.SetOutput(stderr)
	repoRootFlag := fs.String("repo", "", "repository root used to resolve the default socket path")
	socketPathFlag := fs.String("socket", "", "Unix socket path (default: LOOPGATE_SOCKET or <repo>/runtime/state/loopgate.sock)")
	reasonFlag := fs.String("reason", "", "optional operator reason recorded in the audit trail")
	if err := fs.Parse(parseArgs); err != nil {
		return 2
	}
	if approvalID == "" {
		if fs.NArg() != 1 {
			fmt.Fprintf(stderr, "ERROR: %s requires exactly one approval id\n", commandName)
			return 2
		}
		approvalID = strings.TrimSpace(fs.Arg(0))
	} else if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "ERROR: %s requires exactly one approval id\n", commandName)
		return 2
	}

	baseRoot, err := resolveBaseRoot(*repoRootFlag)
	if err != nil {
		fmt.Fprintln(stderr, "ERROR:", err)
		return 1
	}
	client := newPolicyAdminClient(resolveSocketPath(baseRoot, *socketPathFlag), strings.ReplaceAll(commandName, " ", "-"))

	response, err := client.DecidePendingApproval(context.Background(), approvalID, approved, strings.TrimSpace(*reasonFlag))
	if err != nil {
		fmt.Fprintln(stderr, "ERROR: decide approval:", err)
		return 1
	}

	action := "denied"
	if approved {
		action = "approved"
	}
	if strings.TrimSpace(response.AuditEventHash) == "" {
		fmt.Fprintf(stdout, "approval %s %s\n", approvalID, action)
	} else {
		fmt.Fprintf(stdout, "approval %s %s audit_event_hash=%s\n", approvalID, action, response.AuditEventHash)
	}
	if response.Status == controlapipkg.ResponseStatusError {
		return 1
	}
	return 0
}

func runValidate(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repoRootFlag := fs.String("repo", "", "repository root used to resolve default or relative policy paths")
	policyPathFlag := fs.String("policy-file", "", "path to a policy YAML file (defaults to core/policy/policy.yaml under -repo)")
	signaturePathFlag := fs.String("signature-file", "", "path to a detached policy signature file")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	loadedPolicy, err := loadPolicyDocument(*repoRootFlag, *policyPathFlag, *signaturePathFlag)
	if err != nil {
		fmt.Fprintln(stderr, "ERROR:", err)
		return 1
	}

	configuredTools := make([]string, 0, len(loadedPolicy.Policy.Tools.ClaudeCode.ToolPolicies))
	for _, toolName := range config.SupportedClaudeCodeToolPolicyNames() {
		if _, configured := loadedPolicy.Policy.ClaudeCodeToolPolicy(toolName); configured {
			configuredTools = append(configuredTools, toolName)
		}
	}

	fmt.Fprintln(stdout, "policy validation OK")
	fmt.Fprintf(stdout, "policy_path: %s\n", loadedPolicy.PolicyPath)
	if loadedPolicy.SignatureVerified {
		fmt.Fprintf(stdout, "signature_path: %s\n", loadedPolicy.SignaturePath)
		fmt.Fprintln(stdout, "signature_verified: true")
	} else {
		fmt.Fprintln(stdout, "signature_verified: false")
	}
	fmt.Fprintf(stdout, "content_sha256: %s\n", loadedPolicy.ContentSHA256)
	fmt.Fprintf(stdout, "version: %s\n", loadedPolicy.Policy.Version)
	fmt.Fprintf(stdout, "claude_code.deny_unknown_tools: %t\n", loadedPolicy.Policy.ClaudeCodeDenyUnknownTools())
	if len(configuredTools) == 0 {
		fmt.Fprintln(stdout, "claude_code.configured_tools: (none)")
	} else {
		fmt.Fprintf(stdout, "claude_code.configured_tools: %s\n", strings.Join(configuredTools, ", "))
	}
	return 0
}

func runExplain(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("explain", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repoRootFlag := fs.String("repo", "", "repository root used to resolve default or relative policy paths")
	policyPathFlag := fs.String("policy-file", "", "path to a policy YAML file (defaults to core/policy/policy.yaml under -repo)")
	signaturePathFlag := fs.String("signature-file", "", "path to a detached policy signature file")
	toolNameFlag := fs.String("tool", "", "Claude Code tool name to explain (default: all supported tools)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	loadedPolicy, err := loadPolicyDocument(*repoRootFlag, *policyPathFlag, *signaturePathFlag)
	if err != nil {
		fmt.Fprintln(stderr, "ERROR:", err)
		return 1
	}

	fmt.Fprintf(stdout, "policy_path: %s\n", loadedPolicy.PolicyPath)
	if loadedPolicy.SignatureVerified {
		fmt.Fprintf(stdout, "signature_path: %s\n", loadedPolicy.SignaturePath)
	}
	fmt.Fprintf(stdout, "claude_code.deny_unknown_tools: %t\n", loadedPolicy.Policy.ClaudeCodeDenyUnknownTools())

	if trimmedToolName := strings.TrimSpace(*toolNameFlag); trimmedToolName != "" {
		if !isSupportedClaudeCodeToolName(trimmedToolName) {
			fmt.Fprintf(stderr, "ERROR: unsupported Claude Code tool %q\n", trimmedToolName)
			return 2
		}
		printClaudeCodeToolExplanation(stdout, loadedPolicy.Policy, trimmedToolName)
		return 0
	}

	for _, toolName := range config.SupportedClaudeCodeToolPolicyNames() {
		printClaudeCodeToolExplanation(stdout, loadedPolicy.Policy, toolName)
	}
	return 0
}

func runDiff(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("diff", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repoRootFlag := fs.String("repo", "", "repository root used to resolve relative policy paths")
	leftPolicyPathFlag := fs.String("left-policy-file", "", "left policy YAML path (defaults to signed repo policy)")
	leftSignaturePathFlag := fs.String("left-signature-file", "", "detached signature for the left policy file")
	rightPolicyPathFlag := fs.String("right-policy-file", "", "right policy YAML path to compare")
	rightSignaturePathFlag := fs.String("right-signature-file", "", "detached signature for the right policy file")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if strings.TrimSpace(*rightPolicyPathFlag) == "" {
		fmt.Fprintln(stderr, "ERROR: -right-policy-file is required")
		return 2
	}

	leftPolicy, err := loadPolicyDocument(*repoRootFlag, *leftPolicyPathFlag, *leftSignaturePathFlag)
	if err != nil {
		fmt.Fprintln(stderr, "ERROR: load left policy:", err)
		return 1
	}
	rightPolicy, err := loadPolicyDocument(*repoRootFlag, *rightPolicyPathFlag, *rightSignaturePathFlag)
	if err != nil {
		fmt.Fprintln(stderr, "ERROR: load right policy:", err)
		return 1
	}

	diffLines := diffPolicyValues(reflect.ValueOf(leftPolicy.Policy), reflect.ValueOf(rightPolicy.Policy), "")
	fmt.Fprintf(stdout, "left_policy_path: %s\n", leftPolicy.PolicyPath)
	fmt.Fprintf(stdout, "left_content_sha256: %s\n", leftPolicy.ContentSHA256)
	fmt.Fprintf(stdout, "left_signature_verified: %t\n", leftPolicy.SignatureVerified)
	fmt.Fprintf(stdout, "right_policy_path: %s\n", rightPolicy.PolicyPath)
	fmt.Fprintf(stdout, "right_content_sha256: %s\n", rightPolicy.ContentSHA256)
	fmt.Fprintf(stdout, "right_signature_verified: %t\n", rightPolicy.SignatureVerified)
	fmt.Fprintln(stdout, "comparison_mode: normalized_effective_policy")
	fmt.Fprintln(stdout, "comparison_note: not a literal line-by-line source diff; comments, key ordering, and formatting are omitted")
	if len(diffLines) == 0 {
		fmt.Fprintln(stdout, "normalized_policy_diff: (none)")
		return 0
	}
	fmt.Fprintln(stdout, "normalized_policy_diff:")
	for _, diffLine := range diffLines {
		fmt.Fprintf(stdout, "  %s\n", diffLine)
	}
	return 0
}

func runRenderTemplate(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("render-template", flag.ContinueOnError)
	fs.SetOutput(stderr)
	presetFlag := fs.String("preset", "strict", "template preset to render: strict, balanced, read-only, or developer")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	preset, err := config.ResolvePolicyTemplatePreset(*presetFlag)
	if err != nil {
		fmt.Fprintf(stderr, "ERROR: %v\n", err)
		return 2
	}
	fmt.Fprint(stdout, preset.TemplateYAML)
	return 0
}

func loadPolicyDocument(repoRootFlag string, policyPathFlag string, signaturePathFlag string) (loadedPolicyDocument, error) {
	baseRoot, err := resolveBaseRoot(repoRootFlag)
	if err != nil {
		return loadedPolicyDocument{}, err
	}

	trimmedPolicyPath := strings.TrimSpace(policyPathFlag)
	trimmedSignaturePath := strings.TrimSpace(signaturePathFlag)
	policyPath := trimmedPolicyPath
	signaturePath := trimmedSignaturePath
	signatureRequired := false
	if policyPath == "" {
		policyPath = filepath.Join(baseRoot, "core", "policy", "policy.yaml")
		signatureRequired = true
		if signaturePath == "" {
			signaturePath = config.PolicySignaturePath(baseRoot)
		}
	} else {
		policyPath = resolvePathAgainstBase(baseRoot, policyPath)
		if signaturePath != "" {
			signaturePath = resolvePathAgainstBase(baseRoot, signaturePath)
		}
	}

	rawPolicyBytes, err := os.ReadFile(policyPath)
	if err != nil {
		return loadedPolicyDocument{}, fmt.Errorf("read policy file %s: %w", policyPath, err)
	}
	policyHash := sha256.Sum256(rawPolicyBytes)

	signatureVerified := false
	if signatureRequired || signaturePath != "" {
		signatureFile, err := config.LoadPolicySignatureFromPath(signaturePath)
		if err != nil {
			return loadedPolicyDocument{}, err
		}
		if err := config.VerifyPolicyDocumentSignature(rawPolicyBytes, signatureFile); err != nil {
			return loadedPolicyDocument{}, err
		}
		signatureVerified = true
	}

	policy, err := config.ParsePolicyDocument(rawPolicyBytes)
	if err != nil {
		return loadedPolicyDocument{}, err
	}

	return loadedPolicyDocument{
		Policy:            policy,
		PolicyPath:        policyPath,
		SignaturePath:     signaturePath,
		ContentSHA256:     hex.EncodeToString(policyHash[:]),
		SignatureVerified: signatureVerified,
	}, nil
}

func resolveBaseRoot(repoRootFlag string) (string, error) {
	trimmedRepoRoot := strings.TrimSpace(repoRootFlag)
	if trimmedRepoRoot != "" {
		return filepath.Clean(trimmedRepoRoot), nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("determine working directory: %w", err)
	}
	return cwd, nil
}

func resolvePathAgainstBase(baseRoot string, pathValue string) string {
	if filepath.IsAbs(pathValue) {
		return filepath.Clean(pathValue)
	}
	return filepath.Join(baseRoot, pathValue)
}

func resolveSocketPath(baseRoot string, socketPathFlag string) string {
	if trimmedSocketPath := strings.TrimSpace(socketPathFlag); trimmedSocketPath != "" {
		return filepath.Clean(trimmedSocketPath)
	}
	if socketPathFromEnv := strings.TrimSpace(os.Getenv("LOOPGATE_SOCKET")); socketPathFromEnv != "" {
		return filepath.Clean(socketPathFromEnv)
	}
	return filepath.Join(baseRoot, "runtime", "state", "loopgate.sock")
}

func defaultPolicyAdminSessionID(subcommandName string) string {
	trimmedSubcommandName := strings.TrimSpace(subcommandName)
	if trimmedSubcommandName == "" {
		trimmedSubcommandName = "policy-admin"
	}
	return "loopgate-policy-admin-" + trimmedSubcommandName + "-" + strconv.Itoa(os.Getpid())
}

func newPolicyAdminClient(socketPath string, subcommandName string) *loopgate.Client {
	client := loopgate.NewClient(socketPath)
	client.ConfigureSession("loopgate-policy-admin", defaultPolicyAdminSessionID(subcommandName), []string{"approval.read", "approval.write"})
	return client
}

func resolvePolicySigningPrivateKeyPath(flagValue string, envValue string, keyID string) (string, string, error) {
	if trimmedFlagValue := strings.TrimSpace(flagValue); trimmedFlagValue != "" {
		return filepath.Clean(trimmedFlagValue), "-private-key-file", nil
	}
	if trimmedEnvValue := strings.TrimSpace(envValue); trimmedEnvValue != "" {
		return filepath.Clean(trimmedEnvValue), policySigningPrivateKeyFileEnv, nil
	}
	defaultPath, err := defaultPolicySigningPrivateKeyPath(strings.TrimSpace(keyID))
	if err != nil {
		return "", "", err
	}
	return defaultPath, "default operator path", nil
}

func defaultPolicySigningPrivateKeyPath(keyID string) (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("determine default policy signing key directory: %w", err)
	}
	return filepath.Join(filepath.Clean(configDir), "Loopgate", "policy-signing", strings.TrimSpace(keyID)+".pem"), nil
}

func isSupportedClaudeCodeToolName(toolName string) bool {
	for _, supportedToolName := range config.SupportedClaudeCodeToolPolicyNames() {
		if supportedToolName == toolName {
			return true
		}
	}
	return false
}

func printClaudeCodeToolExplanation(w io.Writer, policy config.Policy, toolName string) {
	toolPolicy, configured := policy.ClaudeCodeToolPolicy(toolName)
	baseDecision, baseSource := baseClaudeCodeToolDefault(policy, toolName)
	fmt.Fprintf(w, "\n[%s]\n", toolName)
	fmt.Fprintf(w, "configured: %t\n", configured)
	fmt.Fprintf(w, "base_policy: %s (%s)\n", baseDecision, baseSource)
	if overrideClass, maxDelegation, hasOverrideClass := policy.ClaudeCodeToolOperatorOverride(toolName); hasOverrideClass {
		fmt.Fprintf(w, "operator_override.class: %s\n", overrideClass)
		fmt.Fprintf(w, "operator_override.max_delegation: %s\n", maxDelegation)
		fmt.Fprintf(w, "operator_override.effect: %s\n", describeOperatorOverrideDelegationEffect(maxDelegation))
	}
	if !configured {
		fmt.Fprintln(w, "effective_policy: inherits base policy")
		return
	}

	if toolPolicy.Enabled == nil {
		fmt.Fprintln(w, "tool_policy.enabled: inherit")
	} else {
		fmt.Fprintf(w, "tool_policy.enabled: %t\n", *toolPolicy.Enabled)
	}
	if toolPolicy.RequiresApproval == nil {
		fmt.Fprintln(w, "tool_policy.requires_approval: inherit")
	} else {
		fmt.Fprintf(w, "tool_policy.requires_approval: %t\n", *toolPolicy.RequiresApproval)
	}
	fmt.Fprintf(w, "tool_policy.allowed_roots: %s\n", formatListOrNone(toolPolicy.AllowedRoots))
	fmt.Fprintf(w, "tool_policy.denied_paths: %s\n", formatListOrNone(toolPolicy.DeniedPaths))
	fmt.Fprintf(w, "tool_policy.allowed_domains: %s\n", formatListOrNone(toolPolicy.AllowedDomains))
	fmt.Fprintf(w, "tool_policy.allowed_command_prefixes: %s\n", formatListOrNone(toolPolicy.AllowedCommandPrefixes))
	fmt.Fprintf(w, "tool_policy.denied_command_prefixes: %s\n", formatListOrNone(toolPolicy.DeniedCommandPrefixes))
	fmt.Fprintf(w, "effective_policy: %s\n", describeClaudeCodeToolPolicyEffect(toolPolicy))
}

func baseClaudeCodeToolDefault(policy config.Policy, toolName string) (string, string) {
	switch toolName {
	case "Bash":
		if !policy.Tools.Shell.Enabled {
			return "disabled", "tools.shell.enabled=false"
		}
		if policy.Tools.Shell.RequiresApproval {
			return "approval_required", "tools.shell.requires_approval=true"
		}
		return "allow", "tools.shell"
	case "Write", "Edit", "MultiEdit":
		if !policy.Tools.Filesystem.WriteEnabled {
			return "disabled", "tools.filesystem.write_enabled=false"
		}
		if policy.Tools.Filesystem.WriteRequiresApproval {
			return "approval_required", "tools.filesystem.write_requires_approval=true"
		}
		return "allow", "tools.filesystem write"
	case "Read", "Glob", "Grep":
		if !policy.Tools.Filesystem.ReadEnabled {
			return "disabled", "tools.filesystem.read_enabled=false"
		}
		return "allow", "tools.filesystem read"
	case "WebFetch", "WebSearch":
		if !policy.Tools.HTTP.Enabled {
			return "disabled", "tools.http.enabled=false"
		}
		if policy.Tools.HTTP.RequiresApproval {
			return "approval_required", "tools.http.requires_approval=true"
		}
		return "allow", "tools.http"
	default:
		return "unknown", "unsupported tool mapping"
	}
}

func describeClaudeCodeToolPolicyEffect(toolPolicy config.ClaudeCodeToolPolicy) string {
	hasConstraints := len(toolPolicy.AllowedRoots) > 0 ||
		len(toolPolicy.DeniedPaths) > 0 ||
		len(toolPolicy.AllowedDomains) > 0 ||
		len(toolPolicy.AllowedCommandPrefixes) > 0 ||
		len(toolPolicy.DeniedCommandPrefixes) > 0

	if toolPolicy.Enabled != nil && !*toolPolicy.Enabled {
		return "tool policy disables the tool"
	}
	if toolPolicy.RequiresApproval != nil {
		if *toolPolicy.RequiresApproval {
			if hasConstraints {
				return "tool policy adds constraints and requires approval"
			}
			return "tool policy requires approval"
		}
		if hasConstraints {
			return "tool policy allows the tool when constraints pass"
		}
		return "tool policy allows the tool"
	}
	if toolPolicy.Enabled != nil && *toolPolicy.Enabled {
		if hasConstraints {
			return "tool policy enables the tool when constraints pass"
		}
		return "tool policy enables the tool"
	}
	if hasConstraints {
		return "inherits base policy with additional constraints"
	}
	return "inherits base policy"
}

func describeOperatorOverrideDelegationEffect(maxDelegation string) string {
	switch strings.TrimSpace(maxDelegation) {
	case config.OperatorOverrideDelegationPersistent:
		return "parent policy allows persistent operator-created exceptions for this action class"
	case config.OperatorOverrideDelegationSession:
		return "parent policy allows session-scoped operator-created exceptions for this action class"
	default:
		return "parent policy does not delegate operator-created exceptions for this action class"
	}
}

func formatListOrNone(values []string) string {
	if len(values) == 0 {
		return "(none)"
	}
	return strings.Join(values, ", ")
}

func diffPolicyValues(leftValue reflect.Value, rightValue reflect.Value, pathPrefix string) []string {
	leftValue = dereferencePolicyValue(leftValue)
	rightValue = dereferencePolicyValue(rightValue)

	if !leftValue.IsValid() && !rightValue.IsValid() {
		return nil
	}
	if !leftValue.IsValid() || !rightValue.IsValid() {
		if emptyPolicyCollectionValue(leftValue) && emptyPolicyCollectionValue(rightValue) {
			return nil
		}
		return []string{fmt.Sprintf("%s: %s => %s", pathPrefix, formatPolicyValue(leftValue), formatPolicyValue(rightValue))}
	}

	if leftValue.Type() != rightValue.Type() {
		return []string{fmt.Sprintf("%s: %s => %s", pathPrefix, formatPolicyValue(leftValue), formatPolicyValue(rightValue))}
	}
	if emptyPolicyCollectionValue(leftValue) && emptyPolicyCollectionValue(rightValue) {
		return nil
	}

	if reflect.DeepEqual(leftValue.Interface(), rightValue.Interface()) {
		return nil
	}

	switch leftValue.Kind() {
	case reflect.Struct:
		var diffLines []string
		leftType := leftValue.Type()
		for fieldIndex := 0; fieldIndex < leftValue.NumField(); fieldIndex++ {
			structField := leftType.Field(fieldIndex)
			if !structField.IsExported() {
				continue
			}
			fieldPath := joinPolicyPath(pathPrefix, yamlFieldName(structField))
			diffLines = append(diffLines, diffPolicyValues(leftValue.Field(fieldIndex), rightValue.Field(fieldIndex), fieldPath)...)
		}
		return diffLines
	case reflect.Map:
		return diffPolicyMaps(leftValue, rightValue, pathPrefix)
	case reflect.Slice, reflect.Array:
		return []string{fmt.Sprintf("%s: %s => %s", pathPrefix, formatPolicyValue(leftValue), formatPolicyValue(rightValue))}
	default:
		return []string{fmt.Sprintf("%s: %s => %s", pathPrefix, formatPolicyValue(leftValue), formatPolicyValue(rightValue))}
	}
}

func diffPolicyMaps(leftValue reflect.Value, rightValue reflect.Value, pathPrefix string) []string {
	keySet := make(map[string]struct{})
	for _, key := range leftValue.MapKeys() {
		keySet[fmt.Sprint(key.Interface())] = struct{}{}
	}
	for _, key := range rightValue.MapKeys() {
		keySet[fmt.Sprint(key.Interface())] = struct{}{}
	}
	mapKeys := make([]string, 0, len(keySet))
	for key := range keySet {
		mapKeys = append(mapKeys, key)
	}
	sort.Strings(mapKeys)

	var diffLines []string
	for _, mapKey := range mapKeys {
		fieldPath := joinPolicyPath(pathPrefix, mapKey)
		leftEntry := mapValueByStringKey(leftValue, mapKey)
		rightEntry := mapValueByStringKey(rightValue, mapKey)
		diffLines = append(diffLines, diffPolicyValues(leftEntry, rightEntry, fieldPath)...)
	}
	return diffLines
}

func mapValueByStringKey(mapValue reflect.Value, key string) reflect.Value {
	if !mapValue.IsValid() || mapValue.Kind() != reflect.Map {
		return reflect.Value{}
	}
	typedKey := reflect.ValueOf(key).Convert(mapValue.Type().Key())
	return mapValue.MapIndex(typedKey)
}

func dereferencePolicyValue(value reflect.Value) reflect.Value {
	for value.IsValid() && (value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer) {
		if value.IsNil() {
			return reflect.Value{}
		}
		value = value.Elem()
	}
	return value
}

func yamlFieldName(structField reflect.StructField) string {
	yamlTag := structField.Tag.Get("yaml")
	if yamlTag == "" {
		return strings.ToLower(structField.Name)
	}
	tagName := strings.Split(yamlTag, ",")[0]
	if tagName == "" || tagName == "-" {
		return strings.ToLower(structField.Name)
	}
	return tagName
}

func joinPolicyPath(prefix string, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "." + name
}

func formatPolicyValue(value reflect.Value) string {
	value = dereferencePolicyValue(value)
	if !value.IsValid() {
		return "<unset>"
	}

	switch value.Kind() {
	case reflect.Slice, reflect.Array:
		if value.Len() == 0 {
			return "[]"
		}
		items := make([]string, 0, value.Len())
		for index := 0; index < value.Len(); index++ {
			items = append(items, fmt.Sprint(value.Index(index).Interface()))
		}
		return "[" + strings.Join(items, ", ") + "]"
	case reflect.Map:
		if value.Len() == 0 {
			return "{}"
		}
		return fmt.Sprintf("%v", value.Interface())
	case reflect.String:
		return fmt.Sprintf("%q", value.String())
	default:
		return fmt.Sprint(value.Interface())
	}
}

func emptyPolicyCollectionValue(value reflect.Value) bool {
	value = dereferencePolicyValue(value)
	if !value.IsValid() {
		return true
	}
	switch value.Kind() {
	case reflect.Slice, reflect.Array, reflect.Map:
		return value.Len() == 0
	default:
		return false
	}
}
