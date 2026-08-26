package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSplitAndTrim(t *testing.T) {
	tests := []struct {
		name string
		in   string
		sep  string
		want []string
	}{
		{"empty", "", ",", nil},
		{"single", "host", ",", []string{"host"}},
		{"two", "a,b", ",", []string{"a", "b"}},
		{"spaces", " a , b ", ",", []string{"a", "b"}},
		{"empties inside", "a,,b,", ",", []string{"a", "b"}},
		{"all whitespace", " , , ", ",", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := splitAndTrim(tc.in, tc.sep)
			if !equalSlices(got, tc.want) {
				t.Errorf("got %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestEnvInt(t *testing.T) {
	t.Setenv("TEST_INT_OK", "42")
	t.Setenv("TEST_INT_BAD", "not-a-number")
	if got := envInt("TEST_INT_OK", 7); got != 42 {
		t.Errorf("got %d, want 42", got)
	}
	if got := envInt("TEST_INT_BAD", 7); got != 7 {
		t.Errorf("got %d, want fallback 7", got)
	}
	if got := envInt("TEST_INT_MISSING", 7); got != 7 {
		t.Errorf("got %d, want fallback 7", got)
	}
}

func TestEnvOr(t *testing.T) {
	t.Setenv("TEST_OR_OK", "value")
	if got := envOr("TEST_OR_OK", "default"); got != "value" {
		t.Errorf("got %q, want value", got)
	}
	if got := envOr("TEST_OR_MISSING", "default"); got != "default" {
		t.Errorf("got %q, want default", got)
	}
}

func TestService_HasGatus(t *testing.T) {
	tests := []struct {
		name string
		s    Service
		want bool
	}{
		{"both set", Service{Endpoint: "home_a", GatusHost: "uptime-cloud"}, true},
		{"only endpoint", Service{Endpoint: "home_a"}, false},
		{"only host", Service{GatusHost: "uptime-cloud"}, false},
		{"neither", Service{}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.s.HasGatus(); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestService_Key(t *testing.T) {
	s := Service{Endpoint: "home_audiobookshelf", GatusHost: "uptime-cloud"}
	if got := s.Key(); got != "uptime-cloud|home_audiobookshelf" {
		t.Errorf("got %q", got)
	}
	s2 := Service{Name: "no-gatus"}
	if got := s2.Key(); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		fc      FileConfig
		wantErr string
	}{
		{
			"valid minimal",
			FileConfig{Servers: []Server{{Name: "Home", Services: []Service{{Name: "X", URL: "https://x"}}}, {Name: "Cloud", Services: []Service{{Name: "Y", URL: "https://y"}}}}},
			"",
		},
		{
			"empty servers ok",
			FileConfig{Title: "Foo"},
			"",
		},
		{
			"server no name",
			FileConfig{Servers: []Server{{Services: []Service{{Name: "X", URL: "https://x"}}}}},
			"name is required",
		},
		{
			"duplicate server",
			FileConfig{Servers: []Server{{Name: "Home", Services: []Service{{Name: "A", URL: "u"}}}, {Name: "Home", Services: []Service{{Name: "B", URL: "u"}}}}},
			"duplicate server name",
		},
		{
			"service no name",
			FileConfig{Servers: []Server{{Name: "Home", Services: []Service{{URL: "u"}}}}},
			"name is required",
		},
		{
			"service no url",
			FileConfig{Servers: []Server{{Name: "Home", Services: []Service{{Name: "X"}}}}},
			"url is required",
		},
		{
			"endpoint without gatus_host",
			FileConfig{Servers: []Server{{Name: "Home", Services: []Service{{Name: "X", URL: "u", Endpoint: "home_x"}}}}},
			"endpoint and gatus_host must both be set",
		},
		{
			"gatus_host without endpoint",
			FileConfig{Servers: []Server{{Name: "Home", Services: []Service{{Name: "X", URL: "u", GatusHost: "uptime-cloud"}}}}},
			"endpoint and gatus_host must both be set",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(tc.fc)
			if tc.wantErr == "" {
				if err != nil {
					t.Errorf("got error %v, want none", err)
				}
				return
			}
			if err == nil {
				t.Errorf("got nil, want error containing %q", tc.wantErr)
				return
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("got %q, want substring %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestFileConfig_EndpointKeys(t *testing.T) {
	fc := FileConfig{Servers: []Server{{
		Name: "Home",
		Services: []Service{
			{Name: "A", URL: "u1", Endpoint: "home_a", GatusHost: "uptime-cloud"},
			{Name: "B", URL: "u2"}, // no gatus
			{Name: "C", URL: "u3", Endpoint: "home_c", GatusHost: "uptime-home"},
			{Name: "D", URL: "u4", Hidden: true, Endpoint: "home_d", GatusHost: "uptime-cloud"},
		},
	}}}
	got := fc.EndpointKeys()
	want := []string{"uptime-cloud|home_a", "uptime-home|home_c", "uptime-cloud|home_d"}
	if !equalSlices(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestConfig_File_MtimeCache(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	if err := os.WriteFile(p, []byte(`{"servers":[{"name":"Home","services":[{"name":"A","url":"u"}]}]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	c := Config{filePath: p}

	// First load: reads from disk.
	fc, err := c.File()
	if err != nil {
		t.Fatalf("first load: %v", err)
	}
	if len(fc.Servers) != 1 || fc.Servers[0].Name != "Home" {
		t.Errorf("got %#v", fc)
	}

	// Mutate the file. Sleep first so mtime changes by ≥1s — most
	// filesystems only have 1s mtime granularity.
	time.Sleep(1100 * time.Millisecond)
	if err := os.WriteFile(p, []byte(`{"servers":[{"name":"Cloud","services":[{"name":"B","url":"v"}]}]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	// After mtime change, reload should pick up the new content.
	fc, err = c.File()
	if err != nil {
		t.Fatalf("second load: %v", err)
	}
	if len(fc.Servers) != 1 || fc.Servers[0].Name != "Cloud" {
		t.Errorf("expected reload to pick up new file, got %#v", fc)
	}
}

func TestConfig_File_MissingFile(t *testing.T) {
	c := Config{filePath: "/nonexistent/config.json"}
	_, err := c.File()
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

// helpers
func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
