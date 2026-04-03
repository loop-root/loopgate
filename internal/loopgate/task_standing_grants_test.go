package loopgate

import (
	"context"
	"testing"
)

func TestTaskStandingGrantStatus_DefaultsToSandboxLocalClasses(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	statusResponse, err := client.TaskStandingGrantStatus(context.Background())
	if err != nil {
		t.Fatalf("task standing grant status: %v", err)
	}

	grantByClass := make(map[string]TaskStandingGrantStatus, len(statusResponse.Grants))
	for _, grantStatus := range statusResponse.Grants {
		grantByClass[grantStatus.Class] = grantStatus
	}

	if !grantByClass[TaskExecutionClassLocalWorkspaceOrganize].Granted {
		t.Fatalf("expected %q to be granted by default, got %#v", TaskExecutionClassLocalWorkspaceOrganize, statusResponse.Grants)
	}
	if !grantByClass[TaskExecutionClassLocalDesktopOrganize].Granted {
		t.Fatalf("expected %q to be granted by default, got %#v", TaskExecutionClassLocalDesktopOrganize, statusResponse.Grants)
	}
}

func TestUpdateTaskStandingGrant_PersistsRevocation(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	statusResponse, err := client.UpdateTaskStandingGrant(context.Background(), TaskStandingGrantUpdateRequest{
		Class:   TaskExecutionClassLocalWorkspaceOrganize,
		Granted: false,
	})
	if err != nil {
		t.Fatalf("update task standing grant: %v", err)
	}

	grantByClass := make(map[string]TaskStandingGrantStatus, len(statusResponse.Grants))
	for _, grantStatus := range statusResponse.Grants {
		grantByClass[grantStatus.Class] = grantStatus
	}

	if grantByClass[TaskExecutionClassLocalWorkspaceOrganize].Granted {
		t.Fatalf("expected %q to be revoked, got %#v", TaskExecutionClassLocalWorkspaceOrganize, statusResponse.Grants)
	}

	reloadedStatus, err := client.TaskStandingGrantStatus(context.Background())
	if err != nil {
		t.Fatalf("reload task standing grant status: %v", err)
	}
	reloadedGrantByClass := make(map[string]TaskStandingGrantStatus, len(reloadedStatus.Grants))
	for _, grantStatus := range reloadedStatus.Grants {
		reloadedGrantByClass[grantStatus.Class] = grantStatus
	}
	if reloadedGrantByClass[TaskExecutionClassLocalWorkspaceOrganize].Granted {
		t.Fatalf("expected revoked task standing grant to persist, got %#v", reloadedStatus.Grants)
	}
}
