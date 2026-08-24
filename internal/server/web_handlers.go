package server

import (
	"io"
	"net/http"
	"runtime"
	"strings"
)

// serveIndexHTML 返回 SPA 的 index.html 入口文件，并执行白标占位符替换。
func (s *Server) serveIndexHTML(w http.ResponseWriter) bool {
	indexData, err := readIndexHTMLData()
	if err != nil {
		http.Error(w, "页面加载失败", http.StatusInternalServerError)
		return false
	}
	html := string(indexData)
	html = s.whitelabel.applyWhitelabelToHTML(html)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, html)
	return true
}

// handleWebSPA Web SPA 前端静态资源服务 + SPA 路由回退。
//
// 路由规则：
//   - /api/* /healthz /readyz 前缀由其他路由处理（此 handler 不处理 API）
//   - 根路径 / 直接返回 web/dist/index.html
//   - 其他路径若在 web/dist/ 下存在静态文件（如 /assets/*），直接返回并带正确 Content-Type
//   - 静态文件不存在且非 API 路径时，回退到 index.html（SPA 前端路由）
//   - 若 web/dist/index.html 也不存在（未执行 npm build），降级返回 web/index.html 作为 dev fallback
func (s *Server) handleWebSPA(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	// 排除 API/健康检查路径（双重保险，正常情况下不会进入此 handler）
	if strings.HasPrefix(path, "/api/") ||
		strings.HasPrefix(path, "/healthz") ||
		strings.HasPrefix(path, "/readyz") {
		http.NotFound(w, r)
		return
	}

	// 根路径：直接返回 index.html
	if path == "/" {
		s.serveIndexHTML(w)
		return
	}

	// 去掉开头的 "/"，构建相对于 web/dist 的路径
	relPath := strings.TrimPrefix(path, "/")

	// 尝试作为静态文件返回
	if serveStaticFile(w, relPath) {
		return
	}

	// 静态文件未命中，SPA 路由回退到 index.html
	s.serveIndexHTML(w)
}

func (s *Server) handleWhitelabel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 GET"})
		return
	}
	writeJSON(w, http.StatusOK, s.whitelabel)
}

// handleMetaSystem 公开构建信息（无需登录）。
// GET /api/v1/meta/system
// 仅返回非敏感构建元数据（打包版本/git-hash/打包时间/打包操作系统），
// 供前端首页/页脚展示；不含内存、goroutine、磁盘等运行时细节（那些在 /api/v1/admin/system）。
func (s *Server) handleMetaSystem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 GET"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"version":       geoVersion,
		"build_version": buildVersion,
		"build_commit":  buildCommit,
		"build_at":      buildAt,
		"build_os":      buildOS,
		"go_version":    runtime.Version(),
	})
}
