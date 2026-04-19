package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallLaunchAgent_WritesPlistAndLoadsService(t *testing.T) {
	repoRoot := t.TempDir()
	binaryPath := filepath.Join(t.TempDir(), "loopgate")
	if err := os.WriteFile(binaryPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write fake loopgate binary: %v", err)
	}
	launchAgentsDir := filepath.Join(t.TempDir(), "LaunchAgents")

	var launchctlCalls [][]string
	result, err := installLaunchAgent(launchAgentInstallOptions{
		RepoRoot:        repoRoot,
		BinaryPath:      binaryPath,
		LaunchAgentsDir: launchAgentsDir,
		LoadImmediately: true,
	}, launchAgentDependencies{
		Platform:       "darwin",
		UserUID:        501,
		ExecutablePath: func() (string, error) { return binaryPath, nil },
		UserHomeDir:    func() (string, error) { return t.TempDir(), nil },
		RunLaunchctl: func(args ...string) error {
			launchctlCalls = append(launchctlCalls, append([]string(nil), args...))
			return nil
		},
	})
	if err != nil {
		t.Fatalf("installLaunchAgent: %v", err)
	}

	if result.Label == "" {
		t.Fatal("expected launch agent label")
	}
	if !strings.HasPrefix(result.Label, loopgateLaunchAgentLabelPrefix+".") {
		t.Fatalf("expected launch agent label prefix %q, got %q", loopgateLaunchAgentLabelPrefix+".", result.Label)
	}
	plistBytes, err := os.ReadFile(result.PlistPath)
	if err != nil {
		t.Fatalf("read plist: %v", err)
	}
	plistString := string(plistBytes)
	for _, expectedSnippet := range []string{
		result.Label,
		binaryPath,
		repoRoot,
		filepath.Join(repoRoot, "runtime", "state", "loopgate.sock"),
		loopgateRepoRootEnv,
		"LOOPGATE_SOCKET",
	} {
		if !strings.Contains(plistString, expectedSnippet) {
			t.Fatalf("expected plist to contain %q, got %s", expectedSnippet, plistString)
		}
	}

	if len(launchctlCalls) != 3 {
		t.Fatalf("expected 3 launchctl calls, got %d: %#v", len(launchctlCalls), launchctlCalls)
	}
	if strings.Join(launchctlCalls[0], " ") != "bootout gui/501/"+result.Label {
		t.Fatalf("unexpected bootout call: %#v", launchctlCalls[0])
	}
	if strings.Join(launchctlCalls[1], " ") != "bootstrap gui/501 "+result.PlistPath {
		t.Fatalf("unexpected bootstrap call: %#v", launchctlCalls[1])
	}
	if strings.Join(launchctlCalls[2], " ") != "kickstart -k gui/501/"+result.Label {
		t.Fatalf("unexpected kickstart call: %#v", launchctlCalls[2])
	}
}

func TestResolveLoopgateExecutablePath_RejectsTransientGoRunBinary(t *testing.T) {
	_, err := resolveLoopgateExecutablePath("/var/folders/test/go-build1234/b001/exe/loopgate", launchAgentDependencies{
		Platform: "darwin",
	})
	if err == nil {
		t.Fatal("expected transient go run binary path to be rejected")
	}
	if !strings.Contains(err.Error(), "transient go run executable") {
		t.Fatalf("expected go run error, got %v", err)
	}
}
