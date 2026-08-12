package uniqueness

import (
	"math"
	"testing"
)

func TestTokenize_CJKAndEnglish(t *testing.T) {
	tokens := tokenize("品牌SEO优化 brand optimization")
	// 应包含中文 2-gram 和英文词
	hasBrand := false
	hasSEO := false
	for _, tok := range tokens {
		if tok == "brand" {
			hasBrand = true
		}
		if tok == "seo" {
			hasSEO = true
		}
	}
	if !hasBrand {
		t.Errorf("expected token 'brand', got %v", tokens)
	}
	if !hasSEO {
		t.Errorf("expected token 'seo', got %v", tokens)
	}
}

func TestTokenize_Empty(t *testing.T) {
	if tokens := tokenize(""); tokens != nil {
		t.Errorf("expected nil for empty input, got %v", tokens)
	}
}

func TestMinHash_IdenticalTexts(t *testing.T) {
	text := "这是一段测试文本，用于验证 MinHash 算法的准确性。"
	sim := MinHashSimilarity(text, text)
	if sim < 0.95 {
		t.Errorf("identical texts should have similarity ~1.0, got %.4f", sim)
	}
}

func TestMinHash_CompletelyDifferentTexts(t *testing.T) {
	text1 := "苹果香蕉橙子葡萄西瓜"
	text2 := "computer keyboard monitor mouse printer"
	sim := MinHashSimilarity(text1, text2)
	if sim > 0.1 {
		t.Errorf("completely different texts should have low similarity, got %.4f", sim)
	}
}

func TestCosineSimilarity_IdenticalTexts(t *testing.T) {
	text := "品牌内容优化策略分析报告"
	sim := CosineSimilarity(text, text)
	if sim < 0.99 {
		t.Errorf("identical texts should have cosine ~1.0, got %.4f", sim)
	}
}

func TestCosineSimilarity_PartialOverlap(t *testing.T) {
	text1 := "品牌内容优化策略分析报告"
	text2 := "品牌内容优化策略分析报告与执行计划"
	sim := CosineSimilarity(text1, text2)
	if sim < 0.5 {
		t.Errorf("partially overlapping texts should have moderate similarity, got %.4f", sim)
	}
	if sim > 0.99 {
		t.Errorf("partial overlap should not be 1.0, got %.4f", sim)
	}
}

func TestCombinedSimilarity_WeightedAverage(t *testing.T) {
	text1 := "GEO生成式引擎优化"
	text2 := "GEO生成式引擎优化"
	mh := MinHashSimilarity(text1, text2)
	cs := CosineSimilarity(text1, text2)
	combined := CombinedSimilarity(text1, text2)
	expected := mh*0.4 + cs*0.6
	if math.Abs(combined-expected) > 0.001 {
		t.Errorf("combined should be 0.4*mh+0.6*cosine, got %.4f expected %.4f", combined, expected)
	}
}

func TestDetector_CheckEmptyCorpus(t *testing.T) {
	d := NewDetector(0.7)
	result := d.Check("任意内容")
	if result.Combined != 0 || result.IsDuplicate {
		t.Errorf("empty corpus should return zero result, got %+v", result)
	}
}

func TestDetector_AddAndCheckDuplicate(t *testing.T) {
	d := NewDetector(0.7)
	content := "这是一段关于品牌GEO优化的内容，讨论了如何提升在生成式引擎中的可见度。"
	if err := d.Add(Entry{ID: "doc1", Content: content}); err != nil {
		t.Fatal(err)
	}
	if d.Size() != 1 {
		t.Fatalf("expected size 1, got %d", d.Size())
	}

	// 完全相同内容应判定为重复
	result := d.Check(content)
	if !result.IsDuplicate {
		t.Errorf("identical content should be flagged as duplicate, got similarity %.4f", result.Combined)
	}
	if result.MaxSimilarID != "doc1" {
		t.Errorf("expected max similar id 'doc1', got '%s'", result.MaxSimilarID)
	}
}

func TestDetector_CheckUniqueContent(t *testing.T) {
	d := NewDetector(0.7)
	if err := d.Add(Entry{ID: "doc1", Content: "苹果是一种水果，富含维生素和膳食纤维。"}); err != nil {
		t.Fatal(err)
	}

	result := d.Check("量子计算在密码学中的应用前景广阔。")
	if result.IsDuplicate {
		t.Errorf("unrelated content should not be duplicate, got similarity %.4f", result.Combined)
	}
	if result.Combined > 0.3 {
		t.Errorf("unrelated content should have low similarity, got %.4f", result.Combined)
	}
}

func TestDetector_CheckAndAdd(t *testing.T) {
	d := NewDetector(0.7)
	content := "品牌GEO优化策略：提升生成式引擎可见度的方法。"
	result, err := d.CheckAndAdd(Entry{ID: "doc1", Content: content})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsDuplicate {
		t.Error("first add should not be duplicate")
	}
	if d.Size() != 1 {
		t.Fatalf("expected size 1 after add, got %d", d.Size())
	}

	// 重复内容不应添加
	result2, err := d.CheckAndAdd(Entry{ID: "doc2", Content: content})
	if err != nil {
		t.Fatal(err)
	}
	if !result2.IsDuplicate {
		t.Error("duplicate content should be flagged")
	}
	if d.Size() != 1 {
		t.Fatalf("size should remain 1 (not added duplicate), got %d", d.Size())
	}
}

func TestDetector_Remove(t *testing.T) {
	d := NewDetector(0.7)
	if err := d.Add(Entry{ID: "doc1", Content: "测试内容"}); err != nil {
		t.Fatal(err)
	}
	if !d.Remove("doc1") {
		t.Error("remove should return true for existing entry")
	}
	if d.Size() != 0 {
		t.Fatalf("expected size 0 after remove, got %d", d.Size())
	}
	if d.Remove("doc1") {
		t.Error("remove should return false for non-existing entry")
	}
}

func TestDetector_SetThreshold(t *testing.T) {
	d := NewDetector(0.9)
	if err := d.Add(Entry{ID: "doc1", Content: "品牌GEO优化内容策略"}); err != nil {
		t.Fatal(err)
	}

	// 高阈值下，部分相似不算重复
	result := d.Check("品牌GEO优化内容策略")
	if !result.IsDuplicate {
		t.Errorf("identical content should be duplicate even at high threshold, got %.4f", result.Combined)
	}

	d.SetThreshold(0.99)
	result2 := d.Check("品牌GEO优化内容策略分析")
	if result2.IsDuplicate {
		t.Errorf("at threshold 0.99, partial overlap should not be duplicate, got %.4f", result2.Combined)
	}
}

func TestDetector_FindAllDuplicates(t *testing.T) {
	d := NewDetector(0.7)
	if err := d.Add(Entry{ID: "doc1", Content: "品牌GEO优化策略"}); err != nil {
		t.Fatal(err)
	}
	if err := d.Add(Entry{ID: "doc2", Content: "品牌GEO优化策略"}); err != nil {
		t.Fatal(err)
	}
	if err := d.Add(Entry{ID: "doc3", Content: "量子力学研究"}); err != nil {
		t.Fatal(err)
	}

	pairs := d.FindAllDuplicates()
	if len(pairs) != 1 {
		t.Fatalf("expected 1 duplicate pair, got %d", len(pairs))
	}
	if pairs[0].ID1 != "doc1" || pairs[0].ID2 != "doc2" {
		t.Errorf("expected pair (doc1, doc2), got (%s, %s)", pairs[0].ID1, pairs[0].ID2)
	}
	if pairs[0].Similarity < 0.95 {
		t.Errorf("expected high similarity for identical texts, got %.4f", pairs[0].Similarity)
	}
}

func TestMinHash_Reset(t *testing.T) {
	mh := NewMinHash(128)
	mh.PushAll(tokenize("测试文本内容"))
	sig1 := make([]uint64, len(mh.Signature()))
	copy(sig1, mh.Signature())

	mh.Reset()
	mh.PushAll(tokenize("测试文本内容"))
	sig2 := mh.Signature()

	for i := range sig1 {
		if sig1[i] != sig2[i] {
			t.Errorf("after reset and re-push same content, signatures should match at pos %d", i)
			break
		}
	}
}
