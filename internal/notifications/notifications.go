// Package notifications manages per-user in-app notifications.
// Each user's notifications are persisted as a JSON file in
// {dataDir}/notifications/{userID}.json.
//
// Design notes:
//   - RWMutex: concurrent reads (List, UnreadCount) don't block each other.
//   - In-memory unread count cache: UnreadCount never hits disk.
//   - Atomic writes: write to .tmp then rename to prevent corruption on crash.
//   - Compact JSON: no pretty-printing overhead.
package notifications

import (
	"encoding/json"
	"fmt"
	"os"

	"flowapp/internal/logger"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Kind describes what triggered the notification.
type Kind string

const (
	KindAssign  Kind = "assign"  // a step was assigned to this user
	KindNotify  Kind = "notify"  // the user was listed in a step's notify field
	KindOverdue Kind = "overdue" // a step assigned/notified to this user is overdue
	KindAdmin   Kind = "admin"   // copy sent to admins for every notification
)

// Notification is a single in-app notification entry for a user.
type Notification struct {
	ID           string    `json:"id"`
	Kind         Kind      `json:"kind"`
	InstanceID   string    `json:"instance_id"`
	InstanceName string    `json:"instance_name"`
	WorkflowName string    `json:"workflow_name"`
	StepName     string    `json:"step_name"`
	Message      string    `json:"message"`
	GateURL      string    `json:"gate_url,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	Read         bool      `json:"read"`
}

var notifLog = logger.New("notifications")

// Store manages notifications for all users.
type Store struct {
	mu          sync.RWMutex
	dataDir     string
	unreadCache map[string]int // userID -> unread count (in-memory, never hits disk)
}

// New creates a NotificationStore that persists data under dir.
// The unread cache is rebuilt from disk on startup.
func New(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("notifications: mkdir %s: %w", dir, err)
	}
	s := &Store{dataDir: dir, unreadCache: make(map[string]int)}
	s.rebuildCache()
	return s, nil
}

// Add appends a new notification for userID and persists it atomically.
func (s *Store) Add(userID string, n Notification) {
	if n.ID == "" {
		n.ID = fmt.Sprintf("%d", time.Now().UnixNano())
	}
	if n.CreatedAt.IsZero() {
		n.CreatedAt = time.Now()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.load(userID)
	list = append(list, n)
	if err := s.save(userID, list); err != nil {
		notifLog.Error("save error for user %s: %v", userID, err)
		return
	}
	if !n.Read {
		s.unreadCache[userID]++
	}
}

// List returns all notifications for userID, newest first. No disk I/O beyond the read.
func (s *Store) List(userID string) []Notification {
	s.mu.RLock()
	defer s.mu.RUnlock()
	list := s.load(userID)
	for i, j := 0, len(list)-1; i < j; i, j = i+1, j-1 {
		list[i], list[j] = list[j], list[i]
	}
	return list
}

// UnreadCount returns the cached unread count — zero disk I/O.
func (s *Store) UnreadCount(userID string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.unreadCache[userID]
}

// MarkRead marks a specific notification as read.
func (s *Store) MarkRead(userID, notifID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.load(userID)
	for i := range list {
		if list[i].ID == notifID && !list[i].Read {
			list[i].Read = true
			s.unreadCache[userID]--
			break
		}
	}
	if err := s.save(userID, list); err != nil {
		notifLog.Error("save error for user %s: %v", userID, err)
	}
}

// MarkUnread marks a specific notification as unread.
func (s *Store) MarkUnread(userID, notifID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.load(userID)
	for i := range list {
		if list[i].ID == notifID && list[i].Read {
			list[i].Read = false
			s.unreadCache[userID]++
			break
		}
	}
	if err := s.save(userID, list); err != nil {
		notifLog.Error("save error for user %s: %v", userID, err)
	}
}

// MarkAllRead marks all notifications for userID as read.
func (s *Store) MarkAllRead(userID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.load(userID)
	for i := range list {
		list[i].Read = true
	}
	s.unreadCache[userID] = 0
	if err := s.save(userID, list); err != nil {
		notifLog.Error("save error for user %s: %v", userID, err)
	}
}

// ── internal helpers ──────────────────────────────────────────────────────────

func (s *Store) filePath(userID string) string {
	return filepath.Join(s.dataDir, userID+".json")
}

// load reads from disk. Must be called with s.mu held (any level).
func (s *Store) load(userID string) []Notification {
	data, err := os.ReadFile(s.filePath(userID))
	if err != nil {
		return nil
	}
	var list []Notification
	if err := json.Unmarshal(data, &list); err != nil {
		notifLog.Error("parse error for user %s: %v", userID, err)
		return nil
	}
	return list
}

// save writes atomically via tmp+rename. Must be called with s.mu write-locked.
func (s *Store) save(userID string, list []Notification) error {
	data, err := json.Marshal(list)
	if err != nil {
		return err
	}
	dest := s.filePath(userID)
	tmp := dest + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, dest)
}

// rebuildCache counts unread notifications for all users from disk at startup.
func (s *Store) rebuildCache() {
	entries, err := os.ReadDir(s.dataDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		userID := strings.TrimSuffix(e.Name(), ".json")
		data, err := os.ReadFile(filepath.Join(s.dataDir, e.Name()))
		if err != nil {
			continue
		}
		var list []Notification
		if err := json.Unmarshal(data, &list); err != nil {
			continue
		}
		count := 0
		for _, n := range list {
			if !n.Read {
				count++
			}
		}
		s.unreadCache[userID] = count
	}
}

// Reset clears the in-memory unread cache (call after a FullReset wipes all files).
func (s *Store) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.unreadCache = make(map[string]int)
}
