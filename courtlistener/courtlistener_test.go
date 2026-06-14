package courtlistener_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tamnd/courtlistener-cli/courtlistener"
)

func TestDefaultConfig(t *testing.T) {
	cfg := courtlistener.DefaultConfig()
	if cfg.Rate != 500*time.Millisecond {
		t.Errorf("Rate = %v, want 500ms", cfg.Rate)
	}
	if cfg.Retries <= 0 {
		t.Errorf("Retries = %d, want > 0", cfg.Retries)
	}
	if cfg.Timeout <= 0 {
		t.Errorf("Timeout = %v, want > 0", cfg.Timeout)
	}
	if cfg.UserAgent == "" {
		t.Error("UserAgent is empty")
	}
	if cfg.BaseURL == "" {
		t.Error("BaseURL is empty")
	}
}

func TestNewClientNotNil(t *testing.T) {
	c := courtlistener.NewClient()
	if c == nil {
		t.Fatal("NewClient returned nil")
	}
}

func TestOpinionRoundTrip(t *testing.T) {
	want := courtlistener.Opinion{
		ID:        42,
		CaseName:  "Roe v. Wade",
		Court:     "scotus",
		DateFiled: "1973-01-22",
		Status:    "Precedential",
		Citation:  []string{"410 U.S. 113"},
		Judge:     "Blackmun",
		Snippet:   "The right of privacy...",
		URL:       "https://www.courtlistener.com/opinion/42/roe-v-wade/",
	}
	b, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got courtlistener.Opinion
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != want.ID || got.CaseName != want.CaseName || got.Court != want.Court {
		t.Errorf("round-trip mismatch: got %+v, want %+v", got, want)
	}
	if len(got.Citation) != 1 || got.Citation[0] != "410 U.S. 113" {
		t.Errorf("Citation round-trip failed: got %v", got.Citation)
	}
}

// fakeSearchPayload constructs a minimal CourtListener search API response.
func fakeSearchPayload(id int, caseName, court string) []byte {
	payload := map[string]any{
		"count": 1,
		"results": []map[string]any{
			{
				"cluster_id":   id,
				"caseName":     caseName,
				"court_id":     court,
				"dateFiled":    "1973-01-22",
				"status":       "Precedential",
				"citation":     []string{"410 U.S. 113"},
				"judge":        "Blackmun",
				"snippet":      "The right of privacy...",
				"absolute_url": "/opinion/42/roe-v-wade/",
			},
		},
	}
	b, _ := json.Marshal(payload)
	return b
}

func TestSearchOpinionsHTTP(t *testing.T) {
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		ua := r.Header.Get("User-Agent")
		if ua == "" {
			t.Error("request carried no User-Agent")
		}
		q := r.URL.Query().Get("q")
		if q == "" {
			t.Error("search request missing q param")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fakeSearchPayload(42, "Roe v. Wade", "scotus"))
	}))
	defer srv.Close()

	cfg := courtlistener.DefaultConfig()
	cfg.BaseURL = srv.URL
	cfg.Rate = 0
	c := courtlistener.NewClientWithConfig(cfg)

	results, err := c.SearchOpinions(context.Background(), "abortion", "", "o", 5)
	if err != nil {
		t.Fatalf("SearchOpinions: %v", err)
	}
	if !called {
		t.Error("server was not called")
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	op := results[0]
	if op.ID != 42 {
		t.Errorf("ID = %d, want 42", op.ID)
	}
	if op.CaseName != "Roe v. Wade" {
		t.Errorf("CaseName = %q, want Roe v. Wade", op.CaseName)
	}
	if op.Court != "scotus" {
		t.Errorf("Court = %q, want scotus", op.Court)
	}
	if op.URL != "https://www.courtlistener.com/opinion/42/roe-v-wade/" {
		t.Errorf("URL = %q, want https://www.courtlistener.com/opinion/42/roe-v-wade/", op.URL)
	}
}

func TestSearchOpinionsCourtFilter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		court := r.URL.Query().Get("court")
		if court != "scotus" {
			t.Errorf("court param = %q, want scotus", court)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fakeSearchPayload(42, "Roe v. Wade", "scotus"))
	}))
	defer srv.Close()

	cfg := courtlistener.DefaultConfig()
	cfg.BaseURL = srv.URL
	cfg.Rate = 0
	c := courtlistener.NewClientWithConfig(cfg)

	results, err := c.SearchOpinions(context.Background(), "abortion", "scotus", "o", 5)
	if err != nil {
		t.Fatalf("SearchOpinions: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected at least one result")
	}
}

func TestSearchOpinionsRetriesOn503(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fakeSearchPayload(1, "Brown v. Board of Education", "scotus"))
	}))
	defer srv.Close()

	cfg := courtlistener.DefaultConfig()
	cfg.BaseURL = srv.URL
	cfg.Rate = 0
	cfg.Retries = 5
	c := courtlistener.NewClientWithConfig(cfg)

	start := time.Now()
	results, err := c.SearchOpinions(context.Background(), "civil rights", "", "o", 3)
	if err != nil {
		t.Fatalf("SearchOpinions after retries: %v", err)
	}
	if hits != 3 {
		t.Errorf("server saw %d hits, want 3", hits)
	}
	if time.Since(start) < 500*time.Millisecond {
		t.Error("retries did not back off")
	}
	if len(results) == 0 {
		t.Error("expected at least one result after retries")
	}
}

func TestSearchOpinionsContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := courtlistener.DefaultConfig()
	cfg.BaseURL = srv.URL
	cfg.Rate = 0
	cfg.Retries = 0
	c := courtlistener.NewClientWithConfig(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := c.SearchOpinions(ctx, "due process", "", "o", 5)
	if err == nil {
		t.Error("expected error with cancelled context, got nil")
	}
}
