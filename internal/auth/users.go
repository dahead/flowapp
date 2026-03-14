package auth

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"golang.org/x/crypto/bcrypt"
	"log"
	"os"
	"strings"
	"sync"
	"time"
)

// Role defines the access level of a user within the application.
type Role string

const (
	RoleAdmin   Role = "admin"
	RoleManager Role = "manager" // can delete and clone instances, plus all user rights
	RoleViewer  Role = "viewer"
	RoleUser    Role = "user"
)

// User represents an application user with authentication and role information.
type User struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	Name         string    `json:"name"`
	Role         Role      `json:"role"`
	AppRoles     []string  `json:"app_roles,omitempty"` // domain-specific roles, e.g. "hr", "finance"
	PasswordHash string    `json:"password_hash"`
	CreatedAt    time.Time `json:"created_at"`
	Active       bool      `json:"active"`
}

// CanWrite returns true if the user may create or modify workflow instances.
func (u *User) CanWrite() bool {
	return u.Role == RoleAdmin || u.Role == RoleUser || u.Role == RoleManager
}

// CanAdmin returns true if the user has full administrative access.
func (u *User) CanAdmin() bool { return u.Role == RoleAdmin }

// CanDeleteInstance returns true if the user may permanently delete workflow instances.
// Requires admin or manager role.
func (u *User) CanDeleteInstance() bool {
	return u.Role == RoleAdmin || u.Role == RoleManager
}

// CanCloneInstance returns true if the user may clone workflow instances.
// Requires admin or manager role.
func (u *User) CanCloneInstance() bool {
	return u.Role == RoleAdmin || u.Role == RoleManager
}

// UserStore is an in-memory user registry backed by a JSON file.
// All mutations are written through to disk immediately.
type UserStore struct {
	mu      sync.RWMutex
	path    string
	users   map[string]*User
	byEmail map[string]*User
}

// NewUserStore loads the user store from the given JSON file path.
// If the file does not yet exist an empty store is returned (first-run setup).
func NewUserStore(path string) (*UserStore, error) {
	s := &UserStore{path: path, users: map[string]*User{}, byEmail: map[string]*User{}}
	if _, err := os.Stat(path); err == nil {
		if err := s.load(); err != nil {
			log.Printf("[auth] failed to load user store from %s: %v", path, err)
			return nil, err
		}
	}
	return s, nil
}

// load reads and parses the JSON file into the in-memory maps.
func (s *UserStore) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	var list []*User
	if err := json.Unmarshal(data, &list); err != nil {
		return err
	}
	for _, u := range list {
		s.users[u.ID] = u
		s.byEmail[u.Email] = u
	}
	return nil
}

// save writes the current in-memory state to the JSON file.
func (s *UserStore) save() error {
	list := make([]*User, 0, len(s.users))
	for _, u := range s.users {
		list = append(list, u)
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0644)
}

// Empty returns true if no users have been created yet (first-run state).
func (s *UserStore) Empty() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.users) == 0
}

// Create adds a new user with a bcrypt-hashed password.
// Returns an error if the email is already registered.
func (s *UserStore) Create(email, name, password string, role Role) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.byEmail[email]; exists {
		log.Printf("[auth] create user failed — email already registered: %s", email)
		return nil, fmt.Errorf("E-Mail bereits registriert")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	u := &User{
		ID: newID(), Email: email, Name: name,
		Role: role, PasswordHash: string(hash),
		CreatedAt: time.Now(), Active: true,
	}
	s.users[u.ID] = u
	s.byEmail[u.Email] = u
	if err := s.save(); err != nil {
		log.Printf("[auth] create user failed — save error for %s: %v", email, err)
		return nil, err
	}
	log.Printf("[auth] created user %s (%s) role=%s", u.ID, email, role)
	return u, nil
}

// Authenticate verifies the email/password combination and returns the user on success.
// Returns an error if the credentials are invalid or the account is inactive.
func (s *UserStore) Authenticate(email, password string) (*User, error) {
	s.mu.RLock()
	u := s.byEmail[email]
	s.mu.RUnlock()
	if u == nil || !u.Active {
		log.Printf("[auth] authentication failed — unknown or inactive user: %s", email)
		return nil, fmt.Errorf("Ungültige Zugangsdaten")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return nil, fmt.Errorf("Ungültige Zugangsdaten")
	}
	return u, nil
}

// GetByID looks up a user by their unique ID.
func (s *UserStore) GetByID(id string) (*User, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.users[id]
	return u, ok
}

// List returns all users in the store (order is undefined).
func (s *UserStore) List() []*User {
	s.mu.RLock()
	defer s.mu.RUnlock()
	list := make([]*User, 0, len(s.users))
	for _, u := range s.users {
		list = append(list, u)
	}
	return list
}

// Update modifies an existing user's profile fields and persists the change.
func (s *UserStore) Update(id, name, email string, role Role, appRoles []string, active bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[id]
	if !ok {
		return fmt.Errorf("user not found")
	}
	if email != u.Email {
		if _, exists := s.byEmail[email]; exists {
			log.Printf("[auth] update user failed — email already taken: %s", email)
			return fmt.Errorf("E-Mail bereits vergeben")
		}
		delete(s.byEmail, u.Email)
		s.byEmail[email] = u
		u.Email = email
	}
	u.Name = name
	u.Role = role
	u.AppRoles = appRoles
	u.Active = active
	if err := s.save(); err != nil {
		log.Printf("[auth] update user failed — save error for %s: %v", id, err)
		return err
	}
	return nil
}

// ResetPassword replaces a user's password hash with a new bcrypt hash.
func (s *UserStore) ResetPassword(id, newPassword string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[id]
	if !ok {
		return fmt.Errorf("user not found")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	u.PasswordHash = string(hash)
	if err := s.save(); err != nil {
		log.Printf("[auth] reset password failed — save error for %s: %v", id, err)
		return err
	}
	log.Printf("[auth] password reset for user %s", id)
	return nil
}

// Delete removes a user from the store permanently.
func (s *UserStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[id]
	if !ok {
		log.Printf("[auth] delete user failed — not found: %s", id)
		return fmt.Errorf("user not found")
	}
	delete(s.byEmail, u.Email)
	delete(s.users, id)
	if err := s.save(); err != nil {
		log.Printf("[auth] delete user failed — save error for %s: %v", id, err)
		return err
	}
	log.Printf("[auth] deleted user %s (%s)", id, u.Email)
	return nil
}

// ResolveEmails resolves an assign/notify expression to a list of email addresses.
//
// Supported formats:
//   - "user:<email>"  — match by email address
//   - "user:<name>"   — match by display name (case-insensitive)
//   - "role:<role>"   — all active users with the given app_role
//   - bare email      — returned as-is
func (s *UserStore) ResolveEmails(expr string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	expr = strings.TrimSpace(expr)
	if strings.HasPrefix(expr, "user:") {
		val := strings.TrimPrefix(expr, "user:")
		// try email first
		if u, ok := s.byEmail[val]; ok && u.Active {
			return []string{u.Email}
		}
		// fallback: match by name (case-insensitive)
		for _, u := range s.users {
			if u.Active && strings.EqualFold(u.Name, val) {
				return []string{u.Email}
			}
		}
		return nil
	}
	if strings.HasPrefix(expr, "role:") {
		role := strings.TrimPrefix(expr, "role:")
		var emails []string
		for _, u := range s.users {
			if !u.Active {
				continue
			}
			for _, r := range u.AppRoles {
				if r == role {
					emails = append(emails, u.Email)
					break
				}
			}
		}
		return emails
	}
	// bare email — pass through directly
	return []string{expr}
}

// newID generates a random 24-character hex string for use as a unique ID.
func newID() string {
	b := make([]byte, 12)
	rand.Read(b)
	return hex.EncodeToString(b)
}
