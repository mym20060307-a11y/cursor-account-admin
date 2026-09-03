package cursor

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSplitSessionToken(t *testing.T) {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"auth0|user_XYZ"}`))
	sig := base64.RawURLEncoding.EncodeToString([]byte("sig"))
	jwt := header + "." + payload + "." + sig

	uid, gotJWT, err := splitSessionToken("user_XYZ%3A%3A" + jwt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if uid != "user_XYZ" {
		t.Fatalf("uid=%s", uid)
	}
	if gotJWT != jwt {
		t.Fatalf("jwt mismatch")
	}

	uid2, _, err := splitSessionToken("user_XYZ::" + jwt)
	if err != nil || uid2 != "user_XYZ" {
		t.Fatalf("plain :: parse failed: %v uid=%s", err, uid2)
	}

	if _, _, err := splitSessionToken("not-a-token"); err == nil {
		t.Fatal("expected error")
	}
}

func TestExtractSubFromJWT(t *testing.T) {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"auth0|user_ABC"}`))
	sig := base64.RawURLEncoding.EncodeToString([]byte("sig"))
	jwt := header + "." + payload + "." + sig
	sub, err := extractSubFromJWT(jwt)
	if err != nil {
		t.Fatal(err)
	}
	if sub != "auth0|user_ABC" {
		t.Fatalf("sub=%s", sub)
	}
}

func TestExtractUserIDFromJWT(t *testing.T) {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"auth0|user_01JYA111KEE9Q4PE54ZA6LR4SG","iat":1234567890}`))
	signature := base64.RawURLEncoding.EncodeToString([]byte("fake-signature"))
	fakeJWT := header + "." + payload + "." + signature

	userID, err := extractUserIDFromJWT(fakeJWT)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if userID != "user_01JYA111KEE9Q4PE54ZA6LR4SG" {
		t.Errorf("expected user_01JYA111KEE9Q4PE54ZA6LR4SG, got %s", userID)
	}
}

func TestExtractUserIDFromJWT_NoSeparator(t *testing.T) {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"direct_user_id"}`))
	sig := base64.RawURLEncoding.EncodeToString([]byte("sig"))
	jwt := header + "." + payload + "." + sig

	userID, err := extractUserIDFromJWT(jwt)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if userID != "direct_user_id" {
		t.Errorf("expected direct_user_id, got %s", userID)
	}
}

func TestExtractUserIDFromJWT_Invalid(t *testing.T) {
	_, err := extractUserIDFromJWT("not-a-jwt")
	if err == nil {
		t.Error("expected error for invalid JWT")
	}
}

func TestExtractUserIDFromJWT_MissingSub(t *testing.T) {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"iat":123}`))
	sig := base64.RawURLEncoding.EncodeToString([]byte("sig"))
	jwt := header + "." + payload + "." + sig

	_, err := extractUserIDFromJWT(jwt)
	if err == nil {
		t.Error("expected error for missing sub claim")
	}
}

func TestBuildSessionToken(t *testing.T) {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"auth0|user_ABC123"}`))
	sig := base64.RawURLEncoding.EncodeToString([]byte("sig"))
	jwt := header + "." + payload + "." + sig

	token, err := buildSessionToken(jwt)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}
	if !contains(token, "user_ABC123") {
		t.Errorf("token should contain user ID, got: %s", token[:min(50, len(token))])
	}
	if !contains(token, "%3A%3A") {
		t.Error("token should contain URL-encoded separator")
	}
}

func TestReadTokenFromStorageJSON(t *testing.T) {
	tmpDir := t.TempDir()

	storageData := map[string]interface{}{
		"cursorAuth/accessToken": "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.fake-token-data.signature",
		"other.key":              "other-value",
	}
	data, _ := json.Marshal(storageData)
	os.WriteFile(filepath.Join(tmpDir, "storage.json"), data, 0644)

	token, err := readTokenFromStorageJSON(tmpDir)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if token != "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.fake-token-data.signature" {
		t.Errorf("unexpected token: %s", token)
	}
}

func TestReadTokenFromStorageJSON_NoToken(t *testing.T) {
	tmpDir := t.TempDir()

	storageData := map[string]interface{}{"other.key": "value"}
	data, _ := json.Marshal(storageData)
	os.WriteFile(filepath.Join(tmpDir, "storage.json"), data, 0644)

	_, err := readTokenFromStorageJSON(tmpDir)
	if err == nil {
		t.Error("expected error when no token found")
	}
}

func TestReadTokenFromStorageJSON_NoFile(t *testing.T) {
	_, err := readTokenFromStorageJSON(t.TempDir())
	if err == nil {
		t.Error("expected error when file doesn't exist")
	}
}

func TestGetCursorGlobalStorageDir(t *testing.T) {
	dir, err := getCursorGlobalStorageDir()
	if err != nil {
		t.Logf("Cursor not installed (expected in CI): %v", err)
		return
	}
	if dir == "" {
		t.Error("expected non-empty directory path")
	}
	t.Logf("Found Cursor data dir: %s", dir)
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
