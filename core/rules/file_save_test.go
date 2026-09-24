package rules

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Saving onto a path that is an existing directory fails at the final
// rename, and the store must surface that instead of losing the rules.
func TestFileStoreSaveOntoDirectoryPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "as-dir")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	s := &FileStore{path: path}
	err := s.save([]Rule{{ID: "r", Match: "true", Route: []RouteStep{{Channels: []string{"c"}}}}})
	if err == nil || !strings.Contains(err.Error(), "rules: replace") {
		t.Fatalf("expected a rename failure, got %v", err)
	}
}
