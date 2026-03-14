package flowapp_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"flowapp/internal/dsl"
	"flowapp/internal/engine"
	"flowapp/internal/store"
)

// ═══════════════════════════════════════════════════════
// Helpers
// ═══════════════════════════════════════════════════════

func mustParse(t *testing.T, src string) *dsl.Workflow {
	t.Helper()
	wf, err := dsl.Parse(src)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	return wf
}

func mustInstance(t *testing.T, src string) *engine.Instance {
	t.Helper()
	wf := mustParse(t, src)
	return engine.NewInstance("test-id", "Test Instance", wf)
}

func stepByName(inst *engine.Instance, name string) *engine.StepState {
	for _, sec := range inst.Sections {
		for _, s := range sec.Steps {
			if s.Name == name {
				return s
			}
		}
	}
	return nil
}

func assertStatus(t *testing.T, inst *engine.Instance, stepName string, want engine.Status) {
	t.Helper()
	s := stepByName(inst, stepName)
	if s == nil {
		t.Errorf("step %q not found", stepName)
		return
	}
	if s.Status != want {
		t.Errorf("step %q: got status %q, want %q", stepName, s.Status, want)
	}
}

// ═══════════════════════════════════════════════════════
// DSL Parser Tests
// ═══════════════════════════════════════════════════════

func TestParser_BasicFields(t *testing.T) {
	wf := mustParse(t, `
workflow "Invoice Payment"
priority high
label finance
label legal
`)
	if wf.Name != "Invoice Payment" {
		t.Errorf("name: got %q, want %q", wf.Name, "Invoice Payment")
	}
	if wf.Priority != "high" {
		t.Errorf("priority: got %q, want %q", wf.Priority, "high")
	}
	if len(wf.Labels) != 2 || wf.Labels[0] != "finance" || wf.Labels[1] != "legal" {
		t.Errorf("labels: got %v", wf.Labels)
	}
}

func TestParser_DefaultPriority(t *testing.T) {
	wf := mustParse(t, `workflow "Simple"`)
	if wf.Priority != "medium" {
		t.Errorf("default priority: got %q, want medium", wf.Priority)
	}
}

func TestParser_MissingName(t *testing.T) {
	_, err := dsl.Parse(`priority high`)
	if err == nil {
		t.Error("expected error for missing workflow name")
	}
}

func TestParser_StepsAndSections(t *testing.T) {
	wf := mustParse(t, `
workflow "Test"
section "Phase A"
  step "Step 1"
    note "A note"
    due 2d
  step "Step 2"
    needs "Step 1"
    notify "ops@example.com"
section "Phase B"
  step "Step 3"
    needs "Step 1", "Step 2"
`)
	if len(wf.Sections) != 2 {
		t.Fatalf("sections: got %d, want 2", len(wf.Sections))
	}
	if len(wf.Sections[0].Steps) != 2 {
		t.Fatalf("section A steps: got %d, want 2", len(wf.Sections[0].Steps))
	}
	s1 := wf.Sections[0].Steps[0]
	if s1.Name != "Step 1" {
		t.Errorf("step name: got %q", s1.Name)
	}
	if s1.Note != "A note" {
		t.Errorf("note: got %q", s1.Note)
	}
	if s1.Due != "2d" {
		t.Errorf("due: got %q", s1.Due)
	}
	s2 := wf.Sections[0].Steps[1]
	if len(s2.Needs) != 1 || s2.Needs[0] != "Step 1" {
		t.Errorf("needs: got %v", s2.Needs)
	}
	if s2.Notify != "ops@example.com" {
		t.Errorf("notify: got %q", s2.Notify)
	}
	s3 := wf.Sections[1].Steps[0]
	if len(s3.Needs) != 2 {
		t.Errorf("multi-needs: got %v", s3.Needs)
	}
}

func TestParser_AskSyntax(t *testing.T) {
	wf := mustParse(t, `
workflow "Test"
section "S"
  step "Decision"
    ask "Approved?" -> "Proceed", "Reject", "Hold"
`)
	step := wf.Sections[0].Steps[0]
	if step.Ask == nil {
		t.Fatal("ask is nil")
	}
	if step.Ask.Question != "Approved?" {
		t.Errorf("question: got %q", step.Ask.Question)
	}
	if len(step.Ask.Targets) != 3 {
		t.Errorf("targets: got %v", step.Ask.Targets)
	}
	if step.Ask.Targets[0] != "Proceed" || step.Ask.Targets[1] != "Reject" || step.Ask.Targets[2] != "Hold" {
		t.Errorf("target names: got %v", step.Ask.Targets)
	}
}

func TestParser_GateAndEnds(t *testing.T) {
	wf := mustParse(t, `
workflow "Test"
section "S"
  step "Approve"
    ask "OK?" -> "Yes", "No"
    gate
  step "No"
    needs "Approve"
    ends
`)
	approve := wf.Sections[0].Steps[0]
	no := wf.Sections[0].Steps[1]
	if !approve.Gate {
		t.Error("gate not set")
	}
	if !no.Ends {
		t.Error("ends not set")
	}
}

func TestParser_ListItems(t *testing.T) {
	wf := mustParse(t, `
workflow "Test"
section "S"
  step "Checklist"
    list "Required item"
    list "Required explicit" required
    list "Optional item" optional
`)
	step := wf.Sections[0].Steps[0]
	if len(step.ListItems) != 3 {
		t.Fatalf("list items: got %d, want 3", len(step.ListItems))
	}
	if !step.ListItems[0].Required {
		t.Error("item 0 should be required")
	}
	if !step.ListItems[1].Required {
		t.Error("item 1 should be required")
	}
	if step.ListItems[2].Required {
		t.Error("item 2 should be optional")
	}
}

func TestParser_Comments(t *testing.T) {
	wf := mustParse(t, `
workflow "Test"
# this is a comment
section "S"
  # another comment
  step "Step 1"
`)
	if len(wf.Sections[0].Steps) != 1 {
		t.Errorf("comments should be ignored, got %d steps", len(wf.Sections[0].Steps))
	}
}

func TestParser_Roundtrip_WorkflowFile(t *testing.T) {
	// Load the invoice workflow file and verify key properties
	data, err := os.ReadFile(filepath.Join("..", "..", "workflows", "invoice.workflow"))
	if err != nil {
		t.Skip("invoice.workflow not found, skipping roundtrip test")
	}
	wf, err := dsl.Parse(string(data))
	if err != nil {
		t.Fatalf("failed to parse invoice.workflow: %v", err)
	}
	if wf.Name != "Invoice Payment" {
		t.Errorf("name: got %q", wf.Name)
	}
	if len(wf.Sections) == 0 {
		t.Error("expected sections")
	}
	// Collect all step names
	var stepNames []string
	for _, sec := range wf.Sections {
		for _, step := range sec.Steps {
			stepNames = append(stepNames, step.Name)
		}
	}
	if len(stepNames) == 0 {
		t.Error("expected steps")
	}
	// Create instance and verify all steps exist
	inst := engine.NewInstance("roundtrip", "Roundtrip Test", wf)
	for _, name := range stepNames {
		if stepByName(inst, name) == nil {
			t.Errorf("step %q missing from instance", name)
		}
	}
}

// ═══════════════════════════════════════════════════════
// Engine Tests
// ═══════════════════════════════════════════════════════

func TestEngine_InitialActivation(t *testing.T) {
	inst := mustInstance(t, `
workflow "Test"
section "S"
  step "Start"
  step "Waits"
    needs "Start"
`)
	assertStatus(t, inst, "Start", engine.StatusReady)
	assertStatus(t, inst, "Waits", engine.StatusPending)
}

func TestEngine_NeedsActivation(t *testing.T) {
	inst := mustInstance(t, `
workflow "Test"
section "S"
  step "A"
  step "B"
    needs "A"
`)
	if err := inst.AdvanceStep("A"); err != nil {
		t.Fatalf("advance A: %v", err)
	}
	assertStatus(t, inst, "A", engine.StatusDone)
	assertStatus(t, inst, "B", engine.StatusReady)
}

func TestEngine_ANDJoin(t *testing.T) {
	inst := mustInstance(t, `
workflow "Test"
section "S"
  step "A"
  step "B"
  step "Join"
    needs "A", "B"
`)
	assertStatus(t, inst, "Join", engine.StatusPending)
	inst.AdvanceStep("A")
	assertStatus(t, inst, "Join", engine.StatusPending) // still waiting for B
	inst.AdvanceStep("B")
	assertStatus(t, inst, "Join", engine.StatusReady) // both done → ready
}

func TestEngine_ParallelTracks(t *testing.T) {
	inst := mustInstance(t, `
workflow "Test"
section "S"
  step "Start"
  step "Track A"
    needs "Start"
  step "Track B"
    needs "Start"
  step "Join"
    needs "Track A", "Track B"
`)
	inst.AdvanceStep("Start")
	assertStatus(t, inst, "Track A", engine.StatusReady)
	assertStatus(t, inst, "Track B", engine.StatusReady)
	assertStatus(t, inst, "Join", engine.StatusPending)
	inst.AdvanceStep("Track A")
	assertStatus(t, inst, "Join", engine.StatusPending)
	inst.AdvanceStep("Track B")
	assertStatus(t, inst, "Join", engine.StatusReady)
}

func TestEngine_AskRouting(t *testing.T) {
	inst := mustInstance(t, `
workflow "Test"
section "S"
  step "Decision"
    ask "Go?" -> "Yes Path", "No Path"
  step "Yes Path"
    needs "Decision"
  step "No Path"
    needs "Decision"
`)
	assertStatus(t, inst, "Decision", engine.StatusAsk)
	// choose index 0 = "Yes Path"
	if err := inst.AnswerAsk("Decision", 0); err != nil {
		t.Fatalf("AnswerAsk: %v", err)
	}
	assertStatus(t, inst, "Decision", engine.StatusDone)
	assertStatus(t, inst, "Yes Path", engine.StatusReady)
	assertStatus(t, inst, "No Path", engine.StatusSkipped)
}

func TestEngine_AskTargetsNotPreActivated(t *testing.T) {
	// Ask targets should not start as ready — they must wait for routing
	inst := mustInstance(t, `
workflow "Test"
section "S"
  step "Q"
    ask "Choice?" -> "Path A", "Path B"
  step "Path A"
    needs "Q"
  step "Path B"
    needs "Q"
`)
	// Before answering, targets must be pending
	assertStatus(t, inst, "Path A", engine.StatusPending)
	assertStatus(t, inst, "Path B", engine.StatusPending)
}

func TestEngine_GateTokenGenerated(t *testing.T) {
	inst := mustInstance(t, `
workflow "Test"
section "S"
  step "Approval"
    ask "OK?" -> "Yes", "No"
    gate
  step "Yes"
    needs "Approval"
  step "No"
    needs "Approval"
`)
	s := stepByName(inst, "Approval")
	if s == nil {
		t.Fatal("Approval step not found")
	}
	if s.Status != engine.StatusGate {
		t.Errorf("expected gate status, got %q", s.Status)
	}
	if len(s.GateToken) < 16 {
		t.Errorf("gate token too short: %q", s.GateToken)
	}
}

func TestEngine_GateExpired(t *testing.T) {
	inst := mustInstance(t, `
workflow "Test"
section "S"
  step "Approval"
    ask "OK?" -> "Yes", "No"
    gate
    due 1h
  step "Yes"
    needs "Approval"
  step "No"
    needs "Approval"
`)
	s := stepByName(inst, "Approval")
	// Artificially expire the due date
	past := time.Now().Add(-2 * time.Hour)
	s.DueAt = &past

	_, err := inst.RedeemGate(s.GateToken, 0)
	if err == nil {
		t.Error("expected error for expired gate token")
	}
	if !strings.Contains(err.Error(), "expired") {
		t.Errorf("expected 'expired' in error, got: %v", err)
	}
}

func TestEngine_EndsStatus(t *testing.T) {
	inst := mustInstance(t, `
workflow "Test"
section "S"
  step "Terminal"
    ends
`)
	if err := inst.AdvanceStep("Terminal"); err != nil {
		t.Fatalf("advance: %v", err)
	}
	assertStatus(t, inst, "Terminal", engine.StatusEnded)
}

func TestEngine_RequiredListBlocks(t *testing.T) {
	inst := mustInstance(t, `
workflow "Test"
section "S"
  step "Checklist"
    list "Must do" required
`)
	err := inst.AdvanceStep("Checklist")
	if err == nil {
		t.Error("expected error: required list item unchecked")
	}
	// check the item
	s := stepByName(inst, "Checklist")
	inst.ToggleListItem("Checklist", s.ListItems[0].ID)
	// now it should advance
	if err := inst.AdvanceStep("Checklist"); err != nil {
		t.Errorf("should advance after checking: %v", err)
	}
}

func TestEngine_OptionalListDoesNotBlock(t *testing.T) {
	inst := mustInstance(t, `
workflow "Test"
section "S"
  step "Checklist"
    list "Nice to have" optional
`)
	if err := inst.AdvanceStep("Checklist"); err != nil {
		t.Errorf("optional list should not block: %v", err)
	}
}

func TestEngine_WorkflowComplete(t *testing.T) {
	inst := mustInstance(t, `
workflow "Test"
section "S"
  step "Only Step"
`)
	if inst.Status == engine.StatusDone {
		t.Error("should not be done before any steps")
	}
	inst.AdvanceStep("Only Step")
	if inst.Status != engine.StatusDone {
		t.Errorf("workflow should be done, got %q", inst.Status)
	}
}

func TestEngine_CannotAdvanceNonReady(t *testing.T) {
	inst := mustInstance(t, `
workflow "Test"
section "S"
  step "A"
  step "B"
    needs "A"
`)
	err := inst.AdvanceStep("B") // B is pending, not ready
	if err == nil {
		t.Error("expected error advancing pending step")
	}
}

func TestEngine_Progress(t *testing.T) {
	inst := mustInstance(t, `
workflow "Test"
section "S"
  step "A"
  step "B"
    needs "A"
  step "C"
    needs "B"
`)
	done, total := inst.Progress()
	if total != 3 || done != 0 {
		t.Errorf("initial: done=%d total=%d", done, total)
	}
	inst.AdvanceStep("A")
	done, total = inst.Progress()
	if done != 1 {
		t.Errorf("after A: done=%d", done)
	}
}

func TestEngine_AddComment_BlockedWhenDone(t *testing.T) {
	inst := mustInstance(t, `
workflow "Test"
section "S"
  step "Only"
`)
	inst.AdvanceStep("Only")
	if inst.Status != engine.StatusDone {
		t.Skip("workflow not done")
	}
	err := inst.AddComment("late comment")
	if err == nil {
		t.Error("expected error adding comment to completed workflow")
	}
}

func TestEngine_ListItem_BlockedWhenDone(t *testing.T) {
	inst := mustInstance(t, `
workflow "Test"
section "S"
  step "A"
  step "B"
    needs "A"
    list "Item" optional
`)
	inst.AdvanceStep("A")
	inst.AdvanceStep("B")
	assertStatus(t, inst, "B", engine.StatusDone)
	err := inst.AddListItem("B", "new item")
	if err == nil {
		t.Error("expected error adding list item to done step")
	}
}

// ═══════════════════════════════════════════════════════
// Store Tests
// ═══════════════════════════════════════════════════════

func TestStore_CreateAndReload(t *testing.T) {
	dir := t.TempDir()
	wfDir := filepath.Join(dir, "workflows")
	os.MkdirAll(wfDir, 0755)

	// Write a test workflow
	os.WriteFile(filepath.Join(wfDir, "test.workflow"), []byte(`
workflow "Store Test"
priority medium
label test
section "S"
  step "Alpha"
  step "Beta"
    needs "Alpha"
`), 0644)

	s, err := store.New(wfDir, dir)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}

	inst, err := s.CreateInstance("Store Test", "My Instance", "high")
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	id := inst.ID

	// Reload store from disk
	s2, err := store.New(wfDir, dir)
	if err != nil {
		t.Fatalf("store.New reload: %v", err)
	}
	loaded, ok := s2.Instance(id)
	if !ok {
		t.Fatal("instance not found after reload")
	}
	if loaded.Title != "My Instance" {
		t.Errorf("title: got %q", loaded.Title)
	}
	if loaded.Priority != "high" {
		t.Errorf("priority: got %q", loaded.Priority)
	}
}

func TestStore_DuplicateWorkflowName(t *testing.T) {
	dir := t.TempDir()
	wfDir := filepath.Join(dir, "workflows")
	os.MkdirAll(wfDir, 0755)

	same := []byte("workflow \"Dupe\"\nsection \"S\"\n  step \"X\"\n")
	os.WriteFile(filepath.Join(wfDir, "a.workflow"), same, 0644)
	os.WriteFile(filepath.Join(wfDir, "b.workflow"), same, 0644)

	s, err := store.New(wfDir, dir)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	defs := s.Definitions()
	names := make(map[string]bool)
	for _, d := range defs {
		if names[d.Name] {
			t.Errorf("duplicate name in definitions: %q", d.Name)
		}
		names[d.Name] = true
	}
	if len(defs) != 2 {
		t.Errorf("expected 2 definitions (original + renamed), got %d", len(defs))
	}
}

func TestStore_AdvanceStepPersisted(t *testing.T) {
	dir := t.TempDir()
	wfDir := filepath.Join(dir, "workflows")
	os.MkdirAll(wfDir, 0755)
	os.WriteFile(filepath.Join(wfDir, "t.workflow"), []byte(`
workflow "Persist Test"
section "S"
  step "First"
  step "Second"
    needs "First"
`), 0644)

	s, _ := store.New(wfDir, dir)
	inst, _ := s.CreateInstance("Persist Test", "T", "")
	id := inst.ID

	if err := s.AdvanceStep(id, "First"); err != nil {
		t.Fatalf("AdvanceStep: %v", err)
	}

	// Reload and check
	s2, _ := store.New(wfDir, dir)
	loaded, _ := s2.Instance(id)
	for _, sec := range loaded.Sections {
		for _, step := range sec.Steps {
			if step.Name == "First" && step.Status != engine.StatusDone {
				t.Errorf("First should be done after reload, got %q", step.Status)
			}
			if step.Name == "Second" && step.Status != engine.StatusReady {
				t.Errorf("Second should be ready after reload, got %q", step.Status)
			}
		}
	}
}

// ═══════════════════════════════════════════════════════
// Roundtrip Tests
// ═══════════════════════════════════════════════════════

func TestRoundtrip_AllWorkflowFiles(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join("..", "..", "workflows"))
	if err != nil {
		t.Skip("workflows dir not found")
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".workflow") {
			continue
		}
		t.Run(e.Name(), func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("..", "..", "workflows", e.Name()))
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			wf, err := dsl.Parse(string(data))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			inst := engine.NewInstance("rt", "Roundtrip", wf)
			// All steps must exist in instance
			for _, sec := range wf.Sections {
				for _, step := range sec.Steps {
					if s := stepByName(inst, step.Name); s == nil {
						t.Errorf("step %q missing from instance", step.Name)
					}
				}
			}
			// At least one step must be ready or gate (workflow can start)
			canStart := false
			for _, sec := range inst.Sections {
				for _, s := range sec.Steps {
					if s.Status == engine.StatusReady || s.Status == engine.StatusGate || s.Status == engine.StatusAsk {
						canStart = true
					}
				}
			}
			if !canStart {
				t.Error("no step is ready — workflow cannot start")
			}
		})
	}
}
