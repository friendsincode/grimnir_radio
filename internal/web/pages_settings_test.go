package web

import "testing"

func TestSanitizeUploadFilename(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{in: "../../../etc/passwd", want: "passwd"},
		{in: `..\\..\\windows\\system32\\drivers\\etc\\hosts`, want: "hosts"},
		{in: "/tmp/../../evil.tar.gz", want: "evil.tar.gz"},
		{in: "normal-backup.tar.gz", want: "normal-backup.tar.gz"},
	}

	for _, tc := range cases {
		if got := sanitizeUploadFilename(tc.in); got != tc.want {
			t.Fatalf("sanitizeUploadFilename(%q)=%q, want %q", tc.in, got, tc.want)
		}
	}
}
