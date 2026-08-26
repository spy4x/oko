package config

import (
	"reflect"
	"testing"
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
			if !reflect.DeepEqual(got, tc.want) {
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

func TestEndpointKey(t *testing.T) {
	s := Service{GatusHost: "uptime-cloud", Endpoint: "home_authelia"}
	if got := EndpointKey(s); got != "uptime-cloud|home_authelia" {
		t.Errorf("got %q", got)
	}
}

func TestSectionTitle(t *testing.T) {
	tests := map[string]string{
		"home":     "Home",
		"cloud":    "Cloud",
		"offsite":  "Offsite",
		"portable": "Portable",
		"unknown":  "unknown",
	}
	for in, want := range tests {
		if got := SectionTitle(in); got != want {
			t.Errorf("SectionTitle(%q) = %q, want %q", in, got, want)
		}
	}
}
