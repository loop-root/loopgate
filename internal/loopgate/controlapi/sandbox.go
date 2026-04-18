package controlapi

import (
	"fmt"
	"strings"
)

type SandboxImportRequest struct {
	HostSourcePath  string `json:"host_source_path"`
	DestinationName string `json:"destination_name"`
}

type SandboxStageRequest struct {
	SandboxSourcePath string `json:"sandbox_source_path"`
	OutputName        string `json:"output_name"`
}

type SandboxMetadataRequest struct {
	SandboxSourcePath string `json:"sandbox_source_path"`
}

type SandboxExportRequest struct {
	SandboxSourcePath   string `json:"sandbox_source_path"`
	HostDestinationPath string `json:"host_destination_path"`
}

type SandboxListRequest struct {
	SandboxPath string `json:"sandbox_path"`
}

type SandboxListEntry struct {
	Name       string `json:"name"`
	EntryType  string `json:"entry_type"`
	SizeBytes  int64  `json:"size_bytes"`
	ModTimeUTC string `json:"mod_time_utc"`
}

type SandboxListResponse struct {
	SandboxPath         string             `json:"sandbox_path"`
	SandboxAbsolutePath string             `json:"sandbox_absolute_path"`
	Entries             []SandboxListEntry `json:"entries"`
}

type SandboxOperationResponse struct {
	Action              string `json:"action"`
	EntryType           string `json:"entry_type"`
	SandboxRelativePath string `json:"sandbox_relative_path,omitempty"`
	SandboxAbsolutePath string `json:"sandbox_absolute_path,omitempty"`
	SourceSandboxPath   string `json:"source_sandbox_path,omitempty"`
	HostPath            string `json:"host_path,omitempty"`
	SandboxRoot         string `json:"sandbox_root,omitempty"`
	ArtifactRef         string `json:"artifact_ref,omitempty"`
	ContentSHA256       string `json:"content_sha256,omitempty"`
	SizeBytes           int64  `json:"size_bytes,omitempty"`
}

type SandboxArtifactMetadataResponse struct {
	ArtifactRef         string `json:"artifact_ref"`
	EntryType           string `json:"entry_type"`
	SandboxRelativePath string `json:"sandbox_relative_path"`
	SandboxAbsolutePath string `json:"sandbox_absolute_path"`
	SandboxRoot         string `json:"sandbox_root"`
	SourceSandboxPath   string `json:"source_sandbox_path,omitempty"`
	ContentSHA256       string `json:"content_sha256"`
	SizeBytes           int64  `json:"size_bytes"`
	StagedAtUTC         string `json:"staged_at_utc"`
	ReviewAction        string `json:"review_action"`
	ExportAction        string `json:"export_action"`
}

func (sandboxImportRequest SandboxImportRequest) Validate() error {
	if strings.TrimSpace(sandboxImportRequest.HostSourcePath) == "" {
		return fmt.Errorf("host_source_path is required")
	}
	if strings.TrimSpace(sandboxImportRequest.DestinationName) == "" {
		return fmt.Errorf("destination_name is required")
	}
	return nil
}

func (sandboxStageRequest SandboxStageRequest) Validate() error {
	if strings.TrimSpace(sandboxStageRequest.SandboxSourcePath) == "" {
		return fmt.Errorf("sandbox_source_path is required")
	}
	if strings.TrimSpace(sandboxStageRequest.OutputName) == "" {
		return fmt.Errorf("output_name is required")
	}
	return nil
}

func (sandboxMetadataRequest SandboxMetadataRequest) Validate() error {
	if strings.TrimSpace(sandboxMetadataRequest.SandboxSourcePath) == "" {
		return fmt.Errorf("sandbox_source_path is required")
	}
	return nil
}

func (sandboxListRequest SandboxListRequest) Validate() error {
	// SandboxPath is optional — empty means list the home root.
	return nil
}

func (sandboxExportRequest SandboxExportRequest) Validate() error {
	if strings.TrimSpace(sandboxExportRequest.SandboxSourcePath) == "" {
		return fmt.Errorf("sandbox_source_path is required")
	}
	if strings.TrimSpace(sandboxExportRequest.HostDestinationPath) == "" {
		return fmt.Errorf("host_destination_path is required")
	}
	return nil
}
