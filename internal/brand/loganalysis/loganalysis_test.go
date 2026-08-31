package loganalysis

import (
	"strings"
	"testing"
	"time"
)

func TestClassifyBot(t *testing.T) {
	tests := []struct {
		ua       string
		expected string
	}{
		{"Mozilla/5.0 (compatible; GPTBot/1.0; +https://openai.com/gptbot)", "GPTBot"},
		{"Mozilla/5.0 (compatible; ChatGPT-User/1.0; +https://openai.com/bot)", "ChatGPT-User"},
		{"Mozilla/5.0 (compatible; PerplexityBot/1.0; +https://perplexity.ai)", "PerplexityBot"},
		{"Mozilla/5.0 (compatible; ClaudeBot/1.0; +https://anthropic.com/claudebot)", "ClaudeBot"},
		{"Mozilla/5.0 (compatible; Google-Extended/1.0)", "Google-Extended"},
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36", ""},
		{"Mozilla/5.0 (compatible; Bytespider/1.0)", "Bytespider"},
	}

	for _, tt := range tests {
		result := ClassifyBot(tt.ua)
		if tt.expected == "" {
			if result != nil {
				t.Errorf("ClassifyBot(%q) = %v, want nil", tt.ua, result)
			}
		} else {
			if result == nil {
				t.Errorf("ClassifyBot(%q) = nil, want %s", tt.ua, tt.expected)
			} else if result.Name != tt.expected {
				t.Errorf("ClassifyBot(%q) = %s, want %s", tt.ua, result.Name, tt.expected)
			}
		}
	}
}

func TestParseNginxLog(t *testing.T) {
	log := `192.168.1.1 - - [10/Oct/2023:13:55:36 +0000] "GET /api/v1/products HTTP/1.1" 200 1234 "-" "Mozilla/5.0 (compatible; GPTBot/1.0; +https://openai.com/gptbot)"
192.168.1.2 - - [10/Oct/2023:13:56:00 +0000] "GET /blog/article HTTP/1.1" 200 5678 "-" "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"
10.0.0.1 - - [10/Oct/2023:14:00:00 +0000] "GET / HTTP/1.1" 200 9999 "-" "Mozilla/5.0 (compatible; PerplexityBot/1.0; +https://perplexity.ai)"`

	entries, err := ParseNginxLog(strings.NewReader(log))
	if err != nil {
		t.Fatalf("ParseNginxLog failed: %v", err)
	}
	if len(entries) != 3 {
		t.Errorf("expected 3 entries, got %d", len(entries))
	}
	if entries[0].UserAgent != "Mozilla/5.0 (compatible; GPTBot/1.0; +https://openai.com/gptbot)" {
		t.Errorf("unexpected user agent: %s", entries[0].UserAgent)
	}
}

func TestAnalyzeTraffic(t *testing.T) {
	entries := []LogEntry{
		{Timestamp: time.Now(), UserAgent: "Mozilla/5.0 (compatible; GPTBot/1.0)", Path: "/blog"},
		{Timestamp: time.Now(), UserAgent: "Mozilla/5.0 (compatible; GPTBot/1.0)", Path: "/products"},
		{Timestamp: time.Now(), UserAgent: "Mozilla/5.0 (Windows NT 10.0)", Path: "/"},
		{Timestamp: time.Now(), UserAgent: "Mozilla/5.0 (compatible; PerplexityBot/1.0)", Path: "/blog"},
	}

	summary := AnalyzeTraffic(entries)
	if summary.TotalRequests != 4 {
		t.Errorf("expected 4 total requests, got %d", summary.TotalRequests)
	}
	if summary.AIBotRequests != 3 {
		t.Errorf("expected 3 AI bot requests, got %d", summary.AIBotRequests)
	}
	if len(summary.BotStats) != 2 {
		t.Errorf("expected 2 bot stats, got %d", len(summary.BotStats))
	}
}
