package cursor

import (
	"testing"

	"cursor-account-admin/internal/model"
)

func TestParseUsageSummaryJSON_FreePlan(t *testing.T) {
	jsonStr := `{
	  "billingCycleEnd":"2026-10-01T16:05:14.297Z",
	  "membershipType":"free",
	  "autoModelSelectedDisplayMessage":"You've used 81% of your included total usage",
	  "individualUsage":{
	    "plan":{"enabled":true,"used":0,"limit":0,"totalPercentUsed":80.5},
	    "onDemand":{"enabled":false,"used":0,"limit":null}
	  }
	}`
	usage, err := ParseUsageSummaryJSON(jsonStr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if usage.Plan != "free" {
		t.Errorf("Plan = %q, want free", usage.Plan)
	}
	if usage.PremiumTotal != 100 {
		t.Errorf("PremiumTotal = %d, want 100", usage.PremiumTotal)
	}
	if usage.PremiumUsed != 81 {
		t.Errorf("PremiumUsed = %d, want 81", usage.PremiumUsed)
	}
	if usage.UsageDisplay != "81% / 100%" {
		t.Errorf("UsageDisplay = %q, want 81%% / 100%%", usage.UsageDisplay)
	}
}

func TestParseUsageSummaryJSON_BreakdownTotal(t *testing.T) {
	jsonStr := `{
	  "membershipType":"free",
	  "individualUsage":{
	    "plan":{
	      "used":0,"limit":0,"totalPercentUsed":50,
	      "breakdown":{"included":0,"bonus":200,"total":200}
	    }
	  }
	}`
	usage, err := ParseUsageSummaryJSON(jsonStr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if usage.PremiumTotal != 200 {
		t.Errorf("PremiumTotal = %d, want 200", usage.PremiumTotal)
	}
	if usage.PremiumUsed != 100 {
		t.Errorf("PremiumUsed = %d, want 100", usage.PremiumUsed)
	}
	if usage.UsageDisplay != "100 / 200" {
		t.Errorf("UsageDisplay = %q, want 100 / 200", usage.UsageDisplay)
	}
}

func TestApplyAggregatedTokens(t *testing.T) {
	usage := &model.UsageInfo{}
	applyAggregatedTokens(usage, `{
	  "totalInputTokens":"157196",
	  "totalOutputTokens":"10425",
	  "totalCacheReadTokens":"1737728",
	  "totalCostCents": 69.3477
	}`)
	if usage.TokenInput != 157196 {
		t.Errorf("TokenInput = %d, want 157196", usage.TokenInput)
	}
	if usage.TokenOutput != 10425 {
		t.Errorf("TokenOutput = %d, want 10425", usage.TokenOutput)
	}
	if usage.TokenUsed != 167621 {
		t.Errorf("TokenUsed = %d, want 167621", usage.TokenUsed)
	}
	if usage.TokenCache != 1737728 {
		t.Errorf("TokenCache = %d, want 1737728", usage.TokenCache)
	}
	if usage.CostCents < 69.3 || usage.CostCents > 69.4 {
		t.Errorf("CostCents = %v, want ~69.35", usage.CostCents)
	}
	if usage.TokensDisplay == "" || !contains(usage.TokensDisplay, "167,621") {
		t.Errorf("TokensDisplay = %q, want contain 167,621", usage.TokensDisplay)
	}
}

func TestParseUsageSummaryJSON_PaidLimit(t *testing.T) {
	jsonStr := `{
	  "membershipType":"pro",
	  "individualUsage":{
	    "plan":{"enabled":true,"used":150,"limit":500,"totalPercentUsed":30},
	    "onDemand":{"enabled":true,"used":10,"limit":100}
	  }
	}`
	usage, err := ParseUsageSummaryJSON(jsonStr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if usage.Plan != "pro" {
		t.Errorf("Plan = %q, want pro", usage.Plan)
	}
	if usage.PremiumUsed != 150 || usage.PremiumTotal != 500 {
		t.Errorf("usage = %d/%d, want 150/500", usage.PremiumUsed, usage.PremiumTotal)
	}
	if usage.OnDemandDisplay != "10 / 100" {
		t.Errorf("OnDemandDisplay = %q, want 10 / 100", usage.OnDemandDisplay)
	}
}

func TestParseUsageJSON_Success(t *testing.T) {
	jsonStr := `{
		"gpt-4": {"numRequests": 150, "maxRequestUsage": 500},
		"gpt-3.5-turbo": {"numRequests": 20, "maxRequestUsage": 0}
	}`

	usage, err := ParseUsageJSON(jsonStr)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if usage.PremiumUsed != 150 {
		t.Errorf("PremiumUsed = %d, want 150", usage.PremiumUsed)
	}
	if usage.PremiumTotal != 500 {
		t.Errorf("PremiumTotal = %d, want 500", usage.PremiumTotal)
	}
	if usage.BasicUsed != 20 {
		t.Errorf("BasicUsed = %d, want 20", usage.BasicUsed)
	}
}

func TestParseUsageJSON_APIError(t *testing.T) {
	jsonStr := `{"error":"not_authenticated","description":"The user does not have an active session"}`

	_, err := ParseUsageJSON(jsonStr)
	if err == nil {
		t.Fatal("expected error for API error response")
	}
}

func TestParseUsageJSON_InvalidJSON(t *testing.T) {
	_, err := ParseUsageJSON("not json")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParseUsageData_CurrentFormat(t *testing.T) {
	data := map[string]interface{}{
		"gpt-4": map[string]interface{}{
			"numRequests":     float64(150),
			"maxRequestUsage": float64(500),
		},
		"gpt-3.5-turbo": map[string]interface{}{
			"numRequests":     float64(20),
			"maxRequestUsage": float64(0),
		},
	}

	usage := &model.UsageInfo{}
	parseUsageData(data, usage)

	if usage.PremiumUsed != 150 {
		t.Errorf("PremiumUsed = %d, want 150", usage.PremiumUsed)
	}
	if usage.PremiumTotal != 500 {
		t.Errorf("PremiumTotal = %d, want 500", usage.PremiumTotal)
	}
	if usage.BasicUsed != 20 {
		t.Errorf("BasicUsed = %d, want 20", usage.BasicUsed)
	}
}

func TestParseUsageData_LegacyPremiumModels(t *testing.T) {
	data := map[string]interface{}{
		"premium_models": map[string]interface{}{
			"used":  float64(100),
			"limit": float64(500),
		},
	}

	usage := &model.UsageInfo{}
	parseUsageData(data, usage)

	if usage.PremiumUsed != 100 {
		t.Errorf("PremiumUsed = %d, want 100", usage.PremiumUsed)
	}
	if usage.PremiumTotal != 500 {
		t.Errorf("PremiumTotal = %d, want 500", usage.PremiumTotal)
	}
}

func TestParseUsageData_WithPlan(t *testing.T) {
	data := map[string]interface{}{
		"plan": "pro",
	}
	usage := &model.UsageInfo{}
	parseUsageData(data, usage)
	if usage.Plan != "pro" {
		t.Errorf("Plan = %q, want 'pro'", usage.Plan)
	}
}

func TestParseUsageData_WithSubscription(t *testing.T) {
	data := map[string]interface{}{
		"subscription": map[string]interface{}{
			"plan": "business",
		},
	}
	usage := &model.UsageInfo{}
	parseUsageData(data, usage)
	if usage.Plan != "business" {
		t.Errorf("Plan = %q, want 'business'", usage.Plan)
	}
}

func TestParseUsageData_Empty(t *testing.T) {
	usage := &model.UsageInfo{}
	parseUsageData(map[string]interface{}{}, usage)
	if usage.PremiumUsed != 0 || usage.PremiumTotal != 0 {
		t.Errorf("expected zeros for empty data")
	}
}

func TestExtractUserID(t *testing.T) {
	tests := []struct {
		token    string
		expected string
	}{
		{"user_01ABC%3A%3AeyJhbG...", "user_01ABC"},
		{"user_123::jwt_token", "user_123"},
		{"invalid_token", ""},
		{"", ""},
		{"%3A%3Aonly_token", ""},
	}

	for _, tt := range tests {
		result := extractUserID(tt.token)
		if result != tt.expected {
			t.Errorf("extractUserID(%q) = %q, want %q", tt.token, result, tt.expected)
		}
	}
}

func TestExtractSessionTokenValue(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{
			"WorkosCursorSessionToken=user_01%3A%3Aabc; __cf_bm=xyz; _ga=GA1",
			"user_01%3A%3Aabc",
		},
		{
			"user_01%3A%3Aabc",
			"user_01%3A%3Aabc",
		},
		{
			"WorkosCursorSessionToken=user_02::token",
			"user_02::token",
		},
	}

	for _, tt := range tests {
		result := extractSessionTokenValue(tt.input)
		if result != tt.expected {
			t.Errorf("extractSessionTokenValue(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}
