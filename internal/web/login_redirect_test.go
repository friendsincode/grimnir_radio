package web

import "testing"

func TestSanitizeRedirectTarget(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: ""},
		{name: "safe path", in: "/dashboard", want: "/dashboard"},
		{name: "safe path with query", in: "/dashboard/media?q=abc", want: "/dashboard/media?q=abc"},
		{name: "safe path with fragment", in: "/dashboard#sec", want: "/dashboard#sec"},
		{name: "external absolute url", in: "https://evil.example/phish", want: ""},
		{name: "scheme-relative url", in: "//evil.example/phish", want: ""},
		{name: "javascript scheme", in: "javascript:alert(1)", want: ""},
		{name: "relative path without slash", in: "dashboard", want: ""},
		{name: "login path blocked", in: "/login", want: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := sanitizeRedirectTarget(tc.in); got != tc.want {
				t.Fatalf("sanitizeRedirectTarget(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
