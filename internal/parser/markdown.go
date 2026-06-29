package parser

import (
	"bytes"
	"path/filepath"
	"sort"
	"strings"

	protocol "github.com/tliron/glsp/protocol_3_16"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// ExtractedLink stores information about the links found in documents
type ExtractedLink struct {
	Path      string
	Range     protocol.Range
	PathRange protocol.Range
}

// ParseMarkdown parses raw byte arrays and extracts links and headings.
func ParseMarkdown(uri string, content []byte) (ast.Node, []ExtractedLink, string, bool) {
	md := goldmark.New()
	reader := text.NewReader(content)
	doc := md.Parser().Parse(reader)

	lineOffsets := NewLineOffsetTable(content)
	getLineFromOffset := lineOffsets.GetLineFromOffset

	var extractedLinks []ExtractedLink
	var docTitle string
	var hasH1 bool
	searchFrom := 0

	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		// Extract links
		if entering && n.Kind() == ast.KindLink {
			ln := n.(*ast.Link)
			destPath := string(ln.Destination)

			var startLine, endLine uint32
			var startChar, endChar uint32
			var pathStartLine, pathEndLine uint32
			var pathStartChar, pathEndChar uint32

			pattern := []byte("](" + destPath + ")")
			idx := -1
			if searchFrom < len(content) {
				idx = bytes.Index(content[searchFrom:], pattern)
			}

			if idx != -1 {
				absPatternStart := searchFrom + idx
				endByte := absPatternStart + len(pattern)

				startByte := absPatternStart
				for startByte > searchFrom && content[startByte] != '[' {
					startByte--
				}

				startLine = getLineFromOffset(startByte)
				endLine = getLineFromOffset(endByte)
				startChar = uint32(startByte - lineOffsets[startLine])
				endChar = uint32(endByte - lineOffsets[endLine])

				pathStartByte := absPatternStart + 2
				pathEndByte := pathStartByte + len(destPath)

				pathStartLine = getLineFromOffset(pathStartByte)
				pathEndLine = getLineFromOffset(pathEndByte)
				pathStartChar = uint32(pathStartByte - lineOffsets[pathStartLine])
				pathEndChar = uint32(pathEndByte - lineOffsets[pathEndLine])

				searchFrom = endByte
			} else {
				// Fallback to parent block line range
				parent := n.Parent()
				for parent != nil && parent.Type() == ast.TypeInline {
					parent = parent.Parent()
				}
				if parent != nil && parent.Lines().Len() > 0 {
					first := parent.Lines().At(0)
					last := parent.Lines().At(parent.Lines().Len() - 1)
					startLine = getLineFromOffset(first.Start)
					endLine = getLineFromOffset(last.Stop)
				}
				startChar = 0
				endChar = 999
				pathStartLine = startLine
				pathEndLine = endLine
				pathStartChar = startChar
				pathEndChar = endChar
			}

			extractedLinks = append(extractedLinks, ExtractedLink{
				Path: destPath,
				Range: protocol.Range{
					Start: protocol.Position{Line: startLine, Character: startChar},
					End:   protocol.Position{Line: endLine, Character: endChar},
				},
				PathRange: protocol.Range{
					Start: protocol.Position{Line: pathStartLine, Character: pathStartChar},
					End:   protocol.Position{Line: pathEndLine, Character: pathEndChar},
				},
			})
		}

		// Extract the main H1 Title
		if entering && n.Kind() == ast.KindHeading {
			heading := n.(*ast.Heading)
			if heading.Level == 1 && docTitle == "" {
				var headingText strings.Builder
				for i := 0; i < heading.Lines().Len(); i++ {
					line := heading.Lines().At(i)
					headingText.Write(content[line.Start:line.Stop])
				}
				docTitle = strings.TrimSpace(headingText.String())
				hasH1 = true
			}
		}

		return ast.WalkContinue, nil
	})

	// Fallback title for files without an H1
	if docTitle == "" {
		docTitle = filepath.Base(strings.TrimPrefix(uri, "file://"))
	}

	return doc, extractedLinks, docTitle, hasH1
}

// LineOffsetTable is a helper for converting byte offsets to line numbers.
type LineOffsetTable []int

// NewLineOffsetTable creates a LineOffsetTable from a byte array.
func NewLineOffsetTable(content []byte) LineOffsetTable {
	var table []int
	table = append(table, 0)
	for i, b := range content {
		if b == '\n' {
			table = append(table, i+1)
		}
	}
	return table
}

// GetLineFromOffset converts a byte offset to a 0-indexed line number.
func (t LineOffsetTable) GetLineFromOffset(offset int) uint32 {
	idx := sort.Search(len(t), func(i int) bool {
		return t[i] > offset
	})
	if idx > 0 {
		return uint32(idx - 1)
	}
	return 0
}

// FindLinkAtPosition locates an ExtractedLink at the specified position.
func FindLinkAtPosition(links []ExtractedLink, pos protocol.Position) *ExtractedLink {
	var targetLink *ExtractedLink
	cursorLine := pos.Line
	cursorChar := pos.Character

	for i := range links {
		link := &links[i]
		if cursorLine >= link.Range.Start.Line && cursorLine <= link.Range.End.Line {
			if link.Range.Start.Line == link.Range.End.Line {
				if cursorChar >= link.Range.Start.Character && cursorChar <= link.Range.End.Character {
					targetLink = link
					break
				}
			} else {
				onStartLine := cursorLine == link.Range.Start.Line
				onEndLine := cursorLine == link.Range.End.Line
				if (!onStartLine || cursorChar >= link.Range.Start.Character) &&
					(!onEndLine || cursorChar <= link.Range.End.Character) {
					targetLink = link
					break
				}
			}
		}
	}

	if targetLink == nil {
		for i := range links {
			link := &links[i]
			if cursorLine >= link.Range.Start.Line && cursorLine <= link.Range.End.Line {
				targetLink = link
				break
			}
		}
	}
	return targetLink
}
