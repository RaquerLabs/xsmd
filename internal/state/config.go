package state

import (
	"bufio"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// LoadConfig reads xsmd.toml from the workspace root and updates the Debug state and ignore list.
func (s *ServerState) LoadConfig() {
	s.Mu.Lock()
	defer s.Mu.Unlock()

	// Default to false and empty slice
	s.Debug = false
	s.IgnoreDirs = nil

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

		switch key {
		case "debug":
			s.Debug = (val == "true")
		case "ignore":
			val = strings.TrimSpace(val)
			isArray := strings.HasPrefix(val, "[") && strings.HasSuffix(val, "]")
			if isArray {
				arrStr := val[1 : len(val)-1]
				parts := strings.Split(arrStr, ",")
				var list []string
				for _, p := range parts {
					item := strings.TrimSpace(p)
					item = strings.Trim(item, `"'`) // trim quotes
					if item != "" {
						list = append(list, item)
					}
				}
				s.IgnoreDirs = list
			}
		}
	}
	if err := scanner.Err(); err != nil {
		log.Printf("Failed reading xsmd.toml: %v", err)
	}
}

// IsMarkdownPath reports whether path names a Markdown file.
func IsMarkdownPath(path string) bool {
	return strings.HasSuffix(path, ".md") || strings.HasSuffix(path, ".markdown")
}

// isIgnoredRelPath reports whether the workspace-relative, slash-separated path
// falls inside any of the ignored directories.
func isIgnoredRelPath(relToRootSlash string, ignoreDirs []string) bool {
	for _, ignored := range ignoreDirs {
		cleanIgnored := strings.Trim(ignored, "/")
		if cleanIgnored == "" {
			continue
		}
		if relToRootSlash == cleanIgnored || strings.HasPrefix(relToRootSlash, cleanIgnored+"/") {
			return true
		}
	}
	return false
}

// IsIgnored checks if the relative path relToRoot is within any of the configured ignored directories.
// The caller must hold at least a read lock (s.Mu.RLock()).
func (s *ServerState) IsIgnored(relToRoot string) bool {
	return isIgnoredRelPath(filepath.ToSlash(relToRoot), s.IgnoreDirs)
}
