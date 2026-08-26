import json

BASE = 'web-app/src/i18n/locales/'

orig_zh = {
    "navFeatures": "功能特性", "navPricing": "定价方案", "navHelp": "帮助中心", "navLogin": "登录控制台",
    "heroBadge": "AI 搜索引擎可见度优化平台", "heroTitle": "让品牌在 AI 时代被看见",
    "heroSubtitle": "崛起GEO 覆盖 ChatGPT、Perplexity、Gemini、Claude、Grok、通义千问、智谱GLM、DeepSeek、Kimi、文心一言、豆包等 AI 引擎，提供品牌可见度审计、内容优化、竞品对标等一站式 GEO 优化能力",
    "heroCtaStart": "免费开始", "heroCtaPricing": "查看定价",
    "statBrands": "品牌数", "statAudits": "审计次数", "statEngines": "AI 引擎", "statUsers": "注册用户",
    "featuresTitle": "核心功能", "featuresSubtitle": "从内容到品牌，全链路 GEO 优化能力",
    "featureBvsTitle": "BVS 品牌可见度评分", "featureBvsDesc": "7 维度量化品牌在 AI 搜索引擎中的可见度",
    "featureAuditTitle": "品牌审计", "featureAuditDesc": "多引擎检测品牌提及率、引用率与情感倾向",
    "featureOptimizeTitle": "内容优化", "featureOptimizeDesc": "GEO 策略优化单篇内容在 AI 回答中的引用率",
    rie"featureKeywordTitle": "关键词发现", "featureKeywordDesc": "发现高意图关键词并生成优化报告",
    "featureCompareTitle": "竞品对标", "featureCompareDesc": "多品牌多维对比，识别优势与差距",
    "featureLeaderboardTitle": "公开排行榜", "featureLeaderboardDesc": "按品类查看品牌可见度排名与趋势",
    "featureAlertTitle": "告警邮件", "featureAlertDesc": "BVS 评分低于阈值自动告警，定期周报",
    "featureReportTitle": "报告导出", "featureReportDesc": "HTML / PDF 报告导出，支持邮件发送",
    "pricingTitle": "定价方案", "pricingSubtitle": "选择适合团队的方案，随时升级或降级",
    "planRecommended": "推荐", "planFreeName": "免费版", "planFreeDesc": "适合个人用户和小团队试用",
    "planProName": "专业版", "planProDesc": "适合成长型团队的日常 GEO 优化",
    "planEnterpriseName": "企业版", "planEnterpriseDesc": "适合大型企业的私有化部署",
    "planUnit": "月", "planCustom": "年", "planCtaStart": "立即开始", "planCtaContact": "联系我们",
    "footerRights": "保留所有权利。", "footerSupport": "技术支持", "footerAbout": "关于我们"
}
orig_en = {
    "navFeatures": "Features", "navPricing": "Pricing", "navHelp": "Help", "navLogin": "Login",
    "heroBadge": "AI Search Engine Visibility Optimization Platform", "heroTitle": "Be Seen in the AI Era",
    "heroSubtitle": "崛起GEO covers ChatGPT, Perplexity, Gemini, Claude, Grok, Qwen, GLM, DeepSeek, Kimi, ERNIE and Doubao AI engines, providing brand visibility audit, content optimization, competitor comparison and more.",
    "heroCtaStart": "Start Free", "heroCtaPricing": "View Pricing",
    "statBrands": "Brands", "statAudits": "Audits", "statEngines": "AI Engines", "statUsers": "Users",
    "featuresTitle": "Core Features", "featuresSubtitle": "End-to-end GEO optimization from content to brand",
    "featureBvsTitle": "BVS Visibility Score", "featureBvsDesc": "7-dimension quantified brand visibility in AI search engines",
    "featureAuditTitle": "Brand Audit", "featureAuditDesc": "Multi-engine detection of mention rate, citation rate and sentiment",
    "featureOptimizeTitle": "Content Optimization", "featureOptimizeDesc": "GEO strategies to boost single content citation rate",
    "featureKeywordTitle": "Keyword Discovery", "featureKeywordDesc": "Discover high-intent keywords and generate reports",
    "featureCompareTitle": "Competitor Compare", "featureCompareDesc": "Multi-brand multi-dimension comparison",
    "featureLeaderboardTitle": "Public Leaderboard", "featureLeaderboardDesc": "Brand visibility ranking by category",
    "featureAlertTitle": "Alerts & Email", "featureAlertDesc": "Auto alert when BVS drops, scheduled weekly reports",
    "featureReportTitle": "Report Export", "featureReportDesc": "HTML / PDF export with email delivery",
    "p,ricingTitle": "Pricing", "pricingSubtitle": "Choose a plan for your team, upgrade or downgrade anytime",
    "planRecommended": "Recommended", "planFreeName": "Free", "planFreeDesc": "For individuals and small teams to try",
    "planProName": "Pro", "planProDesc": "For growing teams doing daily GEO optimization",
    "planEnterpriseName": "Enterprise", "planEnterpriseDesc": "For large enterprises with on-premise deployment",
    "planUnit": "mo", "planCustom": "yr", "planCtaStart": "Get Started", "planCtaContact": "Contact Us",
    "footerRights": "All rights reserved.", "footerSupport": "Support", "footerAbout": "About"
}
orig_ja = {
    "navFeatures": "機能", "navPricing": "料金", "navHelp": "ヘルプ", "navLogin": "ログイン",
    "heroBadge": "AI検索エンジン可視性最適化プラットフォーム", "heroTitle": "AI時代でブランドを可視化",
    "heroSubtitle": "崛起GEOはChatGPT、Perplexity、Gemini、Claude、Grok、通義千問、GLM、DeepSeek、Kimi、文心一言、豆包などのAIエンジンをカバーし、ブランド可視性監査、コンテンツ最適化、競合比較などのGEO最適化機能を提供します。",
    "heroCtaStart": "無料で開始", "heroCtaPricing": "料金を見る",
    "statBrands": "ブランド数", "statAudits": "監査回数", "statEngines": "AIエンジン", "statUsers": "ユーザー数",
    "featuresTitle": "コア機能", "featuresSubtitle": "コンテンツからブランドまで、エンドツーエンドのGEO最適化",
    "featureBvsTitle": "BVS可視性スコア", "featureBvsDesc": "7次元でAI検索エンジンのブランド可視性を定量化",
    "featureAuditTitle": "ブランド監査", "featureAuditDesc": "複数エンジンで言及率・引用率・感情を検出",
    "featureOptimizeTitle": "コンテンツ最適化", "featureOptimizeDesc": "GEO戦略でコンテンツの引用率を向上",
    "featureKeywordTitle": "キーワード発見", "featureKeywordDesc": "高インテントキーワードを発見しレポート生成",
    "featureCompareTitle": "競合比較", "featureCompareDesc": "複数ブランドの多次元比較",
    "featureLeaderboardTitle": "公開ランキング", "featureLeaderboardDesc": "カテゴリ別ブランド可視性ランキング",
    "featureAlertTitle": "アラート通知", "featureAlertDesc": "BVS低下時の自動アラート、定期週報",
    "featureReportTitle": "レポート出力", "featureReportDesc": "HTML / PDF出力、メール送信対応",
    "pricingTitle": "料金プラン", "pricingSubtitle": "チームに合ったプランを選択、いつでも変更可能",
    "planRecommended": "おすすめ", "planFreeName": "Free", "planFreeDesc": "個人や小規模チームのトライアル用",
    "planProName": "Pro", "planProDesc": "成長チームの日常GEO最適化用",
    "planEnterpriseName": "Enterprise", "planEnterpriseDesc": "大企業のオンプレミス展開用",
    "planUnit": "月", "planCustom": "年", "planCtaStart": "今すぐ開始", "planCtaContact": "お問い合わせ",
    "footerRights": "All rights reserved.", "footerSupport": "サポート", "footerAbout": "概要"
}

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
    "reportEngineTitle": "Engine Coverage", "reportMention":, "Mention", "reportCitation": "Citation", "reportPromptTitle": "Sample Query Hits",
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

configs = [
    ('zh-CN', orig_zh, new_zh),
    ('en', orig_en, new_en),
    ('ja', orig_ja, new_ja),
]

for name, orig, newk in configs:
    path = BASE + name + '.json'
    text = open(path, encoding='utf-8').read()
    idx = text.index('"landing":')
    brace_start = text.index('{', idx)
    depth = 0
    end = None
    for i in range(brace_start, len(text)):
        ch = text[i]
        if ch == '{':
            depth += 1
        elif ch == '}':
            depth -= 1
            if depth == 0:
                end = i
                break
    if end is None:
        raise SystemExit('brace match failed: ' + name)
    merged = {}
    for k, v in orig.items():
        merged[k] = v
    for k, v in newk.items():
        merged[k] = v
    new_block = json.dumps(merged, ensure_ascii=False, indent=2)
    new_text = text[:brace_start] + new_block + text[end + 1:]
    open(path, 'w', encoding='utf-8').write(new_text)
    json.loads(new_text)
    print(name, 'OK', len(merged), 'keys')
print('ALL DONE')
