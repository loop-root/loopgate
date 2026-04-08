package loopgate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeOperatorMountPathsForSession_rejectsNonHavenActor(t *testing.T) {
	_, err := normalizeOperatorMountPathsForSession("other", []string{"/tmp"})
	if err == nil {
		t.Fatal("expected error for non-haven actor")
	}
}

func TestNormalizeOperatorMountPathsForSession_acceptsHaven(t *testing.T) {
	dir := t.TempDir()
	out, err := normalizeOperatorMountPathsForSession("haven", []string{dir})
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

func TestNormalizePrimaryOperatorMountPathForSession_acceptsMatchingMount(t *testing.T) {
	dir := t.TempDir()
	mounts, err := normalizeOperatorMountPathsForSession("haven", []string{dir})
	if err != nil {
		t.Fatal(err)
	}
	got, err := normalizePrimaryOperatorMountPathForSession("haven", dir, mounts)
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
	mounts, err := normalizeOperatorMountPathsForSession("haven", []string{dirA})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := normalizePrimaryOperatorMountPathForSession("haven", dirB, mounts); err == nil {
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
