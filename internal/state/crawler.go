package state

import (
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/RaquerLabs/xsmd/internal/parser"
)

// FindProjectRoot looks upward for our anchor file
func FindProjectRoot(startPath string) (string, error) {
	current := filepath.Clean(startPath)
	for {
		markerPath := filepath.Join(current, "xsmd.toml")
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
	// Safely copy the ignore list under a read-lock
	s.Mu.RLock()
	ignoreDirs := make([]string, len(s.IgnoreDirs))
	copy(ignoreDirs, s.IgnoreDirs)
	workspaceRoot := s.WorkspaceRoot
	s.Mu.RUnlock()

	return filepath.WalkDir(workspaceRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(workspaceRoot, path)
		if err != nil {
			return err
		}

		// If this is a directory, check if it should be completely skipped
		if d.IsDir() {
			relSlash := filepath.ToSlash(rel)
			for _, ignored := range ignoreDirs {
				cleanIgnored := strings.Trim(ignored, "/")
				if cleanIgnored == "" {
					continue
				}
				if relSlash == cleanIgnored || strings.HasPrefix(relSlash, cleanIgnored+"/") {
					return filepath.SkipDir
				}
			}
			return nil
		}

		// Check if it's a Markdown file
		if strings.HasSuffix(d.Name(), ".md") || strings.HasSuffix(d.Name(), ".markdown") {
			// Redundancy check: make sure the file itself isn't ignored
			relSlash := filepath.ToSlash(rel)
			for _, ignored := range ignoreDirs {
				cleanIgnored := strings.Trim(ignored, "/")
				if cleanIgnored == "" {
					continue
				}
				if relSlash == cleanIgnored || strings.HasPrefix(relSlash, cleanIgnored+"/") {
					return nil
				}
			}

			uri := "file://" + path
			content, err := os.ReadFile(path)
			if err != nil {
				log.Printf("Failed to read watched file %s: %v", path, err)
				return nil
			}

			// Parse outside of the mutex lock to prevent blocking concurrent LSP requests
			doc, links, title, hasH1 := parser.ParseMarkdown(uri, content)

			// Only lock when writing to the shared Index map
			s.Mu.Lock()
			s.Index[uri] = &DocumentInfo{
				URI:     uri,
				Content: content,
				AST:     doc,
				Links:   links,
				Title:   title,
				HasH1:   hasH1,
			}
			s.Mu.Unlock()
		}
		return nil
	})
}
