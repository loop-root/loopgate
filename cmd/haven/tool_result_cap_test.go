package main

import (
	"strings"
	"testing"
)

func TestCapToolResultContentForModel_NoChangeWhenSmall(t *testing.T) {
	s := "hello"
	got := capToolResultContentForModel("fs_read", s)
	if got != s {
		t.Fatalf("got %q want %q", got, s)
	}
}

func TestCapToolResultContentForModel_TruncatesLarge(t *testing.T) {
	max := maxToolResultRunesForCapability("fs_read")
	body := strings.Repeat("x", max+500)
	got := capToolResultContentForModel("fs_read", body)
	if len([]rune(got)) <= max {
		t.Fatalf("expected truncation past max runes %d, got len %d", max, len([]rune(got)))
	}
	if !strings.Contains(got, "Haven truncated tool output") {
		t.Fatalf("expected truncation notice in output")
	}
}

func TestMaxToolResultRunesForCapability_Override(t *testing.T) {
	if maxToolResultRunesForCapability("host.folder.list") >= defaultToolResultMaxRunes {
		t.Fatalf("expected host.folder.list to be tighter than default")
	}
}
