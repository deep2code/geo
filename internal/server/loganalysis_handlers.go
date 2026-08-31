package server

import (
	"io"
	"my-geo/internal/brand/loganalysis"
	"net/http"
)

// handleLogAnalysis 分析上传的服务器日志，识别 AI 爬虫流量。
//
// POST /api/v1/brand/log-analysis
// 请求: multipart/form-data，字段 "log" 为日志文件
// 返回: TrafficSummary（AI 流量统计、爬虫分布、趋势等）
func (s *Server) handleLogAnalysis(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 POST"})
		return
	}

	// 限制上传大小（100MB）
	r.ParseMultipartForm(100 << 20)
	file, header, err := r.FormFile("log")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "请上传日志文件（字段名: log）"})
		return
	}
	defer file.Close()

	_ = header // 可用于校验文件名/类型

	entries, err := loganalysis.ParseNginxLog(file)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "日志解析失败: " + err.Error()})
		return
	}

	summary := loganalysis.AnalyzeTraffic(entries)
	writeJSON(w, http.StatusOK, summary)
}

// handleLogAnalysisText 分析文本形式的日志内容。
//
// POST /api/v1/brand/log-analysis/text
// 请求体: {"log_content": "..."}
// 返回: TrafficSummary
func (s *Server) handleLogAnalysisText(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 POST"})
		return
	}
	var req struct {
		LogContent string `json:"log_content"`
	}
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	if req.LogContent == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "log_content 不能为空"})
		return
	}

	entries, err := loganalysis.ParseNginxLog(io.NopCloser(
		io.Reader(&stringReader{data: req.LogContent}),
	))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "日志解析失败: " + err.Error()})
		return
	}

	summary := loganalysis.AnalyzeTraffic(entries)
	writeJSON(w, http.StatusOK, summary)
}

// stringReader 简单的字符串 reader。
type stringReader struct {
	data string
	pos  int
}

func (r *stringReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}
