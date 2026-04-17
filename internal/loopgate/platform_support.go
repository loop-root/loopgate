package loopgate

import (
	"fmt"
	"runtime"
)

// verifySupportedExecutionPlatform enforces the supported execution OS target (macOS only).
func verifySupportedExecutionPlatform() error {
	if runtime.GOOS == "darwin" {
		return nil
	}
	return fmt.Errorf("unsupported GOOS %q (macOS-only supported target; see docs/ADR/0010-macos-supported-target-and-mcp-removal.md)", runtime.GOOS)
}
