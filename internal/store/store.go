package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"cursor-admin/internal/model"
)

// Store manages account persistence using a JSON file.
type Store struct {
	mu       sync.RWMutex
	filePath string
	accounts map[string]*model.Account
}

// New creates a new Store, loading any existing data from the file.
func New(filePath string) (*Store, error) {
	s := &Store{
		filePath: filePath,
		accounts: make(map[string]*model.Account),
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read store file: %w", err)
	}
	if len(data) == 0 {
		return nil
	}
	var accounts []*model.Account
	if err := json.Unmarshal(data, &accounts); err != nil {
		return fmt.Errorf("failed to parse store file: %w", err)
	}
	for _, a := range accounts {
		s.accounts[a.ID] = a
	}
	return nil
}

func (s *Store) save() error {
	accounts := s.listUnsafe()
	data, err := json.MarshalIndent(accounts, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal accounts: %w", err)
	}
	return os.WriteFile(s.filePath, data, 0644)
}

func (s *Store) listUnsafe() []*model.Account {
	accounts := make([]*model.Account, 0, len(s.accounts))
	for _, a := range s.accounts {
		accounts = append(accounts, a)
	}
	sort.Slice(accounts, func(i, j int) bool {
		return accounts[i].CreatedAt.Before(accounts[j].CreatedAt)
	})
	return accounts
}

// List returns all accounts sorted by creation time.
func (s *Store) List() []*model.Account {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := s.listUnsafe()
	// Return copies to avoid data races
	copies := make([]*model.Account, len(result))
	for i, a := range result {
		copy := *a
		copies[i] = &copy
	}
	return copies
}

// Get returns a copy of the account with the given ID.
func (s *Store) Get(id string) (*model.Account, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, ok := s.accounts[id]
	if !ok {
		return nil, fmt.Errorf("account not found: %s", id)
	}
	copy := *a
	return &copy, nil
}

// FindByEmail returns a copy of the first account matching email (case-insensitive).
func (s *Store) FindByEmail(email string) (*model.Account, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	needle := strings.TrimSpace(email)
	if needle == "" {
		return nil, fmt.Errorf("account not found: empty email")
	}
	for _, a := range s.accounts {
		if strings.EqualFold(strings.TrimSpace(a.Email), needle) {
			copy := *a
			return &copy, nil
		}
	}
	return nil, fmt.Errorf("account not found: %s", email)
}

// Add creates a new account in the store.
func (s *Store) Add(account *model.Account) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if account.ID == "" {
		account.ID = generateID()
	}
	now := time.Now()
	account.CreatedAt = now
	account.UpdatedAt = now
	if account.Status == "" {
		account.Status = "unknown"
	}
	s.accounts[account.ID] = account
	return s.save()
}

// Update modifies an existing account in the store.
func (s *Store) Update(account *model.Account) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.accounts[account.ID]; !ok {
		return fmt.Errorf("account not found: %s", account.ID)
	}
	account.UpdatedAt = time.Now()
	s.accounts[account.ID] = account
	return s.save()
}

// Delete removes an account from the store by ID.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.accounts[id]; !ok {
		return fmt.Errorf("account not found: %s", id)
	}
	delete(s.accounts, id)
	return s.save()
}

// Count returns the number of accounts in the store.
func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.accounts)
}

func generateID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
