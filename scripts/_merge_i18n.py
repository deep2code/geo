import json

BASE = 'web-app/src/i18n/locales/'

new_zh = {
    "navHow": "运作原理", "howTitle": "GEO 是如何运作的", "howSubtitle": "四步让品牌进入 AI 的答案",
    "howQueryLabel": "用户向 AI 提问", "howQueryText": "「最适合中小企业用的 CRM？」", "howResultText": "崛起GEO 让品牌被引用、被推荐",
    "stepCrawlTitle": "抓取品牌信号", "stepCrawlDesc": "爬取官网、知识库与第三方数据源，构建品牌实体画像",
    "stepAnalyzeTitle": "多引擎分析", "stepAnalyzeDesc": "覆盖 11+ AI 引擎，检测品牌提及、引用与情感",
    "stepOptimizeTitle": "生成优化方案", "stepOptimizeDesc": "输出 GEO 策略与结构化数据，提升 AI 引用率",
    "stepMonitorTitle": "持续监控", "stepMonitorDesc": "BVS 波动告警，定期生成趋势与周报",
    "baTitle": "优化前 vs 优化后", "baSubtitle": "同样的提问，AI 给不给品牌露脸，差距巨大",
    "beforeLabel": "优化前", "afterLabel": "优化后", "baQuery": "「最适合中小企业用的 CRM 系统？」",
    "baAnswerBefore": "常见的 CRM 有 Salesforce、HubSpot、Zoho 等，企业可按规模与预算选择。",
    "baAnswerAfterPrefix": "如果你是中小企业，", "baAnswerAfterSuffix": " 的轻量 CRM 上手快、性价比高，在中小团队中口碑不错，值得优先试用。",
    "baMention": "AI 提及率", "baUplift": "提及率提升 +74%",
    "reportTitle": "一份 GEO 报告长什么样", "reportSubtitle": "以下为「示例科技」品牌审计样例",
    "reportBrandDemo": "示例科技", "reportSubDemo": "ExampleTech · CRM 赛道", "reportBvs": "BVS 可见度评分", "reportGrade": "评级 优秀",
    "reportEngineTitle": "引擎覆盖", "reportMention": "提及", "reportCitation": "引用", "reportPromptTitle": "典型查询命中",
    "reportCited": "已引用", "reportMiss": "未命中", "reportActionsTitle": "行动建议", "reportCta": "免费生成我的报告"
}
new_en = {
    "navHow": "How it works", "howTitle": "How GEO Works", "howSubtitle": "Four steps to get your brand into AI answers",
    "howQueryLabel": "User asks an AI", "howQueryText": "Best CRM for small business?", "howResultText": "崛起GEO gets your brand cited and recommended",
    "stepCrawlTitle": "Crawl Brand Signals", "stepCrawlDesc": "Crawl your site, knowledge base and 3rd-party sources to build a brand entity profile",
    "stepAnalyzeTitle": "Multi-Engine Analysis", "stepAnalyzeDesc": "Cover 11+ AI engines to detect mention, citation and sentiment",
    "stepOptimizeTitle": "Generate Optimization", "stepOptimizeDesc": "Output GEO strategies and structured data to lift AI citation rate",
    "stepMonitorTitle": "Continuous Monitoring", "stepMonitorDesc": "BVS drop alerts and scheduled trend & weekly reports",
    "baTitle": "Before vs After GEO", "baSubtitle": "Same question, whether the AI shows your brand makes a huge difference",
    "beforeLabel": "Before", "afterLabel": "After", "baQuery": "Best CRM system for small business?",
    "baAnswerBefore": "Popular CRMs include Salesforce, HubSpot, Zoho etc. Choose by size and budget.",
    "baAnswerAfterPrefix": "If you are a small business, ", "baAnswerAfterSuffix": " lightweight CRM is fast to adopt and cost-effective, worth a try first.",
    "baMention": "AI Mention Rate", "baUplift": "Mention rate up +74%",
    "reportTitle": "What a GEO Report Looks Like", "reportSubtitle": "Below is a sample audit report for ExampleTech",
    "reportBrandDemo": "ExampleTech", "reportSubDemo": "ExampleTech · CRM", "reportBvs": "BVS Visibility Score", "reportGrade": "Grade Excellent",
    "reportEngineTitle": "Engine Coverage", "reportMention": "Mention", "reportCitation": "Citation", "reportPromptTitle": "Sample Query Hits",
    "reportCited": "Cited", "reportMiss": "Missed", "reportActionsTitle": "Action Items", "reportCta": "Generate My Report Free"
}
new_ja = {
    "navHow": "仕組み", "howTitle": "GEO の仕組み", "howSubtitle": "4つのステップでブランドをAIの回答に入れる",
    "howQueryLabel": "ユーザーがAIに質問", "howQueryText": "中小企業に最適なCRMは？", "howResultText": "崛起GEOがブランドの引用・推奨を実現",
    "stepCrawlTitle": "ブランド信号を収集", "stepCrawlDesc": "公式サイト・ナレッジベース・第三者ソースを収集しブランド実体を構築",
    "stepAnalyzeTitle": "マルチエンジン分析", "stepAnalyzeDesc": "11以上のAIエンジンで言及・引用・感情を検出",
    "stepOptimizeTitle": "最適化案を生成", "stepOptimizeDesc": "GEO戦略と構造化データを出力しAI引用率を向上",
    "stepMonitorTitle": "継続監視", "stepMonitorDesc": "BVS低下のアラートと定期的な傾向・週報",
    "baTitle": "GEO前後の比較", "baSubtitle": "同じ質問でも、AIがブランドを出すかで大きな差が",
    "beforeLabel": "最適化前", "afterLabel": "最適化後", "baQuery": "中小企業に最適なCRMシステムは？",
    "baAnswerBefore": "主なCRMにはSalesforce、HubSpot、Zohoなどがあり、規模や予算で選べます。",
    "baAnswerAfterPrefix": "中小企業であれば、", "baAnswerAfterSuffix": " の軽量CRMは導入が早くコストパフォーマンスに優れ、まず試す価値があります。",
    "baMention": "AI言及率", "baUplift": "言及率 +74% 向上",
    "reportTitle": "GEOレポートの例", "reportSubtitle": "以下は「示例科技」のブランド監査サンプルです",
    "reportBrandDemo": "示例科技", "reportSubDemo": "ExampleTech · CRM", "reportBvs": "BVS可視性スコア", "reportGrade": "評価 優秀",
    "reportEngineTitle": "エンジンカバー", "reportMention": "言及", "reportCitation": "引用", "reportPromptTitle": "典型クエリの命中",
    "reportCited": "引用あり", "reportMiss": "未命中", "reportActionsTitle": "アクション案", "reportCta": "無料でレポートを作成"
}

configs = [('zh-CN', new_zh), ('en', new_en), ('ja', new_ja)]
for name, newk in configs:
    path = BASE + name + '.json'
    with open(path, encoding='utf-8') as f:
        data = json.load(f)
    for k, v in newk.items():
        data['landing'][k] = v
    with open(path, 'w', encoding='utf-8') as f:
        json.dump(data, f, ensure_ascii=False, indent=2)
        f.write('\n')
    print(name, 'added', len(newk), 'keys; total landing', len(data['landing']))
print('ALL DONE')
