package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestPrintVersion_StableFormat(t *testing.T) {
	originalVersion := buildVersion
	originalCommit := buildCommit
	originalDate := buildDate
	defer func() {
		buildVersion = originalVersion
		buildCommit = originalCommit
		buildDate = originalDate
	}()

	buildVersion = "v0.1.0"
	buildCommit = "abc1234"
	buildDate = "2026-04-16T12:34:56Z"

	var stdout bytes.Buffer
	printVersion(&stdout)

	output := stdout.String()
	if !strings.Contains(output, "Loopgate v0.1.0\n") {
		t.Fatalf("expected version header, got %q", output)
	}
	if !strings.Contains(output, "commit: abc1234\n") {
		t.Fatalf("expected commit line, got %q", output)
	}
	if !strings.Contains(output, "built_at: 2026-04-16T12:34:56Z\n") {
		t.Fatalf("expected build date line, got %q", output)
	}
}

func TestPrintVersion_OmitsUnknownFields(t *testing.T) {
	originalVersion := buildVersion
	originalCommit := buildCommit
	originalDate := buildDate
	defer func() {
		buildVersion = originalVersion
		buildCommit = originalCommit
		buildDate = originalDate
	}()

	buildVersion = "dev"
	buildCommit = "unknown"
	buildDate = "unknown"

	var stdout bytes.Buffer
	printVersion(&stdout)

	output := stdout.String()
	if output != "Loopgate dev\n" {
		t.Fatalf("expected only the version line, got %q", output)
	}
}
