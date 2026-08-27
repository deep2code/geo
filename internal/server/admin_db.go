package server

import (
	"database/sql"
	"net/http"
	"strings"
	"time"

	"my-geo/internal/auth"
	"my-geo/internal/dbprovider"
)

// ===== 管理后台数据库管理 =====
// 支持在管理后台执行 SQL（管理员专用，PermManageData 鉴权）。
//
// 安全设计：
//   - 只允许单条语句（禁多语句拼接，防注入）；
//   - 写操作（DELETE/UPDATE/INSERT/DROP/ALTER/TRUNCATE 等）必须带 confirm_write=true
//     才执行，前端弹二次确认；
//   - SELECT 强制限制返回行数（默认 200，防大表拖垮服务）；
//   - 统一使用 GEO_MYSQL_DSN（单库架构），读连接池。

// execDBQuery 执行单条 SQL，返回结果集或影响行数。
func execDBQuery(query string, limit int, confirmWrite bool) (*DBQueryResult, error) {
	dsn := dbprovider.DSNFor(dbprovider.ModuleBilling)
	if strings.TrimSpace(dsn) == "" {
		return nil, &dbExecError{"GEO_MYSQL_DSN 未配置"}
	}
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, &dbExecError{"SQL 不能为空"}
	}
	upper := strings.ToUpper(q)

	// 写操作需二次确认
	if isWriteStatement(upper) && !confirmWrite {
		return nil, &dbWriteConfirmError{}
	}
	// SELECT 类限制行数
	if limit <= 0 || limit > 1000 {
		limit = 200
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(5 * time.Minute)

	// 非查询语句（写操作 / DDL）：Exec
	if !isQueryStatement(upper) {
		start := time.Now()
		res, err := db.Exec(q)
		if err != nil {
			return nil, err
		}
		rowsAffected, _ := res.RowsAffected()
		return &DBQueryResult{
			Kind:         "exec",
			RowsAffected: rowsAffected,
			DurationMs:   time.Since(start).Milliseconds(),
		}, nil
	}

	// 查询语句：Query（强制行数上限防大结果集）
	start := time.Now()
	rows, err := db.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	result := &DBQueryResult{Kind: "query", Columns: cols}
	rowBuf := make([]any, len(cols))
	rowPtrs := make([]any, len(cols))
	for i := range rowBuf {
		rowPtrs[i] = &rowBuf[i]
	}
	for rows.Next() {
		if err := rows.Scan(rowPtrs...); err != nil {
			return nil, err
		}
		rec := make([]any, len(cols))
		for i, v := range rowBuf {
			rec[i] = formatDBValue(v)
		}
		result.Rows = append(result.Rows, rec)
		if len(result.Rows) >= limit {
			result.Truncated = true
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	result.DurationMs = time.Since(start).Milliseconds()
	result.RowCount = len(result.Rows)
	return result, nil
}

// writeKeywords 写操作关键字（首词匹配，命中需二次确认）。
var writeKeywords = []string{
	"DELETE", "UPDATE", "INSERT", "REPLACE", "DROP",
	"ALTER", "TRUNCATE", "CREATE", "GRANT", "REVOKE", "RENAME",
}

// isWriteStatement 判断语句首词是否为写操作。
func isWriteStatement(upper string) bool {
	first := firstKeyword(upper)
	for _, kw := range writeKeywords {
		if first == kw {
			return true
		}
	}
	return false
}

// queryKeywords 查询类关键字（首词匹配）。
var queryKeywords = []string{"SELECT", "SHOW", "DESCRIBE", "DESC", "EXPLAIN"}

// isQueryStatement 判断语句首词是否为查询。
func isQueryStatement(upper string) bool {
	first := firstKeyword(upper)
	for _, kw := range queryKeywords {
		if first == kw {
			return true
		}
	}
	return false
}

// firstKeyword 提取语句首个有效关键字（跳过注释与括号）。
func firstKeyword(upper string) string {
	// 去掉行注释 / 块注释（含 MySQL 条件注释 /*!...*/ 和版本注释 /*50000...*/）
	for {
		if i := strings.Index(upper, "/*"); i >= 0 {
			if j := strings.Index(upper[i:], "*/"); j >= 0 {
				upper = upper[:i] + upper[i+j+2:]
				continue
			}
			upper = upper[:i]
			continue
		}
		break
	}
	upper = strings.TrimSpace(strings.TrimPrefix(upper, "--"))
	fields := strings.Fields(upper)
	if len(fields) == 0 {
		return ""
	}
	return strings.ToUpper(strings.Trim(fields[0], "(`'\" "))
}

// formatDBValue 将数据库值转为可 JSON 序列化的表示。
func formatDBValue(v any) any {
	switch t := v.(type) {
	case nil:
		return nil
	case []byte:
		return string(t)
	case time.Time:
		return t.Format("2006-01-02 15:04:05")
	default:
		return t
	}
}

type dbExecError struct{ msg string }

func (e *dbExecError) Error() string { return e.msg }

// dbWriteConfirmError 写操作未带确认时的错误。
type dbWriteConfirmError struct{}

func (e *dbWriteConfirmError) Error() string {
	return "写操作需要二次确认（confirm_write=true）"
}

// DBQueryResult SQL 执行结果。
type DBQueryResult struct {
	Kind         string   `json:"kind"` // query / exec
	Columns      []string `json:"columns,omitempty"`
	Rows         [][]any  `json:"rows,omitempty"`
	RowCount     int      `json:"row_count"`
	Truncated    bool     `json:"truncated"`
	RowsAffected int64    `json:"rows_affected"`
	DurationMs   int64    `json:"duration_ms"`
	Message      string   `json:"message,omitempty"`
}

// handleAdminDBExec 处理管理后台 SQL 执行请求。
//
// POST /api/v1/admin/db/exec  JSON {"sql":"...", "limit":200, "confirm_write":false}
// 需 Owner/Admin 角色（PermManageData）。所有执行（含被拦截的写操作）写入审计日志。
func (s *Server) handleAdminDBExec(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 POST"})
		return
	}
	if !s.requireDataAdmin(w, r) {
		return
	}
	var body struct {
		SQL          string `json:"sql"`
		Limit        int    `json:"limit"`
		ConfirmWrite bool   `json:"confirm_write"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	// 审计字段
	actorID, actor := "", "unknown"
	if u := auth.UserFromContext(r.Context()); u != nil {
		actorID, actor = u.ID, u.Email
	}
	ip := clientIP(r)
	ua := r.UserAgent()
	sqlSnippet := body.SQL
	if len(sqlSnippet) > 300 {
		sqlSnippet = sqlSnippet[:300] + "..."
	}
	action := "admin.db.exec"
	if body.ConfirmWrite {
		action = "admin.db.exec.confirmed"
	}
	writeAudit := func(status string, detail string) {
		s.appendAuditLog(&auth.AdminAuditLog{
			ActorID: actorID, Actor: actor, Action: action, Target: "database",
			Details: map[string]string{
				"sql":    sqlSnippet,
				"status": status,
				"detail": detail,
			},
			IP: ip, UserAgent: ua,
		})
	}

	result, err := execDBQuery(body.SQL, body.Limit, body.ConfirmWrite)
	if err != nil {
		// 写操作未确认 → 200 + 需确认标记（前端据此弹确认框），记审计（blocked）
		if _, ok := err.(*dbWriteConfirmError); ok {
			writeAudit("blocked", "写操作未二次确认被拦截")
			writeJSON(w, http.StatusOK, map[string]any{"need_confirm": true, "error": err.Error()})
			return
		}
		writeAudit("error", err.Error())
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	writeAudit("ok", "")
	writeJSON(w, http.StatusOK, result)
}

// appendAuditLog 追加审计日志（authSvc 未启用时静默跳过）。
func (s *Server) appendAuditLog(al *auth.AdminAuditLog) {
	if s.authSvc == nil {
		return
	}
	_ = s.authSvc.Store().AppendAuditLog(al)
}
