package state

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// LoadConfig reads xsmd.toml from the workspace root and updates the Debug state.
func (s *ServerState) LoadConfig() {
	s.Mu.Lock()
	defer s.Mu.Unlock()

	// Default to false
	s.Debug = false

	if s.WorkspaceRoot == "" {
		return
	}

	tomlPath := filepath.Join(s.WorkspaceRoot, "xsmd.toml")
	f, err := os.Open(tomlPath)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Ignore empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		if key == "debug" {
			s.Debug = (val == "true")
		}
	}
}
