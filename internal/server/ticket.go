package server

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"my-geo/internal/auth"
	"my-geo/internal/httputil"
)

// Ticket 工单。
type Ticket struct {
	ID        string        `json:"id"`
	Subject   string        `json:"subject"`
	Content   string        `json:"content"`
	Category  string        `json:"category"` // bug/feature/billing/usage/other
	Priority  string        `json:"priority"` // low/medium/high/urgent
	Status    string        `json:"status"`   // open/in_progress/resolved/closed
	Contact   string        `json:"contact"`  // 联系方式
	Channel   string        `json:"channel"`  // web/feishu/email
	Replies   []TicketReply `json:"replies,omitempty"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
}

// TicketReply 工单回复。
type TicketReply struct {
	ID        string    `json:"id"`
	Content   string    `json:"content"`
	FromAdmin bool      `json:"from_admin"`
	CreatedAt time.Time `json:"created_at"`
}

// 工单内存存储。
// ⚠️ 进程内存态，重启丢失；多副本部署需迁移到 MySQL（当前仅单实例适用）。
var (
	ticketStore sync.Map // map[string]*Ticket
	ticketSeq   int64    // 工单自增序号
	replySeq    int64    // 回复自增序号
)

// ticketMu 保护 *Ticket 字段的读-改-写（sync.Map 只保护 map 本身，
// 共享指针的 Replies/Status/UpdatedAt 并发读写需另行串行化）。
var ticketMu sync.Mutex

// cloneTicket 深拷贝工单：读路径在锁内拷贝后返回，避免 JSON 序列化读到半写状态。
func cloneTicket(t *Ticket) *Ticket {
	cp := *t
	cp.Replies = append([]TicketReply(nil), t.Replies...)
	return &cp
}

// nextTicketID 生成工单 ID。
func nextTicketID() string {
	n := atomic.AddInt64(&ticketSeq, 1)
	return fmt.Sprintf("TKT-%06d", n)
}

// nextReplyID 生成回复 ID。
func nextReplyID() string {
	n := atomic.AddInt64(&replySeq, 1)
	return fmt.Sprintf("RPY-%06d", n)
}

// validTicketCategory 校验工单分类。
func validTicketCategory(c string) bool {
	switch c {
	case "bug", "feature", "billing", "usage", "other":
		return true
	}
	return false
}

// validTicketPriority 校验工单优先级。
func validTicketPriority(p string) bool {
	switch p {
	case "low", "medium", "high", "urgent":
		return true
	}
	return false
}

// validTicketStatus 校验工单状态。
func validTicketStatus(s string) bool {
	switch s {
	case "open", "in_progress", "resolved", "closed":
		return true
	}
	return false
}

// handleTickets 工单创建 / 列表。
// POST /api/v1/tickets
// GET  /api/v1/tickets?status=&category=&page=&limit=
func (s *Server) handleTickets(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.createTicket(w, r)
	case http.MethodGet:
		s.listTickets(w, r)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 GET/POST"})
	}
}

// createTicket 创建工单。
func (s *Server) createTicket(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Subject  string `json:"subject"`
		Content  string `json:"content"`
		Category string `json:"category"`
		Priority string `json:"priority"`
		Contact  string `json:"contact"`
		Channel  string `json:"channel"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	if strings.TrimSpace(body.Subject) == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "subject 不能为空"})
		return
	}
	if strings.TrimSpace(body.Content) == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "content 不能为空"})
		return
	}
	if !validTicketCategory(body.Category) {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Error: "category 仅支持 bug/feature/billing/usage/other",
			Code:  "INVALID_CATEGORY",
		})
		return
	}
	if !validTicketPriority(body.Priority) {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Error: "priority 仅支持 low/medium/high/urgent",
			Code:  "INVALID_PRIORITY",
		})
		return
	}
	if body.Channel == "" {
		body.Channel = "web"
	}
	now := time.Now()
	t := &Ticket{
		ID:        nextTicketID(),
		Subject:   body.Subject,
		Content:   body.Content,
		Category:  body.Category,
		Priority:  body.Priority,
		Status:    "open",
		Contact:   body.Contact,
		Channel:   body.Channel,
		CreatedAt: now,
		UpdatedAt: now,
	}
	ticketStore.Store(t.ID, t)
	writeJSON(w, http.StatusCreated, t)
}

// listTickets 工单列表（支持过滤与分页）。
func (s *Server) listTickets(w http.ResponseWriter, r *http.Request) {
	statusFilter := strings.TrimSpace(r.URL.Query().Get("status"))
	categoryFilter := strings.TrimSpace(r.URL.Query().Get("category"))
	page, limit := httputil.PageLimit(r, 20, 100)
	// 收集所有工单（读字段与并发回复写竞争，锁内拷贝快照）
	var tickets []*Ticket
	ticketStore.Range(func(_, v any) bool {
		t := v.(*Ticket)
		if statusFilter != "" && t.Status != statusFilter {
			return true
		}
		if categoryFilter != "" && t.Category != categoryFilter {
			return true
		}
		tickets = append(tickets, cloneTicket(t))
		return true
	})
	// 按创建时间倒序（新→旧）
	slices.SortFunc(tickets, func(a, b *Ticket) int {
		return b.CreatedAt.Compare(a.CreatedAt)
	})
	total := len(tickets)
	start := (page - 1) * limit
	if start > total {
		start = total
	}
	end := start + limit
	if end > total {
		end = total
	}
	var pageItems []*Ticket
	if start < end {
		pageItems = tickets[start:end]
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"total":   total,
		"page":    page,
		"limit":   limit,
		"tickets": pageItems,
	})
}

// handleTicketDetail 工单详情 / 回复 / 状态更新。
// GET  /api/v1/tickets/{id}
// POST /api/v1/tickets/{id}/reply
// PUT  /api/v1/tickets/{id}/status
func (s *Server) handleTicketDetail(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/tickets/")
	if rest == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "缺少工单 ID"})
		return
	}
	parts := strings.SplitN(rest, "/", 2)
	ticketID := parts[0]
	var sub string
	if len(parts) == 2 {
		sub = parts[1]
	}

	v, ok := ticketStore.Load(ticketID)
	if !ok {
		writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "工单不存在"})
		return
	}
	t := v.(*Ticket)

	switch {
	case sub == "reply":
		// 回复工单
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 POST"})
			return
		}
		var body struct {
			Content   string `json:"content"`
			FromAdmin bool   `json:"from_admin"`
		}
		if err := readJSON(r, &body); err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
			return
		}
		if strings.TrimSpace(body.Content) == "" {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "content 不能为空"})
			return
		}
		// from_admin 由服务端按管理员权限推导，忽略请求体声明（防伪造管理员回复）
		fromAdmin := auth.RequirePermission(r.Context(), auth.PermManageData) == nil
		reply := TicketReply{
			ID:        nextReplyID(),
			Content:   body.Content,
			FromAdmin: fromAdmin,
			CreatedAt: time.Now(),
		}
		ticketMu.Lock()
		t.Replies = append(t.Replies, reply)
		t.UpdatedAt = time.Now()
		// 管理员回复后自动转为处理中
		if fromAdmin && t.Status == "open" {
			t.Status = "in_progress"
		}
		out := cloneTicket(t)
		ticketMu.Unlock()
		writeJSON(w, http.StatusOK, out)
	case sub == "status":
		// 更新状态
		if r.Method != http.MethodPut {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 PUT"})
			return
		}
		var body struct {
			Status string `json:"status"`
		}
		if err := readJSON(r, &body); err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
			return
		}
		if !validTicketStatus(body.Status) {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{
				Error: "status 仅支持 open/in_progress/resolved/closed",
				Code:  "INVALID_STATUS",
			})
			return
		}
		// 状态变更属管理操作：启用账号体系时要求 PermManageData。
		// 工单无创建者归属字段，无法限定"仅创建者可关单"，先按管理权限收敛。
		if s.authSvc != nil && s.authSvc.Enabled() {
			if err := auth.RequirePermission(r.Context(), auth.PermManageData); err != nil {
				writeJSON(w, http.StatusForbidden, ErrorResponse{Error: err.Error(), Code: "PERMISSION_DENIED"})
				return
			}
		}
		ticketMu.Lock()
		t.Status = body.Status
		t.UpdatedAt = time.Now()
		out := cloneTicket(t)
		ticketMu.Unlock()
		writeJSON(w, http.StatusOK, out)
	case sub == "":
		// 工单详情
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 GET"})
			return
		}
		ticketMu.Lock()
		out := cloneTicket(t)
		ticketMu.Unlock()
		writeJSON(w, http.StatusOK, out)
	default:
		writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "未知的子路径: " + sub})
	}
}
