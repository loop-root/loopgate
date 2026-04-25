package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"loopgate/internal/config"
	"loopgate/internal/ledger"
	"loopgate/internal/loopgate"
	controlapipkg "loopgate/internal/loopgate/controlapi"
)

const defaultConsoleRecentEventCount = 20

type consoleSnapshot struct {
	FetchedAtUTC      string
	Status            operatorStatusReport
	PolicyExplanation consolePolicyExplanation
	OperatorGrants    consoleOperatorGrantStatus
	Approvals         []controlapipkg.OperatorApprovalSummary
	ApprovalError     string
	AuditVerified     bool
	AuditVerifyError  string
	RecentAuditEvents []consoleAuditEvent
	DecisionSummary   consoleDecisionSummary
}

type consoleOperatorGrantStatus struct {
	Present           bool
	SignatureKeyID    string
	ContentSHA256     string
	ActiveGrantCount  int
	RevokedGrantCount int
	ActiveByClass     map[string]int
	Error             string
}

type consoleDecisionSummary struct {
	Allow int
	Ask   int
	Block int
}

type consoleAuditEvent struct {
	TS                string
	Type              string
	Session           string
	RequestID         string
	ApprovalRequestID string
	Capability        string
	Decision          string
	Status            string
	DenialCode        string
	EventHashPrefix   string
	Summary           string
}

func runConsole(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	fs := flag.NewFlagSet("console", flag.ContinueOnError)
	fs.SetOutput(stderr)

	defaultRepoRoot, err := resolveLoopgateRepoRoot("")
	if err != nil {
		return err
	}
	defaultClaudeConfigDir, err := defaultClaudeDir()
	if err != nil {
		return err
	}

	repoRootFlag := fs.String("repo-root", defaultRepoRoot, "repository root containing Loopgate config and signed policy files")
	claudeDirFlag := fs.String("claude-dir", defaultClaudeConfigDir, "Claude config directory")
	socketPathFlag := fs.String("socket", "", "Loopgate socket path (default: <repo>/runtime/state/loopgate.sock)")
	onceFlag := fs.Bool("once", false, "render one console snapshot and exit")
	eventCountFlag := fs.Int("events", defaultConsoleRecentEventCount, "number of recent verified audit events to show")
	if err := fs.Parse(args); err != nil {
		return normalizeFlagParseError(err)
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %s", strings.Join(fs.Args(), " "))
	}
	if *eventCountFlag < 1 {
		*eventCountFlag = 1
	}

	repoRoot, err := resolveLoopgateRepoRoot(strings.TrimSpace(*repoRootFlag))
	if err != nil {
		return err
	}
	socketPath := strings.TrimSpace(*socketPathFlag)
	if socketPath == "" {
		socketPath = filepath.Join(repoRoot, "runtime", "state", "loopgate.sock")
	}
	claudeDir := strings.TrimSpace(*claudeDirFlag)

	if *onceFlag {
		return renderConsoleOnce(stdout, repoRoot, claudeDir, socketPath, *eventCountFlag)
	}

	return runInteractiveConsole(stdin, stdout, repoRoot, claudeDir, socketPath, *eventCountFlag)
}

func renderConsoleOnce(stdout io.Writer, repoRoot string, claudeDir string, socketPath string, eventCount int) error {
	snapshot, err := collectConsoleSnapshot(repoRoot, claudeDir, socketPath, eventCount)
	if err != nil {
		return err
	}
	printConsoleSnapshot(stdout, snapshot, false)
	return nil
}

func runInteractiveConsole(stdin io.Reader, stdout io.Writer, repoRoot string, claudeDir string, socketPath string, eventCount int) error {
	reader := bufio.NewReader(stdin)
	for {
		snapshot, err := collectConsoleSnapshot(repoRoot, claudeDir, socketPath, eventCount)
		if err != nil {
			return err
		}
		printConsoleSnapshot(stdout, snapshot, true)
		fmt.Fprint(stdout, "\nconsole> ")
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				fmt.Fprintln(stdout)
				return nil
			}
			return err
		}
		line = strings.TrimSpace(line)
		if line == "" || line == "r" || line == "refresh" {
			continue
		}
		if line == "q" || line == "quit" || line == "exit" {
			return nil
		}
		if line == "h" || line == "help" || line == "?" {
			printConsoleHelp(stdout)
			continue
		}
		fmt.Fprintf(stdout, "unknown command %q; type help for commands\n", line)
	}
}

func collectConsoleSnapshot(repoRoot string, claudeDir string, socketPath string, eventCount int) (consoleSnapshot, error) {
	statusReport, err := collectOperatorStatusReport(repoRoot, claudeDir, socketPath, true)
	if err != nil {
		return consoleSnapshot{}, err
	}
	snapshot := consoleSnapshot{
		FetchedAtUTC:      time.Now().UTC().Format(time.RFC3339),
		Status:            statusReport,
		PolicyExplanation: collectConsolePolicyExplanation(repoRoot),
	}
	snapshot.OperatorGrants = collectConsoleOperatorGrantStatus(repoRoot)

	if statusReport.Daemon.Healthy {
		approvals, err := collectConsoleApprovals(socketPath)
		if err != nil {
			snapshot.ApprovalError = err.Error()
		} else {
			snapshot.Approvals = approvals
		}
	} else {
		snapshot.ApprovalError = "daemon unavailable"
	}

	events, err := readVerifiedRecentAuditEvents(repoRoot, eventCount)
	if err != nil {
		snapshot.AuditVerifyError = err.Error()
	} else {
		snapshot.AuditVerified = true
		snapshot.RecentAuditEvents = events
		snapshot.DecisionSummary = summarizeConsoleDecisions(events)
	}
	return snapshot, nil
}

func collectConsoleOperatorGrantStatus(repoRoot string) consoleOperatorGrantStatus {
	loadResult, err := config.LoadOperatorOverrideDocumentWithHash(repoRoot)
	if err != nil {
		return consoleOperatorGrantStatus{Error: err.Error()}
	}
	status := consoleOperatorGrantStatus{
		Present:        loadResult.Present,
		SignatureKeyID: strings.TrimSpace(loadResult.SignatureKeyID),
		ContentSHA256:  strings.TrimSpace(loadResult.ContentSHA256),
		ActiveByClass:  map[string]int{},
	}
	for _, grant := range loadResult.Document.Grants {
		switch strings.TrimSpace(grant.State) {
		case "active":
			status.ActiveGrantCount++
			status.ActiveByClass[strings.TrimSpace(grant.Class)]++
		case "revoked":
			status.RevokedGrantCount++
		}
	}
	return status
}

func collectConsoleApprovals(socketPath string) ([]controlapipkg.OperatorApprovalSummary, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client := newConsoleApprovalClient(socketPath, "list")
	defer client.CloseIdleConnections()
	defer func() {
		_ = client.CloseSession(context.Background())
	}()

	response, err := client.ListPendingApprovals(ctx)
	if err != nil {
		return nil, err
	}
	return response.Approvals, nil
}

func newConsoleApprovalClient(socketPath string, suffix string) *loopgate.Client {
	client := loopgate.NewClient(socketPath)
	client.ConfigureSession("loopgate-console", defaultCommandSessionID("console-"+strings.TrimSpace(suffix)), []string{"approval.read"})
	return client
}

func readVerifiedRecentAuditEvents(repoRoot string, eventCount int) ([]consoleAuditEvent, error) {
	if eventCount < 1 {
		eventCount = 1
	}
	runtimeConfig, err := config.LoadRuntimeConfig(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("load runtime config: %w", err)
	}
	activePath := filepath.Join(repoRoot, filepath.FromSlash(defaultAuditLedgerPathSuffix))
	if _, _, err := ledger.ReadSegmentedChainState(activePath, "audit_sequence", consoleAuditRotationSettings(repoRoot, runtimeConfig)); err != nil {
		return nil, fmt.Errorf("verify audit ledger: %w", err)
	}

	fileHandle, err := os.Open(activePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open audit ledger: %w", err)
	}
	defer fileHandle.Close()

	ring := make([]ledger.Event, 0, eventCount)
	scanner := bufio.NewScanner(fileHandle)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
	for scanner.Scan() {
		event, ok := ledger.ParseEvent(scanner.Bytes())
		if !ok {
			return nil, fmt.Errorf("parse audit ledger tail: malformed JSONL")
		}
		if len(ring) < cap(ring) {
			ring = append(ring, event)
			continue
		}
		copy(ring, ring[1:])
		ring[len(ring)-1] = event
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read audit ledger: %w", err)
	}

	events := make([]consoleAuditEvent, 0, len(ring))
	for _, event := range ring {
		events = append(events, summarizeConsoleAuditEvent(event))
	}
	return events, nil
}

func consoleAuditRotationSettings(repoRoot string, runtimeConfig config.RuntimeConfig) ledger.RotationSettings {
	verifyClosedSegments := true
	if runtimeConfig.Logging.AuditLedger.VerifyClosedSegmentsOnStartup != nil {
		verifyClosedSegments = *runtimeConfig.Logging.AuditLedger.VerifyClosedSegmentsOnStartup
	}
	return ledger.RotationSettings{
		MaxEventBytes:                 runtimeConfig.Logging.AuditLedger.MaxEventBytes,
		RotateAtBytes:                 runtimeConfig.Logging.AuditLedger.RotateAtBytes,
		SegmentDir:                    filepath.Join(repoRoot, runtimeConfig.Logging.AuditLedger.SegmentDir),
		ManifestPath:                  filepath.Join(repoRoot, runtimeConfig.Logging.AuditLedger.ManifestPath),
		VerifyClosedSegmentsOnStartup: verifyClosedSegments,
	}
}

func summarizeConsoleAuditEvent(event ledger.Event) consoleAuditEvent {
	summary := consoleAuditEvent{
		TS:                strings.TrimSpace(event.TS),
		Type:              strings.TrimSpace(event.Type),
		Session:           strings.TrimSpace(event.Session),
		RequestID:         consoleEventDataString(event.Data, "request_id"),
		ApprovalRequestID: consoleEventDataString(event.Data, "approval_request_id"),
		Capability:        consoleEventDataString(event.Data, "capability"),
		Decision:          consoleEventDataString(event.Data, "decision"),
		Status:            consoleEventDataString(event.Data, "status"),
		DenialCode:        consoleEventDataString(event.Data, "denial_code"),
		EventHashPrefix:   consoleHashPrefix(consoleEventDataString(event.Data, "event_hash")),
	}
	if toolName := consoleEventDataString(event.Data, "tool_name"); toolName != "" && summary.Capability == "" {
		summary.Capability = toolName
	}
	summary.Summary = consoleAuditEventSummary(summary, event.Data)
	return summary
}

func summarizeConsoleDecisions(events []consoleAuditEvent) consoleDecisionSummary {
	var summary consoleDecisionSummary
	for _, event := range events {
		switch strings.ToLower(strings.TrimSpace(event.Decision)) {
		case "allow":
			summary.Allow++
		case "ask":
			summary.Ask++
		case "block":
			summary.Block++
		}
	}
	return summary
}

func consoleAuditEventSummary(event consoleAuditEvent, data map[string]interface{}) string {
	switch {
	case event.Capability != "" && event.RequestID != "":
		return event.Capability + " request_id=" + event.RequestID
	case event.ApprovalRequestID != "":
		return "approval_request_id=" + event.ApprovalRequestID
	case consoleEventDataString(data, "reason") != "":
		return consoleEventDataString(data, "reason")
	case consoleEventDataString(data, "message") != "":
		return consoleEventDataString(data, "message")
	default:
		return event.Session
	}
}

func consoleEventDataString(data map[string]interface{}, key string) string {
	if data == nil {
		return ""
	}
	value, found := data[key]
	if !found {
		return ""
	}
	switch typedValue := value.(type) {
	case string:
		return strings.TrimSpace(typedValue)
	case fmt.Stringer:
		return strings.TrimSpace(typedValue.String())
	case float64:
		return strconv.FormatInt(int64(typedValue), 10)
	case int:
		return strconv.Itoa(typedValue)
	case int64:
		return strconv.FormatInt(typedValue, 10)
	default:
		return ""
	}
}

func consoleHashPrefix(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 16 {
		return value
	}
	return value[:16]
}

func printConsoleSnapshot(output io.Writer, snapshot consoleSnapshot, interactive bool) {
	statusLabel := "attention"
	if snapshot.Status.OK {
		statusLabel = "ok"
	}
	fmt.Fprintln(output, "Loopgate Console")
	fmt.Fprintf(output, "fetched_at_utc: %s\n", snapshot.FetchedAtUTC)
	fmt.Fprintf(output, "status: %s\n", statusLabel)
	fmt.Fprintln(output)

	printConsoleOverview(output, snapshot.Status)
	printConsolePolicyExplanation(output, snapshot.PolicyExplanation)
	printConsoleGrantSummary(output, snapshot.OperatorGrants)
	printConsoleDecisionSummary(output, snapshot)
	printConsoleApprovals(output, snapshot)
	printConsoleAuditEvents(output, snapshot)
	printConsoleNextSteps(output, snapshot.Status)
	if interactive {
		printConsoleHelp(output)
	}
}

func printConsoleOverview(output io.Writer, report operatorStatusReport) {
	fmt.Fprintln(output, "Overview")
	fmt.Fprintf(output, "  daemon: %s", report.DaemonMode)
	if report.Daemon.Version != "" {
		fmt.Fprintf(output, " version=%s", report.Daemon.Version)
	}
	if report.Daemon.Error != "" {
		fmt.Fprintf(output, " warning=%q", report.Daemon.Error)
	}
	fmt.Fprintln(output)
	fmt.Fprintf(output, "  socket: %s\n", report.SocketPath)
	fmt.Fprintf(output, "  policy: profile=%s signer=%s signer_verified=%t\n", report.Policy.Profile, report.Policy.SignatureKeyID, report.Signer.Verified)
	fmt.Fprintf(output, "  hooks: %s managed_events=%d copied_scripts=%d\n", report.ClaudeHooks.State, report.ClaudeHooks.ManagedEventCount, report.ClaudeHooks.CopiedScriptCount)
	if report.LaunchAgent.Supported {
		fmt.Fprintf(output, "  launch_agent: %s\n", report.LaunchAgent.State)
	}
	fmt.Fprintf(output, "  audit_ledger: %s\n", report.AuditLedgerPath)
	if report.Live != nil && report.Live.Error == "" {
		fmt.Fprintf(output, "  live: pending_approvals=%d capabilities=%d connections=%d\n", report.Live.PendingApprovals, report.Live.CapabilityCount, report.Live.ConnectionCount)
	} else if report.Live != nil && report.Live.Error != "" {
		fmt.Fprintf(output, "  live: unavailable warning=%q\n", report.Live.Error)
	}
	fmt.Fprintln(output)
}

func printConsoleGrantSummary(output io.Writer, status consoleOperatorGrantStatus) {
	fmt.Fprintln(output, "Operator Grants")
	if status.Error != "" {
		fmt.Fprintf(output, "  unavailable: %s\n\n", status.Error)
		return
	}
	if !status.Present {
		fmt.Fprintln(output, "  operator_policy: absent")
		fmt.Fprintln(output, "  active_grants: 0")
		fmt.Fprintln(output)
		return
	}
	fmt.Fprintf(output, "  operator_policy: signed")
	if status.SignatureKeyID != "" {
		fmt.Fprintf(output, " key_id=%s", status.SignatureKeyID)
	}
	if status.ContentSHA256 != "" {
		fmt.Fprintf(output, " sha256=%s", consoleHashPrefix(status.ContentSHA256))
	}
	fmt.Fprintln(output)
	fmt.Fprintf(output, "  active_grants: %d\n", status.ActiveGrantCount)
	fmt.Fprintf(output, "  revoked_grants: %d\n", status.RevokedGrantCount)
	if len(status.ActiveByClass) > 0 {
		classes := make([]string, 0, len(status.ActiveByClass))
		for className := range status.ActiveByClass {
			if strings.TrimSpace(className) != "" {
				classes = append(classes, className)
			}
		}
		sort.Strings(classes)
		for _, className := range classes {
			fmt.Fprintf(output, "  class.%s: %d\n", className, status.ActiveByClass[className])
		}
	}
	fmt.Fprintln(output)
}

func printConsoleDecisionSummary(output io.Writer, snapshot consoleSnapshot) {
	fmt.Fprintln(output, "Recent Decisions")
	if snapshot.AuditVerifyError != "" {
		fmt.Fprintf(output, "  unavailable: %s\n\n", snapshot.AuditVerifyError)
		return
	}
	if !snapshot.AuditVerified {
		fmt.Fprintln(output, "  unavailable: audit ledger not verified")
		fmt.Fprintln(output)
		return
	}
	fmt.Fprintf(output, "  allow: %d\n", snapshot.DecisionSummary.Allow)
	fmt.Fprintf(output, "  ask: %d\n", snapshot.DecisionSummary.Ask)
	fmt.Fprintf(output, "  block: %d\n", snapshot.DecisionSummary.Block)
	fmt.Fprintln(output)
}

func printConsoleApprovals(output io.Writer, snapshot consoleSnapshot) {
	fmt.Fprintln(output, "Approvals")
	if snapshot.ApprovalError != "" {
		fmt.Fprintf(output, "  unavailable: %s\n\n", snapshot.ApprovalError)
		return
	}
	if len(snapshot.Approvals) == 0 {
		fmt.Fprintln(output, "  no pending approvals")
		fmt.Fprintln(output)
		return
	}
	writer := tabwriter.NewWriter(output, 2, 4, 2, ' ', 0)
	fmt.Fprintln(writer, "  ID\tCAPABILITY\tREQUESTER\tEXPIRES\tSUMMARY")
	for _, approval := range snapshot.Approvals {
		fmt.Fprintf(writer, "  %s\t%s\t%s\t%s\t%s\n",
			approval.ApprovalRequestID,
			approval.Capability,
			approval.Requester,
			approval.ExpiresAtUTC,
			consoleApprovalSummary(approval.UIApprovalSummary),
		)
	}
	_ = writer.Flush()
	fmt.Fprintln(output)
}

func consoleApprovalSummary(approval controlapipkg.UIApprovalSummary) string {
	for _, candidate := range []string{
		approval.OperatorIntentLine,
		approval.PlanSummary,
		approval.Path,
		approval.Preview,
		approval.Reason,
	} {
		if trimmed := strings.TrimSpace(candidate); trimmed != "" {
			return trimmed
		}
	}
	if approval.ContentBytes > 0 {
		return fmt.Sprintf("%d bytes", approval.ContentBytes)
	}
	return "-"
}

func printConsoleAuditEvents(output io.Writer, snapshot consoleSnapshot) {
	fmt.Fprintln(output, "Recent Audit Ledger Events")
	if snapshot.AuditVerifyError != "" {
		fmt.Fprintf(output, "  unavailable: %s\n\n", snapshot.AuditVerifyError)
		return
	}
	if !snapshot.AuditVerified {
		fmt.Fprintln(output, "  unavailable: audit ledger not verified")
		fmt.Fprintln(output)
		return
	}
	if len(snapshot.RecentAuditEvents) == 0 {
		fmt.Fprintln(output, "  no recent audit events")
		fmt.Fprintln(output)
		return
	}
	writer := tabwriter.NewWriter(output, 2, 4, 2, ' ', 0)
	fmt.Fprintln(writer, "  TS\tTYPE\tSTATUS\tID\tSUMMARY")
	for _, event := range snapshot.RecentAuditEvents {
		status := firstNonBlank(event.Decision, event.Status, event.DenialCode)
		id := firstNonBlank(event.RequestID, event.ApprovalRequestID, event.EventHashPrefix)
		fmt.Fprintf(writer, "  %s\t%s\t%s\t%s\t%s\n", event.TS, event.Type, status, id, event.Summary)
	}
	_ = writer.Flush()
	fmt.Fprintln(output)
}

func printConsoleNextSteps(output io.Writer, report operatorStatusReport) {
	nextSteps := operatorStatusNextSteps(report)
	if len(nextSteps) == 0 {
		return
	}
	fmt.Fprintln(output, "Next Steps")
	for _, nextStep := range nextSteps {
		fmt.Fprintf(output, "  - %s\n", nextStep)
	}
	fmt.Fprintln(output)
}

func printConsoleHelp(output io.Writer) {
	fmt.Fprintln(output, "Commands")
	fmt.Fprintln(output, "  refresh | r")
	fmt.Fprintln(output, "  help | h")
	fmt.Fprintln(output, "  quit | q")
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return "-"
}
