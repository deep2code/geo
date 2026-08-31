package server

import (
	"my-geo/internal/brand/simulator"
	"net/http"
)

// handleSimulate 内容模拟：修改→查询→对比的 A/B 模拟管道。
//
// POST /api/v1/brand/simulate
// 请求体: SimulateRequest
// 返回: SimulateResult（含对比分析与建议）
func (s *Server) handleSimulate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 POST"})
		return
	}

	var req simulator.SimulateRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	// 使用品牌引擎的监控器获取适配器
	mon := s.brandEngine.Monitor()
	if mon == nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "品牌监控引擎未初始化"})
		return
	}

	sim := simulator.NewSimulator(mon.Adapters())
	result, err := sim.Simulate(r.Context(), &req)
	if err != nil {
		writeInternalError(w, err, "")
		return
	}

	writeJSON(w, http.StatusOK, result)
}
