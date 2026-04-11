package dashboard

import "testing"

func TestLooksLikeBatchImportIgnoresTrailingNewlineOnSingleURL(t *testing.T) {
	if looksLikeBatchImport("https://youtu.be/abc123?si=test\n") {
		t.Fatal("single URL with trailing newline should not be treated as batch import")
	}
}
