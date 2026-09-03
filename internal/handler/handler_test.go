package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"cursor-admin/internal/model"
)

// === Mock implementations ===

type mockStore struct {
	mu       sync.RWMutex
	accounts map[string]*model.Account
}

func newMockStore() *mockStore {
	return &mockStore{accounts: make(map[string]*model.Account)}
}

func (m *mockStore) List() []*model.Account {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*model.Account, 0, len(m.accounts))
	for _, a := range m.accounts {
		copy := *a
		result = append(result, &copy)
	}
	return result
}

func (m *mockStore) Get(id string) (*model.Account, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	a, ok := m.accounts[id]
	if !ok {
		return nil, fmt.Errorf("account not found: %s", id)
	}
	copy := *a
	return &copy, nil
}

func (m *mockStore) Add(account *model.Account) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if account.ID == "" {
		account.ID = fmt.Sprintf("test-%d", len(m.accounts)+1)
	}
	now := time.Now()
	account.CreatedAt = now
	account.UpdatedAt = now
	if account.Status == "" {
		account.Status = "unknown"
	}
	m.accounts[account.ID] = account
	return nil
}

func (m *mockStore) Update(account *model.Account) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.accounts[account.ID]; !ok {
		return fmt.Errorf("account not found: %s", account.ID)
	}
	account.UpdatedAt = time.Now()
	m.accounts[account.ID] = account
	return nil
}

func (m *mockStore) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.accounts[id]; !ok {
		return fmt.Errorf("account not found: %s", id)
	}
	delete(m.accounts, id)
	return nil
}

// === Helper functions ===

func setupHandler(t *testing.T) (*Handler, *mockStore) {
	t.Helper()
	store := newMockStore()
	h := New(store, "<html>test</html>")
	return h, store
}

func setupServer(t *testing.T) (*httptest.Server, *mockStore) {
	t.Helper()
	h, store := setupHandler(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	return httptest.NewServer(mux), store
}

func addTestAccount(store *mockStore, email, password string) *model.Account {
	acc := &model.Account{Email: email, Password: password}
	store.Add(acc)
	return acc
}

// === Tests ===

func TestServeIndex(t *testing.T) {
	server, _ := setupServer(t)
	defer server.Close()

	resp, err := http.Get(server.URL + "/")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("expected Content-Type text/html, got '%s'", ct)
	}
}

func TestListAccounts_Empty(t *testing.T) {
	server, _ := setupServer(t)
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/accounts")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var accounts []interface{}
	json.NewDecoder(resp.Body).Decode(&accounts)
	if len(accounts) != 0 {
		t.Errorf("expected empty list, got %d accounts", len(accounts))
	}
}

func TestListAccounts_WithData(t *testing.T) {
	server, store := setupServer(t)
	defer server.Close()

	addTestAccount(store, "test@example.com", "password123")

	resp, err := http.Get(server.URL + "/api/accounts")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var accounts []map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&accounts)
	if len(accounts) != 1 {
		t.Fatalf("expected 1 account, got %d", len(accounts))
	}
	if accounts[0]["email"] != "test@example.com" {
		t.Errorf("expected email 'test@example.com', got '%v'", accounts[0]["email"])
	}
	// Password should be masked
	password := accounts[0]["password"].(string)
	if password == "password123" {
		t.Error("expected password to be masked")
	}
}

func TestAddAccount_Success(t *testing.T) {
	server, _ := setupServer(t)
	defer server.Close()

	body := bytes.NewBufferString(`{"email":"new@example.com","password":"newpass"}`)
	resp, err := http.Post(server.URL+"/api/accounts", "application/json", body)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Errorf("expected status 201, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	if result["email"] != "new@example.com" {
		t.Errorf("expected email 'new@example.com', got '%v'", result["email"])
	}
}

func TestAddAccount_MissingEmail(t *testing.T) {
	server, _ := setupServer(t)
	defer server.Close()

	body := bytes.NewBufferString(`{"email":"","password":"pass"}`)
	resp, err := http.Post(server.URL+"/api/accounts", "application/json", body)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestAddAccount_InvalidJSON(t *testing.T) {
	server, _ := setupServer(t)
	defer server.Close()

	body := bytes.NewBufferString(`not json`)
	resp, err := http.Post(server.URL+"/api/accounts", "application/json", body)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestImportAccounts_Success(t *testing.T) {
	server, _ := setupServer(t)
	defer server.Close()

	body := bytes.NewBufferString(`{
		"accounts": [
			{"email": "a@test.com", "password": "pass1"},
			{"email": "b@test.com", "password": "pass2"}
		]
	}`)
	resp, err := http.Post(server.URL+"/api/accounts/import", "application/json", body)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	added := result["added"].([]interface{})
	if len(added) != 2 {
		t.Errorf("expected 2 accounts added, got %d", len(added))
	}
}

func TestImportAccounts_Empty(t *testing.T) {
	server, _ := setupServer(t)
	defer server.Close()

	body := bytes.NewBufferString(`{"accounts": []}`)
	resp, err := http.Post(server.URL+"/api/accounts/import", "application/json", body)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestDeleteAccount_Success(t *testing.T) {
	server, store := setupServer(t)
	defer server.Close()

	acc := addTestAccount(store, "delete@example.com", "pass")

	req, _ := http.NewRequest("DELETE", server.URL+"/api/accounts/"+acc.ID, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
}

func TestDeleteAccount_NotFound(t *testing.T) {
	server, _ := setupServer(t)
	defer server.Close()

	req, _ := http.NewRequest("DELETE", server.URL+"/api/accounts/nonexistent", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", resp.StatusCode)
	}
}

func TestUpdateAccount_Success(t *testing.T) {
	server, store := setupServer(t)
	defer server.Close()

	acc := addTestAccount(store, "old@example.com", "oldpass")

	body := bytes.NewBufferString(`{"email":"updated@example.com"}`)
	req, _ := http.NewRequest("PUT", server.URL+"/api/accounts/"+acc.ID, body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	if result["email"] != "updated@example.com" {
		t.Errorf("expected updated email, got '%v'", result["email"])
	}
}

func TestUpdateAccount_NotFound(t *testing.T) {
	server, _ := setupServer(t)
	defer server.Close()

	body := bytes.NewBufferString(`{"email":"new@example.com"}`)
	req, _ := http.NewRequest("PUT", server.URL+"/api/accounts/nonexistent", body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", resp.StatusCode)
	}
}

func addTestAccountWithGroup(store *mockStore, email, password, group string) *model.Account {
	acc := &model.Account{Email: email, Password: password, Group: group}
	store.Add(acc)
	return acc
}

func TestAddAccount_WithGroup(t *testing.T) {
	server, _ := setupServer(t)
	defer server.Close()

	body := bytes.NewBufferString(`{"email":"dev@example.com","password":"pass","group":"golang开发组"}`)
	resp, err := http.Post(server.URL+"/api/accounts", "application/json", body)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Errorf("expected status 201, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	if result["group"] != "golang开发组" {
		t.Errorf("expected group 'golang开发组', got '%v'", result["group"])
	}
}

func TestUpdateAccount_WithGroup(t *testing.T) {
	server, store := setupServer(t)
	defer server.Close()

	acc := addTestAccountWithGroup(store, "test@example.com", "pass", "前端组")

	body := bytes.NewBufferString(`{"group":"算法组"}`)
	req, _ := http.NewRequest("PUT", server.URL+"/api/accounts/"+acc.ID, body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	if result["group"] != "算法组" {
		t.Errorf("expected group '算法组', got '%v'", result["group"])
	}
}

func TestUpdateAccount_ClearGroup(t *testing.T) {
	server, store := setupServer(t)
	defer server.Close()

	acc := addTestAccountWithGroup(store, "test@example.com", "pass", "测试组")

	body := bytes.NewBufferString(`{"group":""}`)
	req, _ := http.NewRequest("PUT", server.URL+"/api/accounts/"+acc.ID, body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	if result["group"] != "" {
		t.Errorf("expected empty group, got '%v'", result["group"])
	}
}

func TestImportAccounts_WithGroup(t *testing.T) {
	server, _ := setupServer(t)
	defer server.Close()

	body := bytes.NewBufferString(`{
		"accounts": [
			{"email": "a@test.com", "password": "pass1", "group": "java组"},
			{"email": "b@test.com", "password": "pass2", "group": "java组"}
		]
	}`)
	resp, err := http.Post(server.URL+"/api/accounts/import", "application/json", body)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	added := result["added"].([]interface{})
	if len(added) != 2 {
		t.Fatalf("expected 2 accounts, got %d", len(added))
	}
	first := added[0].(map[string]interface{})
	if first["group"] != "java组" {
		t.Errorf("expected group 'java组', got '%v'", first["group"])
	}
}

func TestListGroups(t *testing.T) {
	server, store := setupServer(t)
	defer server.Close()

	addTestAccountWithGroup(store, "a@test.com", "p", "golang开发组")
	addTestAccountWithGroup(store, "b@test.com", "p", "golang开发组")
	addTestAccountWithGroup(store, "c@test.com", "p", "算法组")
	addTestAccountWithGroup(store, "d@test.com", "p", "")

	resp, err := http.Get(server.URL + "/api/groups")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var groups []map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&groups)

	if len(groups) != 3 {
		t.Fatalf("expected 3 groups, got %d", len(groups))
	}

	// Should be sorted: golang开发组, 算法组, 未分组 (未分组 last)
	last := groups[len(groups)-1]
	if last["name"] != "未分组" {
		t.Errorf("expected '未分组' to be last, got '%v'", last["name"])
	}
}

func TestListGroups_Empty(t *testing.T) {
	server, _ := setupServer(t)
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/groups")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var groups []map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&groups)

	// No accounts = no groups, result is null/empty
	if len(groups) != 0 {
		t.Errorf("expected 0 groups for empty store, got %d", len(groups))
	}
}

func TestListAccounts_IncludesGroup(t *testing.T) {
	server, store := setupServer(t)
	defer server.Close()

	addTestAccountWithGroup(store, "test@example.com", "pass", "3D组")

	resp, err := http.Get(server.URL + "/api/accounts")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var accounts []map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&accounts)
	if len(accounts) != 1 {
		t.Fatalf("expected 1 account, got %d", len(accounts))
	}
	if accounts[0]["group"] != "3D组" {
		t.Errorf("expected group '3D组', got '%v'", accounts[0]["group"])
	}
}

func TestMaskPassword(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", "***"},
		{"ab", "***"},
		{"abc", "***"},
		{"abcd", "ab*d"},
		{"password123", "pa********3"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := maskPassword(tt.input)
			if result != tt.expected {
				t.Errorf("maskPassword(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}
