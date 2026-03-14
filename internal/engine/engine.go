package engine

import (
	"crypto/rand"
	"encoding/hex"
	"flowapp/internal/dsl"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

type Status string

const (
	StatusPending  Status = "pending"  // needs not yet satisfied
	StatusReady    Status = "ready"    // can be acted on
	StatusAsk      Status = "ask"      // waiting for UI answer
	StatusGate     Status = "gate"     // waiting for external token
	StatusDone     Status = "done"
	StatusSkipped  Status = "skipped"  // bypassed by ask routing
	StatusEnded    Status = "ended"    // ends keyword
)

type Comment struct {
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
}

type AuditEntry struct {
	At      time.Time `json:"at"`
	Action  string    `json:"action"`
	Section string    `json:"section"`
	Step    string    `json:"step"`
	Note    string    `json:"note,omitempty"`
}

type ListItem struct {
	ID       string `json:"id"`
	Text     string `json:"text"`
	Required bool   `json:"required"`
	Checked  bool   `json:"checked"`
	Dynamic  bool   `json:"dynamic,omitempty"`
}

type AskState struct {
	Question string   `json:"question"`
	Targets  []string `json:"targets"` // ordered target step names
}

type Instance struct {
	ID           string          `json:"id"`
	WorkflowName string          `json:"workflow_name"`
	Labels       []string        `json:"labels,omitempty"`
	Title        string          `json:"title"`
	Priority     string          `json:"priority"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
	Sections     []*SectionState `json:"sections"`
	Status       Status          `json:"status"`
	Position     int             `json:"position"`
	Archived     bool            `json:"archived,omitempty"`
	Vars         map[string]string `json:"vars,omitempty"`
	Comments     []Comment       `json:"comments,omitempty"`
	Audit        []AuditEntry    `json:"audit,omitempty"`
}

type SectionState struct {
	Name  string       `json:"name"`
	Steps []*StepState `json:"steps"`
}

type StepState struct {
	Name      string     `json:"name"`
	Status    Status     `json:"status"`
	Note      string     `json:"note,omitempty"`
	Notify    string     `json:"notify,omitempty"`
	Assign    string     `json:"assign,omitempty"`
	Due       string     `json:"due,omitempty"`
	DueAt     *time.Time `json:"due_at,omitempty"`
	StartedAt *time.Time `json:"started_at,omitempty"`
	UpdatedAt time.Time  `json:"updated_at"`
	Needs     []string   `json:"needs,omitempty"`
	ListItems []ListItem  `json:"list_items,omitempty"`
	Ask       *AskState   `json:"ask,omitempty"`
	Comments  []Comment   `json:"step_comments,omitempty"`
	Gate      bool       `json:"gate,omitempty"`
	GateToken string     `json:"gate_token,omitempty"`
	GateUsed  bool       `json:"gate_used,omitempty"`
	Ends      bool       `json:"ends,omitempty"`
	ChosenIdx int        `json:"chosen_idx,omitempty"` // which ask target was chosen
}

// --- Due ---

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

func (s *StepState) IsOverdue() bool {
	return s.DueAt != nil && time.Now().After(*s.DueAt) &&
		s.Status != StatusDone && s.Status != StatusSkipped && s.Status != StatusEnded
}

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

func (s *StepState) RequiredListBlocked() bool {
	for _, li := range s.ListItems {
		if li.Required && !li.Checked {
			return true
		}
	}
	return false
}

// --- Token ---

func generateToken() string {
	b := make([]byte, 24)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// --- Instance ---

func NewInstance(id, title string, wf *dsl.Workflow) *Instance {
	now := time.Now()
	inst := &Instance{
		ID: id, WorkflowName: wf.Name, Title: title,
		Labels: wf.Labels, Priority: wf.Priority,
		CreatedAt: now, UpdatedAt: now, Status: StatusReady,
		Vars: make(map[string]string),
	}
	if inst.Priority == "" {
		inst.Priority = "medium"
	}

	// collect all ask targets so we can keep them pending until explicitly routed
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
			// ask targets that have no explicit needs get a sentinel so they stay pending
			needs := step.Needs
			if askTargets[step.Name] && len(step.Needs) == 0 {
				needs = []string{"__ask_target__"}
			}
			st := &StepState{
				Name: step.Name, Note: step.Note, Notify: step.Notify, Assign: step.Assign,
				Due: step.Due, Needs: needs,
				Ask: askSt, Gate: step.Gate, Ends: step.Ends,
				Status: StatusPending,
			}
			for i, li := range step.ListItems {
				st.ListItems = append(st.ListItems, ListItem{
					ID: fmt.Sprintf("%d", i), Text: li.Text, Required: li.Required,
				})
			}
			ss.Steps = append(ss.Steps, st)
		}
		inst.Sections = append(inst.Sections, ss)
	}

	// initial activation: steps with no needs become ready
	inst.activateReady(now)
	return inst
}

// ApplyVars substitutes $VAR_NAME in step notes, names etc. after instance creation
func (inst *Instance) ApplyVars(vars map[string]string) {
	inst.Vars = vars
	// substitute in title
	inst.Title = substituteVars(inst.Title, vars)
	// substitute in step notes and names
	inst.allSteps(func(s *StepState) {
		s.Note   = substituteVars(s.Note, vars)
		s.Notify = substituteVars(s.Notify, vars)
	})
}

func substituteVars(s string, vars map[string]string) string {
	for k, v := range vars {
		s = strings.ReplaceAll(s, "$"+k, v)
		s = strings.ReplaceAll(s, "${"+k+"}", v)
	}
	return s
}

// activateReady scans all steps and activates those whose needs are satisfied
func (inst *Instance) activateReady(now time.Time) {
	changed := true
	for changed {
		changed = false
		inst.allSteps(func(s *StepState) {
			if s.Status != StatusPending {
				return
			}
			if inst.needsSatisfied(s) {
				log.Printf("[engine] activating step '%s' (was pending)", s.Name)
				inst.activate(s, now)
				changed = true
			}
		})
	}
}

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
	if s.Notify != "" && s.Gate {
		fireNotify(inst, s) // send gate link on activation
	}
}

// AdvanceStep — complete a ready step
func (inst *Instance) AdvanceStep(stepName string) error {
	now := time.Now()
	s := inst.findStepByName(stepName)
	if s == nil {
		return fmt.Errorf("step '%s' not found", stepName)
	}
	log.Printf("[engine] AdvanceStep '%s' status=%s", stepName, s.Status)
	if s.Status != StatusReady {
		return fmt.Errorf("step '%s' is not ready (status: %s)", stepName, s.Status)
	}
	if s.RequiredListBlocked() {
		return fmt.Errorf("step '%s' has unchecked required items", stepName)
	}
	inst.audit("complete", stepName, "")
	return inst.completeStep(s, now)
}

// AnswerAsk — choose an ask target by index
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

	// skip all other ask targets; clear sentinel from chosen target
	for i, target := range s.Ask.Targets {
		t := inst.findStepByName(target)
		if t == nil {
			continue
		}
		if i == chosenIdx {
			// clear sentinel so activateReady can activate it
			t.Needs = filterNeeds(t.Needs)
		} else if t.Status == StatusPending {
			t.Status = StatusSkipped
			t.UpdatedAt = now
		}
	}

	return inst.completeStep(s, now)
}

// RedeemGate — external approval via token
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
	if found.Ask == nil || chosenIdx < 0 || chosenIdx >= len(found.Ask.Targets) {
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

func (inst *Instance) completeStep(s *StepState, now time.Time) error {
	s.Status = StatusDone
	s.UpdatedAt = now
	if s.Notify != "" && !s.Gate { // gate already fired on activation
		fireNotify(inst, s)
	}
	if s.Ends {
		s.Status = StatusEnded
	}
	inst.UpdatedAt = now
	inst.activateReady(now)
	inst.recalc()
	return nil
}

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

func (inst *Instance) HasOverdue() bool {
	found := false
	inst.allSteps(func(s *StepState) {
		if s.IsOverdue() {
			found = true
		}
	})
	return found
}

// --- List items ---

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

// --- Comments ---

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

// --- Audit ---

func (inst *Instance) audit(action, step, note string) {
	inst.Audit = append(inst.Audit, AuditEntry{
		At: time.Now(), Action: action, Step: step, Note: note,
	})
}

// --- Helpers ---

func (inst *Instance) AllStepsDue(fn func(time.Time)) {
	inst.allSteps(func(s *StepState) {
		if s.DueAt != nil {
			fn(*s.DueAt)
		}
	})
}

func (inst *Instance) allSteps(fn func(*StepState)) {
	for _, sec := range inst.Sections {
		for _, s := range sec.Steps {
			fn(s)
		}
	}
}

func (inst *Instance) findStepByName(name string) *StepState {
	var found *StepState
	inst.allSteps(func(s *StepState) {
		if s.Name == name {
			found = s
		}
	})
	return found
}

// FindStepByToken for external gate redemption
func (inst *Instance) FindStepByToken(token string) *StepState {
	var found *StepState
	inst.allSteps(func(s *StepState) {
		if s.GateToken == token {
			found = s
		}
	})
	return found
}

func filterNeeds(needs []string) []string {
	var out []string
	for _, n := range needs {
		if n != "__ask_target__" {
			out = append(out, n)
		}
	}
	return out
}

func fireNotify(inst *Instance, step *StepState) {
	gateInfo := ""
	if step.Gate && step.GateToken != "" {
		gateInfo = fmt.Sprintf(" | approval link: /approve/%s", step.GateToken)
	}
	msg := fmt.Sprintf("[%s] NOTIFY → %s | instance: %s (%s) | step: %s%s\n",
		time.Now().Format(time.RFC3339), step.Notify,
		inst.Title, inst.WorkflowName, step.Name, gateInfo)
	log.Print(msg)
	f, err := os.OpenFile("notifications.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err == nil {
		defer f.Close()
		f.WriteString(msg)
	}
}
