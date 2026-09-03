package model

import "testing"

func TestUsagePercent(t *testing.T) {
	tests := []struct {
		name     string
		usage    *UsageInfo
		expected float64
	}{
		{
			name:     "nil usage returns 0",
			usage:    nil,
			expected: 0,
		},
		{
			name:     "zero total returns 0",
			usage:    &UsageInfo{PremiumUsed: 100, PremiumTotal: 0},
			expected: 0,
		},
		{
			name:     "50 percent usage",
			usage:    &UsageInfo{PremiumUsed: 250, PremiumTotal: 500},
			expected: 50,
		},
		{
			name:     "100 percent usage",
			usage:    &UsageInfo{PremiumUsed: 500, PremiumTotal: 500},
			expected: 100,
		},
		{
			name:     "partial usage",
			usage:    &UsageInfo{PremiumUsed: 150, PremiumTotal: 500},
			expected: 30,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.usage.UsagePercent()
			if result != tt.expected {
				t.Errorf("UsagePercent() = %v, want %v", result, tt.expected)
			}
		})
	}
}
