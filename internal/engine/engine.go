package engine

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flowapp/internal/dsl"
	"flowapp/internal/logger"
	"flowapp/internal/notifications"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var engineLog = logger.New("engine")

// Status represents the lifecycle state of a workflow instance or individual step.
type Status string

const (
	StatusPending Status = "pending" // waiting for dependencies (needs) to be satisfied
	StatusReady   Status = "ready"   // all dependencies met; can be acted on
	StatusAsk     Status = "ask"     // waiting for a UI routing decision
	StatusGate    Status = "gate"    // waiting for an external approval via token link
	StatusDone    Status = "done"    // successfully completed
	StatusSkipped Status = "skipped" // bypassed by an ask routing decision
	StatusEnded   Status = "ended"   // terminal step that explicitly ends the workflow
)

// Comment is a free-text note attached to an instance or a step.
type Comment struct {
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
}

// AuditEntry records a single user action for the instance audit trail.
type AuditEntry struct {
	At      time.Time `json:"at"`
	Action  string    `json:"action"`  // "complete", "ask", "gate"
	Section string    `json:"section"` // section name (currently unused, reserved)
	Step    string    `json:"step"`    // step name
	Note    string    `json:"note,omitempty"`
}

// ListItem is a single checklist entry within a step.
type ListItem struct {
	ID       string `json:"id"`
	Text     string `json:"text"`
	Required bool   `json:"required"` // if true, must be checked before the step can be advanced
	Checked  bool   `json:"checked"`
	Dynamic  bool   `json:"dynamic,omitempty"` // true if added by the user at runtime
}

// AskState holds the question and routing targets for a branching step.
type AskState struct {
	Question string   `json:"question"`
	Targets  []string `json:"targets"` // step names in button order; chosen index maps to target
}

// Instance is the runtime state of a single workflow execution.
// It is serialised to JSON and persisted to disk by the Store.
type Instance struct {
	ID           string            `json:"id"`
	WorkflowName string            `json:"workflow_name"`
	Labels       []string          `json:"labels,omitempty"`
	Title        string            `json:"title"`
	Priority     string            `json:"priority"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
	Sections     []*SectionState   `json:"sections"`
	Status       Status            `json:"status"`
	Position     int               `json:"position"`
	Archived     bool              `json:"archived,omitempty"`
	CreatedBy    string            `json:"created_by,omitempty"` // user ID of the instance creator
	Vars         map[string]string `json:"vars,omitempty"`
	Comments     []Comment         `json:"comments,omitempty"`
	Audit        []AuditEntry      `json:"audit,omitempty"`

	// Runtime-only fields — not persisted to JSON.
	// Injected by the Store before any engine method is called on a loaded instance.
	MailSender    Mailer           `json:"-"`
	EmailResolver EmailResolver    `json:"-"`
	NotifySink    NotificationSink `json:"-"`
}

// SectionState groups a set of steps under a named section.
type SectionState struct {
	Name  string       `json:"name"`
	Steps []*StepState `json:"steps"`
}

// StepState is the runtime state of a single workflow step.
type StepState struct {
	Name            string     `json:"name"`
	Status          Status     `json:"status"`
	Note            string     `json:"note,omitempty"`
	Notify          []string   `json:"notify,omitempty"`      // roles/addresses to notify when step fires
	Assign          []string   `json:"assign,omitempty"`      // assign expressions — user must match any one
	Schedule        string     `json:"schedule,omitempty"`    // raw schedule expression
	ScheduleAt      *time.Time `json:"schedule_at,omitempty"` // resolved activation timestamp
	Due             string     `json:"due,omitempty"`
	DueAt           *time.Time `json:"due_at,omitempty"`
	StartedAt       *time.Time `json:"started_at,omitempty"`
	UpdatedAt       time.Time  `json:"updated_at"`
	Needs           []string   `json:"needs,omitempty"` // step names that must be done before this activates
	ListItems       []ListItem `json:"list_items,omitempty"`
	Ask             *AskState  `json:"ask,omitempty"`
	Comments        []Comment  `json:"step_comments,omitempty"`
	Gate            bool       `json:"gate,omitempty"`             // true: step waits for external token approval
	GateToken       string     `json:"gate_token,omitempty"`       // one-time approval token
	GateUsed        bool       `json:"gate_used,omitempty"`        // true once the token has been redeemed
	Ends            bool       `json:"ends,omitempty"`             // true: completing this step ends the workflow
	ChosenIdx       int        `json:"chosen_idx"`                 // ask/gate routing choice; -1 = not yet chosen
	OverdueNotified bool       `json:"overdue_notified,omitempty"` // true once an overdue notification has been sent
}

// UnmarshalJSON provides backwards-compatible deserialization for StepState.
// Old instances stored Notify/Assign as a single string; new format is []string.
func (s *StepState) UnmarshalJSON(data []byte) error {
	// Use an alias to avoid infinite recursion
	type Alias StepState
	aux := &struct {
		Notify json.RawMessage `json:"notify,omitempty"`
		Assign json.RawMessage `json:"assign,omitempty"`
		*Alias
	}{Alias: (*Alias)(s)}

	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}

	s.Notify = unmarshalStringOrSlice(aux.Notify)
	s.Assign = unmarshalStringOrSlice(aux.Assign)
	return nil
}

// unmarshalStringOrSlice handles both "foo" and ["foo","bar"] JSON values.
func unmarshalStringOrSlice(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	// try array first
	var slice []string
	if err := json.Unmarshal(raw, &slice); err == nil {
		return slice
	}
	// fall back to string (old format)
	var s string
	if err := json.Unmarshal(raw, &s); err == nil && s != "" {
		return []string{s}
	}
	return nil
}

// ── Schedule ──────────────────────────────────────────────────────────────────

// parseSchedule resolves a schedule expression to an absolute time relative to instanceStart.
// Supported formats:
//   - "2025-12-01"  — absolute date (YYYY-MM-DD)
//   - "+3d" / "+2w" / "+4h" — relative offset (days, weeks, hours)
func parseSchedule(s string, instanceStart time.Time) (time.Time, error) {
	s = strings.TrimSpace(strings.Trim(s, `"`))
	if strings.HasPrefix(s, "+") {
		raw := s[1:]
		if len(raw) < 2 {
			return time.Time{}, fmt.Errorf("invalid schedule: %s", s)
		}
		n, err := strconv.Atoi(raw[:len(raw)-1])
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid schedule number: %s", s)
		}
		switch raw[len(raw)-1] {
		case 'h':
			return instanceStart.Add(time.Duration(n) * time.Hour), nil
		case 'd':
			return instanceStart.Add(time.Duration(n) * 24 * time.Hour), nil
		case 'w':
			return instanceStart.Add(time.Duration(n) * 7 * 24 * time.Hour), nil
		}
		return time.Time{}, fmt.Errorf("unknown schedule unit in '%s'", s)
	}
	// absolute date
	t, err := time.ParseInLocation("2006-01-02", s, instanceStart.Location())
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid schedule date '%s': %w", s, err)
	}
	return t, nil
}

// setScheduleAt resolves and stores the ScheduleAt timestamp for a step (once only).
func setScheduleAt(step *StepState, instanceStart time.Time) {
	if step.Schedule == "" || step.ScheduleAt != nil {
		return
	}
	t, err := parseSchedule(step.Schedule, instanceStart)
	if err != nil {
		engineLog.Warn("schedule parse error for step %q: %v", step.Name, err)
		return
	}
	step.ScheduleAt = &t
}

// ── Due ───────────────────────────────────────────────────────────────────────

// parseDue converts a due string (e.g. "2h", "3d", "1w") to a time.Duration.
func parseDue(s string) (time.Duration, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if len(s) < 2 {
		return 0, fmt.Errorf("invalid due: %s", s)
	}
	n, err := strconv.Atoi(s[:len(s)-1])
	if err != nil {
		return 0, fmt.Errorf("invalid due number: %s", s)
	}
	switch s[len(s)-1] {
	case 'h':
		return time.Duration(n) * time.Hour, nil
	case 'd':
		return time.Duration(n) * 24 * time.Hour, nil
	case 'w':
		return time.Duration(n) * 7 * 24 * time.Hour, nil
	}
	return 0, fmt.Errorf("unknown due unit in '%s'", s)
}

// setDueAt resolves and stores the DueAt timestamp for a step (once only).
func setDueAt(step *StepState, now time.Time) {
	if step.Due == "" || step.DueAt != nil {
		return
	}
	d, err := parseDue(step.Due)
	if err != nil {
		return
	}
	t := now.Add(d)
	step.DueAt = &t
}

// DueLabel returns a human-readable due status string, e.g. "due in 2d" or "⚠ overdue 3h ago".
func (s *StepState) DueLabel() string {
	if s.DueAt == nil {
		return ""
	}
	diff := s.DueAt.Sub(time.Now())
	if diff < 0 {
		return "⚠ overdue " + formatDur(-diff) + " ago"
	}
	return "due in " + formatDur(diff)
}

// IsOverdue returns true if the step has a due date that has passed and is not yet terminal.
func (s *StepState) IsOverdue() bool {
	return s.DueAt != nil && time.Now().After(*s.DueAt) &&
		s.Status != StatusDone && s.Status != StatusSkipped && s.Status != StatusEnded
}

// formatDur formats a duration as a short human-readable string (e.g. "45m", "3h", "5d", "2w").
func formatDur(d time.Duration) string {
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	days := int(d.Hours() / 24)
	if days < 14 {
		return fmt.Sprintf("%dd", days)
	}
	return fmt.Sprintf("%dw", days/7)
}

// RequiredListBlocked returns true if any required checklist items remain unchecked.
func (s *StepState) RequiredListBlocked() bool {
	for _, li := range s.ListItems {
		if li.Required && !li.Checked {
			return true
		}
	}
	return false
}

// ── Token ─────────────────────────────────────────────────────────────────────

// generateToken creates a cryptographically random 48-character hex token for gate steps.
func generateToken() string {
	b := make([]byte, 24)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// ── Instance construction ─────────────────────────────────────────────────────

// NewInstance creates a new workflow instance from a parsed workflow definition.
// m and r are optional: pass nil for both in tests or when no mailer is configured.
// Initial activation (steps with no unmet needs) runs immediately, so notifications
// fire correctly for steps that become ready at creation time.
func NewInstance(id, title string, wf *dsl.Workflow, m Mailer, r EmailResolver, sink NotificationSink) *Instance {
	now := time.Now()
	inst := &Instance{
		ID: id, WorkflowName: wf.Name, Title: title,
		Labels:    wf.Labels,
		CreatedAt: now, UpdatedAt: now, Status: StatusReady,
		Vars:          make(map[string]string),
		MailSender:    m,
		EmailResolver: r,
		NotifySink:    sink,
	}
	if inst.Priority == "" {
		inst.Priority = "medium"
	}

	// collect ask target names so we can keep them pending until explicitly routed
	askTargets := map[string]bool{}
	for _, sec := range wf.Sections {
		for _, step := range sec.Steps {
			if step.Ask != nil {
				for _, t := range step.Ask.Targets {
					askTargets[t] = true
				}
			}
		}
	}

	for _, sec := range wf.Sections {
		ss := &SectionState{Name: sec.Name}
		for _, step := range sec.Steps {
			var askSt *AskState
			if step.Ask != nil {
				askSt = &AskState{Question: step.Ask.Question, Targets: step.Ask.Targets}
			}
			// ask targets without explicit needs get a sentinel so they stay pending
			needs := step.Needs
			if askTargets[step.Name] && len(step.Needs) == 0 {
				needs = []string{"__ask_target__"}
			}
			st := &StepState{
				Name: step.Name, Note: step.Note, Notify: step.Notify, Assign: step.Assign,
				Schedule: step.Schedule, Due: step.Due, Needs: needs,
				Ask: askSt, Gate: step.Gate, Ends: step.Ends,
				Status: StatusPending, ChosenIdx: -1,
			}
			setScheduleAt(st, now)
			for i, li := range step.ListItems {
				st.ListItems = append(st.ListItems, ListItem{
					ID: fmt.Sprintf("%d", i), Text: li.Text, Required: li.Required,
				})
			}
			ss.Steps = append(ss.Steps, st)
		}
		inst.Sections = append(inst.Sections, ss)
	}

	// activate all steps whose needs are already satisfied
	inst.activateReady(now)
	return inst
}

// ── Variable substitution ─────────────────────────────────────────────────────

// ApplyVars substitutes $VAR_NAME or ${VAR_NAME} placeholders in the instance title
// and in step notes and notify fields. Called after instance creation when the user
// has provided variable values.
func (inst *Instance) ApplyVars(vars map[string]string) {
	inst.Vars = vars
	inst.Title = substituteVars(inst.Title, vars)
	inst.allSteps(func(s *StepState) {
		s.Note = substituteVars(s.Note, vars)
		for i, n := range s.Notify {
			s.Notify[i] = substituteVars(n, vars)
		}
	})
}

// substituteVars replaces all $KEY and ${KEY} occurrences in s with the corresponding
// values from vars.
func substituteVars(s string, vars map[string]string) string {
	for k, v := range vars {
		s = strings.ReplaceAll(s, "$"+k, v)
		s = strings.ReplaceAll(s, "${"+k+"}", v)
	}
	return s
}

// ── Activation ────────────────────────────────────────────────────────────────

// activateReady scans all pending steps and activates those whose needs are satisfied
// and whose scheduled time (if any) has been reached. Repeats until no further changes
// occur to handle cascading dependencies.
func (inst *Instance) activateReady(now time.Time) {
	changed := true
	for changed {
		changed = false
		inst.allSteps(func(s *StepState) {
			if s.Status != StatusPending {
				return
			}
			if !inst.needsSatisfied(s) {
				return
			}
			if s.ScheduleAt != nil && now.Before(*s.ScheduleAt) {
				return // not yet time
			}
			engineLog.Debug("activating step %q (was pending)", s.Name)
			inst.activate(s, now)
			changed = true
		})
	}
}

// TickScheduled activates any pending steps whose scheduled time has arrived.
// Intended to be called periodically (e.g. every minute) by the Store's background scheduler.
// Returns true if at least one step was activated (caller should persist the instance).
func (inst *Instance) TickScheduled() bool {
	now := time.Now()
	var activated bool
	inst.allSteps(func(s *StepState) {
		if s.Status != StatusPending {
			return
		}
		if s.ScheduleAt == nil || now.Before(*s.ScheduleAt) {
			return
		}
		if !inst.needsSatisfied(s) {
			return
		}
		engineLog.Debug("scheduled activation of step %q", s.Name)
		inst.activate(s, now)
		activated = true
	})
	if activated {
		inst.activateReady(now) // cascade to any newly unblocked steps
		inst.UpdatedAt = now
	}
	return activated
}

// TickOverdue scans all active steps and fires overdue notifications for any that
// have passed their due date and haven't been notified yet. The OverdueNotified
// flag is set on first notification to prevent repeated emails on every tick.
// Returns true if at least one notification was fired.
func (inst *Instance) TickOverdue() bool {
	var fired bool
	inst.allSteps(func(s *StepState) {
		if !s.IsOverdue() {
			return
		}
		if s.OverdueNotified {
			return
		}
		s.OverdueNotified = true
		fired = true
		m, r := inst.MailSender, inst.EmailResolver
		scopy := *s
		// collect all notification targets: assign + notify
		targets := append(append([]string{}, scopy.Assign...), scopy.Notify...)
		if len(targets) == 0 {
			engineLog.Warn("overdue: step %q in %q — no notification target", s.Name, inst.Title)
			return
		}
		engineLog.Info("overdue: firing notification for step %q in %q", s.Name, inst.Title)
		sink := inst.NotifySink
		go func() {
			var allTo []string
			for _, target := range targets {
				if r != nil {
					allTo = append(allTo, r(target)...)
				}
			}
			// deduplicate
			seen := map[string]bool{}
			var to []string
			for _, addr := range allTo {
				if !seen[addr] {
					seen[addr] = true
					to = append(to, addr)
				}
			}
			if len(to) > 0 && m != nil {
				subject := fmt.Sprintf("[flowapp] ⚠ Overdue: %s — %s", scopy.Name, inst.Title)
				plain := fmt.Sprintf("Step %q in workflow %q is overdue.\n\nInstance: %s\nWorkflow: %s\nStep: %s\n",
					scopy.Name, inst.WorkflowName, inst.Title, inst.WorkflowName, scopy.Name)
				if err := m.Send(MailMessage{To: to, Subject: subject, PlainBody: plain}); err != nil {
					engineLog.Error("overdue mail error: %v", err)
				}
			}
			if sink != nil {
				sink.Notify(InAppNotification{
					Kind: string(notifications.KindOverdue), InstanceID: inst.ID, InstanceName: inst.Title,
					WorkflowName: inst.WorkflowName, StepName: scopy.Name,
					Message: fmt.Sprintf("Step \"%s\" in \"%s\" is overdue.", scopy.Name, inst.Title),
					Targets: targets,
				})
			}
		}()
	})
	return fired
}

// needsSatisfied returns true if all steps listed in s.Needs are in a terminal state.
func (inst *Instance) needsSatisfied(s *StepState) bool {
	if len(s.Needs) == 0 {
		return true
	}
	for _, name := range s.Needs {
		dep := inst.findStepByName(name)
		if dep == nil || (dep.Status != StatusDone && dep.Status != StatusSkipped && dep.Status != StatusEnded) {
			return false
		}
	}
	return true
}

// activate transitions a pending step to its active state (ready, ask, or gate),
// sets timestamps and due date, and fires any notifications asynchronously.
func (inst *Instance) activate(s *StepState, now time.Time) {
	if s.Gate {
		s.Status = StatusGate
		s.GateToken = generateToken()
	} else if s.Ask != nil {
		s.Status = StatusAsk
	} else {
		s.Status = StatusReady
	}
	s.StartedAt = &now
	s.UpdatedAt = now
	setDueAt(s, now)
	// gate steps send the approval link notification on activation, not on completion
	if len(s.Notify) > 0 && s.Gate {
		m, r := inst.MailSender, inst.EmailResolver
		scopy := *s
		go fireNotify(inst, &scopy, m, r, inst.NotifySink)
	}
	if len(s.Assign) > 0 {
		m, r := inst.MailSender, inst.EmailResolver
		scopy := *s
		go fireAssignNotify(inst, &scopy, m, r, inst.NotifySink)
	}
}

// ── Step operations ───────────────────────────────────────────────────────────

// StepByName returns the step with the given name, or nil if not found.
// Exported for use by HTTP handlers that need to check step state before acting.
func (inst *Instance) StepByName(name string) *StepState {
	for _, sec := range inst.Sections {
		for _, s := range sec.Steps {
			if s.Name == name {
				return s
			}
		}
	}
	return nil
}

// AdvanceStep completes a ready step, triggers cascading activation,
// and fires any on-completion notifications.
func (inst *Instance) AdvanceStep(stepName string) error {
	now := time.Now()
	s := inst.findStepByName(stepName)
	if s == nil {
		return fmt.Errorf("step '%s' not found", stepName)
	}
	engineLog.Debug("AdvanceStep %q status=%s", stepName, s.Status)
	if s.Status != StatusReady {
		return fmt.Errorf("step '%s' is not ready (status: %s)", stepName, s.Status)
	}
	if s.RequiredListBlocked() {
		return fmt.Errorf("step '%s' has unchecked required items", stepName)
	}
	inst.audit("complete", stepName, "")
	return inst.completeStep(s, now)
}

// AnswerAsk resolves a branching ask step by choosing one of its routing targets.
// The chosen target has its sentinel need removed (so it can activate); all other
// targets are skipped.
func (inst *Instance) AnswerAsk(stepName string, chosenIdx int) error {
	now := time.Now()
	s := inst.findStepByName(stepName)
	if s == nil {
		return fmt.Errorf("step '%s' not found", stepName)
	}
	if s.Status != StatusAsk {
		return fmt.Errorf("step '%s' is not awaiting answer", stepName)
	}
	if s.Ask == nil || chosenIdx < 0 || chosenIdx >= len(s.Ask.Targets) {
		return fmt.Errorf("invalid choice index %d", chosenIdx)
	}
	s.ChosenIdx = chosenIdx
	chosen := s.Ask.Targets[chosenIdx]
	inst.audit("ask", stepName, fmt.Sprintf("→ %s", chosen))

	for i, target := range s.Ask.Targets {
		t := inst.findStepByName(target)
		if t == nil {
			continue
		}
		if i == chosenIdx {
			// remove the sentinel so activateReady can pick it up
			t.Needs = filterNeeds(t.Needs)
		} else if t.Status == StatusPending {
			t.Status = StatusSkipped
			t.UpdatedAt = now
		}
	}

	return inst.completeStep(s, now)
}

// RedeemGate validates an approval token and completes the corresponding gate step.
// For gate steps with an ask definition, the chosen routing index is applied.
// For simple gate steps (no ask), the step is completed without routing.
func (inst *Instance) RedeemGate(token string, chosenIdx int) (*StepState, error) {
	now := time.Now()
	var found *StepState
	inst.allSteps(func(s *StepState) {
		if s.GateToken == token && s.Status == StatusGate {
			found = s
		}
	})
	if found == nil {
		return nil, fmt.Errorf("token not found or already used")
	}
	if found.DueAt != nil && time.Now().After(*found.DueAt) {
		return nil, fmt.Errorf("approval link has expired")
	}

	// simple gate (no routing): just complete
	if found.Ask == nil {
		inst.audit("gate", found.Name, "approved (token)")
		found.GateUsed = true
		err := inst.completeStep(found, now)
		return found, err
	}

	// gate with ask routing
	if chosenIdx < 0 || chosenIdx >= len(found.Ask.Targets) {
		return nil, fmt.Errorf("invalid choice index %d", chosenIdx)
	}
	found.ChosenIdx = chosenIdx
	chosen := found.Ask.Targets[chosenIdx]
	inst.audit("gate", found.Name, fmt.Sprintf("→ %s (token)", chosen))

	for i, target := range found.Ask.Targets {
		t := inst.findStepByName(target)
		if t == nil {
			continue
		}
		if i == chosenIdx {
			t.Needs = filterNeeds(t.Needs)
		} else if t.Status == StatusPending {
			t.Status = StatusSkipped
			t.UpdatedAt = now
		}
	}

	found.GateUsed = true
	err := inst.completeStep(found, now)
	return found, err
}

// completeStep marks a step done, fires on-completion notifications asynchronously,
// and cascades activation to any newly unblocked steps.
func (inst *Instance) completeStep(s *StepState, now time.Time) error {
	s.Status = StatusDone
	s.UpdatedAt = now
	// gate steps already sent their notification on activation
	if len(s.Notify) > 0 && !s.Gate {
		m, r := inst.MailSender, inst.EmailResolver
		scopy := *s
		go fireNotify(inst, &scopy, m, r, inst.NotifySink)
	}
	if s.Ends {
		s.Status = StatusEnded
	}
	inst.UpdatedAt = now
	inst.activateReady(now)
	inst.recalc()
	return nil
}

// recalc updates the overall instance status.
// If all steps are terminal the instance is marked done and auto-archived.
func (inst *Instance) recalc() {
	allTerminal := true
	inst.allSteps(func(s *StepState) {
		if s.Status != StatusDone && s.Status != StatusSkipped && s.Status != StatusEnded {
			allTerminal = false
		}
	})
	if allTerminal {
		inst.Status = StatusDone
		inst.Archived = true
	} else {
		inst.Status = StatusReady
	}
}

// ── Progress & overdue ────────────────────────────────────────────────────────

// Progress returns the number of completed steps and the total step count.
func (inst *Instance) Progress() (int, int) {
	done, total := 0, 0
	inst.allSteps(func(s *StepState) {
		total++
		if s.Status == StatusDone || s.Status == StatusSkipped || s.Status == StatusEnded {
			done++
		}
	})
	return done, total
}

// HasOverdue returns true if any active step has passed its due date.
func (inst *Instance) HasOverdue() bool {
	found := false
	inst.allSteps(func(s *StepState) {
		if s.IsOverdue() {
			found = true
		}
	})
	return found
}

// ── List items ────────────────────────────────────────────────────────────────

// ToggleListItem flips the checked state of a single checklist item within a step.
func (inst *Instance) ToggleListItem(stepName, itemID string) error {
	s := inst.findStepByName(stepName)
	if s == nil {
		return fmt.Errorf("step '%s' not found", stepName)
	}
	if s.Status != StatusReady && s.Status != StatusAsk && s.Status != StatusGate {
		return fmt.Errorf("step '%s' is not active", stepName)
	}
	for i, li := range s.ListItems {
		if li.ID == itemID {
			s.ListItems[i].Checked = !s.ListItems[i].Checked
			s.UpdatedAt = time.Now()
			inst.UpdatedAt = time.Now()
			return nil
		}
	}
	return fmt.Errorf("list item '%s' not found", itemID)
}

// CheckAllListItems marks every checklist item in the step as checked.
func (inst *Instance) CheckAllListItems(stepName string) {
	s := inst.findStepByName(stepName)
	if s == nil {
		return
	}
	if s.Status != StatusReady && s.Status != StatusAsk && s.Status != StatusGate {
		return
	}
	for i := range s.ListItems {
		s.ListItems[i].Checked = true
	}
	s.UpdatedAt = time.Now()
	inst.UpdatedAt = time.Now()
}

// AddListItem appends a new dynamic (user-created) checklist item to an active step.
func (inst *Instance) AddListItem(stepName, text string) error {
	s := inst.findStepByName(stepName)
	if s == nil {
		return fmt.Errorf("step '%s' not found", stepName)
	}
	if s.Status != StatusReady && s.Status != StatusAsk && s.Status != StatusGate {
		return fmt.Errorf("step '%s' is not active (status: %s)", stepName, s.Status)
	}
	id := fmt.Sprintf("d%d", time.Now().UnixNano())
	s.ListItems = append(s.ListItems, ListItem{ID: id, Text: text, Required: false, Dynamic: true})
	s.UpdatedAt = time.Now()
	inst.UpdatedAt = time.Now()
	return nil
}

// ── Comments ──────────────────────────────────────────────────────────────────

// AddStepComment appends a comment to a specific step. Text is truncated at 500 characters.
func (inst *Instance) AddStepComment(stepName, text string) error {
	s := inst.findStepByName(stepName)
	if s == nil {
		return fmt.Errorf("step '%s' not found", stepName)
	}
	if len(text) > 500 {
		text = text[:500]
	}
	s.Comments = append(s.Comments, Comment{Text: text, CreatedAt: time.Now()})
	inst.UpdatedAt = time.Now()
	return nil
}

// AddComment appends a comment to the instance as a whole. Text is truncated at 255 characters.
// Returns an error if the instance is already completed.
func (inst *Instance) AddComment(text string) error {
	if inst.Status == StatusDone {
		return fmt.Errorf("cannot add comments to a completed workflow")
	}
	if len(text) > 255 {
		text = text[:255]
	}
	inst.Comments = append(inst.Comments, Comment{Text: text, CreatedAt: time.Now()})
	inst.UpdatedAt = time.Now()
	return nil
}

// ── Audit ─────────────────────────────────────────────────────────────────────

// audit appends an entry to the instance audit trail.
func (inst *Instance) audit(action, step, note string) {
	inst.Audit = append(inst.Audit, AuditEntry{
		At: time.Now(), Action: action, Step: step, Note: note,
	})
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// AllStepsDue calls fn with the DueAt time of every step that has one.
// Used by the board to check due-date filters.
func (inst *Instance) AllStepsDue(fn func(time.Time)) {
	inst.allSteps(func(s *StepState) {
		if s.DueAt != nil {
			fn(*s.DueAt)
		}
	})
}

// allSteps iterates over every step in every section and calls fn.
func (inst *Instance) allSteps(fn func(*StepState)) {
	for _, sec := range inst.Sections {
		for _, s := range sec.Steps {
			fn(s)
		}
	}
}

// findStepByName returns the first step with the given name, or nil.
func (inst *Instance) findStepByName(name string) *StepState {
	var found *StepState
	inst.allSteps(func(s *StepState) {
		if s.Name == name {
			found = s
		}
	})
	return found
}

// FindStepByToken returns the step that holds the given gate token, or nil.
// Used by the Store to locate the instance for an incoming approval link.
func (inst *Instance) FindStepByToken(token string) *StepState {
	var found *StepState
	inst.allSteps(func(s *StepState) {
		if s.GateToken == token {
			found = s
		}
	})
	return found
}

// filterNeeds removes the internal "__ask_target__" sentinel from a needs slice,
// leaving only real step-name dependencies.
func filterNeeds(needs []string) []string {
	var out []string
	for _, n := range needs {
		if n != "__ask_target__" {
			out = append(out, n)
		}
	}
	return out
}

// ── Mailer types ──────────────────────────────────────────────────────────────

// UserResolver maps an assign/notify expression to a list of internal user IDs.
// It is used for in-app notification fan-out and is independent of any mail or
// messaging configuration. Implement this to support any future notification channel.
type UserResolver func(expr string) []string

// EmailResolver maps an assign/notify expression to a list of resolved email addresses.
// Implemented by auth.UserStore.ResolveEmails.
type EmailResolver func(expr string) []string

// Mailer dispatches a single outbound email message.
// Defined here (rather than in the mailer package) to avoid an import cycle.
type Mailer interface {
	Send(msg MailMessage) error
}

// MailMessage is a minimal email representation used within the engine.
// It mirrors mailer.Message but lives here to keep the engine package dependency-free.
type MailMessage struct {
	From      string
	To        []string
	Subject   string
	PlainBody string
	HTMLBody  string
}

// InAppNotification carries the data for a single in-app notification event.
// The Store converts this into per-user notification records.
type InAppNotification struct {
	Kind         string // "assign", "notify", "overdue", "admin"
	InstanceID   string
	InstanceName string
	WorkflowName string
	StepName     string
	Message      string
	GateURL      string
	// Targets lists the resolve expressions that should receive this notification
	// (e.g. assign expression, notify address, "role:admin").
	Targets []string
}

// NotificationSink receives in-app notification events from the engine.
// Implemented by the store layer to fan out to per-user files.
type NotificationSink interface {
	Notify(n InAppNotification)
}

// ── Notification helpers ──────────────────────────────────────────────────────

// resolveAll resolves a list of target expressions to a deduplicated list of email addresses.
func resolveAll(targets []string, resolve EmailResolver) []string {
	if resolve == nil {
		return nil
	}
	seen := map[string]bool{}
	var result []string
	for _, target := range targets {
		for _, email := range resolve(target) {
			if !seen[email] {
				seen[email] = true
				result = append(result, email)
			}
		}
	}
	return result
}

// fireAssignNotify logs and emails assignment notifications for all assign targets.
func fireAssignNotify(inst *Instance, step *StepState, m Mailer, resolve EmailResolver, sink NotificationSink) {
	engineLog.Info("[%s] ASSIGN → %v | instance: %s (%s) | step: %s",
		time.Now().Format(time.RFC3339), step.Assign,
		inst.Title, inst.WorkflowName, step.Name)

	if sink != nil {
		sink.Notify(InAppNotification{
			Kind: string(notifications.KindAssign), InstanceID: inst.ID, InstanceName: inst.Title,
			WorkflowName: inst.WorkflowName, StepName: step.Name,
			Message: fmt.Sprintf("You have been assigned to step \"%s\" in \"%s\".", step.Name, inst.Title),
			Targets: step.Assign,
		})
	}

	if m == nil || resolve == nil {
		return
	}
	to := resolveAll(step.Assign, resolve)
	if len(to) == 0 {
		engineLog.Warn("assign: no emails resolved for %v", step.Assign)
		return
	}
	subject := fmt.Sprintf("[flowapp] Assigned to you: %s — %s", step.Name, inst.Title)
	plain := fmt.Sprintf("You have been assigned to step %q in workflow %q.\n\nInstance: %s\nWorkflow: %s\nStep: %s\n",
		step.Name, inst.WorkflowName, inst.Title, inst.WorkflowName, step.Name)
	if step.Due != "" {
		plain += fmt.Sprintf("Due: %s\n", step.Due)
	}
	if err := m.Send(MailMessage{To: to, Subject: subject, PlainBody: plain}); err != nil {
		engineLog.Error("assign mail error: %v", err)
	}
}

// fireNotify logs and emails step-ready notifications for all notify targets.
// For gate steps the approval link is included in the message body.
func fireNotify(inst *Instance, step *StepState, m Mailer, resolve EmailResolver, sink NotificationSink) {
	gateURL := ""
	if step.Gate && step.GateToken != "" {
		gateURL = fmt.Sprintf("/approve/%s", step.GateToken)
	}
	logLine := fmt.Sprintf("[%s] NOTIFY → %v | instance: %s (%s) | step: %s",
		time.Now().Format(time.RFC3339), step.Notify,
		inst.Title, inst.WorkflowName, step.Name)
	if gateURL != "" {
		logLine += " | approval link: " + gateURL
	}
	engineLog.Info("%s", logLine)

	msg := fmt.Sprintf("Step \"%s\" is ready in \"%s\".", step.Name, inst.Title)
	if gateURL != "" {
		msg = fmt.Sprintf("Approval required for step \"%s\" in \"%s\".", step.Name, inst.Title)
	}
	if sink != nil {
		sink.Notify(InAppNotification{
			Kind: string(notifications.KindNotify), InstanceID: inst.ID, InstanceName: inst.Title,
			WorkflowName: inst.WorkflowName, StepName: step.Name,
			Message: msg, GateURL: gateURL,
			Targets: step.Notify,
		})
	}

	if m == nil || resolve == nil {
		return
	}
	to := resolveAll(step.Notify, resolve)
	if len(to) == 0 {
		engineLog.Warn("notify: no emails resolved for %v", step.Notify)
		return
	}
	subject := fmt.Sprintf("[flowapp] %s — step %q ready", inst.Title, step.Name)
	plain := fmt.Sprintf("Step %q is ready in workflow %q.\n\nInstance: %s\nWorkflow: %s\n",
		step.Name, inst.WorkflowName, inst.Title, inst.WorkflowName)
	if gateURL != "" {
		plain += fmt.Sprintf("\nApproval link: %s\n", gateURL)
	}
	if step.Due != "" {
		plain += fmt.Sprintf("Due: %s\n", step.Due)
	}
	if err := m.Send(MailMessage{To: to, Subject: subject, PlainBody: plain}); err != nil {
		engineLog.Error("notify mail error: %v", err)
	}
}
