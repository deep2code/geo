package util

import (
	"reflect"
	"testing"
)

func TestSplitSentences(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", []string{""}},
		{"无标点文本", []string{"无标点文本"}},
		{"你好。世界。", []string{"你好。", "世界。"}},
		{"A! B?", []string{"A!", " B?"}},
	}
	for _, c := range cases {
		got := SplitSentences(c.in)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("SplitSentences(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestCountSentences(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"你好。世界。", 2},
		{"无标点文本", 1},
		{"", 1},
		{"第一行\n\n第二行\n第三行", 3},
	}
	for _, c := range cases {
		if got := CountSentences(c.in); got != c.want {
			t.Errorf("CountSentences(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}
