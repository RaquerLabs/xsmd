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
		if rest, ok := strings.CutPrefix(p, prefix); ok {
			p = rest
			break
		}
	}
	// On Windows, a URI might look like /C:/path. Trim the leading slash if followed by drive letter.
	isWindowsDrivePath := len(p) >= 3 && p[0] == '/' && isASCIILetter(p[1]) && p[2] == ':'
	if isWindowsDrivePath {
		p = p[1:]
	}
	if !strings.HasPrefix(p, "/") && !filepath.IsAbs(p) {
		p = "/" + p
	}
	return filepath.Clean(p)
}

func isASCIILetter(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// SplitAnchor separates a link path from its trailing #anchor, if present.
func SplitAnchor(linkPath string) (path, anchor string) {
	if idx := strings.Index(linkPath, "#"); idx != -1 {
		return linkPath[:idx], linkPath[idx:]
	}
	return linkPath, ""
}

// ResolveLinkPath resolves a link path (which may be relative to the source URI or absolute to workspace root)
// to a standardized absolute filesystem path. It also strips anchors.
func (s *ServerState) ResolveLinkPath(sourceURI string, linkPath string) string {
	linkPath, _ = SplitAnchor(linkPath)

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
