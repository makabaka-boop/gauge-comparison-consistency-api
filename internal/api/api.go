// Package api 提供纯后端 JSON API：POST /check 校验比较记录构成的校准网，
// GET /healthz 做存活检查。
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"gaugeblock/internal/solver"
)

// 输入规模与数值上限（与任务约束一致）。
const (
	minStandards = 2
	maxStandards = 2000
	maxRecords   = 6000
	maxDelta     = 1_000_000_000
)

// Request 是 /check 的请求体。
type Request struct {
	Standards []string        `json:"standards"`
	Records   []solver.Record `json:"records"`
}

// NewHandler 构造 API 的路由处理器。
func NewHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /check", handleCheck)
	return mux
}

func handleCheck(w http.ResponseWriter, r *http.Request) {
	var req Request
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid JSON body: "+err.Error())
		return
	}
	// 请求体中只允许一个 JSON 值。
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusUnprocessableEntity, "request body must contain a single JSON object")
		return
	}

	if err := Validate(req); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, solver.Solve(req.Standards, req.Records))
}

// Validate 只做结构与范围校验，不进入任何数值约束传播。
func Validate(req Request) error {
	if len(req.Standards) < minStandards {
		return fmt.Errorf("standards: need at least %d ids", minStandards)
	}
	if len(req.Standards) > maxStandards {
		return fmt.Errorf("standards: at most %d ids allowed", maxStandards)
	}

	seen := make(map[string]struct{}, len(req.Standards))
	for _, id := range req.Standards {
		if !isGraphicASCII(id) {
			return fmt.Errorf("standards: id %q must be non-empty printable ASCII (0x21-0x7e)", id)
		}
		if _, dup := seen[id]; dup {
			return fmt.Errorf("standards: duplicate id %q", id)
		}
		seen[id] = struct{}{}
	}

	if len(req.Records) > maxRecords {
		return fmt.Errorf("records: at most %d comparisons allowed", maxRecords)
	}

	recIDs := make(map[string]struct{}, len(req.Records))
	for i, rec := range req.Records {
		if rec.ID == "" {
			return fmt.Errorf("records[%d]: id must be non-empty", i)
		}
		if _, dup := recIDs[rec.ID]; dup {
			return fmt.Errorf("records[%d]: duplicate record id %q", i, rec.ID)
		}
		recIDs[rec.ID] = struct{}{}

		if _, ok := seen[rec.From]; !ok {
			return fmt.Errorf("records[%d] (%s): from %q is not a known standard", i, rec.ID, rec.From)
		}
		if _, ok := seen[rec.To]; !ok {
			return fmt.Errorf("records[%d] (%s): to %q is not a known standard", i, rec.ID, rec.To)
		}
		if rec.Delta > maxDelta || rec.Delta < -maxDelta {
			return fmt.Errorf("records[%d] (%s): delta %d out of range [-%d,%d]", i, rec.ID, rec.Delta, maxDelta, maxDelta)
		}
	}
	return nil
}

// isGraphicASCII 要求 id 非空且每个字节都是可见 ASCII（不含空格）。
func isGraphicASCII(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x21 || c > 0x7e {
			return false
		}
	}
	return true
}

type errorBody struct {
	Error string `json:"error"`
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorBody{Error: msg})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
