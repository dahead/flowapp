package store

import (
	"encoding/json"
	"flowapp/internal/dsl"
	"flowapp/internal/engine"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Store is the central data layer. It holds workflow definitions (loaded from .workflow files)
// and active instances (persisted as JSON files in dataDir). All public methods are safe
// for concurrent use.
type Store struct {
	mu          sync.RWMutex
	dataDir     string
	workflowDir string
	definitions map[string]*dsl.Workflow
	instances   map[string]*engine.Instance

	// optional mailer and email resolver; nil = log-only notifications
	mailer        engine.Mailer
	emailResolver engine.EmailResolver
}

// New creates a Store, loads all workflow definitions and persisted instances,
// then starts a background file-watcher for hot-reloading workflows and a
// scheduler goroutine for time-based step activation.
func New(workflowDir, dataDir string) (*Store, error) {
	log.Printf("[store] starting — workflowDir=%s dataDir=%s", workflowDir, dataDir)
	s := &Store{
		workflowDir: workflowDir, dataDir: dataDir,
		definitions: make(map[string]*dsl.Workflow),
		instances:   make(map[string]*engine.Instance),
	}
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, err
	}
	if err := s.loadDefinitions(); err != nil {
		return nil, err
	}
	if err := s.loadInstances(); err != nil {
		return nil, err
	}
	go s.watchWorkflows()
	go s.runScheduler()
	return s, nil
}

// SetMailer configures the mailer and email resolver used for notifications.
// Call this once at startup after the Store is created.
func (s *Store) SetMailer(m engine.Mailer, r engine.EmailResolver) {
	s.mailer = m
	s.emailResolver = r
}

// inject sets the runtime-only Mailer and EmailResolver fields on an instance
// before any engine method is called. Instances loaded from disk don't carry
// these fields, so they must be re-injected each time.
func (s *Store) inject(inst *engine.Instance) {
	inst.MailSender = s.mailer
	inst.EmailResolver = s.emailResolver
}

// runScheduler ticks every minute and activates any scheduled steps whose time has arrived.
func (s *Store) runScheduler() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		s.mu.Lock()
		for _, inst := range s.instances {
			if inst.Status == engine.StatusDone {
				continue
			}
			s.inject(inst)
			if inst.TickScheduled() {
				if err := s.save(inst); err != nil {
					log.Printf("[scheduler] save error for %s: %v", inst.ID, err)
				}
			}
		}
		s.mu.Unlock()
	}
}

// watchWorkflows monitors the workflow directory for file changes and hot-reloads
// definitions when .workflow files are created, modified, or deleted.
// A 300ms debounce prevents multiple rapid reloads on a single save.
func (s *Store) watchWorkflows() {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("fsnotify: %v", err)
		return
	}
	defer watcher.Close()
	watcher.Add(s.workflowDir)
	var debounce <-chan time.Time
	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if strings.HasSuffix(event.Name, ".workflow") {
				debounce = time.After(300 * time.Millisecond)
			}
		case <-debounce:
			s.mu.Lock()
			s.definitions = make(map[string]*dsl.Workflow)
			if err := s.loadDefinitions(); err != nil {
				log.Printf("hot-reload error: %v", err)
			} else {
				log.Printf("[store] hot-reload: %d workflow(s) reloaded", len(s.definitions))
			}
			s.mu.Unlock()
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Printf("fsnotify error: %v", err)
		}
	}
}

// loadDefinitions parses all .workflow files in workflowDir and registers them.
// Duplicate workflow names are suffixed with " -1", " -2", etc.
// Files that fail to parse or contain dependency cycles are skipped with a warning.
func (s *Store) loadDefinitions() error {
	entries, err := os.ReadDir(s.workflowDir)
	if err != nil {
		return fmt.Errorf("reading workflow dir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".workflow") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.workflowDir, e.Name()))
		if err != nil {
			return err
		}
		wf, err := dsl.Parse(string(data))
		if err != nil {
			log.Printf("parse error in %s: %v (skipping)", e.Name(), err)
			continue
		}
		if err := dsl.DetectCycles(wf); err != nil {
			log.Printf("[store] cycle detected in %s: %v (skipping)", e.Name(), err)
			continue
		}
		// deduplicate names by appending a counter
		origName := wf.Name
		for i := 1; ; i++ {
			if _, dup := s.definitions[wf.Name]; !dup {
				break
			}
			wf.Name = fmt.Sprintf("%s (copy %d)", origName, i)
		}
		if wf.Name != origName {
			log.Printf("[store] WARNING: duplicate name '%s' in %s — renamed to '%s'", origName, e.Name(), wf.Name)
		}
		s.definitions[wf.Name] = wf
		log.Printf("[store] loaded workflow: %s (from %s)", wf.Name, e.Name())
	}
	return nil
}

// loadInstances reads all instance JSON files from dataDir into memory.
func (s *Store) loadInstances() error {
	entries, err := os.ReadDir(s.dataDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") || e.Name() == "users.json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dataDir, e.Name()))
		if err != nil {
			return err
		}
		var inst engine.Instance
		if err := json.Unmarshal(data, &inst); err != nil {
			return err
		}
		s.instances[inst.ID] = &inst
	}
	log.Printf("[store] loaded %d instance(s)", len(s.instances))
	return nil
}

// save serialises an instance to its JSON file in dataDir.
func (s *Store) save(inst *engine.Instance) error {
	data, err := json.MarshalIndent(inst, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.dataDir, inst.ID+".json"), data, 0644)
}

// Definitions returns all currently loaded workflow definitions.
func (s *Store) Definitions() []*dsl.Workflow {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var list []*dsl.Workflow
	for _, wf := range s.definitions {
		list = append(list, wf)
	}
	return list
}

// AllLabels returns the deduplicated set of labels defined across all workflows.
func (s *Store) AllLabels() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	seen := map[string]bool{}
	var out []string
	for _, wf := range s.definitions {
		for _, l := range wf.Labels {
			if !seen[l] {
				seen[l] = true
				out = append(out, l)
			}
		}
	}
	return out
}

// CreateInstance creates a new instance from the named workflow definition.
// priority may be empty to use the workflow default.
// Mailer and EmailResolver are set before the initial activation so that
// notifications fire correctly for steps that are immediately ready.
func (s *Store) CreateInstance(workflowName, title, priority string) (*engine.Instance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	wf, ok := s.definitions[workflowName]
	if !ok {
		return nil, fmt.Errorf("workflow '%s' not found", workflowName)
	}
	wfCopy := *wf
	if priority != "" {
		wfCopy.Priority = priority
	}
	id := fmt.Sprintf("%d", time.Now().UnixNano())
	inst := engine.NewInstance(id, title, &wfCopy, s.mailer, s.emailResolver)
	s.instances[id] = inst
	log.Printf("[store] created instance %s — '%s' (%s)", id, title, workflowName)
	return inst, s.save(inst)
}

// CloneInstance creates a fresh copy of an existing instance using the same workflow definition.
// The clone title gets " (copy)" appended.
func (s *Store) CloneInstance(id string) (*engine.Instance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	src, ok := s.instances[id]
	if !ok {
		return nil, fmt.Errorf("instance not found")
	}
	wf, ok := s.definitions[src.WorkflowName]
	if !ok {
		return nil, fmt.Errorf("workflow definition not found")
	}
	wfCopy := *wf
	wfCopy.Priority = src.Priority
	newID := fmt.Sprintf("%d", time.Now().UnixNano())
	inst := engine.NewInstance(newID, src.Title+" (copy)", &wfCopy, s.mailer, s.emailResolver)
	s.instances[newID] = inst
	log.Printf("[store] cloned instance %s → %s ('%s')", id, newID, inst.Title)
	return inst, s.save(inst)
}

// Instances returns all non-archived instances.
func (s *Store) Instances() []*engine.Instance {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var list []*engine.Instance
	for _, inst := range s.instances {
		if !inst.Archived {
			list = append(list, inst)
		}
	}
	return list
}

// ArchivedInstances returns all archived instances.
func (s *Store) ArchivedInstances() []*engine.Instance {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var list []*engine.Instance
	for _, inst := range s.instances {
		if inst.Archived {
			list = append(list, inst)
		}
	}
	return list
}

// Instance returns a single instance by ID.
func (s *Store) Instance(id string) (*engine.Instance, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	inst, ok := s.instances[id]
	return inst, ok
}

// FindByToken returns the instance and step that hold the given gate token.
// Returns nil, nil if no matching token exists.
func (s *Store) FindByToken(token string) (*engine.Instance, *engine.StepState) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, inst := range s.instances {
		if s := inst.FindStepByToken(token); s != nil {
			return inst, s
		}
	}
	return nil, nil
}

// AdvanceStep completes a ready step in the given instance and persists the result.
func (s *Store) AdvanceStep(id, stepName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	inst, ok := s.instances[id]
	if !ok {
		return fmt.Errorf("instance not found")
	}
	s.inject(inst)
	if err := inst.AdvanceStep(stepName); err != nil {
		return err
	}
	return s.save(inst)
}

// AnswerAsk resolves an ask step's routing decision and persists the result.
func (s *Store) AnswerAsk(id, stepName string, chosenIdx int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	inst, ok := s.instances[id]
	if !ok {
		return fmt.Errorf("instance not found")
	}
	s.inject(inst)
	if err := inst.AnswerAsk(stepName, chosenIdx); err != nil {
		return err
	}
	return s.save(inst)
}

// RedeemGate validates an approval token and completes the corresponding gate step.
// Returns the updated instance and step on success.
func (s *Store) RedeemGate(token string, chosenIdx int) (*engine.Instance, *engine.StepState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var targetInst *engine.Instance
	for _, inst := range s.instances {
		if inst.FindStepByToken(token) != nil {
			targetInst = inst
			break
		}
	}
	if targetInst == nil {
		return nil, nil, fmt.Errorf("token not found")
	}
	s.inject(targetInst)
	step, err := targetInst.RedeemGate(token, chosenIdx)
	if err != nil {
		return nil, nil, err
	}
	log.Printf("[store] gate redeemed — instance %s step '%s' choice=%d", targetInst.ID, step.Name, chosenIdx)
	return targetInst, step, s.save(targetInst)
}

// UpdateInstance updates the title, priority, and label set of an instance.
// Empty strings for title or priority leave those fields unchanged.
func (s *Store) UpdateInstance(id, title, priority, labels string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	inst, ok := s.instances[id]
	if !ok {
		return fmt.Errorf("instance not found")
	}
	if title != "" {
		inst.Title = title
	}
	if priority != "" {
		inst.Priority = priority
	}
	var parsed []string
	for _, l := range strings.Split(labels, ",") {
		l = strings.ToLower(strings.TrimSpace(l))
		if l != "" {
			parsed = append(parsed, l)
		}
	}
	inst.Labels = parsed
	log.Printf("[store] updated instance %s — title='%s' priority=%s labels=%v", id, inst.Title, inst.Priority, inst.Labels)
	inst.UpdatedAt = time.Now()
	return s.save(inst)
}

// ToggleListItem flips the checked state of a checklist item and persists the result.
func (s *Store) ToggleListItem(id, stepName, itemID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	inst, ok := s.instances[id]
	if !ok {
		return fmt.Errorf("instance not found")
	}
	if err := inst.ToggleListItem(stepName, itemID); err != nil {
		return err
	}
	return s.save(inst)
}

// CheckAllListItems marks all checklist items in a step as checked and persists the result.
func (s *Store) CheckAllListItems(id, stepName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	inst, ok := s.instances[id]
	if !ok {
		return fmt.Errorf("instance not found")
	}
	inst.CheckAllListItems(stepName)
	return s.save(inst)
}

// AddListItem appends a dynamic checklist item to an active step and persists the result.
func (s *Store) AddListItem(id, stepName, text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	inst, ok := s.instances[id]
	if !ok {
		return fmt.Errorf("instance not found")
	}
	if err := inst.AddListItem(stepName, text); err != nil {
		return err
	}
	return s.save(inst)
}

// AddStepComment appends a comment to a step and persists the result.
func (s *Store) AddStepComment(id, stepName, text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	inst, ok := s.instances[id]
	if !ok {
		return fmt.Errorf("instance not found")
	}
	if err := inst.AddStepComment(stepName, text); err != nil {
		return err
	}
	return s.save(inst)
}

// AddComment appends a comment to the instance and persists the result.
func (s *Store) AddComment(id, text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	inst, ok := s.instances[id]
	if !ok {
		return fmt.Errorf("instance not found")
	}
	if err := inst.AddComment(text); err != nil {
		return err
	}
	return s.save(inst)
}

// ApplyVars substitutes workflow variable placeholders in an instance and persists the result.
func (s *Store) ApplyVars(id string, vars map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	inst, ok := s.instances[id]
	if !ok {
		return fmt.Errorf("instance not found")
	}
	inst.ApplyVars(vars)
	return s.save(inst)
}

// ReorderInstances updates the position field of each instance according to the
// provided ordered slice of IDs and persists each change.
func (s *Store) ReorderInstances(ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, id := range ids {
		if inst, ok := s.instances[id]; ok {
			inst.Position = i
			if err := s.save(inst); err != nil {
				return err
			}
		}
	}
	log.Printf("[store] reordered %d instances", len(ids))
	return nil
}

// ArchiveInstance marks an instance as archived and persists the result.
func (s *Store) ArchiveInstance(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	inst, ok := s.instances[id]
	if !ok {
		return fmt.Errorf("instance not found")
	}
	inst.Archived = true
	inst.UpdatedAt = time.Now()
	log.Printf("[store] archived instance %s ('%s')", id, inst.Title)
	return s.save(inst)
}

// DeleteInstance removes an instance from memory and deletes its JSON file from disk.
func (s *Store) DeleteInstance(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if inst, ok := s.instances[id]; ok {
		log.Printf("[store] deleted instance %s ('%s')", id, inst.Title)
	}
	delete(s.instances, id)
	return os.Remove(filepath.Join(s.dataDir, id+".json"))
}
