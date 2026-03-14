package tests

import (
	"flowapp/internal/auth"
	"flowapp/internal/dsl"
	"flowapp/internal/engine"
	"testing"
	"time"
)

func TestScheduleParsing(t *testing.T) {
	input := `workflow Test Schedule
section Main
step Immediate
  assign "user:max"
step Delayed
  assign "role:finance"
  schedule +3d
  needs "Immediate"
`
	wf, err := dsl.Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	steps := wf.Sections[0].Steps
	if steps[0].Assign != "user:max" {
		t.Errorf("expected assign user:max, got %q", steps[0].Assign)
	}
	if steps[1].Schedule != "+3d" {
		t.Errorf("expected schedule +3d, got %q", steps[1].Schedule)
	}
	if steps[1].Assign != "role:finance" {
		t.Errorf("expected assign role:finance, got %q", steps[1].Assign)
	}
}

func TestScheduleActivation(t *testing.T) {
	input := `workflow Test Schedule Activation
section Main
step Start
step Scheduled
  schedule +1d
  needs "Start"
`
	wf, err := dsl.Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	inst := engine.NewInstance("test-sched-1", "Test", wf, nil, nil)

	// "Start" should be ready, "Scheduled" pending (needs Start)
	start := inst.Sections[0].Steps[0]
	scheduled := inst.Sections[0].Steps[1]
	if start.Status != engine.StatusReady {
		t.Errorf("expected Start=ready, got %s", start.Status)
	}
	if scheduled.Status != engine.StatusPending {
		t.Errorf("expected Scheduled=pending, got %s", scheduled.Status)
	}
	if scheduled.ScheduleAt == nil {
		t.Fatal("expected ScheduleAt to be set")
	}
	if scheduled.ScheduleAt.Before(time.Now()) {
		t.Error("expected ScheduleAt to be in the future")
	}
}

func TestScheduleAbsoluteDate(t *testing.T) {
	input := `workflow Test Absolute Schedule
section Main
step Reminder
  schedule "2099-01-01"
`
	wf, err := dsl.Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	inst := engine.NewInstance("test-sched-2", "Test", wf, nil, nil)
	step := inst.Sections[0].Steps[0]
	// step has no needs and schedule is far future — should stay pending
	if step.Status != engine.StatusPending {
		t.Errorf("expected pending (future schedule), got %s", step.Status)
	}
	if step.ScheduleAt == nil {
		t.Fatal("expected ScheduleAt to be set")
	}
}

func TestTickScheduled(t *testing.T) {
	input := `workflow Test Tick
section Main
step Past
  schedule "2000-01-01"
`
	wf, err := dsl.Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	inst := engine.NewInstance("test-tick-1", "Test", wf, nil, nil)

	// Manually override ScheduleAt to past so TickScheduled activates it
	past := time.Now().Add(-time.Hour)
	inst.Sections[0].Steps[0].ScheduleAt = &past
	inst.Sections[0].Steps[0].Status = engine.StatusPending

	activated := inst.TickScheduled()
	if !activated {
		t.Error("expected TickScheduled to activate the step")
	}
	if inst.Sections[0].Steps[0].Status == engine.StatusPending {
		t.Error("expected step to no longer be pending after tick")
	}
}

func TestAssignFilter(t *testing.T) {
	input := `workflow Test Assign
section Main
step Task A
  assign "user:alice"
step Task B
  assign "role:finance"
  needs "Task A"
`
	wf, err := dsl.Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	inst := engine.NewInstance("test-assign-1", "Test", wf, nil, nil)
	stepA := inst.Sections[0].Steps[0]
	if stepA.Assign != "user:alice" {
		t.Errorf("expected assign user:alice, got %q", stepA.Assign)
	}
	stepB := inst.Sections[0].Steps[1]
	if stepB.Assign != "role:finance" {
		t.Errorf("expected assign role:finance, got %q", stepB.Assign)
	}
}

func TestAppRolesMatching(t *testing.T) {
	// User with app role "finance" should match step assigned to "role:finance"
	u := &auth.User{Name: "max", Email: "max@example.com", AppRoles: []string{"finance", "hr"}}

	// simulate the matching logic from matchAssignFilter
	assign := "role:finance"
	matched := false
	if len(assign) > 5 && assign[:5] == "role:" {
		roleName := assign[5:]
		for _, r := range u.AppRoles {
			if r == roleName {
				matched = true
			}
		}
	}
	if !matched {
		t.Error("expected user with AppRole 'finance' to match assign 'role:finance'")
	}

	// user without the role should not match
	u2 := &auth.User{Name: "bob", Email: "bob@example.com", AppRoles: []string{"manager"}}
	matched2 := false
	if len(assign) > 5 && assign[:5] == "role:" {
		roleName := assign[5:]
		for _, r := range u2.AppRoles {
			if r == roleName {
				matched2 = true
			}
		}
	}
	if matched2 {
		t.Error("expected user without 'finance' role to NOT match assign 'role:finance'")
	}
}
