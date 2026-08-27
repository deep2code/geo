// Package util 提供文本 diff 对比工具，用于展示内容优化前后的差异。
package util

import (
	"strings"
)

// DiffType 变更类型。
type DiffType string

const (
	DiffAdded   DiffType = "added"
	DiffRemoved DiffType = "removed"
	DiffUnchanged DiffType = "unchanged"
)

// DiffLine 单行 diff 结果。
type DiffLine struct {
	Type    DiffType `json:"type"`
	Content string   `json:"content"`
	OldNum int      `json:"old_num,omitempty"`
	NewNum int      `json:"new_num,omitempty"`
}

// DiffResult diff 对比结果。
type DiffResult struct {
	Lines      []DiffLine `json:"lines"`
	Added      int        `json:"added"`
	Removed    int        `json:"removed"`
	Unchanged  int        `json:"unchanged"`
	Similarity float64    `json:"similarity"` // 0-1，越高越相似
}

// ComputeDiff 按行对比两段文本，返回差异结果。
// 采用最长公共子序列（LCS）算法计算行级 diff。
func ComputeDiff(oldText, newText string) *DiffResult {
	oldLines := strings.Split(oldText, "\n")
	newLines := strings.Split(newText, "\n")

	lcs := lcsRows(oldLines, newLines)
	result := &DiffResult{}

	oi, ni, li := 0, 0, 0
	oldNum, newNum := 1, 1

	for oi < len(oldLines) || ni < len(newLines) {
		if li < len(lcs) && oi < len(oldLines) && ni < len(newLines) &&
			oldLines[oi] == lcs[li] && newLines[ni] == lcs[li] {
			result.Lines = append(result.Lines, DiffLine{
				Type:    DiffUnchanged,
				Content: oldLines[oi],
				OldNum:  oldNum,
				NewNum:  newNum,
			})
			result.Unchanged++
			oi++
			ni++
			li++
			oldNum++
			newNum++
		} else if oi < len(oldLines) && (li >= len(lcs) || oldLines[oi] != lcs[li]) {
			result.Lines = append(result.Lines, DiffLine{
				Type:    DiffRemoved,
				Content: oldLines[oi],
				OldNum:  oldNum,
			})
			result.Removed++
			oi++
			oldNum++
		} else if ni < len(newLines) {
			result.Lines = append(result.Lines, DiffLine{
				Type:    DiffAdded,
				Content: newLines[ni],
				NewNum:  newNum,
			})
			result.Added++
			ni++
			newNum++
		}
	}

	total := result.Added + result.Removed + result.Unchanged
	if total > 0 {
		result.Similarity = float64(result.Unchanged) / float64(total)
	}
	return result
}

// lcsRows 计算两组字符串的最长公共子序列。
func lcsRows(a, b []string) []string {
	na, nb := len(a), len(b)
	// dp[i][j] = LCS 长度
	dp := make([][]int, na+1)
	for i := range dp {
		dp[i] = make([]int, nb+1)
	}
	for i := 1; i <= na; i++ {
		for j := 1; j <= nb; j++ {
			if a[i-1] == b[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] > dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}

	// 回溯提取 LCS
	result := make([]string, 0, dp[na][nb])
	i, j := na, nb
	for i > 0 && j > 0 {
		if a[i-1] == b[j-1] {
			result = append(result, a[i-1])
			i--
			j--
		} else if dp[i-1][j] > dp[i][j-1] {
			i--
		} else {
			j--
		}
	}
	// 反转
	for l, r := 0, len(result)-1; l < r; l, r = l+1, r-1 {
		result[l], result[r] = result[r], result[l]
	}
	return result
}
