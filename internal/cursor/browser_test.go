package cursor

import (
	"cursor-account-admin/internal/model"
	"testing"
)

// ============================================================
// Tests for ParseScrapeResultJSON — validating the contract
// between the browser JS scraper and the Go parsing layer.
//
// Each test simulates the JSON output that scrapeUsageJS would
// produce when run against real Cursor dashboard DOM content.
// ============================================================

// DOM Sample 1 (English locale, Team plan):
//
//	Card 1 "Your included usage": US$20 / US$20
//	Card 2 "On-Demand Usage": US$15.62 (single amount)
//	Reset: 2026年3月5日
func TestParseScrapeResult_Sample1_TeamPlan_SingleOnDemand(t *testing.T) {
	jsonStr := `{
		"premium_used": 2000,
		"premium_total": 2000,
		"usage_display": "US$20 / US$20",
		"on_demand_display": "US$15.62",
		"reset_date": "2026年3月5日",
		"plan": "team",
		"debug": "url=https://www.cursor.com/dashboard?tab=usage cards=2 body_len=500"
	}`

	usage := ParseScrapeResultJSON(jsonStr)
	if usage == nil {
		t.Fatal("expected non-nil usage")
	}

	if usage.PremiumUsed != 2000 {
		t.Errorf("PremiumUsed = %d, want 2000", usage.PremiumUsed)
	}
	if usage.PremiumTotal != 2000 {
		t.Errorf("PremiumTotal = %d, want 2000", usage.PremiumTotal)
	}
	if usage.UsageDisplay != "US$20 / US$20" {
		t.Errorf("UsageDisplay = %q, want %q", usage.UsageDisplay, "US$20 / US$20")
	}
	if usage.OnDemandDisplay != "US$15.62" {
		t.Errorf("OnDemandDisplay = %q, want %q", usage.OnDemandDisplay, "US$15.62")
	}
	if usage.ResetDate != "2026年3月5日" {
		t.Errorf("ResetDate = %q, want %q", usage.ResetDate, "2026年3月5日")
	}
	if usage.Plan != "team" {
		t.Errorf("Plan = %q, want %q", usage.Plan, "team")
	}
	pct := usage.UsagePercent()
	if pct != 100 {
		t.Errorf("UsagePercent = %.1f, want 100", pct)
	}
}

// DOM Sample 2 (English locale, Team plan, On-Demand with total):
//
//	Card 2 "On-Demand Usage (Team)": US$584.55 / US$1,800
func TestParseScrapeResult_Sample2_TeamPlan_OnDemandWithTotal(t *testing.T) {
	jsonStr := `{
		"premium_used": 2000,
		"premium_total": 2000,
		"usage_display": "US$20 / US$20",
		"on_demand_display": "US$584.55 / US$1,800",
		"reset_date": "2026年3月5日",
		"plan": "team",
		"debug": "url=https://www.cursor.com/dashboard?tab=usage cards=2 body_len=800"
	}`

	usage := ParseScrapeResultJSON(jsonStr)
	if usage == nil {
		t.Fatal("expected non-nil usage")
	}
	if usage.OnDemandDisplay != "US$584.55 / US$1,800" {
		t.Errorf("OnDemandDisplay = %q, want %q", usage.OnDemandDisplay, "US$584.55 / US$1,800")
	}
}

// Chinese locale (cursor.com/cn/): card labels in Chinese,
// dollar amounts still use "$" but may lack "US" prefix.
//
// Simulates: Strategy 1 card detected via "包含用量"
//
//	Card 1: "您的已包含用量" → .text-xl: "$20", "/ $20"
//	Card 2: "按需用量" → .text-xl: "$15.62"
func TestParseScrapeResult_ChineseLocale_DollarWithoutUS(t *testing.T) {
	jsonStr := `{
		"premium_used": 2000,
		"premium_total": 2000,
		"usage_display": "$20 / $20",
		"on_demand_display": "$15.62",
		"reset_date": "2026年3月5日",
		"plan": "team",
		"debug": "url=https://cursor.com/cn/dashboard?tab=usage cards=2 included_raw=[\"$20\",\"/ $20\"]"
	}`

	usage := ParseScrapeResultJSON(jsonStr)
	if usage == nil {
		t.Fatal("expected non-nil usage")
	}
	if usage.PremiumUsed != 2000 {
		t.Errorf("PremiumUsed = %d, want 2000", usage.PremiumUsed)
	}
	if usage.PremiumTotal != 2000 {
		t.Errorf("PremiumTotal = %d, want 2000", usage.PremiumTotal)
	}
	if usage.UsageDisplay != "$20 / $20" {
		t.Errorf("UsageDisplay = %q, want %q", usage.UsageDisplay, "$20 / $20")
	}
	if usage.OnDemandDisplay != "$15.62" {
		t.Errorf("OnDemandDisplay = %q, want %q", usage.OnDemandDisplay, "$15.62")
	}
	if usage.ResetDate != "2026年3月5日" {
		t.Errorf("ResetDate = %q, want %q", usage.ResetDate, "2026年3月5日")
	}
}

// Chinese locale: Strategy 2 fallback (card keyword didn't match,
// but .text-xl elements still have dollar amounts).
// This simulates the case where neither CN nor EN keywords matched.
func TestParseScrapeResult_Strategy2_TextXlFallback(t *testing.T) {
	// Strategy 2 collected dollars from ALL .text-xl elements
	jsonStr := `{
		"premium_used": 2000,
		"premium_total": 2000,
		"usage_display": "$20 / $20",
		"on_demand_display": "$15.62",
		"reset_date": "2026年3月5日",
		"plan": "team",
		"debug": "url=https://cursor.com/cn/dashboard?tab=usage cards=5 s2_dollars=[\"$20\",\"$20\",\"$15.62\"] body_len=8000"
	}`

	usage := ParseScrapeResultJSON(jsonStr)
	if usage == nil {
		t.Fatal("expected non-nil usage")
	}
	if usage.PremiumUsed != 2000 {
		t.Errorf("PremiumUsed = %d, want 2000", usage.PremiumUsed)
	}
	if usage.PremiumTotal != 2000 {
		t.Errorf("PremiumTotal = %d, want 2000", usage.PremiumTotal)
	}
}

// Chinese locale with On-Demand having both used and total
func TestParseScrapeResult_ChineseLocale_OnDemandWithTotal(t *testing.T) {
	jsonStr := `{
		"premium_used": 2000,
		"premium_total": 2000,
		"usage_display": "$20 / $20",
		"on_demand_display": "$584.55 / $1,800",
		"reset_date": "2026年3月5日",
		"plan": "team",
		"debug": "url=https://cursor.com/cn/dashboard?tab=usage cards=2"
	}`

	usage := ParseScrapeResultJSON(jsonStr)
	if usage == nil {
		t.Fatal("expected non-nil usage")
	}
	if usage.OnDemandDisplay != "$584.55 / $1,800" {
		t.Errorf("OnDemandDisplay = %q, want %q", usage.OnDemandDisplay, "$584.55 / $1,800")
	}
}

// Request-based plan (Free/Pro): "150 / 500"
func TestParseScrapeResult_RequestBasedPlan(t *testing.T) {
	jsonStr := `{
		"premium_used": 150,
		"premium_total": 500,
		"usage_display": "150 / 500",
		"on_demand_display": "",
		"reset_date": "",
		"plan": "pro",
		"debug": "url=https://www.cursor.com/dashboard?tab=usage cards=1"
	}`

	usage := ParseScrapeResultJSON(jsonStr)
	if usage == nil {
		t.Fatal("expected non-nil usage")
	}
	if usage.PremiumUsed != 150 {
		t.Errorf("PremiumUsed = %d, want 150", usage.PremiumUsed)
	}
	if usage.PremiumTotal != 500 {
		t.Errorf("PremiumTotal = %d, want 500", usage.PremiumTotal)
	}
	if usage.UsageDisplay != "150 / 500" {
		t.Errorf("UsageDisplay = %q, want %q", usage.UsageDisplay, "150 / 500")
	}
	if usage.OnDemandDisplay != "" {
		t.Errorf("OnDemandDisplay should be empty, got %q", usage.OnDemandDisplay)
	}
	pct := usage.UsagePercent()
	if pct != 30 {
		t.Errorf("UsagePercent = %.1f, want 30", pct)
	}
}

// JS error response
func TestParseScrapeResult_JSError(t *testing.T) {
	jsonStr := `{"error": "Cannot read property 'innerText' of null"}`
	usage := ParseScrapeResultJSON(jsonStr)
	if usage != nil {
		t.Error("expected nil for JS error")
	}
}

// Invalid JSON
func TestParseScrapeResult_InvalidJSON(t *testing.T) {
	usage := ParseScrapeResultJSON("not json at all")
	if usage != nil {
		t.Error("expected nil for invalid JSON")
	}
}

// Empty page (loading state)
func TestParseScrapeResult_EmptyPage(t *testing.T) {
	jsonStr := `{
		"premium_used": 0, "premium_total": 0,
		"usage_display": "", "on_demand_display": "", "reset_date": "", "plan": "",
		"debug": "url=https://www.cursor.com/dashboard?tab=usage cards=0 body_len=50"
	}`

	usage := ParseScrapeResultJSON(jsonStr)
	if usage == nil {
		t.Fatal("expected non-nil")
	}
	if usage.PremiumTotal != 0 {
		t.Errorf("PremiumTotal = %d, want 0", usage.PremiumTotal)
	}
}

// Large dollar amounts with commas
func TestParseScrapeResult_LargeDollarAmounts(t *testing.T) {
	jsonStr := `{
		"premium_used": 123456,
		"premium_total": 500000,
		"usage_display": "US$1,234.56 / US$5,000",
		"on_demand_display": "US$2,500.00 / US$10,000",
		"reset_date": "2026年4月1日",
		"plan": "business",
		"debug": ""
	}`

	usage := ParseScrapeResultJSON(jsonStr)
	if usage == nil {
		t.Fatal("expected non-nil usage")
	}
	if usage.PremiumUsed != 123456 {
		t.Errorf("PremiumUsed = %d, want 123456", usage.PremiumUsed)
	}
	if usage.PremiumTotal != 500000 {
		t.Errorf("PremiumTotal = %d, want 500000", usage.PremiumTotal)
	}
	if usage.OnDemandDisplay != "US$2,500.00 / US$10,000" {
		t.Errorf("OnDemandDisplay = %q, want %q", usage.OnDemandDisplay, "US$2,500.00 / US$10,000")
	}
}

// Strategy 2 fallback with 4 dollar amounts (included + on-demand)
func TestParseScrapeResult_Strategy2_FourDollars(t *testing.T) {
	// When card detection fails, Strategy 2 collects all .text-xl dollars:
	// [used, total, od_used, od_total]
	jsonStr := `{
		"premium_used": 2000,
		"premium_total": 2000,
		"usage_display": "$20 / $20",
		"on_demand_display": "$584.55 / $1,800",
		"reset_date": "2026年3月5日",
		"plan": "team",
		"debug": "url=https://cursor.com/cn/dashboard?tab=usage cards=5 s2_dollars=[\"$20\",\"$20\",\"$584.55\",\"$1,800\"]"
	}`

	usage := ParseScrapeResultJSON(jsonStr)
	if usage == nil {
		t.Fatal("expected non-nil usage")
	}
	if usage.UsageDisplay != "$20 / $20" {
		t.Errorf("UsageDisplay = %q, want %q", usage.UsageDisplay, "$20 / $20")
	}
	if usage.OnDemandDisplay != "$584.55 / $1,800" {
		t.Errorf("OnDemandDisplay = %q, want %q", usage.OnDemandDisplay, "$584.55 / $1,800")
	}
}

// ============================================================
// Free / Hobby plan tests
// ============================================================

// Free plan detected via "free plan" keyword in body, no usage numbers
func TestParseScrapeResult_FreePlan_NoUsage(t *testing.T) {
	jsonStr := `{
		"premium_used": 0,
		"premium_total": 0,
		"usage_display": "Free Plan",
		"on_demand_display": "",
		"reset_date": "",
		"plan": "free",
		"debug": "url=https://www.cursor.com/dashboard?tab=usage cards=0 body_len=300"
	}`

	usage := ParseScrapeResultJSON(jsonStr)
	if usage == nil {
		t.Fatal("expected non-nil usage")
	}
	if usage.Plan != "free" {
		t.Errorf("Plan = %q, want %q", usage.Plan, "free")
	}
	if usage.UsageDisplay != "Free Plan" {
		t.Errorf("UsageDisplay = %q, want %q", usage.UsageDisplay, "Free Plan")
	}
	if usage.PremiumTotal != 0 {
		t.Errorf("PremiumTotal = %d, want 0", usage.PremiumTotal)
	}
	if usage.PremiumUsed != 0 {
		t.Errorf("PremiumUsed = %d, want 0", usage.PremiumUsed)
	}
}

// Hobby plan (treated same as free)
func TestParseScrapeResult_HobbyPlan(t *testing.T) {
	jsonStr := `{
		"premium_used": 0,
		"premium_total": 0,
		"usage_display": "Free Plan",
		"on_demand_display": "",
		"reset_date": "",
		"plan": "hobby",
		"debug": "url=https://www.cursor.com/dashboard?tab=usage body_len=400"
	}`

	usage := ParseScrapeResultJSON(jsonStr)
	if usage == nil {
		t.Fatal("expected non-nil usage")
	}
	if usage.Plan != "hobby" {
		t.Errorf("Plan = %q, want %q", usage.Plan, "hobby")
	}
	if usage.UsageDisplay != "Free Plan" {
		t.Errorf("UsageDisplay = %q, want %q", usage.UsageDisplay, "Free Plan")
	}
}

// Free plan detected via "upgrade" keyword (no dollar amounts, no plan keyword)
func TestParseScrapeResult_FreePlan_DetectedViaUpgrade(t *testing.T) {
	jsonStr := `{
		"premium_used": 0,
		"premium_total": 0,
		"usage_display": "Free Plan",
		"on_demand_display": "",
		"reset_date": "",
		"plan": "free",
		"debug": "url=https://www.cursor.com/dashboard?tab=usage cards=1 body_len=600"
	}`

	usage := ParseScrapeResultJSON(jsonStr)
	if usage == nil {
		t.Fatal("expected non-nil usage")
	}
	if usage.Plan != "free" {
		t.Errorf("Plan = %q, want %q", usage.Plan, "free")
	}
}

// isValidUsage tests
func TestIsValidUsage(t *testing.T) {
	tests := []struct {
		name  string
		input *model.UsageInfo
		want  bool
	}{
		{"nil", nil, false},
		{"empty", &model.UsageInfo{}, false},
		{"paid with total", &model.UsageInfo{PremiumTotal: 2000, UsageDisplay: "US$20 / US$20", Plan: "team"}, true},
		{"paid with display only", &model.UsageInfo{UsageDisplay: "150 / 500"}, true},
		{"free plan", &model.UsageInfo{Plan: "free", UsageDisplay: "Free Plan"}, true},
		{"free plan no display", &model.UsageInfo{Plan: "free"}, true},
		{"hobby plan", &model.UsageInfo{Plan: "hobby"}, true},
		{"plan only no usage", &model.UsageInfo{Plan: "pro"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isValidUsage(tt.input)
			if got != tt.want {
				t.Errorf("isValidUsage() = %v, want %v", got, tt.want)
			}
		})
	}
}
