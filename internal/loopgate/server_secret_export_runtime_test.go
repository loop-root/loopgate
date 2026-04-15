package loopgate

import (
	"context"
	"testing"

	policypkg "loopgate/internal/policy"
	toolspkg "loopgate/internal/tools"
)

type fakeLoopgateTool struct {
	name        string
	category    string
	operation   string
	description string
	output      string
}

func (fakeTool fakeLoopgateTool) Name() string      { return fakeTool.name }
func (fakeTool fakeLoopgateTool) Category() string  { return fakeTool.category }
func (fakeTool fakeLoopgateTool) Operation() string { return fakeTool.operation }
func (fakeTool fakeLoopgateTool) Schema() toolspkg.Schema {
	return toolspkg.Schema{Description: fakeTool.description}
}
func (fakeTool fakeLoopgateTool) Execute(context.Context, map[string]string) (string, error) {
	return fakeTool.output, nil
}

func TestIsHighRiskCapability_UntrustedWriteRemainsHighRisk(t *testing.T) {
	tool := fakeLoopgateTool{
		name:      "fs_write",
		category:  "filesystem",
		operation: toolspkg.OpWrite,
	}

	if !isHighRiskCapability(tool, policypkg.CheckResult{Decision: policypkg.Allow}) {
		t.Fatalf("expected untrusted write to remain high risk")
	}
}

type secretHeuristicOptOutTool struct {
	fakeLoopgateTool
}

func (t secretHeuristicOptOutTool) Schema() toolspkg.Schema {
	return toolspkg.Schema{
		Description: t.description,
		Args:        []toolspkg.ArgDef{{Name: "path", Required: true, Type: "path"}},
	}
}

func (secretHeuristicOptOutTool) SecretExportNameHeuristicOptOut() bool { return true }

type rawSecretExportExplicitlyBlockedTool struct {
	fakeLoopgateTool
}

func (rawSecretExportExplicitlyBlockedTool) RawSecretExportProhibited() bool { return true }

func TestCapabilityProhibitsRawSecretExport_OptOutAllowsRegisteredTool(t *testing.T) {
	repoRoot := t.TempDir()
	client, status, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	server.registry.Register(secretHeuristicOptOutTool{fakeLoopgateTool{
		name:        "secret.notexport",
		category:    "filesystem",
		operation:   toolspkg.OpRead,
		description: "test opt-out of secret name heuristic",
		output:      "ok",
	}})
	capabilities := append(capabilityNames(status.Capabilities), "secret.notexport")
	client.ConfigureSession("test-actor", "test-session", capabilities)
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}
	resp, err := client.ExecuteCapability(context.Background(), CapabilityRequest{
		RequestID:  "req-optout",
		Capability: "secret.notexport",
		Arguments:  map[string]string{"path": "."},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if resp.Status != ResponseStatusSuccess {
		t.Fatalf("expected successful read for heuristic opt-out tool, got %#v", resp)
	}
}

func TestCapabilityProhibitsRawSecretExport_UnregisteredHeuristicNameDenied(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}
	resp, err := client.ExecuteCapability(context.Background(), CapabilityRequest{
		RequestID:  "req-heur",
		Capability: "secret.no.such.tool",
		Arguments:  map[string]string{},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if resp.Status != ResponseStatusDenied || resp.DenialCode != DenialCodeSecretExportProhibited {
		t.Fatalf("expected secret export prohibition for unregistered heuristic name, got %#v", resp)
	}
}

func TestCapabilityProhibitsRawSecretExport_RegisteredBlockedNameStillDenied(t *testing.T) {
	repoRoot := t.TempDir()
	client, status, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	server.registry.Register(rawSecretExportExplicitlyBlockedTool{fakeLoopgateTool{
		name:        "secret.blocked",
		category:    "filesystem",
		operation:   toolspkg.OpRead,
		description: "test blocked via RawSecretExportProhibited",
		output:      "ok",
	}})
	capabilities := append(capabilityNames(status.Capabilities), "secret.blocked")
	client.ConfigureSession("test-actor", "test-session", capabilities)
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}
	resp, err := client.ExecuteCapability(context.Background(), CapabilityRequest{
		RequestID:  "req-blocked",
		Capability: "secret.blocked",
		Arguments:  map[string]string{},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if resp.Status != ResponseStatusDenied || resp.DenialCode != DenialCodeSecretExportProhibited {
		t.Fatalf("expected secret export prohibition, got %#v", resp)
	}
}
