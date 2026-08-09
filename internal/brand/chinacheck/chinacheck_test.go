package chinacheck

import (
	"context"
	"testing"
	"time"
)

// TestChinaCheckConnectivity 验证 China-Check MCP 公共端点可达。
// 该测试会发起真实网络请求（45s 超时），无网络环境下可通过 -short 跳过。
func TestChinaCheckConnectivity(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过网络测试（-short 模式）")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	cc := New()

	// 步骤 1：搜索
	sr, err := cc.Search(ctx, "腾讯", 3)
	if err != nil {
		t.Fatalf("Search 失败: %v", err)
	}
	t.Logf("搜索命中 %d 条（total=%d）", len(sr.Companies), sr.Total)
	for i, c := range sr.Companies {
		t.Logf("  [%d] %s | ID=%s | 信用代码=%s | 法人=%s | 成立=%s | 资本=%s | 地区=%s",
			i, c.NameZh, c.CompanyID, c.RegistrationNo, c.LegalPersonName,
			c.EstablishedAt, c.RegisteredCapital, c.Base)
	}
	if len(sr.Companies) == 0 {
		t.Skip("搜索结果为空，跳过 snapshot 步骤（可能查询词无对应工商记录）")
	}
	// 步骤 2：取最匹配的 snapshot
	best := sr.Companies[0]
	snap, err := cc.GetSnapshot(ctx, best.CompanyID, "")
	if err != nil {
		t.Fatalf("GetSnapshot 失败: %v", err)
	}
	if snap == nil || snap.Snapshot == nil {
		t.Fatalf("GetSnapshot 返回空")
	}
	s := snap.Snapshot
	t.Logf("Snapshot OK: %s | 信用代码=%s | 状态=%s | 行业=%s | 省份=%s | 地址=%s",
		s.CompanyName, s.CreditCode, s.RegistrationStatus, s.Industry, s.Province, s.RegisteredAddress)
}
