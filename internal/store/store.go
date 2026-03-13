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

type Store struct {
	mu          sync.RWMutex
	dataDir     string
	workflowDir string
	definitions map[string]*dsl.Workflow
	instances   map[string]*engine.Instance
}

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
	return s, nil
}

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
		s.definitions[wf.Name] = wf
		log.Printf("[store] loaded workflow: %s", wf.Name)
	}
	return nil
}

func (s *Store) loadInstances() error {
	entries, err := os.ReadDir(s.dataDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
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
		inst.EnsureIndices()
		s.instances[inst.ID] = &inst
	}
	log.Printf("[store] loaded %d instance(s)", len(s.instances))
	return nil
}

func (s *Store) save(inst *engine.Instance) error {
	data, err := json.MarshalIndent(inst, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.dataDir, inst.ID+".json"), data, 0644)
}

func (s *Store) Definitions() []*dsl.Workflow {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var list []*dsl.Workflow
	for _, wf := range s.definitions {
		list = append(list, wf)
	}
	return list
}

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
	inst := engine.NewInstance(id, title, &wfCopy)
	s.instances[id] = inst
	log.Printf("[store] created instance %s — '%s' (%s)", id, title, workflowName)
	return inst, s.save(inst)
}

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
	inst := engine.NewInstance(newID, src.Title+" (copy)", &wfCopy)
	s.instances[newID] = inst
	log.Printf("[store] cloned instance %s → %s ('%s')", id, newID, inst.Title)
	return inst, s.save(inst)
}

func (s *Store) Instances() []*engine.Instance {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var list []*engine.Instance
	for _, inst := range s.instances {
		list = append(list, inst)
	}
	return list
}

func (s *Store) Instance(id string) (*engine.Instance, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	inst, ok := s.instances[id]
	return inst, ok
}

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

func (s *Store) AdvanceStep(id, stepName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	inst, ok := s.instances[id]
	if !ok {
		return fmt.Errorf("instance not found")
	}
	if err := inst.AdvanceStep(stepName); err != nil {
		return err
	}
	return s.save(inst)
}

func (s *Store) AnswerAsk(id, stepName string, chosenIdx int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	inst, ok := s.instances[id]
	if !ok {
		return fmt.Errorf("instance not found")
	}
	if err := inst.AnswerAsk(stepName, chosenIdx); err != nil {
		return err
	}
	return s.save(inst)
}

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
	step, err := targetInst.RedeemGate(token, chosenIdx)
	if err != nil {
		return nil, nil, err
	}
	log.Printf("[store] gate redeemed — instance %s step '%s' choice=%d", targetInst.ID, step.Name, chosenIdx)
	return targetInst, step, s.save(targetInst)
}

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

func (s *Store) AddComment(id, text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	inst, ok := s.instances[id]
	if !ok {
		return fmt.Errorf("instance not found")
	}
	inst.AddComment(text)
	return s.save(inst)
}

func (s *Store) DeleteInstance(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if inst, ok := s.instances[id]; ok {
		log.Printf("[store] deleted instance %s ('%s')", id, inst.Title)
	}
	delete(s.instances, id)
	return os.Remove(filepath.Join(s.dataDir, id+".json"))
}
