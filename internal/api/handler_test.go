package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gaugenet/internal/solver"
)

func doSolve(t *testing.T, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/solve", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, req)

	var out map[string]any
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("non-JSON response: %v: %s", err, rec.Body.String())
		}
	}
	return rec.Code, out
}

func TestHealthz(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz status = %d", rec.Code)
	}
}

func TestSolveConsistent(t *testing.T) {
	body := `{
		"standards": ["a", "b", "c"],
		"comparisons": [
			{"id": "e1", "from": "a", "to": "b", "delta": 2},
			{"id": "e2", "from": "b", "to": "c", "delta": 3},
			{"id": "e3", "from": "a", "to": "c", "delta": 5}
		]
	}`
	status, out := doSolve(t, body)
	if status != http.StatusOK {
		t.Fatalf("status = %d: %v", status, out)
	}
	if cons, _ := out["consistent"].(bool); !cons {
		t.Fatalf("expected consistent, got %v", out)
	}
	comps, ok := out["components"].([]any)
	if !ok || len(comps) != 1 {
		t.Fatalf("expected one component, got %v", out["components"])
	}
	comp := comps[0].(map[string]any)
	if comp["zero"] != "a" {
		t.Fatalf("zero = %v", comp["zero"])
	}
	values := comp["values"].([]any)
	want := []float64{0, 2, 5}
	for i, v := range values {
		row := v.(map[string]any)
		if row["value"] != want[i] {
			t.Fatalf("value %d = %v, want %v", i, row["value"], want[i])
		}
	}
}

func TestSolveConflictReportsPath(t *testing.T) {
	// Flipped sign on the closing edge of a four-node loop.
	body := `{
		"standards": ["s0", "s1", "s2", "s3"],
		"comparisons": [
			{"id": "r1", "from": "s0", "to": "s1", "delta": 1},
			{"id": "r2", "from": "s1", "to": "s2", "delta": 1},
			{"id": "r3", "from": "s2", "to": "s3", "delta": 1},
			{"id": "r4", "from": "s0", "to": "s3", "delta": -3}
		]
	}`
	status, out := doSolve(t, body)
	if status != http.StatusOK {
		t.Fatalf("conflict is a valid computation, status = %d: %v", status, out)
	}
	if cons, _ := out["consistent"].(bool); cons {
		t.Fatalf("expected inconsistent result: %v", out)
	}
	c := out["conflict"].(map[string]any)
	if c["recordId"] != "r4" || c["expected"] != float64(3) || c["residual"] != float64(-6) {
		t.Fatalf("unexpected conflict header: %v", c)
	}
	path := c["path"].([]any)
	if len(path) != 3 {
		t.Fatalf("path length = %d, want 3", len(path))
	}
	// Steps chain s0 -> s3 with cumulative 1, 2, 3.
	wantFrom := []string{"s0", "s1", "s2"}
	wantTo := []string{"s1", "s2", "s3"}
	wantCum := []float64{1, 2, 3}
	for i, st := range path {
		s := st.(map[string]any)
		if s["from"] != wantFrom[i] || s["to"] != wantTo[i] {
			t.Fatalf("step %d endpoints wrong: %v", i, s)
		}
		if s["cumulative"] != wantCum[i] {
			t.Fatalf("step %d cumulative = %v, want %v", i, s["cumulative"], wantCum[i])
		}
	}
	cycle := c["cycle"].([]any)
	if len(cycle) != 4 {
		t.Fatalf("cycle length = %d, want 4", len(cycle))
	}
	closing := cycle[3].(map[string]any)
	if closing["from"] != "s3" || closing["to"] != "s0" ||
		closing["signedDelta"] != float64(3) || closing["cumulative"] != float64(6) {
		t.Fatalf("closing step wrong: %v", closing)
	}
}

func TestStructuralErrorsAre422(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"invalid json", `{not json`},
		{"unknown field", `{"standards":["a","b"],"comparisons":[],"extra":1}`},
		{"trailing data", `{"standards":["a","b"],"comparisons":[]} garbage`},
		{"too few standards", `{"standards":["a"],"comparisons":[]}`},
		{"duplicate standards", `{"standards":["a","a"],"comparisons":[]}`},
		{"non ascii id", `{"standards":["a","b"],"comparisons":[]}`},
		{"unknown standard reference", `{"standards":["a","b"],"comparisons":[{"id":"x","from":"a","to":"z","delta":1}]}`},
		{"duplicate record id", `{"standards":["a","b"],"comparisons":[
			{"id":"x","from":"a","to":"b","delta":1},
			{"id":"x","from":"b","to":"a","delta":-1}]}`},
		{"delta out of range", `{"standards":["a","b"],"comparisons":[{"id":"x","from":"a","to":"b","delta":1000000001}]}`},
		{"delta wrong type", `{"standards":["a","b"],"comparisons":[{"id":"x","from":"a","to":"b","delta":"1"}]}`},
		{"missing standards", `{"comparisons":[]}`},
		{"empty record id", `{"standards":["a","b"],"comparisons":[{"id":"","from":"a","to":"b","delta":1}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, out := doSolve(t, tc.body)
			if status != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422: %v", status, out)
			}
			if _, ok := out["error"].(string); !ok {
				t.Fatalf("422 body missing error string: %v", out)
			}
			// Structural failure must never enter the computation.
			if _, present := out["conflict"]; present {
				t.Fatalf("structural error must not produce a conflict: %v", out)
			}
			if _, present := out["components"]; present {
				t.Fatalf("structural error must not produce components: %v", out)
			}
		})
	}
}

func TestEmptyComparisonsIsConsistent(t *testing.T) {
	body := `{"standards":["a","b"]}`
	status, out := doSolve(t, body)
	if status != http.StatusOK {
		t.Fatalf("status = %d: %v", status, out)
	}
	if cons, _ := out["consistent"].(bool); !cons {
		t.Fatalf("expected consistent: %v", out)
	}
	comps := out["components"].([]any)
	if len(comps) != 2 {
		t.Fatalf("two isolated standards => 2 components, got %d", len(comps))
	}
}

func TestConflictBodyRoundTripsSolverType(t *testing.T) {
	// Guards the JSON contract used by reviewers: decode a real response
	// back into solver types and assert the essential fields.
	body := `{"standards":["a","b"],"comparisons":[
		{"id":"e1","from":"a","to":"b","delta":1},
		{"id":"e2","from":"a","to":"b","delta":2}]}`
	status, raw := doSolve(t, body)
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	var res solver.Result
	if err := json.Unmarshal(encoded, &res); err != nil {
		t.Fatal(err)
	}
	if res.Consistent || res.Conflict == nil {
		t.Fatalf("expected conflict, got %+v", res)
	}
	if res.Conflict.RecordID != "e2" || res.Conflict.Expected != 1 ||
		res.Conflict.Delta != 2 || res.Conflict.Residual != 1 {
		t.Fatalf("decoded conflict wrong: %+v", res.Conflict)
	}
	if len(res.Conflict.Path) != 1 || res.Conflict.Path[0].RecordID != "e1" {
		t.Fatalf("decoded path wrong: %+v", res.Conflict.Path)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/solve", nil)
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestBodyTooLarge(t *testing.T) {
	// A syntactically plausible JSON prefix long enough that the decoder
	// hits the body limit while reading; invalid-but-short junk would
	// instead fail with a 422 syntax error.
	var buf bytes.Buffer
	buf.WriteString(`{"standards":["a","b"],"comparisons":[{"id":"`)
	buf.Write(bytes.Repeat([]byte("x"), maxBodyBytes))
	buf.WriteString(`"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/solve", &buf)
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
}
