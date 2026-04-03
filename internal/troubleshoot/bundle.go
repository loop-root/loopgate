package troubleshoot

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"morph/internal/config"
)

// WriteOperatorBundle creates outDir, writes report.json, and appends last maxLines of each diagnostic log (if present).
func WriteOperatorBundle(repoRoot string, rc config.RuntimeConfig, outDir string, logTailLines int) error {
	if logTailLines < 1 {
		logTailLines = 200
	}
	rep, err := BuildReport(repoRoot, rc)
	if err != nil {
		return err
	}
	if err := WriteReportJSON(outDir, rep); err != nil {
		return err
	}

	tailsDir := filepath.Join(outDir, "diagnostic_log_tails")
	if err := os.MkdirAll(tailsDir, 0o700); err != nil {
		return fmt.Errorf("mkdir tails: %w", err)
	}
	if !rc.Logging.Diagnostic.Enabled {
		readme := filepath.Join(tailsDir, "README.txt")
		msg := "Diagnostic logging was disabled in effective runtime config; no log tails copied.\n"
		if err := os.WriteFile(readme, []byte(msg), 0o600); err != nil {
			return fmt.Errorf("write tails readme: %w", err)
		}
		return nil
	}

	diagDir := filepath.Join(repoRoot, filepath.Clean(rc.Logging.Diagnostic.ResolvedDirectory()))
	basenames := []string{
		rc.Logging.Diagnostic.Files.Audit,
		rc.Logging.Diagnostic.Files.Server,
		rc.Logging.Diagnostic.Files.Client,
		rc.Logging.Diagnostic.Files.Socket,
		rc.Logging.Diagnostic.Files.Memory,
		rc.Logging.Diagnostic.Files.Ledger,
		rc.Logging.Diagnostic.Files.Model,
	}
	for _, base := range basenames {
		name := filepath.Base(strings.TrimSpace(base))
		if name == "" || name == "." {
			continue
		}
		src := filepath.Join(diagDir, name)
		if _, statErr := os.Stat(src); statErr != nil {
			continue
		}
		tailText, tailErr := tailFileLines(src, logTailLines)
		if tailErr != nil {
			return fmt.Errorf("tail %s: %w", src, tailErr)
		}
		dest := filepath.Join(tailsDir, name+".tail.txt")
		if err := os.WriteFile(dest, []byte(tailText), 0o600); err != nil {
			return fmt.Errorf("write tail %s: %w", dest, err)
		}
	}
	return nil
}

func tailFileLines(path string, maxLines int) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	ring := make([]string, 0, maxLines)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if len(ring) < cap(ring) {
			ring = append(ring, line)
		} else {
			copy(ring, ring[1:])
			ring[len(ring)-1] = line
		}
	}
	if err := scanner.Err(); err != nil && err != io.EOF {
		return "", err
	}
	return strings.Join(ring, "\n") + "\n", nil
}
