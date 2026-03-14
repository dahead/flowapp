package auth

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"golang.org/x/crypto/bcrypt"
	"log"
	"os"
	"sync"
	"time"
)

type Role string

const (
	RoleAdmin  Role = "admin"
	RoleUser   Role = "user"
	RoleViewer Role = "viewer"
)

type User struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	Name         string    `json:"name"`
	Role         Role      `json:"role"`
	PasswordHash string    `json:"password_hash"`
	CreatedAt    time.Time `json:"created_at"`
	Active       bool      `json:"active"`
}

func (u *User) CanWrite() bool { return u.Role == RoleAdmin || u.Role == RoleUser }
func (u *User) CanAdmin() bool { return u.Role == RoleAdmin }

type UserStore struct {
	mu      sync.RWMutex
	path    string
	users   map[string]*User
	byEmail map[string]*User
}

func NewUserStore(path string) (*UserStore, error) {
	s := &UserStore{path: path, users: map[string]*User{}, byEmail: map[string]*User{}}
	if _, err := os.Stat(path); err == nil {
		if err := s.load(); err != nil {
			log.Printf("[auth] failed to load user store from %s: %v", path, err)
			return nil, err
		}
		log.Printf("[auth] loaded %d user(s) from %s", len(s.users), path)
	} else {
		log.Printf("[auth] no user store found at %s — starting empty", path)
	}
	return s, nil
}

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

func (s *UserStore) save() error {
	list := make([]*User, 0, len(s.users))
	for _, u := range s.users {
		list = append(list, u)
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0600)
}

func (s *UserStore) Empty() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.users) == 0
}

func (s *UserStore) Create(email, name, password string, role Role) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.byEmail[email]; exists {
		log.Printf("[auth] create user failed — email already registered: %s", email)
		return nil, fmt.Errorf("E-Mail bereits registriert")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("[auth] create user failed — bcrypt error for %s: %v", email, err)
		return nil, err
	}
	u := &User{
		ID: newID(), Email: email, Name: name, Role: role,
		PasswordHash: string(hash), CreatedAt: time.Now(), Active: true,
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

func (s *UserStore) Authenticate(email, password string) (*User, error) {
	s.mu.RLock()
	u := s.byEmail[email]
	s.mu.RUnlock()
	if u == nil || !u.Active {
		log.Printf("[auth] authentication failed — unknown or inactive user: %s", email)
		return nil, fmt.Errorf("Ungültige Zugangsdaten")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		log.Printf("[auth] authentication failed — wrong password for user: %s", email)
		return nil, fmt.Errorf("Ungültige Zugangsdaten")
	}
	log.Printf("[auth] authenticated user %s (%s)", u.ID, email)
	return u, nil
}

func (s *UserStore) GetByID(id string) (*User, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.users[id]
	return u, ok
}

func (s *UserStore) List() []*User {
	s.mu.RLock()
	defer s.mu.RUnlock()
	list := make([]*User, 0, len(s.users))
	for _, u := range s.users {
		list = append(list, u)
	}
	return list
}

func (s *UserStore) Update(id, name, email string, role Role, active bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[id]
	if !ok {
		log.Printf("[auth] update user failed — not found: %s", id)
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
	u.Active = active
	if err := s.save(); err != nil {
		log.Printf("[auth] update user failed — save error for %s: %v", id, err)
		return err
	}
	log.Printf("[auth] updated user %s (%s) role=%s active=%v", id, email, role, active)
	return nil
}

func (s *UserStore) ResetPassword(id, newPassword string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[id]
	if !ok {
		log.Printf("[auth] reset password failed — user not found: %s", id)
		return fmt.Errorf("user not found")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("[auth] reset password failed — bcrypt error for %s: %v", id, err)
		return err
	}
	u.PasswordHash = string(hash)
	if err := s.save(); err != nil {
		log.Printf("[auth] reset password failed — save error for %s: %v", id, err)
		return err
	}
	log.Printf("[auth] password reset for user %s (%s)", id, u.Email)
	return nil
}

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

func newID() string {
	b := make([]byte, 12)
	rand.Read(b)
	return hex.EncodeToString(b)
}
