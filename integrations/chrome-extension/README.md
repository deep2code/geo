# GEO CMS Analyzer - Chrome 扩展（Manifest V3）

基于 Manifest V3 的 Chrome 浏览器扩展，用于调用 GEO CMS 分析服务检测网页正文和选中文本的内容质量。

## 功能特性

- **侧边栏分析**：点击扩展图标或右键菜单「GEO 分析此页」打开侧边栏，自动提取当前页面正文并调用 GEO API 展示质量分数与优化建议
- **选中文本分析**：在任意网页选中文字后，右上角会悬浮出现「GEO 文本分析」按钮，点击即可对选中文本做即时分析
- **白标定制**：支持在选项页配置自定义品牌名称
- **Token 鉴权**：可选配置访问 Token，以 Bearer 方式调用受保护的 API

## 文件结构

```
chrome-extension/
├── manifest.json                  # MV3 清单文件（权限、入口、图标）
├── background/
│   └── service-worker.js          # 后台 Service Worker：action 点击、右键菜单、脚本注入
├── sidepanel.html                 # 侧边栏页面结构
├── sidepanel.js                   # 侧边栏逻辑：读取配置、发起分析、渲染结果
├── content-script.js              # 页面注入脚本：监听选中文本、悬浮按钮、弹窗结果
├── options.html                   # 选项页 UI（Endpoint / Token / 白标名称）
├── options.js                     # 选项页逻辑：保存/读取/测试连接
├── icons/
│   ├── icon16.svg
│   ├── icon32.svg
│   ├── icon48.svg
│   └── icon128.svg                # 扩展图标占位（SVG 可直接替换为 PNG）
└── README.md
```

## 安装步骤（开发模式加载已解压扩展）

1. 打开 Chrome 浏览器，在地址栏输入 **`chrome://extensions`** 并回车
2. 右上角打开 **「开发者模式」**（Developer mode）开关
3. 点击左上角的 **「加载已解压的扩展程序」**（Load unpacked）
4. 在弹出的文件选择对话框中，选择本目录：
   ```
   /Users/junjunyi/src-code/my-geo/integrations/chrome-extension
   ```
5. 加载成功后，扩展会出现在列表中，可以点击扩展工具栏的 🧩 图标把它固定到工具栏

## 首次使用配置

1. 右键扩展图标 → **「选项」**，或在 `chrome://extensions` 中点击扩展卡片的「详情」→「扩展程序选项」
2. 填写：
   - **GEO Endpoint**（必填）：例如 `https://geo.example.com`，会自动拼接 `/api/v1/cms/check`
   - **访问 Token**（可选）：如果 API 需要鉴权，填入 Bearer Token
   - **白标名称**（可选）：自定义品牌标题
3. 点击 **「测试连接」** 验证配置无误，再点击 **「保存设置」**

## 日常使用

### 方式一：分析整页正文
- 点击工具栏的扩展图标 → 侧边栏自动打开，点击「分析此页」
- 或在网页空白处右键 → **「GEO 分析此页」**

### 方式二：分析选中文本
- 在网页中用鼠标高亮选中一段文字
- 选区右上角会出现蓝色的 **「GEO 文本分析」** 悬浮按钮
- 点击按钮，弹出小卡片展示分数与优化建议

## GEO API 约定（`POST /api/v1/cms/check`）

扩展会以 JSON 形式 POST 以下字段：

```json
{
  "url": "https://example.com/page",
  "title": "页面标题",
  "html": "<p>正文 HTML（或选中文本的 HTML）</p>",
  "plainText": "纯文本版本",
  "mode": "selection"   // 仅文本分析时有，整页分析无该字段
}
```

期望响应：

```json
{
  "score": 82,
  "suggestions": [
    { "severity": "warning", "message": "标题长度建议 15-30 字" },
    { "severity": "good",    "message": "关键词分布良好" }
  ]
}
```

- `score` 范围 0-100，≥80 绿色、≥60 黄色、<60 红色
- `suggestions[].severity` 可选：`good` / `warning` / `error` / `info`
- 兼容字段别名：`overallScore`、`issues`、`level`、`text`、`description`、`title`

## 开发调试

- **Service Worker 调试**：`chrome://extensions` → 扩展卡片 → 「Service Worker」链接 → 打开 DevTools Console
- **侧边栏调试**：打开侧边栏后右键 → 「检查」
- **Content Script 调试**：在普通网页 DevTools Console 顶部下拉选择对应扩展上下文
- 修改代码后，回到 `chrome://extensions` 点击扩展卡片上的 🔄 **刷新按钮** 即可重新加载

## 已知限制

- 图标为 SVG 占位，正式发布前建议替换为 PNG 格式（Chrome Web Store 有时要求 PNG）
- Content Script 的选中文本分析直接从页面发请求，若 GEO 服务端未开启 CORS 会失败；可改为经 Service Worker 转发（已在 sidepanel 中使用此方式）
