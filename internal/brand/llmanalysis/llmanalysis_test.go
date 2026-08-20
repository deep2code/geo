package llmanalysis

import (
	"context"
	"testing"
)

func TestFallbackSentiment(t *testing.T) {
	label, _, _ := fallbackSentiment("Acme", nil, "Acme is the best and most recommended tool on the market.")
	if label != "positive" {
		t.Fatalf("应判 positive，得到 %s", label)
	}
	label, _, _ = fallbackSentiment("Acme", nil, "Acme has poor performance and many complaints.")
	if label != "negative" {
		t.Fatalf("应判 negative，得到 %s", label)
	}
	label, _, _ = fallbackSentiment("Acme", nil, "今日天气不错。")
	if label != "neutral" {
		t.Fatalf("未提及品牌应 neutral，得到 %s", label)
	}
}

func TestFallbackExtractSources(t *testing.T) {
	srcs := fallbackExtractSources("参考 https://g2.com/acme 和 https://blog.example.com/post 的内容。")
	if len(srcs) != 2 {
		t.Fatalf("应提取 2 个 URL，得到 %d", len(srcs))
	}
	if srcs[0].URL != "https://g2.com/acme" {
		t.Fatalf("URL 提取错误: %v", srcs[0].URL)
	}
}

func TestParseJSON(t *testing.T) {
	var v struct {
		Label string `json:"label"`
	}
	// 模型常把 JSON 包在代码围栏里，解析应容忍
	raw := "```json\n{\"label\":\"positive\"}\n```"
	if err := parseJSON(raw, &v); err != nil {
		t.Fatal(err)
	}
	if v.Label != "positive" {
		t.Fatalf("label 应为 positive，得到 %s", v.Label)
	}
}

func TestAnalyzerDisabledFallback(t *testing.T) {
	// judge=nil → 判定层自动降级，Sentiment 仍返回有效结果
	a := New(nil)
	if a.Enabled() {
		t.Fatal("无判定模型时 Enabled() 应为 false")
	}
	label, _, _ := a.Sentiment(context.Background(), "Acme", nil, "Acme is great and trusted.")
	if label != "positive" {
		t.Fatalf("降级词典法应判 positive，得到 %s", label)
	}
	srcs, err := a.ExtractSources(context.Background(), "visit https://example.com", "acme.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(srcs) == 0 {
		t.Fatal("降级提取不应为空")
	}
	flags, err := a.Accuracy(context.Background(), "Acme", "Acme 是一家好公司", []Fact{{Statement: "Acme 成立于 2010 年"}})
	if err != nil {
		t.Fatal(err)
	}
	_ = flags // 降级比对允许为空或弱信号
}
