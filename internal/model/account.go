package model

import "time"

// Account represents a Cursor account with its credentials and usage data.
type Account struct {
	ID           string     `json:"id"`
	Email        string     `json:"email"`
	Password     string     `json:"password"`
	Group        string     `json:"group,omitempty"`         // Department/group name for organization
	SessionToken string     `json:"session_token,omitempty"` // WorkosCursorSessionToken value
	Usage        *UsageInfo `json:"usage,omitempty"`
	Status       string     `json:"status"` // "active", "error", "unknown"
	ErrorMsg     string     `json:"error_msg,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// UsageInfo holds usage statistics fetched from the Cursor dashboard.
// Supports both dollar-based (Team Plan) and request-based (Free/Pro) formats.
type UsageInfo struct {
	// Numeric fields for progress bar (cents for dollar plans, counts for request plans)
	PremiumUsed  int `json:"premium_used"`
	PremiumTotal int `json:"premium_total"`
	BasicUsed    int `json:"basic_used"`
	BasicTotal   int `json:"basic_total"`

	// Token totals from aggregated usage events
	TokenInput      int64   `json:"token_input,omitempty"`       // 新增输入
	TokenOutput     int64   `json:"token_output,omitempty"`      // Output
	TokenCacheWrite int64   `json:"token_cache_write,omitempty"` // 创建(缓存写入)
	TokenCache      int64   `json:"token_cache,omitempty"`       // 命中(缓存读取)
	TokenUsed       int64   `json:"token_used,omitempty"`        // input + output
	RequestCount    int     `json:"request_count,omitempty"`     // 总请求数
	CostCents       float64 `json:"cost_cents,omitempty"`        // 总成本(美分)

	// Display strings scraped directly from the dashboard page
	UsageDisplay    string `json:"usage_display,omitempty"`     // e.g. "85% / 100%" or "150 / 500"
	TokensDisplay   string `json:"tokens_display,omitempty"`    // e.g. "Token 已用 167,621"
	OnDemandDisplay string `json:"on_demand_display,omitempty"` // e.g. "US$15.62"
	ResetDate       string `json:"reset_date,omitempty"`        // e.g. "2026年3月5日"

	Plan      string    `json:"plan"`       // e.g. "free", "pro", "team", "business"
	FetchedAt time.Time `json:"fetched_at"` // When this data was fetched
}

// UsagePercent returns the usage percentage (0-100).
func (u *UsageInfo) UsagePercent() float64 {
	if u == nil || u.PremiumTotal == 0 {
		return 0
	}
	return float64(u.PremiumUsed) / float64(u.PremiumTotal) * 100
}
