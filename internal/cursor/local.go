package cursor

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// LocalAccount is the currently logged-in Cursor account on this machine.
type LocalAccount struct {
	Email        string
	SessionToken string
}

// ExtractLocalAccount reads the current Cursor account (email + session token)
// from the local Cursor installation.
func ExtractLocalAccount() (*LocalAccount, error) {
	cursorDir, err := getCursorGlobalStorageDir()
	if err != nil {
		return nil, fmt.Errorf("cannot find Cursor data directory: %w", err)
	}

	email, jwtToken, err := readAuthFromSQLite(cursorDir)
	if err != nil {
		// Fallback: storage.json + sqlite3 CLI (legacy paths)
		jwtToken, err = readTokenFromStorageJSON(cursorDir)
		if err != nil {
			jwtToken, err = readTokenFromSQLiteCLI(cursorDir)
			if err != nil {
				return nil, fmt.Errorf(
					"could not extract account from local Cursor installation at %s — "+
						"make sure Cursor is installed and you are logged in: %w", cursorDir, err)
			}
		}
		email, _ = readEmailFromStorageJSON(cursorDir)
	}

	if jwtToken == "" {
		return nil, fmt.Errorf("empty access token from local Cursor")
	}

	sessionToken, err := buildSessionToken(jwtToken)
	if err != nil {
		return nil, err
	}

	email = strings.TrimSpace(email)
	if email == "" {
		// Fallback placeholder so the account can still be stored/updated by token identity
		userID, idErr := extractUserIDFromJWT(jwtToken)
		if idErr != nil || userID == "" {
			return nil, fmt.Errorf("local Cursor has no email and JWT has no user id")
		}
		email = userID + "@local.cursor"
	}

	return &LocalAccount{
		Email:        email,
		SessionToken: sessionToken,
	}, nil
}

// ExtractLocalToken reads the Cursor access token from the local Cursor
// installation and converts it to a WorkosCursorSessionToken format.
// It tries storage.json first, then falls back to sqlite3 CLI.
func ExtractLocalToken() (string, error) {
	acc, err := ExtractLocalAccount()
	if err != nil {
		return "", err
	}
	return acc.SessionToken, nil
}

// ApplyLocalResult describes writing a session into the local Cursor install.
type ApplyLocalResult struct {
	Email           string
	StoppedCursor   bool
	RestartedCursor bool
	RestartError    string // non-empty if write succeeded but relaunch failed
}

// ApplyToLocalCursor writes email + access JWT into the local Cursor
// state.vscdb (and storage.json if present). If Cursor is running it is
// stopped first so the DB is not locked, then Cursor is relaunched.
func ApplyToLocalCursor(email, sessionToken string) (*ApplyLocalResult, error) {
	email = strings.TrimSpace(email)
	sessionToken = strings.TrimSpace(sessionToken)
	if email == "" {
		return nil, fmt.Errorf("邮箱为空")
	}
	if sessionToken == "" {
		return nil, fmt.Errorf("Session Token 为空")
	}

	userID, accessJWT, err := splitSessionToken(sessionToken)
	if err != nil {
		return nil, err
	}

	cursorDir, err := getCursorGlobalStorageDir()
	if err != nil {
		return nil, err
	}

	// Resolve exe before kill so we can relaunch the same install.
	exePath, _ := resolveCursorExecutable()

	stopped, err := stopCursorProcesses()
	if err != nil {
		return nil, fmt.Errorf("无法关闭正在运行的 Cursor: %w", err)
	}
	if stopped {
		// Allow file locks to release
		sleepMillis(1500)
	}

	authUserID := userID
	if sub, subErr := extractSubFromJWT(accessJWT); subErr == nil && sub != "" {
		authUserID = sub
	} else if !strings.Contains(userID, "|") {
		authUserID = "auth0|" + userID
	}

	if err := writeAuthToSQLite(cursorDir, email, accessJWT, authUserID); err != nil {
		return nil, err
	}
	_ = writeAuthToStorageJSON(cursorDir, email, accessJWT, authUserID)

	result := &ApplyLocalResult{Email: email, StoppedCursor: stopped}
	if exePath == "" {
		exePath, _ = resolveCursorExecutable()
	}
	if err := startCursorProcess(exePath); err != nil {
		result.RestartError = err.Error()
	} else {
		result.RestartedCursor = true
	}
	return result, nil
}

// splitSessionToken parses WorkosCursorSessionToken into userID + JWT.
// Accepts "userId%3A%3Ajwt" or "userId::jwt".
func splitSessionToken(sessionToken string) (userID, jwt string, err error) {
	raw := strings.TrimSpace(sessionToken)
	if decoded, decErr := url.QueryUnescape(raw); decErr == nil {
		raw = decoded
	}
	parts := strings.SplitN(raw, "::", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", "", fmt.Errorf("无效的 Session Token 格式（需要 userId::jwt）")
	}
	userID = strings.TrimSpace(parts[0])
	jwt = strings.TrimSpace(parts[1])
	if len(strings.Split(jwt, ".")) != 3 {
		return "", "", fmt.Errorf("Session Token 中的 JWT 无效")
	}
	return userID, jwt, nil
}

func extractSubFromJWT(jwtToken string) (string, error) {
	parts := strings.Split(jwtToken, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("invalid JWT format")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		padded := parts[1]
		for len(padded)%4 != 0 {
			padded += "="
		}
		payload, err = base64.URLEncoding.DecodeString(padded)
		if err != nil {
			return "", err
		}
	}
	var claims map[string]interface{}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", err
	}
	sub, ok := claims["sub"].(string)
	if !ok || strings.TrimSpace(sub) == "" {
		return "", fmt.Errorf("JWT has no sub")
	}
	return strings.TrimSpace(sub), nil
}

func writeAuthToSQLite(cursorDir, email, accessJWT, authUserID string) error {
	dbPath := filepath.Join(cursorDir, "state.vscdb")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return fmt.Errorf("state.vscdb 不存在: %s", dbPath)
	}

	abs, absErr := filepath.Abs(dbPath)
	if absErr != nil {
		abs = dbPath
	}
	dsn := "file:" + filepath.ToSlash(abs) + "?_pragma=busy_timeout(5000)"
	if runtime.GOOS == "windows" {
		dsn = "file:///" + filepath.ToSlash(abs) + "?_pragma=busy_timeout(5000)"
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return fmt.Errorf("打开 state.vscdb 失败: %w", err)
	}
	defer db.Close()

	pairs := map[string]string{
		"cursorAuth/accessToken":  accessJWT,
		"cursorAuth/refreshToken": accessJWT,
		"cursorAuth/cachedEmail":  email,
		"cursorAuth/userId":       authUserID,
	}
	for key, value := range pairs {
		if _, err := db.Exec(
			`INSERT INTO ItemTable (key, value) VALUES (?, ?)
			 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
			key, value,
		); err != nil {
			// Some older DBs use REPLACE semantics without ON CONFLICT target
			if _, err2 := db.Exec(`INSERT OR REPLACE INTO ItemTable (key, value) VALUES (?, ?)`, key, value); err2 != nil {
				return fmt.Errorf("写入 %s 失败: %v / %v", key, err, err2)
			}
		}
	}
	return nil
}

func writeAuthToStorageJSON(cursorDir, email, accessJWT, authUserID string) error {
	storagePath := filepath.Join(cursorDir, "storage.json")
	data, err := os.ReadFile(storagePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var storage map[string]interface{}
	if err := json.Unmarshal(data, &storage); err != nil {
		return err
	}
	storage["cursorAuth/accessToken"] = accessJWT
	storage["cursorAuth/refreshToken"] = accessJWT
	storage["cursorAuth/cachedEmail"] = email
	storage["cursorAuth/userId"] = authUserID
	out, err := json.MarshalIndent(storage, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(storagePath, out, 0644)
}

func stopCursorProcesses() (stopped bool, err error) {
	switch runtime.GOOS {
	case "windows":
		cmd := exec.Command("taskkill", "/F", "/IM", "Cursor.exe")
		out, runErr := cmd.CombinedOutput()
		msg := strings.ToLower(string(out))
		if runErr != nil {
			// 128 / not found → Cursor was not running
			if strings.Contains(msg, "not found") || strings.Contains(msg, "没有找到") || strings.Contains(msg, "not running") {
				return false, nil
			}
			// taskkill sometimes returns exit 128 when no process
			if exitErr, ok := runErr.(*exec.ExitError); ok && exitErr.ExitCode() == 128 {
				return false, nil
			}
			return false, fmt.Errorf("%v: %s", runErr, strings.TrimSpace(string(out)))
		}
		return true, nil
	case "darwin", "linux":
		cmd := exec.Command("pkill", "-f", "Cursor")
		runErr := cmd.Run()
		if runErr != nil {
			if exitErr, ok := runErr.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
				return false, nil // no matching process
			}
			return false, runErr
		}
		return true, nil
	default:
		return false, nil
	}
}

// resolveCursorExecutable finds Cursor.exe / Cursor.app / cursor binary.
// Prefers the path of a currently running Cursor process when available.
func resolveCursorExecutable() (string, error) {
	if p := cursorPathFromRunningProcess(); p != "" {
		return p, nil
	}

	var candidates []string
	switch runtime.GOOS {
	case "windows":
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			candidates = append(candidates,
				filepath.Join(local, "Programs", "cursor", "Cursor.exe"),
				filepath.Join(local, "Programs", "Cursor", "Cursor.exe"),
			)
		}
		if pf := os.Getenv("ProgramFiles"); pf != "" {
			candidates = append(candidates, filepath.Join(pf, "Cursor", "Cursor.exe"))
		}
		if p, err := exec.LookPath("Cursor.exe"); err == nil {
			candidates = append(candidates, p)
		}
		if p, err := exec.LookPath("cursor"); err == nil {
			// cursor.cmd lives at <install>/resources/app/bin/cursor.cmd
			candidates = append(candidates,
				filepath.Clean(filepath.Join(filepath.Dir(p), "..", "..", "..", "Cursor.exe")),
				p,
			)
		}
	case "darwin":
		candidates = append(candidates, "/Applications/Cursor.app")
		if p, err := exec.LookPath("cursor"); err == nil {
			candidates = append(candidates, p)
		}
	default:
		if p, err := exec.LookPath("cursor"); err == nil {
			candidates = append(candidates, p)
		}
		if p, err := exec.LookPath("Cursor"); err == nil {
			candidates = append(candidates, p)
		}
	}

	for _, c := range candidates {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c, nil
		}
		// macOS .app is a directory bundle
		if runtime.GOOS == "darwin" && strings.HasSuffix(c, ".app") {
			if st, err := os.Stat(c); err == nil && st.IsDir() {
				return c, nil
			}
		}
	}
	return "", fmt.Errorf("找不到 Cursor 可执行文件")
}

func cursorPathFromRunningProcess() string {
	switch runtime.GOOS {
	case "windows":
		cmd := exec.Command("powershell", "-NoProfile", "-Command",
			`(Get-CimInstance Win32_Process -Filter "Name = 'Cursor.exe'" | Select-Object -First 1 -ExpandProperty ExecutablePath)`)
		out, err := cmd.Output()
		if err != nil {
			return ""
		}
		p := strings.TrimSpace(string(out))
		if p != "" {
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				return p
			}
		}
		return ""
	case "darwin", "linux":
		cmd := exec.Command("sh", "-c", `ps -eo command= 2>/dev/null | grep -i '[C]ursor' | head -n 1`)
		out, err := cmd.Output()
		if err != nil {
			return ""
		}
		line := strings.TrimSpace(string(out))
		if line == "" {
			return ""
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			return ""
		}
		p := fields[0]
		if _, err := os.Stat(p); err == nil {
			return p
		}
		return ""
	default:
		return ""
	}
}

// startCursorProcess launches Cursor and returns without waiting for exit.
func startCursorProcess(exePath string) error {
	exePath = strings.TrimSpace(exePath)
	if exePath == "" {
		var err error
		exePath, err = resolveCursorExecutable()
		if err != nil {
			return err
		}
	}

	switch runtime.GOOS {
	case "windows":
		// `start` returns immediately; empty title avoids treating path as title.
		cmd := exec.Command("cmd", "/C", "start", "", exePath)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("启动 Cursor 失败: %v: %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	case "darwin":
		var cmd *exec.Cmd
		if strings.HasSuffix(exePath, ".app") {
			cmd = exec.Command("open", exePath)
		} else if exePath != "" {
			cmd = exec.Command("open", exePath)
		} else {
			cmd = exec.Command("open", "-a", "Cursor")
		}
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("启动 Cursor 失败: %v: %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	default:
		cmd := exec.Command(exePath)
		cmd.Stdout = nil
		cmd.Stderr = nil
		cmd.Stdin = nil
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("启动 Cursor 失败: %w", err)
		}
		go func() { _ = cmd.Wait() }()
		return nil
	}
}

func sleepMillis(ms int) {
	time.Sleep(time.Duration(ms) * time.Millisecond)
}

// getCursorGlobalStorageDir returns the path to Cursor's globalStorage directory.
func getCursorGlobalStorageDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	var dir string
	switch runtime.GOOS {
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData == "" {
			appData = filepath.Join(home, "AppData", "Roaming")
		}
		dir = filepath.Join(appData, "Cursor", "User", "globalStorage")
	case "darwin":
		dir = filepath.Join(home, "Library", "Application Support", "Cursor", "User", "globalStorage")
	case "linux":
		dir = filepath.Join(home, ".config", "Cursor", "User", "globalStorage")
	default:
		return "", fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return "", fmt.Errorf("Cursor data directory not found: %s", dir)
	}

	return dir, nil
}

// readAuthFromSQLite reads email and accessToken from state.vscdb via pure Go SQLite.
func readAuthFromSQLite(cursorDir string) (email, accessToken string, err error) {
	dbPath := filepath.Join(cursorDir, "state.vscdb")
	if _, statErr := os.Stat(dbPath); os.IsNotExist(statErr) {
		return "", "", fmt.Errorf("state.vscdb not found")
	}

	// Read-only open; Cursor may hold a write lock while running.
	abs, absErr := filepath.Abs(dbPath)
	if absErr != nil {
		abs = dbPath
	}
	dsn := "file:" + filepath.ToSlash(abs) + "?mode=ro&_pragma=busy_timeout(3000)"
	if runtime.GOOS == "windows" {
		// modernc on Windows prefers URL-escaped absolute paths
		dsn = "file:///" + filepath.ToSlash(abs) + "?mode=ro&_pragma=busy_timeout(3000)"
	}

	db, openErr := sql.Open("sqlite", dsn)
	if openErr != nil {
		return "", "", openErr
	}
	defer db.Close()

	accessToken, err = queryItemValue(db, "cursorAuth/accessToken")
	if err != nil || accessToken == "" {
		return "", "", fmt.Errorf("cursorAuth/accessToken not found: %w", err)
	}

	email, _ = queryItemValue(db, "cursorAuth/cachedEmail")
	return email, accessToken, nil
}

func queryItemValue(db *sql.DB, key string) (string, error) {
	var value string
	err := db.QueryRow(`SELECT value FROM ItemTable WHERE key = ?`, key).Scan(&value)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(value), nil
}

// readTokenFromStorageJSON tries to read the access token from storage.json.
func readTokenFromStorageJSON(cursorDir string) (string, error) {
	storage, err := readStorageJSON(cursorDir)
	if err != nil {
		return "", err
	}

	keys := []string{
		"cursorAuth/accessToken",
		"auth.accessToken",
		"cursor.auth.token",
		"auth.token",
		"token",
		"authToken",
	}

	for _, key := range keys {
		if val, ok := storage[key]; ok {
			if token, ok := val.(string); ok && token != "" && len(token) > 20 {
				return token, nil
			}
		}
	}

	return "", fmt.Errorf("no auth token found in storage.json")
}

func readEmailFromStorageJSON(cursorDir string) (string, error) {
	storage, err := readStorageJSON(cursorDir)
	if err != nil {
		return "", err
	}
	keys := []string{
		"cursorAuth/cachedEmail",
		"cursorAuth/email",
		"auth.email",
	}
	for _, key := range keys {
		if val, ok := storage[key]; ok {
			if email, ok := val.(string); ok && strings.Contains(email, "@") {
				return strings.TrimSpace(email), nil
			}
		}
	}
	return "", fmt.Errorf("no email found in storage.json")
}

func readStorageJSON(cursorDir string) (map[string]interface{}, error) {
	storagePath := filepath.Join(cursorDir, "storage.json")
	data, err := os.ReadFile(storagePath)
	if err != nil {
		return nil, err
	}
	var storage map[string]interface{}
	if err := json.Unmarshal(data, &storage); err != nil {
		return nil, err
	}
	return storage, nil
}

// readTokenFromSQLiteCLI tries to read the access token using the sqlite3 CLI.
func readTokenFromSQLiteCLI(cursorDir string) (string, error) {
	dbPath := filepath.Join(cursorDir, "state.vscdb")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return "", fmt.Errorf("state.vscdb not found")
	}

	cmd := exec.Command("sqlite3", dbPath,
		"SELECT value FROM ItemTable WHERE key = 'cursorAuth/accessToken'")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("sqlite3 command failed: %w (is sqlite3 installed?)", err)
	}

	token := strings.TrimSpace(string(output))
	if token == "" {
		return "", fmt.Errorf("no token found in state.vscdb")
	}

	return token, nil
}

// buildSessionToken converts a JWT access token to WorkosCursorSessionToken format.
// Format: userId%3A%3AjwtToken
func buildSessionToken(jwtToken string) (string, error) {
	// Already a session token (e.g. refreshToken stored as userId%3A%3Ajwt)
	if strings.Contains(jwtToken, "%3A%3A") || strings.Contains(jwtToken, "::") {
		if decoded, err := url.QueryUnescape(jwtToken); err == nil && strings.Contains(decoded, "::") {
			// Prefer URL-encoded form for cookie use
			if strings.Contains(jwtToken, "%3A%3A") {
				return jwtToken, nil
			}
			parts := strings.SplitN(decoded, "::", 2)
			if len(parts) == 2 {
				return parts[0] + "%3A%3A" + parts[1], nil
			}
		}
	}

	userID, err := extractUserIDFromJWT(jwtToken)
	if err != nil {
		return "", fmt.Errorf("failed to parse JWT: %w", err)
	}

	sessionToken := userID + "%3A%3A" + jwtToken
	return sessionToken, nil
}

// extractUserIDFromJWT decodes a JWT and extracts the user ID from the "sub" claim.
// The sub field typically looks like "auth0|user_01JYA..." — we extract after the "|".
func extractUserIDFromJWT(jwtToken string) (string, error) {
	parts := strings.Split(jwtToken, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("invalid JWT format (expected 3 parts, got %d)", len(parts))
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		padded := parts[1]
		for len(padded)%4 != 0 {
			padded += "="
		}
		payload, err = base64.URLEncoding.DecodeString(padded)
		if err != nil {
			return "", fmt.Errorf("failed to decode JWT payload: %w", err)
		}
	}

	var claims map[string]interface{}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", fmt.Errorf("failed to parse JWT claims: %w", err)
	}

	sub, ok := claims["sub"].(string)
	if !ok || sub == "" {
		return "", fmt.Errorf("JWT has no 'sub' claim")
	}

	if idx := strings.LastIndex(sub, "|"); idx >= 0 {
		return sub[idx+1:], nil
	}

	return sub, nil
}
