package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RaquerLabs/xsmd/internal/state"
)

func TestLogToFileWritesToWorkspaceRoot(t *testing.T) {
	dir := t.TempDir()
	s := state.NewServerState()
	s.WorkspaceRoot = dir
	s.Debug = true

	logToFile(s)("hello workspace")

	content, err := os.ReadFile(filepath.Join(dir, "xsmd.log"))
	if err != nil {
		t.Fatalf("expected xsmd.log in workspace root: %v", err)
	}
	if !strings.Contains(string(content), "hello workspace") {
		t.Errorf("expected log message in xsmd.log, got %q", content)
	}
}

func TestLogToFileFallsBackToCwd(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	logToFile(state.NewServerState())("hello cwd")

	content, err := os.ReadFile(filepath.Join(dir, "xsmd.log"))
	if err != nil {
		t.Fatalf("expected xsmd.log in cwd when workspace root is empty: %v", err)
	}
	if !strings.Contains(string(content), "hello cwd") {
		t.Errorf("expected log message in xsmd.log, got %q", content)
	}
}
