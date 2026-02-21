package azuracast

import "testing"

func TestSafeExtractPath_RejectsTraversalAndAbsolute(t *testing.T) {
	dest := t.TempDir()

	if _, err := safeExtractPath(dest, "../outside.txt"); err == nil {
		t.Fatalf("expected traversal path to be rejected")
	}
	if _, err := safeExtractPath(dest, "/etc/passwd"); err == nil {
		t.Fatalf("expected absolute path to be rejected")
	}
}

func TestSafeExtractPath_AllowsNestedRelativePath(t *testing.T) {
	dest := t.TempDir()
	if _, err := safeExtractPath(dest, "media/station/song.mp3"); err != nil {
		t.Fatalf("expected nested relative path to be accepted: %v", err)
	}
}
