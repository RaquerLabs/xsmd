package parser

import (
	"path/filepath"
	"strings"

	protocol "github.com/tliron/glsp/protocol_3_16"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// ExtractedLink stores information about the links found in documents
type ExtractedLink struct {
	Path  string
	Range protocol.Range
}

// ParseMarkdown parses raw byte arrays and extracts links and headings.
func ParseMarkdown(uri string, content []byte) (ast.Node, []ExtractedLink, string, bool) {
	md := goldmark.New()
	reader := text.NewReader(content)
	doc := md.Parser().Parse(reader)

	var lineOffsets []int
	lineOffsets = append(lineOffsets, 0)
	for i, b := range content {
		if b == '\n' {
			lineOffsets = append(lineOffsets, i+1)
		}
	}

	getLineFromOffset := func(offset int) uint32 {
		for lineNum, startOffset := range lineOffsets {
			if offset >= startOffset && (lineNum == len(lineOffsets)-1 || offset < lineOffsets[lineNum+1]) {
				return uint32(lineNum)
			}
		}
		return 0
	}

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

			pattern := "](" + destPath + ")"
			idx := -1
			if searchFrom < len(content) {
				idx = strings.Index(string(content[searchFrom:]), pattern)
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
			}

			extractedLinks = append(extractedLinks, ExtractedLink{
				Path: destPath,
				Range: protocol.Range{
					Start: protocol.Position{Line: startLine, Character: startChar},
					End:   protocol.Position{Line: endLine, Character: endChar},
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
