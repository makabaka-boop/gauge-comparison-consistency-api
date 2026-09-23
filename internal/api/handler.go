// Package api exposes the calibration-network solver as a JSON HTTP API
// using only the standard library's net/http mux.
package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"unicode/utf8"

	"gaugenet/internal/solver"
)

const (
	// MaxStandards / MaxComparisons / MaxDelta mirror the service contract.
	MaxStandards   = 2000
	MinStandards   = 2
	MaxComparisons = 6000
	MaxDelta       = 1_000_000_000

	maxBodyBytes = 8 << 20 // 8 MiB
)

type comparisonInput struct {
	ID    string `json:"id"`
	From  string `json:"from"`
	To    string `json:"to"`
	Delta int64  `json:"delta"`
}

type solveRequest struct {
	Standards   []string          `json:"standards"`
	Comparisons []comparisonInput `json:"comparisons"`
}

type errorResponse struct {
	Error string `json:"error"`
}

// Handler wires the routes and returns the root http.Handler.
func Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /solve", handleSolve)
	return mux
}

func handleSolve(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	var req solveRequest
	if err := dec.Decode(&req); err != nil {
		status := http.StatusUnprocessableEntity
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			status = http.StatusRequestEntityTooLarge
		}
		writeJSON(w, status, errorResponse{Error: describeDecodeError(err)})
		return
	}
	// Reject trailing data after the JSON object.
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		writeJSON(w, http.StatusUnprocessableEntity, errorResponse{
			Error: "request body must contain exactly one JSON object",
		})
		return
	}

	records, err := validate(&req)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, errorResponse{Error: err.Error()})
		return
	}

	// standards are already deduplicated and in input order; the solver
	// sorts records itself, so pass the decoded slice through.
	result := solver.Solve(req.Standards, records)
	writeJSON(w, http.StatusOK, result)
}

// validate enforces every structural rule. Nothing past this point should
// ever trigger on malformed input: the solver assumes a valid instance.
func validate(req *solveRequest) ([]solver.Record, error) {
	if req.Standards == nil {
		return nil, errors.New("field 'standards' is required")
	}
	if len(req.Standards) < MinStandards {
		return nil, errors.New("field 'standards' must contain at least 2 entries")
	}
	if len(req.Standards) > MaxStandards {
		return nil, errors.New("field 'standards' must contain at most 2000 entries")
	}

	known := make(map[string]struct{}, len(req.Standards))
	for i, id := range req.Standards {
		if id == "" {
			return nil, errors.New("standards[" + itoa(i) + "] is empty")
		}
		if !isASCIIStandardID(id) {
			return nil, errors.New("standards[" + itoa(i) + "] must be non-empty printable ASCII without spaces")
		}
		if _, dup := known[id]; dup {
			return nil, errors.New("duplicate standard id: " + id)
		}
		known[id] = struct{}{}
	}

	if req.Comparisons == nil {
		// Absent comparisons means an empty network of isolated standards.
		req.Comparisons = []comparisonInput{}
	}
	if len(req.Comparisons) > MaxComparisons {
		return nil, errors.New("field 'comparisons' must contain at most 6000 entries")
	}

	records := make([]solver.Record, 0, len(req.Comparisons))
	recordIDs := make(map[string]struct{}, len(req.Comparisons))
	for i, c := range req.Comparisons {
		at := "comparisons[" + itoa(i) + "]"
		if c.ID == "" {
			return nil, errors.New(at + ".id must be a non-empty UTF-8 string")
		}
		if !utf8.ValidString(c.ID) {
			return nil, errors.New(at + ".id is not valid UTF-8")
		}
		if _, dup := recordIDs[c.ID]; dup {
			return nil, errors.New(at + ": duplicate comparison id: " + c.ID)
		}
		recordIDs[c.ID] = struct{}{}

		if c.From == "" || c.To == "" {
			return nil, errors.New(at + ".from and " + at + ".to must be non-empty standard ids")
		}
		if _, ok := known[c.From]; !ok {
			return nil, errors.New(at + ".from references unknown standard id: " + c.From)
		}
		if _, ok := known[c.To]; !ok {
			return nil, errors.New(at + ".to references unknown standard id: " + c.To)
		}
		if c.Delta > MaxDelta || c.Delta < -MaxDelta {
			return nil, errors.New(at + ".delta must be an integer in [-10^9, 10^9]")
		}

		records = append(records, solver.Record{
			ID:    c.ID,
			From:  c.From,
			To:    c.To,
			Delta: c.Delta,
		})
	}
	return records, nil
}

// isASCIIStandardID accepts printable ASCII excluding spaces and control
// characters (0x21..0x7E).
func isASCIIStandardID(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x21 || c > 0x7E {
			return false
		}
	}
	return true
}

func describeDecodeError(err error) string {
	var syntaxErr *json.SyntaxError
	if errors.As(err, &syntaxErr) {
		return "invalid JSON: " + err.Error()
	}
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		if typeErr.Field != "" {
			return "invalid JSON type at '" + typeErr.Field + "': " + err.Error()
		}
		return "invalid JSON type: " + err.Error()
	}
	return "invalid JSON: " + err.Error()
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
