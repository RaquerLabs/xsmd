package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigAndLogging(t *testing.T) {
	// Create a temp dir inside the workspace
	tempDir, err := os.MkdirTemp(".", "xsmd-config-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a dummy xsmd.toml file with debug = true
	tomlContent := []byte("# some comments\n\ndebug = true\n")
	tomlPath := filepath.Join(tempDir, "xsmd.toml")
	if err := os.WriteFile(tomlPath, tomlContent, 0644); err != nil {
		t.Fatalf("failed to write xsmd.toml: %v", err)
	}

	s := NewServerState()
	s.WorkspaceRoot = tempDir

	// Test loading config
	s.LoadConfig()
	if !s.Debug {
		t.Errorf("expected Debug to be true, got false")
	}

	// Test logging with debug = true
	var loggedMsg string
	s.DebugLog = func(msg string) {
		loggedMsg = msg
	}

	s.Log("hello debug")
	if loggedMsg != "hello debug" {
		t.Errorf("expected loggedMsg to be 'hello debug', got '%s'", loggedMsg)
	}

	// Test with debug = false
	tomlContentFalse := []byte("debug = false\n")
	if err := os.WriteFile(tomlPath, tomlContentFalse, 0644); err != nil {
		t.Fatalf("failed to write false xsmd.toml: %v", err)
	}

	s.LoadConfig()
	if s.Debug {
		t.Errorf("expected Debug to be false, got true")
	}

	loggedMsg = ""
	s.Log("should not log")
	if loggedMsg != "" {
		t.Errorf("expected no logging when Debug is false, got '%s'", loggedMsg)
	}
}

func TestLoadConfigIgnoreDirs(t *testing.T) {
	// Create a temp dir inside the workspace
	tempDir, err := os.MkdirTemp(".", "xsmd-config-ignore-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	tomlContent := []byte("ignore = [\"/journal\", \"/templates/daily\"]\ndebug = true\n")
	tomlPath := filepath.Join(tempDir, "xsmd.toml")
	if err := os.WriteFile(tomlPath, tomlContent, 0644); err != nil {
		t.Fatalf("failed to write xsmd.toml: %v", err)
	}

	s := NewServerState()
	s.WorkspaceRoot = tempDir

	s.LoadConfig()
	if !s.Debug {
		t.Errorf("expected Debug to be true")
	}

	expectedIgnore := []string{"/journal", "/templates/daily"}
	if len(s.IgnoreDirs) != len(expectedIgnore) {
		t.Fatalf("expected %d ignore dirs, got %d", len(expectedIgnore), len(s.IgnoreDirs))
	}
	for i, v := range expectedIgnore {
		if s.IgnoreDirs[i] != v {
			t.Errorf("expected IgnoreDirs[%d] to be '%s', got '%s'", i, v, s.IgnoreDirs[i])
		}
	}

	// Test IsIgnored checks
	testCases := []struct {
		path     string
		expected bool
	}{
		{"journal/entry.md", true},
		{"journal", true},
		{"templates/daily/meeting.md", true},
		{"templates/daily", true},
		{"journal-club/entry.md", false},
		{"templates/weekly/status.md", false},
		{"notes/general.md", false},
	}

	for _, tc := range testCases {
		res := s.IsIgnored(tc.path)
		if res != tc.expected {
			t.Errorf("path '%s': expected IsIgnored=%v, got %v", tc.path, tc.expected, res)
		}
	}
}
