package tests

import (
	"encoding/json"
	"flowapp/internal/engine"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestJSONStore(t *testing.T) {
	dir := t.TempDir()

	inst := &engine.Instance{
		ID:           "test-123",
		WorkflowName: "test-workflow",
		Title:        "Test Instance",
		Priority:     "high",
		CreatedAt:    time.Now().Truncate(time.Second),
		UpdatedAt:    time.Now().Truncate(time.Second),
		Status:       "active",
	}

	// write
	data, err := json.MarshalIndent(inst, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	filePath := filepath.Join(dir, inst.ID+".json")
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		t.Fatal(err)
	}

	// read back
	raw, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}
	var loaded engine.Instance
	if err := json.Unmarshal(raw, &loaded); err != nil {
		t.Fatal(err)
	}

	if loaded.ID != inst.ID {
		t.Fatalf("expected ID %q, got %q", inst.ID, loaded.ID)
	}
	if loaded.Title != inst.Title {
		t.Fatalf("expected Title %q, got %q", inst.Title, loaded.Title)
	}
	if loaded.WorkflowName != inst.WorkflowName {
		t.Fatalf("expected WorkflowName %q, got %q", inst.WorkflowName, loaded.WorkflowName)
	}
	if loaded.Priority != inst.Priority {
		t.Fatalf("expected Priority %q, got %q", inst.Priority, loaded.Priority)
	}
	if loaded.Status != inst.Status {
		t.Fatalf("expected Status %q, got %q", inst.Status, loaded.Status)
	}
}
