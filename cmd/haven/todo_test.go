package main

import (
	"context"
	"testing"

	"morph/internal/loopgate"
)

func TestAddTodo_ExecutesLoopgateCapabilityAndRefreshesWakeState(t *testing.T) {
	client := &fakeLoopgateClient{
		wakeStateResp: loopgate.MemoryWakeStateResponse{
			Scope: "global",
			UnresolvedItems: []loopgate.MemoryWakeStateOpenItem{{
				ID:   "todo_123",
				Text: "Pack the gym bag",
			}},
		},
	}
	client.executeCapabilityFn = func(_ context.Context, request loopgate.CapabilityRequest) (loopgate.CapabilityResponse, error) {
		if request.Capability != "todo.add" {
			t.Fatalf("expected todo.add capability, got %q", request.Capability)
		}
		if request.Arguments["text"] != "Pack the gym bag" {
			t.Fatalf("expected normalized todo text, got %q", request.Arguments["text"])
		}
		return loopgate.CapabilityResponse{
			Status: loopgate.ResponseStatusSuccess,
			StructuredResult: map[string]interface{}{
				"item_id": "todo_123",
			},
		}, nil
	}

	app, _ := testApp(t, client)
	response := app.AddTodo("  Pack   the gym bag  ")
	if response.Error != "" {
		t.Fatalf("expected add todo to succeed, got %s", response.Error)
	}
	if !response.Applied || response.ItemID != "todo_123" {
		t.Fatalf("unexpected add todo response %#v", response)
	}
	if app.GetMemoryStatus().UnresolvedItemCount != 1 {
		t.Fatalf("expected wake state refresh after add, got %#v", app.GetMemoryStatus())
	}
}

func TestAddTask_ForwardsSchedulingMetadata(t *testing.T) {
	client := &fakeLoopgateClient{
		wakeStateResp: loopgate.MemoryWakeStateResponse{
			Scope: "global",
			UnresolvedItems: []loopgate.MemoryWakeStateOpenItem{{
				ID:              "todo_456",
				Text:            "Review downloads cleanup",
				TaskKind:        "scheduled",
				SourceKind:      "user",
				NextStep:        "Ask whether to separate invoices",
				ScheduledForUTC: "2026-03-20T09:30:00Z",
			}},
		},
	}
	client.executeCapabilityFn = func(_ context.Context, request loopgate.CapabilityRequest) (loopgate.CapabilityResponse, error) {
		if request.Capability != "todo.add" {
			t.Fatalf("expected todo.add capability, got %q", request.Capability)
		}
		if request.Arguments["task_kind"] != "scheduled" {
			t.Fatalf("expected scheduled task kind, got %#v", request.Arguments)
		}
		if request.Arguments["next_step"] != "Ask whether to separate invoices" {
			t.Fatalf("expected next_step to be forwarded, got %#v", request.Arguments)
		}
		if request.Arguments["scheduled_for_utc"] != "2026-03-20T09:30:00Z" {
			t.Fatalf("expected scheduled_for_utc to be forwarded, got %#v", request.Arguments)
		}
		if request.Arguments["execution_class"] != loopgate.TaskExecutionClassLocalWorkspaceOrganize {
			t.Fatalf("expected execution_class to be forwarded, got %#v", request.Arguments)
		}
		return loopgate.CapabilityResponse{
			Status: loopgate.ResponseStatusSuccess,
			StructuredResult: map[string]interface{}{
				"item_id": "todo_456",
			},
		}, nil
	}

	app, _ := testApp(t, client)
	response := app.AddTask(TaskDraft{
		Text:            "Review downloads cleanup",
		NextStep:        "Ask whether to separate invoices",
		ScheduledForUTC: "2026-03-20T09:30:00Z",
		ExecutionClass:  loopgate.TaskExecutionClassLocalWorkspaceOrganize,
	})
	if response.Error != "" {
		t.Fatalf("expected add task to succeed, got %s", response.Error)
	}
	if !response.Applied || response.ItemID != "todo_456" {
		t.Fatalf("unexpected add task response %#v", response)
	}
}

func TestCompleteTodo_ExecutesLoopgateCapabilityAndRefreshesWakeState(t *testing.T) {
	client := &fakeLoopgateClient{
		wakeStateResp: loopgate.MemoryWakeStateResponse{
			Scope: "global",
		},
	}
	client.executeCapabilityFn = func(_ context.Context, request loopgate.CapabilityRequest) (loopgate.CapabilityResponse, error) {
		if request.Capability != "todo.complete" {
			t.Fatalf("expected todo.complete capability, got %q", request.Capability)
		}
		if request.Arguments["item_id"] != "todo_123" {
			t.Fatalf("expected todo item id, got %q", request.Arguments["item_id"])
		}
		return loopgate.CapabilityResponse{
			Status: loopgate.ResponseStatusSuccess,
		}, nil
	}

	app, _ := testApp(t, client)
	response := app.CompleteTodo("todo_123")
	if response.Error != "" {
		t.Fatalf("expected complete todo to succeed, got %s", response.Error)
	}
	if !response.Applied || response.ItemID != "todo_123" {
		t.Fatalf("unexpected complete todo response %#v", response)
	}
	if app.GetMemoryStatus().UnresolvedItemCount != 0 {
		t.Fatalf("expected wake state refresh after complete, got %#v", app.GetMemoryStatus())
	}
}

func TestAddTodo_RejectsEmptyText(t *testing.T) {
	app, _ := testApp(t, &fakeLoopgateClient{})
	response := app.AddTodo("   ")
	if response.Error == "" {
		t.Fatal("expected empty todo text to be rejected")
	}
}
