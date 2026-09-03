package handler

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"cursor-account-admin/internal/cursor"
	"cursor-account-admin/internal/model"
)

// AccountStore defines the interface for account storage operations.
type AccountStore interface {
	List() []*model.Account
	Get(id string) (*model.Account, error)
	Add(account *model.Account) error
	Update(account *model.Account) error
	Delete(id string) error
}

// Handler handles all HTTP requests for the application.
type Handler struct {
	store     AccountStore
	indexHTML string
}

// New creates a new Handler with the given dependencies.
func New(store AccountStore, indexHTML string) *Handler {
	return &Handler{
		store:     store,
		indexHTML: indexHTML,
	}
}

// RegisterRoutes registers all HTTP routes on the given ServeMux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", h.serveIndex)
	mux.HandleFunc("GET /api/accounts", h.listAccounts)
	mux.HandleFunc("POST /api/accounts", h.addAccount)
	mux.HandleFunc("POST /api/accounts/import", h.importAccounts)
	mux.HandleFunc("PUT /api/accounts/{id}", h.updateAccount)
	mux.HandleFunc("DELETE /api/accounts/{id}", h.deleteAccount)
	mux.HandleFunc("POST /api/accounts/{id}/browser-login", h.browserLogin)
	mux.HandleFunc("POST /api/accounts/{id}/local-login", h.localLogin)
	mux.HandleFunc("POST /api/accounts/{id}/refresh", h.refreshAccount)
	mux.HandleFunc("POST /api/login-all", h.loginAll)
	mux.HandleFunc("POST /api/refresh-all", h.refreshAll)
	mux.HandleFunc("GET /api/groups", h.listGroups)
}

// serveIndex serves the main dashboard page.
func (h *Handler) serveIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(h.indexHTML))
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// accountToResponse converts an Account to a response map, masking sensitive fields.
func accountToResponse(a *model.Account) map[string]interface{} {
	m := map[string]interface{}{
		"id":         a.ID,
		"email":      a.Email,
		"password":   maskPassword(a.Password),
		"group":      a.Group,
		"status":     a.Status,
		"created_at": a.CreatedAt,
		"updated_at": a.UpdatedAt,
	}
	if a.Usage != nil {
		m["usage"] = a.Usage
	}
	if a.ErrorMsg != "" {
		m["error_msg"] = a.ErrorMsg
	}
	return m
}

// maskPassword masks a password for display.
func maskPassword(p string) string {
	if len(p) <= 3 {
		return "***"
	}
	return p[:2] + strings.Repeat("*", len(p)-3) + p[len(p)-1:]
}

// listAccounts returns all accounts as JSON.
func (h *Handler) listAccounts(w http.ResponseWriter, r *http.Request) {
	accounts := h.store.List()
	result := make([]map[string]interface{}, len(accounts))
	for i, a := range accounts {
		result[i] = accountToResponse(a)
	}
	writeJSON(w, http.StatusOK, result)
}

// addAccount creates a new account.
func (h *Handler) addAccount(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Group    string `json:"group"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if input.Email == "" {
		writeError(w, http.StatusBadRequest, "email is required")
		return
	}

	account := &model.Account{
		Email:    strings.TrimSpace(input.Email),
		Password: input.Password,
		Group:    strings.TrimSpace(input.Group),
		Status:   "unknown",
	}

	if err := h.store.Add(account); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, accountToResponse(account))
}

// importAccounts creates multiple accounts from a bulk import.
func (h *Handler) importAccounts(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Accounts []struct {
			Email    string `json:"email"`
			Password string `json:"password"`
			Group    string `json:"group"`
		} `json:"accounts"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(input.Accounts) == 0 {
		writeError(w, http.StatusBadRequest, "no accounts provided")
		return
	}

	var added []map[string]interface{}
	var errors []string

	for _, acc := range input.Accounts {
		if acc.Email == "" {
			errors = append(errors, "skipped entry with empty email")
			continue
		}
		account := &model.Account{
			Email:    strings.TrimSpace(acc.Email),
			Password: acc.Password,
			Group:    strings.TrimSpace(acc.Group),
			Status:   "unknown",
		}
		if err := h.store.Add(account); err != nil {
			errors = append(errors, "failed to add "+acc.Email+": "+err.Error())
			continue
		}
		added = append(added, accountToResponse(account))
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"added":  added,
		"errors": errors,
	})
}

// updateAccount modifies an existing account.
func (h *Handler) updateAccount(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	account, err := h.store.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	var input struct {
		Email    string  `json:"email"`
		Password string  `json:"password"`
		Group    *string `json:"group"` // pointer to distinguish between "" and not-provided
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if input.Email != "" {
		account.Email = strings.TrimSpace(input.Email)
	}
	if input.Password != "" {
		account.Password = input.Password
	}
	if input.Group != nil {
		account.Group = strings.TrimSpace(*input.Group)
	}

	if err := h.store.Update(account); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, accountToResponse(account))
}

// deleteAccount removes an account by ID.
func (h *Handler) deleteAccount(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if err := h.store.Delete(id); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "account deleted"})
}

// localLogin signs in an account using the best available credential, then
// writes that session into the local Cursor client:
// 1) saved SessionToken  2) matching local Cursor session  3) browser + password
func (h *Handler) localLogin(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	account, err := h.store.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "账户不存在")
		return
	}

	targetEmail := strings.TrimSpace(account.Email)

	// 1) Already have saved login session → use it directly
	if tok := strings.TrimSpace(account.SessionToken); tok != "" {
		log.Printf("[handler] 使用已保存 Token 登录: %s", targetEmail)
		if err := h.applyLocalToken(account, tok); err != nil {
			log.Printf("[handler] 已保存 Token 失效 (%v)，尝试其它方式: %s", err, targetEmail)
		} else {
			h.writeLoginApplied(w, account, "saved")
			return
		}
	}

	// 2) Local Cursor currently logged in as this email
	local, lerr := cursor.ExtractLocalAccount()
	if lerr == nil {
		localEmail := strings.TrimSpace(local.Email)
		if strings.EqualFold(localEmail, targetEmail) {
			log.Printf("[handler] 本机 Cursor 登录: %s", targetEmail)
			if err := h.applyLocalToken(account, local.SessionToken); err != nil {
				writeError(w, http.StatusBadGateway, err.Error())
				return
			}
			h.writeLoginApplied(w, account, "local")
			return
		}
		log.Printf("[handler] 本机是 %s，目标是 %s", localEmail, targetEmail)
	} else {
		log.Printf("[handler] 本机 Cursor 不可用: %v", lerr)
	}

	// 3) Browser login needs password — most imported accounts have none
	if strings.TrimSpace(account.Password) == "" {
		writeError(w, http.StatusBadRequest,
			"该账号没有可用登录态：已存 Token 失效，本机 Cursor 也不是此邮箱，且未保存密码。请编辑填入密码后再点登录，或在 Cursor 客户端先登录该账号后点「同步本机 Cursor」")
		return
	}

	if err := h.runBrowserLogin(account); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.writeLoginApplied(w, account, "browser")
}

// writeLoginApplied refreshes platform usage (already done) then writes the
// session into local Cursor so the IDE switches to this account.
func (h *Handler) writeLoginApplied(w http.ResponseWriter, account *model.Account, mode string) {
	applied, err := cursor.ApplyToLocalCursor(account.Email, account.SessionToken)
	if err != nil {
		writeError(w, http.StatusInternalServerError,
			"平台用量已刷新，但写入本机 Cursor 失败: "+err.Error())
		return
	}

	msg := "已登录并写入本机 Cursor（" + account.Email + "）"
	if applied != nil && applied.RestartedCursor {
		if applied.StoppedCursor {
			msg += "。已关闭并自动重新打开 Cursor"
		} else {
			msg += "。已自动打开 Cursor"
		}
	} else if applied != nil && applied.RestartError != "" {
		if applied.StoppedCursor {
			msg += "。已关闭 Cursor，但自动重启失败（" + applied.RestartError + "），请手动打开"
		} else {
			msg += "。自动打开 Cursor 失败（" + applied.RestartError + "），请手动打开"
		}
	} else if applied != nil && applied.StoppedCursor {
		msg += "。已关闭 Cursor，请手动重新打开"
	} else {
		msg += "。请打开 Cursor 客户端使用该账号"
	}

	localInfo := map[string]interface{}{
		"email":             account.Email,
		"stopped_cursor":    applied != nil && applied.StoppedCursor,
		"restarted_cursor":  applied != nil && applied.RestartedCursor,
	}
	if applied != nil && applied.RestartError != "" {
		localInfo["restart_error"] = applied.RestartError
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"mode":    mode,
		"message": msg,
		"account": accountToResponse(account),
		"local":   localInfo,
	})
}

// browserLogin opens a browser window for the user to log in and get usage data.
func (h *Handler) browserLogin(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	account, err := h.store.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "账户不存在")
		return
	}

	if err := h.runBrowserLogin(account); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, accountToResponse(account))
}

func (h *Handler) runBrowserLogin(account *model.Account) error {
	log.Printf("[handler] 启动浏览器登录: %s", account.Email)

	result, err := cursor.BrowserLogin(account.Email, account.Password)
	if err != nil {
		return err
	}

	account.SessionToken = result.Cookies
	account.UpdatedAt = time.Now()
	account.Status = "active"
	account.ErrorMsg = ""

	if result.Usage != nil {
		account.Usage = result.Usage
		log.Printf("[handler] 用量获取成功: %s", account.Email)
	} else {
		account.ErrorMsg = "登录成功，但未能获取用量数据"
	}

	if err := h.store.Update(account); err != nil {
		return err
	}
	return nil
}

// loginAll syncs the currently logged-in local Cursor account into this platform
// (one machine = one Cursor session). Matching email is updated; otherwise added.
func (h *Handler) loginAll(w http.ResponseWriter, r *http.Request) {
	local, err := cursor.ExtractLocalAccount()
	if err != nil {
		writeError(w, http.StatusBadRequest, "读取本机 Cursor 失败: "+err.Error())
		return
	}

	localEmail := strings.TrimSpace(local.Email)
	log.Printf("[handler] 同步本机 Cursor: %s", localEmail)

	var matched *model.Account
	for _, acc := range h.store.List() {
		if strings.EqualFold(strings.TrimSpace(acc.Email), localEmail) {
			matched = acc
			break
		}
	}

	if matched == nil {
		matched = &model.Account{
			Email:        localEmail,
			SessionToken: local.SessionToken,
			Status:       "active",
			Group:        "本地Cursor",
		}
		usage, uerr := cursor.FetchUsage(local.SessionToken)
		if uerr != nil {
			matched.ErrorMsg = "已写入 Token，用量获取失败: " + uerr.Error()
			matched.Status = "error"
		} else {
			matched.Usage = usage
		}
		matched.UpdatedAt = time.Now()
		if err := h.store.Add(matched); err != nil {
			writeError(w, http.StatusInternalServerError, "添加失败: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"message": "已从本机 Cursor 新增账号 " + localEmail,
			"account": accountToResponse(matched),
		})
		return
	}

	if err := h.applyLocalToken(matched, local.SessionToken); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message": "已用本机 Cursor 同步账号 " + localEmail,
		"account": accountToResponse(matched),
	})
}

// applyLocalToken writes session token from local Cursor and refreshes usage.
func (h *Handler) applyLocalToken(account *model.Account, sessionToken string) error {
	account.SessionToken = strings.TrimSpace(sessionToken)
	account.UpdatedAt = time.Now()
	account.Status = "active"
	account.ErrorMsg = ""
	if account.Group == "" {
		account.Group = "本地Cursor"
	}

	usage, err := cursor.FetchUsage(account.SessionToken)
	if err != nil {
		account.Status = "error"
		account.ErrorMsg = "Token 已写入，用量获取失败: " + err.Error()
		_ = h.store.Update(account)
		return err
	}
	account.Usage = usage
	if err := h.store.Update(account); err != nil {
		return err
	}
	log.Printf("[handler] 本机 Token 已应用并刷新用量: %s", account.Email)
	return nil
}

// refreshAccount refreshes usage for one account via Cursor usage API + token.
func (h *Handler) refreshAccount(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	account, err := h.store.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "账户不存在")
		return
	}
	if strings.TrimSpace(account.SessionToken) == "" {
		writeError(w, http.StatusBadRequest, "该账户没有 Session Token，无法刷新用量")
		return
	}

	usage, err := cursor.FetchUsage(account.SessionToken)
	if err != nil {
		account.Status = "error"
		account.ErrorMsg = "刷新用量失败: " + err.Error()
		account.UpdatedAt = time.Now()
		_ = h.store.Update(account)
		writeError(w, http.StatusBadGateway, account.ErrorMsg)
		return
	}

	account.Usage = usage
	account.Status = "active"
	account.ErrorMsg = ""
	account.UpdatedAt = time.Now()
	if err := h.store.Update(account); err != nil {
		writeError(w, http.StatusInternalServerError, "保存失败: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, accountToResponse(account))
}

// refreshAll refreshes usage for all accounts that have a session token.
func (h *Handler) refreshAll(w http.ResponseWriter, r *http.Request) {
	accounts := h.store.List()
	results := make([]map[string]interface{}, 0, len(accounts))
	var errors []string

	for i, account := range accounts {
		if strings.TrimSpace(account.SessionToken) == "" {
			errors = append(errors, account.Email+": 无 Session Token")
			results = append(results, accountToResponse(account))
			continue
		}
		log.Printf("[handler] 刷新用量 (%d/%d): %s", i+1, len(accounts), account.Email)
		usage, err := cursor.FetchUsage(account.SessionToken)
		if err != nil {
			account.Status = "error"
			account.ErrorMsg = "刷新用量失败: " + err.Error()
			errors = append(errors, account.Email+": "+err.Error())
		} else {
			account.Usage = usage
			account.Status = "active"
			account.ErrorMsg = ""
		}
		account.UpdatedAt = time.Now()
		if err := h.store.Update(account); err != nil {
			log.Printf("[handler] 保存账户失败 %s: %v", account.ID, err)
			errors = append(errors, account.Email+": 保存失败")
		}
		results = append(results, accountToResponse(account))
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"accounts": results,
		"errors":   errors,
	})
}

// listGroups returns all unique group names with account counts.
func (h *Handler) listGroups(w http.ResponseWriter, r *http.Request) {
	accounts := h.store.List()
	groupMap := make(map[string]int)
	for _, a := range accounts {
		g := a.Group
		if g == "" {
			g = "未分组"
		}
		groupMap[g]++
	}

	type groupInfo struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	var groups []groupInfo
	for name, count := range groupMap {
		groups = append(groups, groupInfo{Name: name, Count: count})
	}

	// Sort: "未分组" last, then alphabetical
	for i := 0; i < len(groups); i++ {
		for j := i + 1; j < len(groups); j++ {
			swap := false
			if groups[i].Name == "未分组" {
				swap = true
			} else if groups[j].Name != "未分组" && groups[i].Name > groups[j].Name {
				swap = true
			}
			if swap {
				groups[i], groups[j] = groups[j], groups[i]
			}
		}
	}

	writeJSON(w, http.StatusOK, groups)
}
