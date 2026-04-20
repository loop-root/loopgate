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

func TestMainHelpFlagPrintsUsage(t *testing.T) {
	originalArgs := os.Args
	defer func() { os.Args = originalArgs }()

	os.Args = []string{"loopgate", "-h"}
	exitCode := 0
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("main panicked: %v", recovered)
		}
	}()

	stdoutReader, stdoutWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stdout: %v", err)
	}
	originalStdout := os.Stdout
	os.Stdout = stdoutWriter
	defer func() { os.Stdout = originalStdout }()

	exitProcess = func(code int) {
		exitCode = code
		panic(exitSentinel{})
	}
	defer func() { exitProcess = os.Exit }()

	func() {
		defer func() {
			_ = stdoutWriter.Close()
			if recovered := recover(); recovered != nil {
				if _, ok := recovered.(exitSentinel); !ok {
					panic(recovered)
				}
			}
		}()
		main()
	}()

	stdoutBytes, readErr := io.ReadAll(stdoutReader)
	if readErr != nil {
		t.Fatalf("read stdout: %v", readErr)
	}
	if exitCode != 0 {
		t.Fatalf("expected exit code 0 for help, got %d", exitCode)
	}
	if !strings.Contains(string(stdoutBytes), "loopgate setup") {
		t.Fatalf("expected help output to mention setup, got %q", string(stdoutBytes))
	}
	if !strings.Contains(string(stdoutBytes), "loopgate-doctor") {
		t.Fatalf("expected help output to mention companion tools, got %q", string(stdoutBytes))
	}
}

func TestMainUnknownSubcommandPrintsUsage(t *testing.T) {
	originalArgs := os.Args
	defer func() { os.Args = originalArgs }()

	os.Args = []string{"loopgate", "frob"}
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
		t.Fatalf("expected exit code 2 for unknown subcommand, got %d", exitCode)
	}
	if !strings.Contains(string(stderrBytes), `unknown subcommand "frob"`) {
		t.Fatalf("expected unknown subcommand message, got %q", string(stderrBytes))
	}
	if !strings.Contains(string(stderrBytes), "loopgate setup") {
		t.Fatalf("expected usage summary with setup command, got %q", string(stderrBytes))
	}
}
