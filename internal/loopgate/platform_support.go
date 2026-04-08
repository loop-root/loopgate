package loopgate

import (
	"fmt"
	"os"
	"runtime"
)

// verifySupportedExecutionPlatform enforces the supported production OS target (macOS only).
//
// LOOPGATE_ALLOW_NON_DARWIN=1 opts out for Linux CI and cross-platform development; production
// operators should not set this. See docs/adr/0010-macos-supported-target-and-mcp-removal.md.
func verifySupportedExecutionPlatform() error {
	if runtime.GOOS == "darwin" {
		return nil
	}
	if os.Getenv("LOOPGATE_ALLOW_NON_DARWIN") != "" {
		return nil
	}
	return fmt.Errorf("unsupported GOOS %q (macOS-only supported target; for development/CI set LOOPGATE_ALLOW_NON_DARWIN=1; see docs/adr/0010-macos-supported-target-and-mcp-removal.md)", runtime.GOOS)
}
