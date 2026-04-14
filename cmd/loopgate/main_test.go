package main

import (
	"io"
	"os"
	"strings"
	"testing"
)

func TestMainRejectsDeprecatedAcceptPolicyFlag(t *testing.T) {
	originalArgs := os.Args
	defer func() { os.Args = originalArgs }()

	os.Args = []string{"loopgate", "--accept-policy"}
	exitCode := 0
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("main panicked: %v", recovered)
		}
	}()

	stderrReader, stderrWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stderr: %v", err)
	}
	originalStderr := os.Stderr
	os.Stderr = stderrWriter
	defer func() { os.Stderr = originalStderr }()

	exitProcess = func(code int) {
		exitCode = code
		panic(exitSentinel{})
	}
	defer func() { exitProcess = os.Exit }()

	func() {
		defer func() {
			_ = stderrWriter.Close()
			if recovered := recover(); recovered != nil {
				if _, ok := recovered.(exitSentinel); !ok {
					panic(recovered)
				}
			}
		}()
		main()
	}()

	stderrBytes, readErr := io.ReadAll(stderrReader)
	if readErr != nil {
		t.Fatalf("read stderr: %v", readErr)
	}
	if exitCode != 2 {
		t.Fatalf("expected exit code 2 for deprecated flag, got %d", exitCode)
	}
	if !strings.Contains(string(stderrBytes), "accept-policy") {
		t.Fatalf("expected deprecated flag message, got %q", string(stderrBytes))
	}
}

type exitSentinel struct{}
