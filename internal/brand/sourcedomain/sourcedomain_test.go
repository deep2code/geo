package sourcedomain

import "testing"

func TestExtractDomain(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://www.g2.com/products/x", "g2.com"},
		{"https://zhihu.com/question/1", "zhihu.com"},
		{"http://BLOG.G2.COM:8080/x", "blog.g2.com"},
		{"www.reddit.com/r/go", "reddit.com"},
		{"example.com/path", "example.com"},
		{"", ""},
		{"https://bad url.com/x", ""}, // 含空格，无法解析 → 空
	}
	for _, c := range cases {
		if got := ExtractDomain(c.in); got != c.want {
			t.Errorf("ExtractDomain(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCategorizeDomain(t *testing.T) {
	cases := []struct {
		domain string
		want   string
	}{
		{"g2.com", "review_site"},
		{"reviews.g2.com", "review_site"},
		{"zhihu.com", "social"},
		{"www.zhihu.com", "social"},
		{"github.com", "docs"},
		{"medium.com", "blog"},
		{"youtube.com", "video"},
		{"36kr.com", "news"},
		{"unknown-domain.xyz", "other"},
		{"", "other"},
	}
	for _, c := range cases {
		if got := CategorizeDomain(c.domain); got != c.want {
			t.Errorf("CategorizeDomain(%q) = %q, want %q", c.domain, got, c.want)
		}
	}
}
