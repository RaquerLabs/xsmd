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

func TestApplyInitializationOptions(t *testing.T) {
	s := NewServerState()

	s.ApplyInitializationOptions(map[string]any{
		"debug":  true,
		"ignore": []any{"/journal", "/templates/daily"},
	})

	if !s.Debug {
		t.Errorf("expected Debug to be true")
	}
	expected := []string{"/journal", "/templates/daily"}
	if len(s.IgnoreDirs) != len(expected) {
		t.Fatalf("expected %d ignore dirs, got %d", len(expected), len(s.IgnoreDirs))
	}
	for i, v := range expected {
		if s.IgnoreDirs[i] != v {
			t.Errorf("expected IgnoreDirs[%d] to be '%s', got '%s'", i, v, s.IgnoreDirs[i])
		}
	}
}

func TestApplyInitializationOptionsOverridesConfig(t *testing.T) {
	tempDir, err := os.MkdirTemp(".", "xsmd-config-init-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// xsmd.toml enables debug and ignores /journal
	tomlContent := []byte("ignore = [\"/journal\"]\ndebug = true\n")
	if err := os.WriteFile(filepath.Join(tempDir, "xsmd.toml"), tomlContent, 0644); err != nil {
		t.Fatalf("failed to write xsmd.toml: %v", err)
	}

	s := NewServerState()
	s.WorkspaceRoot = tempDir
	s.LoadConfig()

	// Client options win: disable debug, ignore /notes instead
	s.ApplyInitializationOptions(map[string]any{
		"debug":  false,
		"ignore": []any{"/notes"},
	})

	if s.Debug {
		t.Errorf("expected client debug=false to override toml debug=true")
	}
	expected := []string{"/notes"}
	if len(s.IgnoreDirs) != len(expected) || s.IgnoreDirs[0] != expected[0] {
		t.Errorf("expected ignore %v, got %v", expected, s.IgnoreDirs)
	}
}

func TestApplyInitializationOptionsNilAndUnknown(t *testing.T) {
	s := NewServerState()

	// nil options are a no-op
	s.ApplyInitializationOptions(nil)
	if s.Debug || len(s.IgnoreDirs) != 0 {
		t.Errorf("expected no-op for nil options, got Debug=%v IgnoreDirs=%v", s.Debug, s.IgnoreDirs)
	}

	// Unknown fields and non-decodable values must not panic or corrupt state
	s.ApplyInitializationOptions(map[string]any{"debug": true, "bogus": "x", "ignore": 42})
	if !s.Debug {
		t.Errorf("expected debug to be applied despite unknown fields")
	}
}

func TestOnDebugChangeFiresOnConfigAndInitOptions(t *testing.T) {
	tempDir, err := os.MkdirTemp(".", "xsmd-debug-change-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	s := NewServerState()
	s.WorkspaceRoot = tempDir

	var got []bool
	s.OnDebugChange = func(debug bool) { got = append(got, debug) }

	// debug = true in xsmd.toml must notify true
	if err := os.WriteFile(filepath.Join(tempDir, "xsmd.toml"), []byte("debug = true\n"), 0644); err != nil {
		t.Fatalf("failed to write xsmd.toml: %v", err)
	}
	s.LoadConfig()
	if len(got) != 1 || got[0] != true {
		t.Fatalf("expected one notification with true after LoadConfig, got %v", got)
	}

	// debug = false in xsmd.toml must notify false
	if err := os.WriteFile(filepath.Join(tempDir, "xsmd.toml"), []byte("debug = false\n"), 0644); err != nil {
		t.Fatalf("failed to write xsmd.toml: %v", err)
	}
	s.LoadConfig()
	if len(got) != 2 || got[1] != false {
		t.Fatalf("expected second notification with false, got %v", got)
	}

	// Client initializationOptions win and must notify their value
	s.ApplyInitializationOptions(map[string]any{"debug": true})
	if len(got) != 3 || got[2] != true {
		t.Fatalf("expected third notification with true from init options, got %v", got)
	}

	// No debug key in options: value unchanged, but the callback still fires
	// with the current value (the recorder treats it as a no-op).
	s.ApplyInitializationOptions(map[string]any{"ignore": []any{"/x"}})
	if len(got) != 4 || got[3] != true {
		t.Fatalf("expected fourth notification with unchanged true, got %v", got)
	}
}
