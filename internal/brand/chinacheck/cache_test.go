package chinacheck

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCache_BasicRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.jsonl")
	ca, err := NewCache(path, WithTTL(time.Hour), WithMaxItems(100))
	if err != nil {
		t.Fatalf("NewCache: %v", err)
	}
	if ca.Path() != path {
		t.Fatalf("path mismatch: %s vs %s", ca.Path(), path)
	}
	st := ca.Stats()
	if st.Count != 0 {
		t.Fatalf("fresh cache should be empty, got %d", st.Count)
	}

	// SetSearch + GetSearch
	sr := &SearchResult{Total: 3, Companies: []CompanyHit{
		{CompanyID: "1", NameZh: "A有限公司", RegistrationNo: "CODE1"},
		{CompanyID: "2", NameZh: "B有限公司"},
	}}
	if err := ca.SetSearch("zh", "腾讯", 5, sr); err != nil {
		t.Fatalf("SetSearch: %v", err)
	}
	got, ok := ca.GetSearch("zh", "腾讯", 5)
	if !ok {
		t.Fatalf("GetSearch 应当命中")
	}
	if got.Total != 3 || len(got.Companies) != 2 || got.Companies[0].CompanyID != "1" {
		t.Fatalf("GetSearch 数据不对: %+v", got)
	}
	// 不同 limit / lang 不互通
	if _, ok := ca.GetSearch("zh", "腾讯", 10); ok {
		t.Fatalf("不同 limit 不应命中")
	}
	if _, ok := ca.GetSearch("en", "腾讯", 5); ok {
		t.Fatalf("不同 lang 不应命中")
	}

	// SetSnapshot + GetSnapshot（双 key）
	snap := &SnapshotResponse{
		CompanyID: "1",
		Snapshot: &Snapshot{
			CompanyName:         "A有限公司",
			CreditCode:          "CODE1",
			LegalRepresentative: "张三",
		},
	}
	if err := ca.SetSnapshot("1", "腾讯A", snap); err != nil {
		t.Fatalf("SetSnapshot: %v", err)
	}
	// 用 ID 查
	if got, ok := ca.GetSnapshot("1", ""); !ok || got.CompanyID != "1" || got.Snapshot.LegalRepresentative != "张三" {
		t.Fatalf("GetSnapshot by ID 失败: ok=%v got=%+v", ok, got)
	}
	// 用查询词查
	if got, ok := ca.GetSnapshot("", "腾讯A"); !ok || got.Snapshot.CreditCode != "CODE1" {
		t.Fatalf("GetSnapshot by query 失败: ok=%v got=%+v", ok, got)
	}
	// 新 key 查不到
	if _, ok := ca.GetSnapshot("999", ""); ok {
		t.Fatalf("未知 ID 不应命中")
	}

	// 持久化重启后仍可恢复
	ca2, err := NewCache(path, WithMaxItems(100))
	if err != nil {
		t.Fatalf("重载缓存失败: %v", err)
	}
	st2 := ca2.Stats()
	if st2.Count < 3 { // 至少 search 1 条 + snapshot ID/query 2 条
		t.Fatalf("重载后条目太少: %d", st2.Count)
	}
	if sr2, ok := ca2.GetSearch("zh", "腾讯", 5); !ok || sr2.Total != 3 {
		t.Fatalf("重载后 search 不命中")
	}
	if sp2, ok := ca2.GetSnapshot("1", ""); !ok || sp2.Snapshot.LegalRepresentative != "张三" {
		t.Fatalf("重载后 snapshot 不命中")
	}

	// Clear
	if err := ca2.Clear(); err != nil {
		t.Fatalf("Clear 失败: %v", err)
	}
	if ca2.Stats().Count != 0 {
		t.Fatalf("Clear 后应为 0")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("Clear 后文件应被删除")
	}
}

func TestCache_TTLExpire(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.jsonl")
	// TTL 极短
	ca, err := NewCache(path, WithTTL(50*time.Millisecond), WithMaxItems(100))
	if err != nil {
		t.Fatalf("NewCache: %v", err)
	}
	if err := ca.SetSearch("zh", "q1", 5, &SearchResult{Total: 1}); err != nil {
		t.Fatalf("SetSearch: %v", err)
	}
	if _, ok := ca.GetSearch("zh", "q1", 5); !ok {
		t.Fatalf("未过期时应命中")
	}
	time.Sleep(100 * time.Millisecond)
	if _, ok := ca.GetSearch("zh", "q1", 5); ok {
		t.Fatalf("过期后不应命中")
	}
	// 加载时也会过滤过期
	ca2, err := NewCache(path, WithTTL(50*time.Millisecond), WithMaxItems(100))
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if ca2.Stats().Count != 0 {
		t.Fatalf("加载时应过滤过期条目, got %d", ca2.Stats().Count)
	}
}

func TestCache_EvictionAndCompact(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.jsonl")
	maxN := 5
	ca, err := NewCache(path, WithMaxItems(maxN), WithTTL(time.Hour))
	if err != nil {
		t.Fatalf("NewCache: %v", err)
	}
	// 写入 N+3 条：触发淘汰 + 压缩
	for i := 0; i < maxN+3; i++ {
		sr := &SearchResult{Total: i}
		if err := ca.SetSearch("zh", string(rune('A'+i)), 5, sr); err != nil {
			t.Fatalf("SetSearch %d: %v", i, err)
		}
	}
	st := ca.Stats()
	if st.Count != maxN {
		t.Fatalf("淘汰后条目应为 %d, got %d", maxN, st.Count)
	}
	// 最老的 3 条（A/B/C）应当被淘汰，最新的 5 条（D/E/F/G/H）存在
	checks := []struct {
		q   string
		hit bool
	}{
		{"A", false}, {"B", false}, {"C", false},
		{"D", true}, {"E", true}, {"F", true}, {"G", true}, {"H", true},
	}
	for _, c := range checks {
		_, ok := ca.GetSearch("zh", c.q, 5)
		if ok != c.hit {
			t.Errorf("query=%s hit=%v expected %v", c.q, ok, c.hit)
		}
	}

	// Compact：写多轮 set 覆盖同一个 key 多次（append 模式下文件会有冗余行），压缩后文件变小
	for round := 0; round < 10; round++ {
		for i := 0; i < maxN; i++ {
			sr := &SearchResult{Total: round*100 + i}
			if err := ca.SetSearch("zh", "dupkey", i+1, sr); err != nil {
				t.Fatalf("dup SetSearch: %v", err)
			}
		}
	}
	beforeCompact := ca.Stats()
	if beforeCompact.FileSizeByte <= 0 {
		t.Fatalf("多次覆盖后文件应 >0 bytes")
	}
	if err := ca.Compact(); err != nil {
		t.Fatalf("Compact: %v", err)
	}
	afterCompact := ca.Stats()
	// 压缩后：条目应该刚好 == maxN（eviction 逻辑已经限制），文件大小 < 之前
	if afterCompact.Count > maxN {
		t.Fatalf("压缩后条目 %d > max %d", afterCompact.Count, maxN)
	}
	if afterCompact.FileSizeByte >= beforeCompact.FileSizeByte {
		t.Errorf("压缩后文件未变小: before %d → after %d",
			beforeCompact.FileSizeByte, afterCompact.FileSizeByte)
	}
}
