package store

import (
	"os"
	"path/filepath"
	"testing"

	"cursor-admin/internal/model"
)

func newTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test_accounts.json")
	s, err := New(filePath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	return s, filePath
}

func TestNew_CreatesStoreWithEmptyFile(t *testing.T) {
	s, _ := newTestStore(t)
	if s.Count() != 0 {
		t.Errorf("expected empty store, got %d accounts", s.Count())
	}
}

func TestNew_LoadsExistingData(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test_accounts.json")

	// Write initial data
	data := `[{"id":"abc123","email":"test@example.com","password":"pass123","status":"unknown","created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-01T00:00:00Z"}]`
	if err := os.WriteFile(filePath, []byte(data), 0644); err != nil {
		t.Fatalf("failed to write test data: %v", err)
	}

	s, err := New(filePath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	if s.Count() != 1 {
		t.Errorf("expected 1 account, got %d", s.Count())
	}
}

func TestAdd(t *testing.T) {
	s, _ := newTestStore(t)

	acc := &model.Account{
		Email:    "test@example.com",
		Password: "pass123",
	}

	if err := s.Add(acc); err != nil {
		t.Fatalf("failed to add account: %v", err)
	}

	if acc.ID == "" {
		t.Error("expected non-empty ID after Add")
	}
	if acc.Status != "unknown" {
		t.Errorf("expected status 'unknown', got '%s'", acc.Status)
	}
	if acc.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}
	if s.Count() != 1 {
		t.Errorf("expected 1 account, got %d", s.Count())
	}
}

func TestAdd_MultiplePersists(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test_accounts.json")

	s1, err := New(filePath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	s1.Add(&model.Account{Email: "a@test.com", Password: "p1"})
	s1.Add(&model.Account{Email: "b@test.com", Password: "p2"})

	// Create a new store from the same file
	s2, err := New(filePath)
	if err != nil {
		t.Fatalf("failed to create store2: %v", err)
	}
	if s2.Count() != 2 {
		t.Errorf("expected 2 accounts after reload, got %d", s2.Count())
	}
}

func TestGet(t *testing.T) {
	s, _ := newTestStore(t)

	acc := &model.Account{Email: "test@example.com", Password: "pass123"}
	s.Add(acc)

	got, err := s.Get(acc.ID)
	if err != nil {
		t.Fatalf("failed to get account: %v", err)
	}
	if got.Email != "test@example.com" {
		t.Errorf("expected email 'test@example.com', got '%s'", got.Email)
	}
}

func TestGet_NotFound(t *testing.T) {
	s, _ := newTestStore(t)

	_, err := s.Get("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent account")
	}
}

func TestUpdate(t *testing.T) {
	s, _ := newTestStore(t)

	acc := &model.Account{Email: "test@example.com", Password: "pass123"}
	s.Add(acc)

	acc.Email = "updated@example.com"
	acc.Status = "active"
	if err := s.Update(acc); err != nil {
		t.Fatalf("failed to update account: %v", err)
	}

	got, _ := s.Get(acc.ID)
	if got.Email != "updated@example.com" {
		t.Errorf("expected updated email, got '%s'", got.Email)
	}
	if got.Status != "active" {
		t.Errorf("expected status 'active', got '%s'", got.Status)
	}
}

func TestUpdate_NotFound(t *testing.T) {
	s, _ := newTestStore(t)

	acc := &model.Account{ID: "nonexistent", Email: "test@example.com"}
	err := s.Update(acc)
	if err == nil {
		t.Error("expected error for nonexistent account")
	}
}

func TestDelete(t *testing.T) {
	s, _ := newTestStore(t)

	acc := &model.Account{Email: "test@example.com", Password: "pass123"}
	s.Add(acc)

	if err := s.Delete(acc.ID); err != nil {
		t.Fatalf("failed to delete account: %v", err)
	}
	if s.Count() != 0 {
		t.Errorf("expected 0 accounts after delete, got %d", s.Count())
	}
}

func TestDelete_NotFound(t *testing.T) {
	s, _ := newTestStore(t)

	err := s.Delete("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent account")
	}
}

func TestList_Sorted(t *testing.T) {
	s, _ := newTestStore(t)

	s.Add(&model.Account{Email: "c@test.com", Password: "p"})
	s.Add(&model.Account{Email: "a@test.com", Password: "p"})
	s.Add(&model.Account{Email: "b@test.com", Password: "p"})

	list := s.List()
	if len(list) != 3 {
		t.Fatalf("expected 3 accounts, got %d", len(list))
	}
	// Should be sorted by creation time
	if list[0].Email != "c@test.com" {
		t.Errorf("expected first account to be c@test.com, got %s", list[0].Email)
	}
	if list[2].Email != "b@test.com" {
		t.Errorf("expected last account to be b@test.com, got %s", list[2].Email)
	}
}

func TestList_ReturnsCopies(t *testing.T) {
	s, _ := newTestStore(t)

	s.Add(&model.Account{Email: "test@example.com", Password: "pass123"})

	list := s.List()
	list[0].Email = "modified@example.com"

	// Original should be unchanged
	original, _ := s.Get(list[0].ID)
	if original.Email != "test@example.com" {
		t.Error("List() should return copies, not references to internal data")
	}
}

func TestGenerateID(t *testing.T) {
	id1 := generateID()
	id2 := generateID()

	if id1 == "" {
		t.Error("expected non-empty ID")
	}
	if len(id1) != 16 {
		t.Errorf("expected ID length 16, got %d", len(id1))
	}
	if id1 == id2 {
		t.Error("expected unique IDs")
	}
}
