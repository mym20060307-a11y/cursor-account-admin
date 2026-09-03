package localsync

import (
	"log"
	"strings"
	"time"

	"cursor-admin/internal/cursor"
	"cursor-admin/internal/model"
)

// usageRefreshEvery controls how often we re-query Cursor usage API for an
// already-synced local account (token scan remains at the sync interval).
const usageRefreshEvery = 30 * time.Second

// AccountStore is the subset of store operations needed for local sync.
type AccountStore interface {
	FindByEmail(email string) (*model.Account, error)
	Add(account *model.Account) error
	Update(account *model.Account) error
}

// StartLocalSync periodically reads the local Cursor account and upserts it
// into the account store. Pass interval <= 0 to disable.
func StartLocalSync(store AccountStore, interval time.Duration) {
	if interval <= 0 {
		log.Printf("[localsync] disabled (interval=%v)", interval)
		return
	}

	log.Printf("[localsync] started, interval=%s, usageRefresh=%s", interval, usageRefreshEvery)
	go func() {
		var lastSkip string
		run := func() {
			msg := syncOnce(store)
			if msg == "" {
				lastSkip = ""
				return
			}
			if msg != lastSkip {
				log.Printf("[localsync] %s", msg)
				lastSkip = msg
			}
		}
		run()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			run()
		}
	}()
}

// syncOnce upserts the local Cursor account and refreshes usage via API.
func syncOnce(store AccountStore) string {
	local, err := cursor.ExtractLocalAccount()
	if err != nil {
		return "skip: " + err.Error()
	}

	email := strings.TrimSpace(local.Email)
	token := strings.TrimSpace(local.SessionToken)
	if email == "" || token == "" {
		return "skip: empty email or token"
	}

	existing, findErr := store.FindByEmail(email)
	isNew := findErr != nil

	var acc *model.Account
	tokenChanged := false
	if isNew {
		acc = &model.Account{
			Email:        email,
			SessionToken: token,
			Status:       "active",
			Group:        "本地Cursor",
		}
	} else {
		acc = existing
		tokenChanged = acc.SessionToken != token
		if tokenChanged {
			acc.SessionToken = token
			acc.Status = "active"
			acc.ErrorMsg = ""
		}
		if acc.Group == "" {
			acc.Group = "本地Cursor"
		}
	}

	needUsage := isNew || tokenChanged || acc.Usage == nil ||
		acc.Usage.FetchedAt.IsZero() ||
		time.Since(acc.Usage.FetchedAt) >= usageRefreshEvery

	if !isNew && !tokenChanged && !needUsage {
		return ""
	}

	if needUsage {
		usage, uerr := cursor.FetchUsage(token)
		if uerr != nil {
			if isNew {
				// Still save the account even if usage fails
				if addErr := store.Add(acc); addErr != nil {
					return "add failed " + email + ": " + addErr.Error()
				}
				log.Printf("[localsync] added local account: %s (usage pending: %v)", email, uerr)
				return "usage fetch failed: " + uerr.Error()
			}
			if tokenChanged {
				if updErr := store.Update(acc); updErr != nil {
					return "update failed " + email + ": " + updErr.Error()
				}
			}
			return "usage fetch failed: " + uerr.Error()
		}
		acc.Usage = usage
		acc.Status = "active"
		acc.ErrorMsg = ""
	}

	if isNew {
		if addErr := store.Add(acc); addErr != nil {
			return "add failed " + email + ": " + addErr.Error()
		}
		log.Printf("[localsync] added local account: %s (plan=%s)", email, planOf(acc))
		return ""
	}

	if updErr := store.Update(acc); updErr != nil {
		return "update failed " + email + ": " + updErr.Error()
	}
	if tokenChanged {
		log.Printf("[localsync] updated token+usage for: %s (plan=%s)", email, planOf(acc))
	} else if needUsage {
		log.Printf("[localsync] refreshed usage for: %s (plan=%s)", email, planOf(acc))
	}
	return ""
}

func planOf(acc *model.Account) string {
	if acc != nil && acc.Usage != nil && acc.Usage.Plan != "" {
		return acc.Usage.Plan
	}
	return "-"
}
