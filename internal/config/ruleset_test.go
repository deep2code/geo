package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"my-geo/internal/models"
)

func TestDefaultRuleSet(t *testing.T) {
	rs := DefaultRuleSet()
	if rs.Name != "default" || rs.Version == "" {
		t.Fatalf("默认规则集字段异常: %+v", rs)
	}
	if len(rs.Weights) != len(DefaultWeights()) {
		t.Fatalf("默认权重条目数不符: got %d want %d", len(rs.Weights), len(DefaultWeights()))
	}
	if len(rs.StrategyEffectiveness) != len(StrategyEffectiveness) {
		t.Fatalf("默认策略系数条目数不符: got %d want %d", len(rs.StrategyEffectiveness), len(StrategyEffectiveness))
	}
}

func TestLoadRuleSetValid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rs.json")
	rs := DefaultRuleSet()
	rs.Name = "test"
	// 用内置默认集写入临时文件再读回，验证往返。
	if err := writeRuleSetForTest(t, path, rs); err != nil {
		t.Fatal(err)
	}
	got, err := LoadRuleSet(path)
	if err != nil {
		t.Fatalf("LoadRuleSet 失败: %v", err)
	}
	if got.Name != "test" {
		t.Fatalf("name 不符: %s", got.Name)
	}
	if got.Weights["quotation"] != 8.0 {
		t.Fatalf("权重未保留: %v", got.Weights)
	}
}

func TestLoadRuleSetInvalid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte(`{"version":"","name":"x"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRuleSet(path); err == nil {
		t.Fatal("期望校验失败，但 LoadRuleSet 未报错")
	}
}

func TestSetStrategyEffectiveness(t *testing.T) {
	before := StrategyEffectiveness[models.StrategyQuotation]
	rs := &RuleSet{
		Version: "1.0.0", Name: "x",
		StrategyEffectiveness: map[models.StrategyType]float64{
			models.StrategyQuotation: 0.99,
			models.StrategyType("unknown_strategy"): 0.5, // 应被忽略
		},
	}
	SetStrategyEffectiveness(rs.StrategyEffectiveness)
	if StrategyEffectiveness[models.StrategyQuotation] != 0.99 {
		t.Fatalf("策略系数未覆盖: %v", StrategyEffectiveness[models.StrategyQuotation])
	}
	// 还原，避免影响其他测试
	SetStrategyEffectiveness(map[models.StrategyType]float64{models.StrategyQuotation: before})
}

// writeRuleSetForTest 用与 LoadRuleSet 相同的 JSON 编码写回文件。
func writeRuleSetForTest(t *testing.T, path string, rs *RuleSet) error {
	t.Helper()
	b, err := json.MarshalIndent(rs, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0644)
}
