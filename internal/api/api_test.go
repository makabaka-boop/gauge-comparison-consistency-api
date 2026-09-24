package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gaugeblock/internal/solver"
)

func TestHealthz(t *testing.T) {
	srv := httptest.NewServer(NewHandler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "ok" {
		t.Fatalf("body = %v", body)
	}
}

func TestCheckConsistent(t *testing.T) {
	req := Request{
		Standards: []string{"G1", "G2", "G3"},
		Records: []solver.Record{
			{ID: "c1", From: "G1", To: "G2", Delta: 100},
			{ID: "c2", From: "G2", To: "G3", Delta: 200},
			{ID: "c3", From: "G1", To: "G3", Delta: 300},
		},
	}
	rr := postCheck(t, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var res solver.Result
	if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if !res.Consistent {
		t.Fatalf("expected consistent, got %s", rr.Body.String())
	}
	if res.Values["G1"] != 0 || res.Values["G2"] != 100 || res.Values["G3"] != 300 {
		t.Fatalf("values = %v", res.Values)
	}
	// values 对象的 JSON 键必须按 id 排序。
	got := rr.Body.String()
	i1 := strings.Index(got, `"G1"`)
	i2 := strings.Index(got, `"G2"`)
	i3 := strings.Index(got, `"G3"`)
	if !(i1 < i2 && i2 < i3) {
		t.Fatalf("values keys not sorted: %s", got)
	}
}

func TestCheckConflict(t *testing.T) {
	req := Request{
		Standards: []string{"G1", "G2", "G3"},
		Records: []solver.Record{
			{ID: "c1", From: "G1", To: "G2", Delta: 100},
			{ID: "c2", From: "G2", To: "G3", Delta: 200},
			{ID: "c3", From: "G3", To: "G1", Delta: 1}, // 应为 G1-G3=-300
		},
	}
	rr := postCheck(t, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var res solver.Result
	if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.Consistent || res.Conflict == nil {
		t.Fatalf("expected conflict, got %s", rr.Body.String())
	}
	if res.Conflict.Record.ID != "c3" {
		t.Fatalf("failing record = %s", res.Conflict.Record.ID)
	}
	// 路径 G3->G2（反向 -200）->G1（反向 -100），合计 -300。
	if res.Conflict.ImpliedDelta != -300 || res.Conflict.Path.Total != -300 {
		t.Fatalf("conflict = %+v", res.Conflict)
	}
	if res.Conflict.Mismatch != 301 {
		t.Fatalf("mismatch = %d, want 301", res.Conflict.Mismatch)
	}
}

func TestStructuralErrorsAre422(t *testing.T) {
	cases := map[string]string{
		"malformed JSON":       `{not json`,
		"unknown field":        `{"standards":["A","B"],"records":[],"bogus":1}`,
		"too few standards":    `{"standards":["A"],"records":[]}`,
		"too many standards":   jsonStandards(2001, 0),
		"duplicate standards":  `{"standards":["A","A"],"records":[]}`,
		"non-ascii standard":   `{"standards":["A","块"],"records":[]}`,
		"empty standard id":    `{"standards":["A",""],"records":[]}`,
		"unknown from":         `{"standards":["A","B"],"records":[{"id":"r","from":"X","to":"B","delta":1}]}`,
		"unknown to":           `{"standards":["A","B"],"records":[{"id":"r","from":"A","to":"X","delta":1}]}`,
		"duplicate record id":  `{"standards":["A","B"],"records":[{"id":"r","from":"A","to":"B","delta":1},{"id":"r","from":"A","to":"B","delta":1}]}`,
		"empty record id":      `{"standards":["A","B"],"records":[{"id":"","from":"A","to":"B","delta":1}]}`,
		"delta too large":      `{"standards":["A","B"],"records":[{"id":"r","from":"A","to":"B","delta":1000000001}]}`,
		"delta too small":      `{"standards":["A","B"],"records":[{"id":"r","from":"A","to":"B","delta":-1000000001}]}`,
		"non-integer delta":    `{"standards":["A","B"],"records":[{"id":"r","from":"A","to":"B","delta":1.5}]}`,
		"wrong type standards": `{"standards":"A","records":[]}`,
		"two JSON values":      `{"standards":["A","B"],"records":[]}{}`,
		"too many records":     jsonStandards(2, 6001),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/check", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			NewHandler().ServeHTTP(rec, req)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422; body = %s", rec.Code, rec.Body.String())
			}
			var eb errorBody
			if err := json.Unmarshal(rec.Body.Bytes(), &eb); err != nil || eb.Error == "" {
				t.Fatalf("expected error body, got %s", rec.Body.String())
			}
		})
	}
}

func TestMethodNotAllowed(t *testing.T) {
	srv := httptest.NewServer(NewHandler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/check") // 仅允许 POST
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", resp.StatusCode)
	}
}

func TestBoundaryValuesAccepted(t *testing.T) {
	// delta 恰为 ±10^9、记录数恰为 6000 均应通过。
	const m = 6000
	req := Request{Standards: []string{"A", "B"}}
	req.Records = make([]solver.Record, 0, m)
	req.Records = append(req.Records, solver.Record{ID: "r0", From: "A", To: "B", Delta: 1_000_000_000})
	for i := 1; i < m; i++ {
		// 与 r0 一致的平行/自比较记录，delta 取边界值。
		if i%2 == 0 {
			req.Records = append(req.Records, solver.Record{ID: "r" + itoa(i), From: "B", To: "A", Delta: -1_000_000_000})
		} else {
			req.Records = append(req.Records, solver.Record{ID: "r" + itoa(i), From: "B", To: "B", Delta: 0})
		}
	}
	rr := postCheck(t, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rr.Code, rr.Body.String())
	}
	var res solver.Result
	if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if !res.Consistent || res.Values["B"] != 1_000_000_000 {
		t.Fatalf("unexpected: %s", rr.Body.String())
	}
}

func TestEmptyBodyIs422(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/check", strings.NewReader(""))
	rec := httptest.NewRecorder()
	NewHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
}

func TestEmptyRecordsIsConsistent(t *testing.T) {
	// 没有比较时各标准件自成零点。
	req := Request{Standards: []string{"A", "B"}}
	rr := postCheck(t, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rr.Code, rr.Body.String())
	}
	var res solver.Result
	if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if !res.Consistent || res.Values["A"] != 0 || res.Values["B"] != 0 {
		t.Fatalf("unexpected result: %s", rr.Body.String())
	}
}

func postCheck(t *testing.T, v any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/check", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	NewHandler().ServeHTTP(rec, req)
	return rec
}

// jsonStandards 构造一个规模测试请求：n 个标准件、m 条全部合法但可能超限的记录。
func jsonStandards(n, m int) string {
	var b strings.Builder
	b.WriteString(`{"standards":[`)
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`"S`)
		b.WriteString(itoa(i))
		b.WriteByte('"')
	}
	b.WriteString(`],"records":[`)
	for i := 0; i < m; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`{"id":"r`)
		b.WriteString(itoa(i))
		b.WriteString(`","from":"S0","to":"S1","delta":0}`)
	}
	b.WriteString(`]}`)
	return b.String()
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[pos:])
}
