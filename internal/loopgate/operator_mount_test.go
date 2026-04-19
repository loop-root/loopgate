package loopgate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeOperatorMountPathsForSession_rejectsNonOperatorActor(t *testing.T) {
	_, err := normalizeOperatorMountPathsForSession("other", []string{"/tmp"})
	if err == nil {
		t.Fatal("expected error for non-operator actor")
	}
}

func TestNormalizeOperatorMountPathsForSession_acceptsOperator(t *testing.T) {
	dir := t.TempDir()
	out, err := normalizeOperatorMountPathsForSession("operator", []string{dir})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("got %#v", out)
	}
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	if out[0] != filepath.Clean(resolved) {
		t.Fatalf("got %q want %q", out[0], filepath.Clean(resolved))
	}
}

func TestNormalizeOperatorMountPathsForSession_rejectsLegacyHavenAlias(t *testing.T) {
	dir := t.TempDir()
	if _, err := normalizeOperatorMountPathsForSession("haven", []string{dir}); err == nil {
		t.Fatal("expected haven alias to be rejected")
	}
}

func TestNormalizePrimaryOperatorMountPathForSession_acceptsMatchingMount(t *testing.T) {
	dir := t.TempDir()
	mounts, err := normalizeOperatorMountPathsForSession("operator", []string{dir})
	if err != nil {
		t.Fatal(err)
	}
	got, err := normalizePrimaryOperatorMountPathForSession("operator", dir, mounts)
	if err != nil {
		t.Fatal(err)
	}
	if got != mounts[0] {
		t.Fatalf("got %q want %q", got, mounts[0])
	}
}

func TestNormalizePrimaryOperatorMountPathForSession_rejectsPathOutsideMounts(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()
	mounts, err := normalizeOperatorMountPathsForSession("operator", []string{dirA})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := normalizePrimaryOperatorMountPathForSession("operator", dirB, mounts); err == nil {
		t.Fatal("expected error for primary mount outside granted mounts")
	}
}

func TestIsDangerousOperatorMountPath(t *testing.T) {
	if !isDangerousOperatorMountPath("/etc") {
		t.Fatal("expected /etc dangerous")
	}
	tmp := filepath.Join(os.TempDir(), "opmount-test-safe")
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		t.Fatal(err)
	}
	if isDangerousOperatorMountPath(tmp) {
		t.Fatal("temp dir should not be dangerous")
	}
}
