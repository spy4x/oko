package gatus

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestParseHealthGreen(t *testing.T) {
	b := `<svg><path fill="#40cc11"/></svg>`
	got, err := parseHealth(b)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got == nil || *got != true {
		t.Errorf("got %v, want true", got)
	}
}

func TestParseHealthRed(t *testing.T) {
	b := `<svg><path fill="#e05d44"/></svg>`
	got, err := parseHealth(b)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got == nil || *got != false {
		t.Errorf("got %v, want false", got)
	}
}

func TestParseHealthUnknown(t *testing.T) {
	b := `<svg><path fill="#abcdef"/></svg>`
	got, err := parseHealth(b)
	if err == nil {
		t.Fatalf("expected error, got %v", got)
	}
	if got != nil {
		t.Errorf("got %v, want nil on error", got)
	}
}

func TestParseUptime(t *testing.T) {
	tests := []struct {
		name string
		body string
		want float64
		err  bool
	}{
		{"simple", `<svg><text>99.99%</text></svg>`, 99.99, false},
		{"whitespace", `<svg><text> 87.5% </text></svg>`, 87.5, false},
		{"in larger svg", `<svg><g><text>50.00%</text></g></svg>`, 50.0, false},
		{"no percent", `<svg><text>unknown</text></svg>`, 0, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseUptime(tc.body)
			if tc.err {
				if err == nil {
					t.Fatalf("expected error, got %v", *got)
				}
				return
			}
			if err != nil {
				t.Fatalf("err: %v", err)
			}
			if got == nil || *got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSplitKey(t *testing.T) {
	tests := []struct {
		in       string
		wantHost string
		wantEnd  string
		wantOK   bool
	}{
		{"uptime-cloud|home_audiobookshelf", "uptime-cloud", "home_audiobookshelf", true},
		{"cloud_mail-(https)", "", "", false},
		{"|foo", "", "", false},
		{"foo|", "", "", false},
		{"no_pipe", "", "", false},
		{"", "", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			h, e, ok := splitKey(tc.in)
			if ok != tc.wantOK || h != tc.wantHost || e != tc.wantEnd {
				t.Errorf("splitKey(%q) = (%q, %q, %v), want (%q, %q, %v)",
					tc.in, h, e, ok, tc.wantHost, tc.wantEnd, tc.wantOK)
			}
		})
	}
}

func TestFetchAll_Parallel(t *testing.T) {
	// Mock server with 200ms delay on each badge — proves parallelism.
	var healthHits, uptimeHits int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		path := strings.TrimPrefix(r.URL.Path, "/api/v1/endpoints/")
		path = strings.TrimSuffix(path, "/badge.svg")
		// Path is now "{endpoint}/{kind}" where kind may contain slashes
		// (e.g. "uptimes/30d"). Split at FIRST slash, not last.
		idx := strings.Index(path, "/")
		if idx < 0 {
			http.NotFound(w, r)
			return
		}
		key := path[:idx]
		kindStr := path[idx+1:]
		w.Header().Set("Content-Type", "image/svg+xml")
		switch kindStr {
		case "health":
			atomic.AddInt32(&healthHits, 1)
			_, _ = w.Write([]byte(`<svg><path fill="#40cc11"/></svg>`))
		case "uptimes/30d":
			atomic.AddInt32(&uptimeHits, 1)
			_, _ = w.Write([]byte(`<svg><text>99.99%</text></svg>`))
		default:
			http.NotFound(w, r)
		}
		_ = key
	}))
	defer srv.Close()

	// Extract host from https URL
	host := strings.TrimPrefix(srv.URL, "https://")
	c := NewClient([]string{host}, 2*time.Second)
	c.SetHTTPClient(srv.Client()) // trusts the test cert

	keys := []string{
		host[:strings.Index(host, ".")] + "|home_a",
		host[:strings.Index(host, ".")] + "|home_b",
		host[:strings.Index(host, ".")] + "|home_c",
		host[:strings.Index(host, ".")] + "|home_d",
	}
	start := time.Now()
	out, err := c.FetchAll(t.Context(), keys)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	// 4 endpoints × 2 badges = 8 sequential fetches × 200ms = 1.6s.
	// Parallel: ≤ 400ms (each endpoint fans out 2 badges; sequential between endpoints is fine for the test).
	if elapsed > 600*time.Millisecond {
		t.Errorf("took %v, want <600ms (parallel)", elapsed)
	}
	if len(out) != 4 {
		t.Errorf("got %d statuses, want 4", len(out))
	}
	if atomic.LoadInt32(&healthHits) != 4 || atomic.LoadInt32(&uptimeHits) != 4 {
		t.Errorf("hits health=%d uptime=%d, want 4/4", healthHits, uptimeHits)
	}
	for k, st := range out {
		if st.Healthy == nil || !*st.Healthy {
			t.Errorf("%s: expected healthy=true", k)
		}
		if st.Uptime == nil || *st.Uptime != 99.99 {
			t.Errorf("%s: expected uptime=99.99, got %v", k, st.Uptime)
		}
		if !strings.HasPrefix(st.DetailURL, "https://"+host+"/endpoints/") {
			t.Errorf("%s: detail URL %q", k, st.DetailURL)
		}
	}
}

func TestFetchAll_OneFailedOneOK(t *testing.T) {
	// Server only handles health; uptime returns 404.
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/v1/endpoints/")
		path = strings.TrimSuffix(path, "/badge.svg")
		idx := strings.Index(path, "/")
		if idx < 0 {
			http.NotFound(w, r)
			return
		}
		kindStr := path[idx+1:]
		if kindStr == "health" {
			w.Header().Set("Content-Type", "image/svg+xml")
			_, _ = w.Write([]byte(`<svg><path fill="#40cc11"/></svg>`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "https://")
	short := host[:strings.Index(host, ".")]
	c := NewClient([]string{host}, 2*time.Second)
	c.SetHTTPClient(srv.Client())

	out, err := c.FetchAll(t.Context(), []string{short + "|home_a"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	st, ok := out[short+"|home_a"]
	if !ok {
		t.Fatal("expected key in result map (one badge succeeded)")
	}
	if st.Healthy == nil || !*st.Healthy {
		t.Error("expected healthy=true")
	}
	if st.Uptime != nil {
		t.Errorf("expected uptime=nil, got %v", *st.Uptime)
	}
}

func TestFetchAll_BothFailed_KeyAbsent(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "https://")
	short := host[:strings.Index(host, ".")]
	c := NewClient([]string{host}, 2*time.Second)
	c.SetHTTPClient(srv.Client())

	out, err := c.FetchAll(t.Context(), []string{short + "|home_a"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if _, ok := out[short+"|home_a"]; ok {
		t.Error("expected key absent when both badges failed")
	}
}

func TestNewClient_HostMapping(t *testing.T) {
	c := NewClient([]string{"uptime-cloud.example.com", "uptime-home.example.com"}, time.Second)
	if c.hosts["uptime-cloud"] != "uptime-cloud.example.com" {
		t.Errorf("got %q", c.hosts["uptime-cloud"])
	}
	if c.hosts["uptime-home"] != "uptime-home.example.com" {
		t.Errorf("got %q", c.hosts["uptime-home"])
	}
}
