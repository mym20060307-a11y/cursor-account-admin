package cursor

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"cursor-admin/internal/model"
)

const (
	usageSummaryURL    = "https://cursor.com/api/usage-summary"
	aggregatedUsageURL = "https://cursor.com/api/dashboard/get-aggregated-usage-events"
	filteredUsageURL   = "https://cursor.com/api/dashboard/get-filtered-usage-events"
)

// FetchUsage queries Cursor usage-summary API using a WorkosCursorSessionToken,
// and supplements with aggregated token totals when available.
func FetchUsage(sessionToken string) (*model.UsageInfo, error) {
	token := extractSessionTokenValue(strings.TrimSpace(sessionToken))
	if token == "" {
		return nil, fmt.Errorf("empty session token")
	}

	client := &http.Client{Timeout: 20 * time.Second}

	body, err := doCursorGET(client, usageSummaryURL, token)
	if err != nil {
		return nil, err
	}
	usage, err := ParseUsageSummaryJSON(string(body))
	if err != nil {
		return nil, err
	}

	if aggBody, aggErr := doCursorPOST(client, aggregatedUsageURL, token, "{}"); aggErr == nil {
		applyAggregatedTokens(usage, string(aggBody))
	}
	if evtBody, evtErr := doCursorPOST(client, filteredUsageURL, token, `{"page":1,"pageSize":1}`); evtErr == nil {
		applyRequestCount(usage, string(evtBody))
	}

	return usage, nil
}

func doCursorGET(client *http.Client, url, sessionToken string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	setCursorHeaders(req, sessionToken)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求用量失败: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取用量响应失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("用量 API 返回 %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	return body, nil
}

func doCursorPOST(client *http.Client, url, sessionToken, jsonBody string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	setCursorHeaders(req, sessionToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://cursor.com")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API 返回 %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	return body, nil
}

func setCursorHeaders(req *http.Request, sessionToken string) {
	req.Header.Set("Cookie", "WorkosCursorSessionToken="+sessionToken)
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; cursor-admin/1.0)")
	req.Header.Set("Accept", "application/json")
}

// ParseUsageSummaryJSON parses /api/usage-summary response into UsageInfo.
func ParseUsageSummaryJSON(jsonStr string) (*model.UsageInfo, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
		return nil, fmt.Errorf("JSON 解析失败: %w", err)
	}
	if errMsg, ok := raw["error"].(string); ok {
		return nil, fmt.Errorf("API 错误: %s", errMsg)
	}

	usage := &model.UsageInfo{FetchedAt: time.Now()}

	if mt, ok := raw["membershipType"].(string); ok {
		usage.Plan = strings.ToLower(mt)
	}

	indiv, _ := raw["individualUsage"].(map[string]interface{})
	if indiv != nil {
		if plan, ok := indiv["plan"].(map[string]interface{}); ok {
			used := toInt(plan["used"])
			limit := toInt(plan["limit"])
			pct := 0.0
			if v, ok := plan["totalPercentUsed"].(float64); ok {
				pct = v
			}

			if limit > 0 {
				usage.PremiumUsed = used
				usage.PremiumTotal = limit
				usage.UsageDisplay = fmt.Sprintf("%d / %d", used, limit)
			} else {
				// Free / percent-based plans: expose used%% / 100%% clearly
				pctInt := int(pct + 0.5)
				if pctInt < 0 {
					pctInt = 0
				}
				if pctInt > 100 {
					pctInt = 100
				}
				usage.PremiumUsed = pctInt
				usage.PremiumTotal = 100
				usage.UsageDisplay = fmt.Sprintf("%d%% / 100%%", pctInt)
			}

			// Prefer breakdown totals when plan used/limit are zero but bonus/total exist
			if bd, ok := plan["breakdown"].(map[string]interface{}); ok {
				total := toInt(bd["total"])
				included := toInt(bd["included"])
				bonus := toInt(bd["bonus"])
				if limit == 0 && total > 0 {
					// Approximate used from percent against breakdown total
					approxUsed := int(float64(total)*pct/100.0 + 0.5)
					usage.PremiumUsed = approxUsed
					usage.PremiumTotal = total
					usage.UsageDisplay = fmt.Sprintf("%d / %d", approxUsed, total)
					_ = included
					_ = bonus
				}
			}
		}
		if onDemand, ok := indiv["onDemand"].(map[string]interface{}); ok {
			if enabled, _ := onDemand["enabled"].(bool); enabled {
				used := toInt(onDemand["used"])
				if lim, ok := onDemand["limit"].(float64); ok && lim > 0 {
					usage.OnDemandDisplay = fmt.Sprintf("%d / %d", used, int(lim))
				} else if used > 0 {
					usage.OnDemandDisplay = fmt.Sprintf("%d", used)
				}
			}
		}
	}

	if usage.UsageDisplay == "" {
		if msg, ok := raw["autoModelSelectedDisplayMessage"].(string); ok && msg != "" {
			usage.UsageDisplay = msg
		}
	}

	if end, ok := raw["billingCycleEnd"].(string); ok && end != "" {
		if t, err := time.Parse(time.RFC3339, end); err == nil {
			usage.ResetDate = t.Local().Format("2006年1月2日")
		} else {
			usage.ResetDate = end
		}
	}

	if (usage.Plan == "free" || usage.Plan == "hobby") && usage.UsageDisplay == "" {
		usage.UsageDisplay = "0% / 100%"
		usage.PremiumUsed = 0
		usage.PremiumTotal = 100
	}

	return usage, nil
}

func applyAggregatedTokens(usage *model.UsageInfo, jsonStr string) {
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
		return
	}
	in := toInt64(raw["totalInputTokens"])
	out := toInt64(raw["totalOutputTokens"])
	cache := toInt64(raw["totalCacheReadTokens"])
	cacheWrite := toInt64(raw["totalCacheWriteTokens"])
	if cacheWrite == 0 {
		// Some responses only expose write tokens inside aggregations[].
		if arr, ok := raw["aggregations"].([]interface{}); ok {
			for _, item := range arr {
				m, ok := item.(map[string]interface{})
				if !ok {
					continue
				}
				cacheWrite += toInt64(m["cacheWriteTokens"])
			}
		}
	}
	cost := toFloat64(raw["totalCostCents"])

	total := in + out
	if total <= 0 && cache <= 0 && cacheWrite <= 0 {
		return
	}
	usage.TokenInput = in
	usage.TokenOutput = out
	usage.TokenCache = cache
	usage.TokenCacheWrite = cacheWrite
	usage.TokenUsed = total
	usage.CostCents = cost
	if cache > 0 {
		usage.TokensDisplay = fmt.Sprintf("Token 已用 %s（含缓存 %s）", formatInt(total), formatInt(cache))
	} else {
		usage.TokensDisplay = fmt.Sprintf("Token 已用 %s", formatInt(total))
	}
}

func applyRequestCount(usage *model.UsageInfo, jsonStr string) {
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
		return
	}
	if n := toInt(raw["totalUsageEventsCount"]); n > 0 {
		usage.RequestCount = n
	}
}

func toFloat64(v interface{}) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	case string:
		var f float64
		fmt.Sscan(n, &f)
		return f
	default:
		return 0
	}
}

func toInt64(v interface{}) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int:
		return int64(n)
	case int64:
		return n
	case json.Number:
		i, _ := n.Int64()
		return i
	case string:
		var i int64
		fmt.Sscan(n, &i)
		return i
	default:
		return 0
	}
}

func formatInt(n int64) string {
	if n < 0 {
		return "-" + formatInt(-n)
	}
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	rem := len(s) % 3
	if rem > 0 {
		b.WriteString(s[:rem])
		if len(s) > rem {
			b.WriteByte(',')
		}
	}
	for i := rem; i < len(s); i += 3 {
		b.WriteString(s[i : i+3])
		if i+3 < len(s) {
			b.WriteByte(',')
		}
	}
	return b.String()
}

func toInt(v interface{}) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	default:
		return 0
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// ParseUsageJSON parses raw JSON from the usage API into a UsageInfo struct.
func ParseUsageJSON(jsonStr string) (*model.UsageInfo, error) {
	var rawData map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &rawData); err != nil {
		return nil, fmt.Errorf("JSON 解析失败: %w", err)
	}
	if errMsg, ok := rawData["error"].(string); ok {
		desc, _ := rawData["description"].(string)
		return nil, fmt.Errorf("API 错误: %s — %s", errMsg, desc)
	}
	usage := &model.UsageInfo{FetchedAt: time.Now()}
	parseUsageData(rawData, usage)
	return usage, nil
}

// parseUsageData extracts usage information from the Cursor API response.
func parseUsageData(data map[string]interface{}, usage *model.UsageInfo) {
	premiumKeywords := []string{"gpt-4", "gpt-4o", "claude-3.5", "claude-3-opus", "o1", "o3"}

	for key, val := range data {
		modelData, ok := val.(map[string]interface{})
		if !ok {
			continue
		}
		numReqs, maxReqs := 0, 0
		if nr, ok := modelData["numRequests"].(float64); ok {
			numReqs = int(nr)
		}
		if mr, ok := modelData["maxRequestUsage"].(float64); ok {
			maxReqs = int(mr)
		}

		isPremium := false
		keyLower := strings.ToLower(key)
		for _, kw := range premiumKeywords {
			if strings.Contains(keyLower, kw) {
				isPremium = true
				break
			}
		}

		if isPremium {
			usage.PremiumUsed += numReqs
			if maxReqs > usage.PremiumTotal {
				usage.PremiumTotal = maxReqs
			}
		} else {
			usage.BasicUsed += numReqs
			if maxReqs > usage.BasicTotal {
				usage.BasicTotal = maxReqs
			}
		}
	}

	if usage.PremiumUsed == 0 && usage.PremiumTotal == 0 {
		for _, val := range data {
			modelData, ok := val.(map[string]interface{})
			if !ok {
				continue
			}
			if nr, ok := modelData["numRequests"].(float64); ok {
				usage.PremiumUsed += int(nr)
			}
			if mr, ok := modelData["maxRequestUsage"].(float64); ok && int(mr) > usage.PremiumTotal {
				usage.PremiumTotal = int(mr)
			}
		}
	}

	if pm, ok := data["premium_models"].(map[string]interface{}); ok {
		if used, ok := pm["used"].(float64); ok {
			usage.PremiumUsed = int(used)
		}
		if limit, ok := pm["limit"].(float64); ok {
			usage.PremiumTotal = int(limit)
		}
	}

	if nr, ok := data["numRequests"].(float64); ok && usage.PremiumUsed == 0 {
		usage.PremiumUsed = int(nr)
	}
	if nrt, ok := data["numRequestsTotal"].(float64); ok && usage.PremiumTotal == 0 {
		usage.PremiumTotal = int(nrt)
	}

	if plan, ok := data["plan"].(string); ok {
		usage.Plan = plan
	}
	if sub, ok := data["subscription"].(map[string]interface{}); ok {
		if plan, ok := sub["plan"].(string); ok {
			usage.Plan = plan
		}
	}
}

// extractSessionTokenValue extracts the WorkosCursorSessionToken value from a cookie header string.
func extractSessionTokenValue(token string) string {
	if strings.Contains(token, "WorkosCursorSessionToken=") {
		for _, part := range strings.Split(token, ";") {
			part = strings.TrimSpace(part)
			if strings.HasPrefix(part, "WorkosCursorSessionToken=") {
				return strings.TrimPrefix(part, "WorkosCursorSessionToken=")
			}
		}
	}
	return token
}

// extractUserID extracts the user ID from a WorkosCursorSessionToken value.
func extractUserID(token string) string {
	if parts := strings.SplitN(token, "%3A%3A", 2); len(parts) == 2 && parts[0] != "" {
		return parts[0]
	}
	if parts := strings.SplitN(token, "::", 2); len(parts) == 2 && parts[0] != "" {
		return parts[0]
	}
	return ""
}
