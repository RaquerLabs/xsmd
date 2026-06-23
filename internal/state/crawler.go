package state

import (
	"log"
	"os"
	"path/filepath"
	"strings"
)

// FindProjectRoot looks upward for our anchor file
func FindProjectRoot(startPath string) (string, error) {
	current := filepath.Clean(startPath)
	for {
		markerPath := filepath.Join(current, "gcgb-md.toml")
		if _, err := os.Stat(markerPath); err == nil {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", os.ErrNotExist
		}
		current = parent
	}
}

// CrawlWorkspace looks for all markdown files underneath the project root
func (s *ServerState) CrawlWorkspace() error {
	s.Mu.Lock()
	defer s.Mu.Unlock()

	return filepath.WalkDir(s.WorkspaceRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && (strings.HasSuffix(d.Name(), ".md") || strings.HasSuffix(d.Name(), ".markdown")) {
			uri := "file://" + path
			err := s.ParseAndIndexFile(uri, path)
			if err != nil {
				log.Printf("Failed to parse %s: %v", path, err)
			}
		}
		return nil
	})
}
