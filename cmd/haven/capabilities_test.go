package main

import (
	"strings"
	"testing"

	"morph/internal/loopgate"
	"morph/internal/tools"
)

func TestBuildResidentCapabilitySummary_DedupesHostDisplayName(t *testing.T) {
	caps := []loopgate.CapabilitySummary{
		{Name: "host.folder.list"},
		{Name: "host.folder.read"},
		{Name: "host.organize.plan"},
		{Name: "host.plan.apply"},
	}
	summary := buildResidentCapabilitySummary(caps)
	if !strings.Contains(summary, "Granted host folders") {
		t.Fatalf("expected summary to include unified host label, got %q", summary)
	}
	if strings.Count(summary, "Granted host folders") > 1 {
		t.Fatalf("expected single deduped host label, got %q", summary)
	}
}

func TestBuildResidentCapabilityFacts_IncludesHostPackWhenAllPresent(t *testing.T) {
	caps := []loopgate.CapabilitySummary{
		{Name: "host.folder.list"},
		{Name: "host.folder.read"},
		{Name: "host.organize.plan"},
		{Name: "host.plan.apply"},
	}
	facts := buildResidentCapabilityFacts(caps)
	var hostPackFact string
	var mentionsHostList bool
	for _, line := range facts {
		if strings.Contains(line, "typed host-folder tools") {
			hostPackFact = line
		}
		if strings.Contains(line, "host.folder.list") {
			mentionsHostList = true
		}
	}
	if hostPackFact == "" || !mentionsHostList {
		t.Fatalf("expected resident facts describing host.* tools, got %v", facts)
	}
	if !strings.Contains(hostPackFact, "shell_exec") {
		t.Fatalf("expected host-pack fact to contrast shell_exec, got %q", hostPackFact)
	}
}

func TestBuildResidentCapabilityFacts_OmitsHostPackWhenIncomplete(t *testing.T) {
	caps := []loopgate.CapabilitySummary{
		{Name: "host.folder.list"},
		{Name: "host.folder.read"},
	}
	facts := buildResidentCapabilityFacts(caps)
	for _, line := range facts {
		if strings.Contains(line, "host.organize.plan") {
			t.Fatalf("did not expect full host-pack fact when capabilities incomplete, got %q", line)
		}
	}
}

func TestBuildResidentCapabilityFacts_MemoryRememberHintMatchesRegistry(t *testing.T) {
	caps := []loopgate.CapabilitySummary{
		{Name: "memory.remember"},
	}

	facts := buildResidentCapabilityFacts(caps)
	var memoryFact string
	for _, fact := range facts {
		if strings.Contains(fact, "memory.remember proposes short durable facts") {
			memoryFact = fact
			break
		}
	}
	if memoryFact == "" {
		t.Fatalf("expected memory.remember runtime fact, got %#v", facts)
	}
	if !strings.Contains(memoryFact, "work context") {
		t.Fatalf("expected work-context guidance in memory.remember runtime fact, got %q", memoryFact)
	}
	if !strings.Contains(memoryFact, "standing goals") {
		t.Fatalf("expected goals guidance in memory.remember runtime fact, got %q", memoryFact)
	}
	if !strings.Contains(memoryFact, "goal.current_sprint") {
		t.Fatalf("expected supported goal key example in memory.remember runtime fact, got %q", memoryFact)
	}
	if !strings.Contains(memoryFact, "work.focus_area") {
		t.Fatalf("expected supported work key example in memory.remember runtime fact, got %q", memoryFact)
	}
	if strings.Contains(memoryFact, "context.recent_topic") {
		t.Fatalf("did not expect unsupported context key example in memory.remember runtime fact, got %q", memoryFact)
	}
}

func TestBuildHavenCapabilityAuditWarnings_WarnsWhenAllowlistedCapabilityMissingFromLoopgate(t *testing.T) {
	warnings := buildHavenCapabilityAuditWarnings(
		[]string{"fs_read", "host.folder.list"},
		[]loopgate.CapabilitySummary{{Name: "fs_read"}},
	)

	if len(warnings) != 1 {
		t.Fatalf("expected one warning, got %d: %#v", len(warnings), warnings)
	}
	if !strings.Contains(warnings[0], "host.folder.list") || !strings.Contains(warnings[0], "not offered by Loopgate") {
		t.Fatalf("expected Loopgate availability warning, got %q", warnings[0])
	}
}

func TestBuildCapabilityAllowlistWarnings_WarnsWhenLocalCapabilityMissingFromRegistry(t *testing.T) {
	registry := tools.NewRegistry()

	warnings := buildCapabilityAllowlistWarnings(
		map[string]struct{}{"host.folder.list": {}},
		registry,
	)

	if len(warnings) != 1 {
		t.Fatalf("expected one warning, got %d: %#v", len(warnings), warnings)
	}
	if !strings.Contains(warnings[0], "host.folder.list") || !strings.Contains(warnings[0], "not registered in the tool registry") {
		t.Fatalf("expected registry warning, got %q", warnings[0])
	}
}

func TestBuildCapabilityAllowlistWarnings_IgnoresLoopgateMediatedCapabilities(t *testing.T) {
	registry := tools.NewRegistry()

	warnings := buildCapabilityAllowlistWarnings(
		map[string]struct{}{
			"fs_read":  {},
			"fs_write": {},
			"fs_list":  {},
			"fs_mkdir": {},
		},
		registry,
	)

	if len(warnings) != 0 {
		t.Fatalf("expected no warnings for Loopgate-mediated fs_* capabilities, got %#v", warnings)
	}
}

func TestBuildCapabilityAllowlistWarnings_NoWarningsWhenLocalCapabilityRegistered(t *testing.T) {
	registry := tools.NewRegistry()
	registry.Register(&tools.HostFolderList{})

	warnings := buildCapabilityAllowlistWarnings(
		map[string]struct{}{"host.folder.list": {}},
		registry,
	)

	if len(warnings) != 0 {
		t.Fatalf("expected no warnings, got %#v", warnings)
	}
}
