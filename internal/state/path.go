package state

import (
	"path/filepath"
	"strings"
)

// CleanURIPath converts a URI (which may have double or triple slashes, e.g. file:// or file:///)
// to a standardized absolute filesystem path.
func CleanURIPath(uri string) string {
	p := uri
	prefixes := []string{"file://localhost", "file:///", "file://", "file:/", "file:"}
	for _, prefix := range prefixes {
		if strings.HasPrefix(p, prefix) {
			p = strings.TrimPrefix(p, prefix)
			break
		}
	}
	// On Windows, a URI might look like /C:/path. Trim the leading slash if followed by drive letter.
	if len(p) >= 3 && p[0] == '/' && p[2] == ':' && ((p[1] >= 'a' && p[1] <= 'z') || (p[1] >= 'A' && p[1] <= 'Z')) {
		p = p[1:]
	}
	if !strings.HasPrefix(p, "/") && !filepath.IsAbs(p) {
		p = "/" + p
	}
	return filepath.Clean(p)
}

// CleanURIPath converts a URI to a standardized absolute filesystem path.
func (s *ServerState) CleanURIPath(uri string) string {
	return CleanURIPath(uri)
}


// ResolveLinkPath resolves a link path (which may be relative to the source URI or absolute to workspace root)
// to a standardized absolute filesystem path. It also strips anchors.
func (s *ServerState) ResolveLinkPath(sourceURI string, linkPath string) string {
	if idx := strings.Index(linkPath, "#"); idx != -1 {
		linkPath = linkPath[:idx]
	}

	if strings.HasPrefix(linkPath, "/") {
		cleanPath := filepath.Clean(linkPath)
		cleanPath = strings.TrimPrefix(cleanPath, string(filepath.Separator))
		cleanPath = strings.TrimPrefix(cleanPath, "/")
		return filepath.Join(s.WorkspaceRoot, cleanPath)
	}

	sourceAbsPath := CleanURIPath(sourceURI)
	sourceDir := filepath.Dir(sourceAbsPath)
	return filepath.Clean(filepath.Join(sourceDir, linkPath))
}
