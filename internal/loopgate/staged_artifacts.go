package loopgate

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"loopgate/internal/sandbox"
)

const stagedArtifactRefPrefix = "staged://artifacts/"

type stagedArtifactRecord struct {
	SchemaVersion       string `json:"schema_version"`
	ArtifactID          string `json:"artifact_id"`
	StagedAtUTC         string `json:"staged_at_utc"`
	EntryType           string `json:"entry_type"`
	SourceSandboxPath   string `json:"source_sandbox_path"`
	SandboxRelativePath string `json:"sandbox_relative_path"`
	ContentSHA256       string `json:"content_sha256"`
	SizeBytes           int64  `json:"size_bytes"`
}

func (server *Server) stageArtifactMetadata(sandboxSourcePath string) (SandboxArtifactMetadataResponse, error) {
	_, sandboxRelativePath, err := server.sandboxPaths.ResolveOutputsPath(sandboxSourcePath)
	if err != nil {
		return SandboxArtifactMetadataResponse{}, err
	}
	artifactRecord, err := loadStagedArtifactRecord(server.repoRoot, sandboxRelativePath)
	if err != nil {
		return SandboxArtifactMetadataResponse{}, err
	}
	return sandboxArtifactMetadataResponse(artifactRecord), nil
}

func (server *Server) writeStagedArtifact(resolvedSourcePath string, sourceRelativePath string, destinationAbsolutePath string, destinationRelativePath string, entryType string) (stagedArtifactRecord, error) {
	contentSHA256, sizeBytes, err := digestSandboxEntry(destinationAbsolutePath, entryType)
	if err != nil {
		return stagedArtifactRecord{}, err
	}
	artifactID, err := randomHex(16)
	if err != nil {
		return stagedArtifactRecord{}, fmt.Errorf("generate staged artifact id: %w", err)
	}
	artifactRecord := stagedArtifactRecord{
		SchemaVersion:       "loopgate.staged_artifact.v1",
		ArtifactID:          artifactID,
		StagedAtUTC:         server.now().UTC().Format(time.RFC3339Nano),
		EntryType:           entryType,
		SourceSandboxPath:   sourceRelativePath,
		SandboxRelativePath: destinationRelativePath,
		ContentSHA256:       contentSHA256,
		SizeBytes:           sizeBytes,
	}
	if err := writeStagedArtifactRecord(recordPathForSandboxArtifact(server.repoRoot, destinationRelativePath), artifactRecord); err != nil {
		return stagedArtifactRecord{}, err
	}
	_ = resolvedSourcePath
	return artifactRecord, nil
}

func recordPathForSandboxArtifact(repoRoot string, sandboxRelativePath string) string {
	pathHash := sha256.Sum256([]byte(sandboxRelativePath))
	return filepath.Join(repoRoot, "runtime", "state", "staged_artifacts", hex.EncodeToString(pathHash[:])+".json")
}

func stagedArtifactRef(artifactID string) string {
	return stagedArtifactRefPrefix + artifactID
}

func loadStagedArtifactRecord(repoRoot string, sandboxRelativePath string) (stagedArtifactRecord, error) {
	recordBytes, err := os.ReadFile(recordPathForSandboxArtifact(repoRoot, sandboxRelativePath))
	if err != nil {
		if os.IsNotExist(err) {
			return stagedArtifactRecord{}, fmt.Errorf("%w: %s", errSandboxArtifactNotStaged, sandboxRelativePath)
		}
		return stagedArtifactRecord{}, fmt.Errorf("read staged artifact record: %w", err)
	}
	var artifactRecord stagedArtifactRecord
	decoder := json.NewDecoder(bytes.NewReader(recordBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&artifactRecord); err != nil {
		return stagedArtifactRecord{}, fmt.Errorf("decode staged artifact record: %w", err)
	}
	if err := artifactRecord.Validate(); err != nil {
		return stagedArtifactRecord{}, fmt.Errorf("invalid staged artifact record: %w", err)
	}
	if artifactRecord.SandboxRelativePath != sandboxRelativePath {
		return stagedArtifactRecord{}, fmt.Errorf("invalid staged artifact record: sandbox_relative_path mismatch")
	}
	return artifactRecord, nil
}

func writeStagedArtifactRecord(path string, artifactRecord stagedArtifactRecord) error {
	if err := artifactRecord.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create staged artifact dir: %w", err)
	}
	recordBytes, err := json.MarshalIndent(artifactRecord, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal staged artifact record: %w", err)
	}
	tempPath := path + ".tmp"
	tempFile, err := os.OpenFile(tempPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open staged artifact temp file: %w", err)
	}
	defer func() { _ = tempFile.Close() }()
	if _, err := tempFile.Write(recordBytes); err != nil {
		return fmt.Errorf("write staged artifact temp file: %w", err)
	}
	if len(recordBytes) == 0 || recordBytes[len(recordBytes)-1] != '\n' {
		if _, err := io.WriteString(tempFile, "\n"); err != nil {
			return fmt.Errorf("write staged artifact newline: %w", err)
		}
	}
	if err := tempFile.Sync(); err != nil {
		return fmt.Errorf("sync staged artifact temp file: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("close staged artifact temp file: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("rename staged artifact temp file: %w", err)
	}
	if recordDir, err := os.Open(filepath.Dir(path)); err == nil {
		_ = recordDir.Sync()
		_ = recordDir.Close()
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("chmod staged artifact file: %w", err)
	}
	return nil
}

func (artifactRecord stagedArtifactRecord) Validate() error {
	if strings.TrimSpace(artifactRecord.ArtifactID) == "" {
		return fmt.Errorf("artifact_id is required")
	}
	if strings.TrimSpace(artifactRecord.StagedAtUTC) == "" {
		return fmt.Errorf("staged_at_utc is required")
	}
	switch artifactRecord.EntryType {
	case "file", "directory":
	default:
		return fmt.Errorf("invalid staged artifact entry_type %q", artifactRecord.EntryType)
	}
	if strings.TrimSpace(artifactRecord.SourceSandboxPath) == "" {
		return fmt.Errorf("source_sandbox_path is required")
	}
	if strings.TrimSpace(artifactRecord.SandboxRelativePath) == "" {
		return fmt.Errorf("sandbox_relative_path is required")
	}
	if strings.TrimSpace(artifactRecord.ContentSHA256) == "" {
		return fmt.Errorf("content_sha256 is required")
	}
	if artifactRecord.SizeBytes < 0 {
		return fmt.Errorf("size_bytes must be non-negative")
	}
	return nil
}

func sandboxArtifactMetadataResponse(artifactRecord stagedArtifactRecord) SandboxArtifactMetadataResponse {
	return SandboxArtifactMetadataResponse{
		ArtifactRef:         stagedArtifactRef(artifactRecord.ArtifactID),
		EntryType:           artifactRecord.EntryType,
		SandboxRelativePath: artifactRecord.SandboxRelativePath,
		SandboxAbsolutePath: sandbox.VirtualizeRelativeHomePath(artifactRecord.SandboxRelativePath),
		SandboxRoot:         sandbox.VirtualHome,
		SourceSandboxPath:   sandbox.VirtualizeRelativeHomePath(artifactRecord.SourceSandboxPath),
		ContentSHA256:       artifactRecord.ContentSHA256,
		SizeBytes:           artifactRecord.SizeBytes,
		StagedAtUTC:         artifactRecord.StagedAtUTC,
		ReviewAction:        "review staged artifact metadata before export",
		ExportAction:        "sandbox export",
	}
}

func digestSandboxEntry(entryPath string, entryType string) (string, int64, error) {
	switch entryType {
	case "file":
		return digestSandboxFile(entryPath)
	case "directory":
		return digestSandboxDirectory(entryPath)
	default:
		return "", 0, fmt.Errorf("unsupported sandbox entry type %q", entryType)
	}
}

func digestSandboxFile(filePath string) (string, int64, error) {
	fileInfo, err := os.Lstat(filePath)
	if err != nil {
		return "", 0, fmt.Errorf("stat staged file: %w", err)
	}
	if fileInfo.Mode()&os.ModeSymlink != 0 {
		return "", 0, fmt.Errorf("%w: %s", sandbox.ErrSymlinkNotAllowed, filePath)
	}
	fileHandle, err := os.Open(filePath)
	if err != nil {
		return "", 0, fmt.Errorf("open staged file: %w", err)
	}
	defer fileHandle.Close()
	hasher := sha256.New()
	sizeBytes, err := io.Copy(hasher, fileHandle)
	if err != nil {
		return "", 0, fmt.Errorf("hash staged file: %w", err)
	}
	return hex.EncodeToString(hasher.Sum(nil)), sizeBytes, nil
}

func digestSandboxDirectory(directoryPath string) (string, int64, error) {
	hasher := sha256.New()
	if _, err := io.WriteString(hasher, "sandbox.directory.v1\n"); err != nil {
		return "", 0, fmt.Errorf("seed directory hash: %w", err)
	}
	var totalSizeBytes int64
	if err := filepath.WalkDir(directoryPath, func(currentPath string, dirEntry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if dirEntry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: %s", sandbox.ErrSymlinkNotAllowed, currentPath)
		}
		relativePath, err := filepath.Rel(directoryPath, currentPath)
		if err != nil {
			return fmt.Errorf("compute directory digest relative path: %w", err)
		}
		normalizedRelativePath := filepath.ToSlash(relativePath)
		if normalizedRelativePath == "." {
			normalizedRelativePath = ""
		}
		if dirEntry.IsDir() {
			_, err = io.WriteString(hasher, fmt.Sprintf("D:%s\n", normalizedRelativePath))
			return err
		}
		fileHash, fileSizeBytes, err := digestSandboxFile(currentPath)
		if err != nil {
			return err
		}
		totalSizeBytes += fileSizeBytes
		_, err = io.WriteString(hasher, fmt.Sprintf("F:%s:%d:%s\n", normalizedRelativePath, fileSizeBytes, fileHash))
		return err
	}); err != nil {
		return "", 0, fmt.Errorf("hash staged directory: %w", err)
	}
	return hex.EncodeToString(hasher.Sum(nil)), totalSizeBytes, nil
}
