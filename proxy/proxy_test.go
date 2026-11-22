package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseRequestLogLevel(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want RequestLogLevel
	}{
		{"off", LogOff},
		{"warnings", LogOnWarnings},
		{"all", LogAll},
	} {
		got, err := ParseRequestLogLevel(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("ParseRequestLogLevel(%q) = %v, %v; want %v", tc.in, got, err, tc.want)
		}
	}
	if _, err := ParseRequestLogLevel("verbose"); err == nil {
		t.Error("ParseRequestLogLevel(\"verbose\") returned nil error")
	}
}

func TestShouldLog(t *testing.T) {
	warned := &Warnings{}
	warned.add("rewriter: rule did not match")
	clean := &Warnings{}

	for _, tc := range []struct {
		level    RequestLogLevel
		method   string
		path     string
		warnings *Warnings
		want     bool
	}{
		{LogOff, http.MethodPost, "/v1/messages", clean, false},
		{LogOff, http.MethodPost, "/v1/messages", warned, false},
		{LogOnWarnings, http.MethodPost, "/v1/messages", clean, false},
		{LogOnWarnings, http.MethodPost, "/v1/messages", warned, true},
		{LogAll, http.MethodPost, "/v1/messages", clean, true},
		{LogAll, http.MethodGet, "/v1/messages", clean, false},
		{LogAll, http.MethodPost, "/v1/messages/count_tokens", clean, false},
	} {
		p := &Proxy{logLevel: tc.level}
		r := httptest.NewRequest(tc.method, tc.path, nil)
		if got := p.shouldLog(r, tc.warnings); got != tc.want {
			t.Errorf("level=%v %s %s warnings=%v: shouldLog = %v, want %v",
				tc.level, tc.method, tc.path, tc.warnings.HasWarnings(), got, tc.want)
		}
	}
}
