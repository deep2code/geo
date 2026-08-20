# 环境变量 → 数据库配置存储（管理后台可改）

> 落地日期：2026-08-20
> 需求：项目所有环境变量改为数据库中存储，带默认值，管理后台提供修改界面。

## 读取链（三级）

```
config.Env(key, fallback)
  → 1. DB app_settings.svalue（非空，非 bootstrap 项）   ← 管理后台可改，即时生效
  → 2. 环境变量 GEO_*
  → 3. 代码默认值 fallback
```

- **bootstrap 项例外**：引导/安全边界变量（`*_MYSQL_DSN`、`GEO_JWT_SECRET`）必须在 DB 加载前确定，
  DB 不覆盖，管理后台**只读**展示。共 6 项。
- **requires_restart**：启动时一次性消费的连接类变量（HTTP 端口、Redis、SMTP、支付凭据等），
  DB 修改后提示"需重启服务生效"。其余运行期读取的变量**保存即生效**。

## 启动流程

```
main.go: LoadDotEnv → config.Validate() → config.InitSettings(GEO_AUTH_MYSQL_DSN, seed=true)
                                             ├─ seed：注册表默认值幂等写入 app_settings（不覆盖用户已改 value）
                                             ├─ load：加载 svalue 非空项为内存覆盖表
                                             └─ 失败仅告警不退出 → 回退环境变量+默认值，系统照常运行
```

设置库与账号体系同库（`GEO_AUTH_MYSQL_DSN`）。

## 配置注册表

- `internal/config/settings_catalog_gen.go`：**自动生成**（`scripts/gen_settings_catalog.py`），
  扫描全部 `config.Env("GEO_*","default")` / `os.Getenv("GEO_*")` 调用点（72 项），
  自动推断分类（前缀映射）/敏感标记/类型/引导标记。
- `internal/config/settings_catalog.go`：**手工补充**——引擎 API Key / BaseURL / Model（代码中动态拼接
  `engineEnvKeys`，脚本抓不到）+ 需重启标注的连接类变量。
- 合并逻辑 `mergeCatalog`：手工项标注优先，去重。

### 重新生成注册表

```bash
/opt/homebrew/bin/python3 scripts/gen_settings_catalog.py
```

## 管理后台 API

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/v1/admin/settings?category=&q=` | 列出全部配置（secret 脱敏），含分类统计 |
| PUT | `/api/v1/admin/settings` | 更新 `{key, value}`；secret 传空/`********`=不变 |
| POST | `/api/v1/admin/settings/reset` | 恢复默认 `{key}` |

鉴权：与其它 `/api/admin/*` 一致（`X-Admin-Key`）。`GEO_ADMIN_KEY` 读取已改为
`config.Env`（DB 覆盖优先），管理后台改 key 立即生效。

## 前端

- `web-app/src/pages/Admin/SettingsTab.tsx`：新 Tab「系统设置」
  - 分类筛选 chips + 关键字搜索；按分类分组表格
  - 每行：变量名 / 描述 / 类型 / 当前值（secret 掩码）/ 来源徽标（DB/环境变量/默认值/未设置）
  - 编辑弹窗（secret 用密码框，留空=不变）；重置默认；requires_restart 项黄字提示
  - bootstrap 项禁用编辑
- i18n：zh-CN / en / ja 三语（admin.settings* 约 30 key）

## 数据表

`deploy/initdb/02-schema.sql` 第 12 节：`app_settings(skey PK, svalue, default_value, description,
category, stype, is_secret, is_bootstrap, requires_restart, updated_at)`

## 验证

- `go build ./...` ✅ ｜ `go vet ./...` ✅ ｜ `go test ./internal/config/ ./internal/server/` ✅
- 前端 `npx tsc -b --noEmit` ✅
- config 包新增单测：读取链顺序 / 来源标记 / catalog 无重复 / bootstrap 校验 / 引擎 key 登记

## 备注

- `os.Getenv` 直读点已尽量收敛到 `config.Env`；`config.Validate()` 保留环境变量语义（启动期 DB 未加载）。
- 迁移注意：老部署无该表时 InitSettings seed 会失败 → 仅告警，功能回退环境变量模式；执行一次
  `deploy/initdb/02-schema.sql` 后自动启用。
