// Package dbprovider 统一按功能模块选择数据库后端的工厂层。
//
// 设计原则：
//  1. 按功能模块特征选型数据库（而不是"一刀切"全用 SQLite）；
//  2. 每个模块同时提供"高性能后端"与"零依赖后端（纯 Go/本地文件）"两套实现；
//  3. 通过环境变量按模块独立切换，且默认值全部为零依赖实现，保证默认行为与原工程一致；
//  4. 调用方只依赖接口（Store），不依赖具体实现，便于新增后端。
//
// 选型表（与模块一对一映射）：
//
//	| 模块               | 数据特征                                     | 高性能后端 | 零依赖兜底  | 环境变量开关                               |
//	|--------------------|----------------------------------------------|-----------|-------------|--------------------------------------------|
//	| OfflineCompanies   | 千万级行 + 全文检索 + 只读为主 + 低频批量导入 | DuckDB    | SQLite      | GEO_OFFLINE_DB_TYPE=duckdb/sqlite          |
//	| AuditHistory       | 时序写入 + 按品牌时间范围查询 + JSON 列      | MySQL     | SQLite      | GEO_HISTORY_DB_TYPE=mysql/sqlite           |
//	| ChinaCheckCache    | K/V + TTL + 高频读                           | Redis     | JSONL 文件  | GEO_CHINACHECK_CACHE_TYPE=redis/jsonl      |
package dbprovider

// Type 数据库后端类型（按模块独立配置）。
type Type string

const (
	// TypeAuto 表示由 provider 根据环境/可用性自动选择（一般等价于默认值）。
	TypeAuto Type = ""
	// TypeSQLite 纯 Go SQLite（modernc.org/sqlite，默认零依赖）。
	TypeSQLite Type = "sqlite"
	// TypeMySQL MySQL（需要网络服务，生产推荐）。
	TypeMySQL Type = "mysql"
	// TypeDuckDB DuckDB（列式 + 全文，千万级离线检索推荐）。
	// 运行时需要 libduckdb；未安装时回退 SQLite。
	TypeDuckDB Type = "duckdb"
	// TypeJSONL JSON Lines 本地文件（K/V 缓存用，零依赖）。
	TypeJSONL Type = "jsonl"
	// TypeRedis Redis（需要网络服务，高并发 K/V 缓存推荐）。
	TypeRedis Type = "redis"
)

// Known 所有已知后端类型（用于 UI/验证）。
var Known = []Type{TypeSQLite, TypeMySQL, TypeDuckDB, TypeJSONL, TypeRedis}

// String 返回小写形式（便于序列化 / 日志打印）。
func (t Type) String() string { return string(t) }

// RequiresExternal 该后端类型是否需要外部服务/二进制依赖。
// 返回 false 表示纯 Go/本地文件，可零依赖运行。
func (t Type) RequiresExternal() bool {
	switch t {
	case TypeMySQL, TypeDuckDB, TypeRedis:
		return true
	case TypeSQLite, TypeJSONL, TypeAuto:
		return false
	}
	return false
}

// Fallback 当请求的后端类型不可用时，对应应回退的零依赖类型。
func (t Type) Fallback() Type {
	switch t {
	case TypeMySQL, TypeSQLite, TypeDuckDB:
		return TypeSQLite
	case TypeRedis, TypeJSONL:
		return TypeJSONL
	}
	return TypeSQLite
}
