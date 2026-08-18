# GEO 项目代码审核报告

> 审核日期：2026-08-18
> 审核范围：Go 后端（103 文件 / 34.8K 行）+ 前端 web-app + Docker/CI 部署配置
> 结论：架构设计良好（模块化接口抽象、SQL 全部参数化、多阶段构建、非 root 运行），但存在 **10 项 P0 级问题**（安全漏洞 + 构建阻断）、**12 项 P1**、**10 项 P2**。其中 2 项会导致 CI/Docker 构建直接失败，应立即修复。

---

## 一、总体评价

**做得好的方面：**
- 模块划分清晰：`optimizer / brand / scorer / llm / server` 职责边界合理，`offlinedb.DB / chinacheck.Cache / Strategy / Provider` 接口抽象恰当
- SQL 全部参数化，无注入风险；事务与 rows 关闭正确
- Docker 三阶段构建 + 非 root 用户 + setcap 低端口，安全基线不错
- 前端有代码分割（manualChunks）、PWA、i18n，工程化完整

**核心风险集中在三处：** 密钥/凭据管理、LLM 层成本与超时、缓存一致性；以及测试覆盖几乎为零。

---

## 二、P0 级问题（10 项，必须优先修复）

### P0-1 【构建阻断】vite.config.ts:59 硬编码本机绝对路径
```ts
outDir: '/Users/junjunyi/src-code/my-geo/internal/server/web/dist',
```
该路径仅在本机可用，**CI 和 Docker 构建会失败**或输出到错误位置。
修复：改为相对路径 `outDir: path.resolve(__dirname, '../internal/server/web/dist')`（文件已 import path，直接用）。

### P0-2 【安全】Admin Key / Bearer Token 明文存浏览器存储
`web-app/src/services/api.ts:75-93`：`localStorage.setItem(ADMIN_KEY, key)`、`setItem(AUTH_TOKEN_KEY, token)`。
XSS 可直接窃取管理员密钥提权。修复：Admin Key 改 `sessionStorage` 或 httpOnly Cookie；同时 `useAppStore.ts:93` 将 `favicon_url` 直接写入 DOM，需做协议白名单校验（仅允许 `https:`）。

### P0-3 【安全】日志泄露数据库密码
`auth.go:964`：`slog.Info("账号体系已启用", slog.String("db_path", st.Path()))`，而 `Path()`（auth.go:627）返回**含密码的完整 DSN**。日志一旦外泄即暴露数据库凭据。
修复：仿照 `dbprovider.go:126-129` 的 `Describe()` 脱敏（`user:***@tcp(...)`），或只记录主机名。

### P0-4 【安全】改密码后旧 refresh token 仍有效
`auth.go:765-777 UpdatePassword` 与 `handlers.go:327 ChangePassword` 均未吊销该用户的 refresh token。攻击者持有的旧 token 在用户改密后仍可刷新出新会话。
修复：改密时 `DELETE FROM refresh_tokens WHERE user_id=?` 全量吊销；`auth.go:1099` 的 `_ = svc.store.RevokeRefreshToken(...)` 吞错也要改为必须检查错误。

### P0-5 【安全】MySQL 默认明文传输
`dbprovider.go:72` 强制追加 `tls=false`。跨主机/跨网段部署时数据库账号密码与全部数据明文传输。
修复：默认改为 `tls=preferred` 或要求显式配置。

### P0-6 【安全】内部错误细节直接回给客户端
多处 `writeJSON(w, 500, ...err.Error())`（handlers.go:133/145/400/512/555，server.go brand/audit:1093、offlinedb/stats:1835、mail/send:1422 等），可能泄露 DSN、SMTP、LLM 密钥。
修复：全量替换为已有的 `writeInternalError`（server.go:1035），日志记详情、响应只给通用信息。

### P0-7 【功能 Bug】docker-compose 密码不一致
`docker-compose.yml:16` MySQL 用 `${GEO_MYSQL_PASSWORD:-geoPass}`，但 `:68-71` 应用 DSN 硬编码 `geo:geoPass@...`。用户一旦设置 `GEO_MYSQL_PASSWORD=strongPass`，应用连接必然失败。
修复：统一引用 `geo:${GEO_MYSQL_PASSWORD:-geoPass}@tcp(...)`。

### P0-8 【功能 Bug】健康检查路径不一致
Dockerfile:84 检查 `/api/v1/health`，docker-compose.yml:84 检查 `/healthz`，二者指向不同端点。
修复：统一为 `/api/v1/health`；同时 `/healthz`、`/readyz`（server.go:435-436）未列入鉴权白名单，启用 `GEO_API_KEY` 后 K8s 探活会 401，需一并加入 `PublicPaths`。

### P0-9 【安全】白标注入 HTML 未转义（XSS）
`server.go:636-649 buildInjectBlock` 将 `FaviconURL`、`PrimaryColor`、`BrandName` 直接拼入 HTML，未转义。环境变量可控时注入 `<script>`。
修复：用 `html/template` 或 `html.EscapeString`；favicon/logo URL 做协议白名单。

### P0-10 【安全】默认弱凭据 DSN 静默使用
`auth.go:541`：未配置 `GEO_AUTH_MYSQL_DSN` 时静默使用 `geo_auth:geo_auth_pass@tcp(127.0.0.1:3306)/geo_auth` 已知弱口令连接。
修复：生产环境未配置 DSN 时应直接报错退出，不落默认值。

---

## 三、P1 级问题（12 项，建议尽快修复）

### P1-1 并发安全：chinacheck 会话竞态 + goroutine 泄漏
`chinacheck.go:266 ensureSession` 的"读-判-写"无锁，并发请求会重复握手、覆盖 session；`chinacheck.go:333` 用 `go func()` 发 notifications 请求，调用量大时 goroutine 堆积且脱离 ctx 控制。
修复：初始化加 `sync.Once` 或 `sync.Mutex`；通知改为同步发送或复用调用方 context。

### P1-2 缓存一致性：JSONL 缓存过期条目"删而不持久"
`jsonl_cache.go:175 get()` 中过期条目仅从内存 map 删除，磁盘 JSONL 未清理；重启后 `load()` 重新读入已过期条目（脏数据复活）。
修复：删除时同步标记或周期性重写文件。

### P1-3 LLM 层成本/超时失控
`openai.go:99`：`io.ReadAll(resp.Body)` 无 `LimitReader`，恶意响应可致内存暴涨；请求体未设 `max_tokens`，`temperature` 写死 0.4，token 成本不可控；`llm.go:135` 无 per-call 超时包装。
修复：限流读取 + 暴露 max_tokens/temperature 参数 + per-call 超时。

### P1-4 缓存穿透/击穿无防护
`chinacheck.go:135`、`jsonl_cache.go`：未命中时并发相同查询会同时打到外部 API（无 singleflight）；空结果不入缓存导致不存在的品牌反复穿透网络。
修复：对 miss 加 singleflight/互斥，空结果做短 TTL 负缓存。

### P1-5 排行榜 N+1 查询
`server.go:3841-3873 getAllBrandLatestRecords` 先 `Brands()` 再循环逐品牌 `Latest()`，`handleLeaderboardBrand`（4015）再查一次。
修复：改为一条 `GROUP BY` 取最新记录。

### P1-6 登录无限流无锁定
`handlers.go:166-186 Login` 无失败计数、无锁定、无限流，可被暴力破解；失败时未记日志。
修复：按 IP/账号失败计数 + 指数退避 + 记日志；`requestIP`（handlers.go:80-93）直接信任 XFF，仅经可信代理时取 XFF。

### P1-7 JWT 手工实现缺陷
`auth.go:279-308 parseJWT`：`c.Exp > 0` 才检查过期——**无 exp 的 token 永不过期**；secret 无长度校验（注释要求 ≥32 字节但未执行）；Refresh 中 `err.Error()` 泄露签名细节。
修复：强制校验 exp 存在 + secret 长度，或改用成熟 JWT 库。

### P1-8 密码哈希迭代偏低
`auth.go:327` PBKDF2 120K 迭代低于 OWASP 建议（≥600K）；密码策略仅要求长度 ≥8。
修复：提高迭代数 + 增加复杂度校验。

### P1-9 邮件头注入 + 任意文件读取
`mail.go:255-258` To/Cc 未校验邮箱格式直接拼接（`\r\n` 可注入 Bcc）；`mail.go:330` `os.ReadFile(att.Source)` 若 Source 来自用户输入存在任意文件读取。
修复：校验/净化邮箱地址；附件路径加白名单。

### P1-10 STARTTLS 可降级明文
`mail.go:185-191` auto/starttls 模式下服务器不支持 STARTTLS 时明文发送 SMTP 凭据；`resolveTLSMode`（mail.go:412）端口 25 直接 none。
修复：starttls 模式强制要求，否则报错。

### P1-11 构建上下文与忽略文件缺失
- `.dockerignore` 缺 `web-app/node_modules/`（COPY web-app/ 会拖入本地依赖，可能覆盖容器内 npm ci 产物）、`integrations/`、`scripts/`、`.github/`、`docs/`、`deploy/`
- `.gitignore` 缺 `node_modules/`，存在误提交风险

### P1-12 二进制部署以 root 运行 + 弱密码
`deploy.sh:129,136` systemd `User=root`；`deploy.sh:58-63` 自动从模板创建 `.env` 导致默认弱密码上线。
修复：创建系统用户 `geo` 以 `User=geo` 运行；强制首次部署修改密码。

---

## 四、P2 级问题（10 项，代码质量）

| # | 问题 | 位置 | 建议 |
|---|------|------|------|
| P2-1 | server.go 4068 行巨型文件 | internal/server/server.go | 按 admin.go/healthz.go 先例拆出 report/chinacheck/offlinedb/history/leaderboard/compare/cms |
| P2-2 | 60+ 处 `if r.Method != xx{405}` | server.go 各 handler | Go ≥1.22 用 `mux.HandleFunc("POST /api/v1/analyze",...)` 免手动判断 |
| P2-3 | 错误响应格式不统一 | server.go | `map[string]string{"error":...}` 与 `ErrorResponse`（带 Code）混用，统一错误结构 + 错误码枚举 |
| P2-4 | 死代码 | server.go:724-727 handleHealth/handleReady、:349 humanBytes、:1060 scoreToGrade、middleware.go:331-349 withAuth/isPublicPath | 删除 |
| P2-5 | 重复代码：appendCitationPlaceholder/splitSentences | autorewriter.go:566 vs strategies/cite_sources.go:39-62 | 下沉共享包 |
| P2-6 | 重复参数笔误 | brand.go:437 `firstNonEmpty(ccSnap.CompanyName, ccSnap.CompanyName)` | 第二个参数应为其他字段 |
| P2-7 | 魔法数字 | scorer.go（0.8/0.2/60/15/75/30） | 命名常量 |
| P2-8 | GET 带副作用 | readiness/crawlability/ci-gate 的 GET 触发审计执行 | 改为 POST |
| P2-9 | 破坏性接口无角色分级 | offlinedb/clear、history/clear、chinacheck/cache 仅靠全局 API Key | 引入角色分级 |
| P2-10 | 生产构建开启 sourcemap | vite.config.ts:61 `sourcemap: true` | 改 `'hidden'` 或 `false` |

**其他：**
- 三处 MySQL 初始化（offlinedb / history/mysql / mysql_cache）SET NAMES+DDL 重复，可合并
- `history/mysql.go:175` `id, _ := res.LastInsertId()` 吞错
- `dbprovider.go:53/69` `multiStatements=true` 与 `interpolateParams=true` 扩大注入影响面，建议按模块收敛
- 限流 `getGlobalBucket`（server.go:452）每请求锁全局 map，高并发瓶颈，可改 sharded map
- `readIndexHTMLData`（server.go:652-675）每请求重读 embed + 5 次 ReplaceAll，应包级缓存一次
- README 声明 MIT 许可但 Dockerfile LABEL 写 Apache-2.0，需统一

---

## 五、测试覆盖（严重不足）

全仓 103 个 Go 文件仅 **5 个测试文件**（chinacheck、offlinedb、roi、uniqueness、cache）。**零测试**的模块：

- brand.go / scorer.go / readiness（熔断逻辑）/ autorewriter
- optimizer 全部 13 个策略
- llm.Manager 熔断器状态机
- history / crawler / mail / auth 的 token 生命周期

建议优先为**熔断器状态机、缓存过期/压缩边界、JWT 解析、策略验证**补充单元测试；CI 增加 `tsc --noEmit` 类型检查、ESLint、`govulncheck` 安全扫描。

---

## 六、建议修复路线图

**第一批（半天内，阻断项）**：P0-1 vite 路径 → P0-7 compose 密码 → P0-8 健康检查统一 → P0-3 日志脱敏
**第二批（1-2 天，安全加固）**：P0-2/4/5/6/9/10 → P1-6/7/8/9/10
**第三批（持续）**：P1-1/2/3/4/5 并发与缓存 → P2 系列代码质量 → 测试补齐
