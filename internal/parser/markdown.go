package parser

import (
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf16"

	protocol "github.com/tliron/glsp/protocol_3_16"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
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

	var extractedLinks []ExtractedLink
	var docTitle string
	var hasH1 bool

	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		switch n.Kind() {
		case ast.KindLink:
			extractedLinks = append(extractedLinks, extractLink(content, lineOffsets, n.(*ast.Link)))

		case ast.KindHeading:
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

// extractLink computes the source ranges of an inline link: the whole
// '[label](destination)' construct and the destination path alone. Both are
// exact when the construct can be located in the raw source, and fall back to
// the parent block's line range otherwise (e.g. reference-style links).
func extractLink(content []byte, lineOffsets LineOffsetTable, ln *ast.Link) ExtractedLink {
	link := ExtractedLink{Path: string(ln.Destination)}

	// The link's label segments point into the raw source, so the construct
	// is located exactly: '[' before the first label byte, ']' after the
	// last. findLinkSpan then parses the destination and optional title
	// following goldmark's inline link grammar, so ranges stay precise for
	// titles, angle-bracket destinations, escapes, and destinations split
	// across lines. No source search is involved, so links never shadow or
	// displace each other.
	if labelStart, closeBracket, ok := linkLabelSpan(ln); ok {
		if _, pathStart, pathEnd, end, ok := findLinkSpan(content, closeBracket); ok {
			link.Range = positionRange(content, lineOffsets, labelStart, end)
			link.PathRange = positionRange(content, lineOffsets, pathStart, pathEnd)
			return link
		}
	}

	// Fallback to parent block line range
	parent := ln.Parent()
	for parent != nil && parent.Type() == ast.TypeInline {
		parent = parent.Parent()
	}
	var startLine, endLine uint32
	if parent != nil && parent.Lines().Len() > 0 {
		first := parent.Lines().At(0)
		last := parent.Lines().At(parent.Lines().Len() - 1)
		startLine = lineOffsets.GetLineFromOffset(first.Start)
		endLine = lineOffsets.GetLineFromOffset(last.Stop)
	}
	link.Range = protocol.Range{
		Start: protocol.Position{Line: startLine, Character: 0},
		End:   protocol.Position{Line: endLine, Character: 999},
	}
	link.PathRange = link.Range
	return link
}

// positionRange converts a byte span into an LSP range. LSP positions count
// UTF-16 code units, not bytes.
func positionRange(content []byte, lineOffsets LineOffsetTable, start, end int) protocol.Range {
	startLine := lineOffsets.GetLineFromOffset(start)
	endLine := lineOffsets.GetLineFromOffset(end)
	return protocol.Range{
		Start: protocol.Position{
			Line:      startLine,
			Character: utf16Len(content[lineOffsets[startLine]:start]),
		},
		End: protocol.Position{
			Line:      endLine,
			Character: utf16Len(content[lineOffsets[endLine]:end]),
		},
	}
}

// linkLabelSpan returns the raw source byte offsets of a link's label: the
// opening '[' and the closing ']'. The link's child segments (text, code,
// raw HTML) point into the original source, so the '[' is the byte right
// before the first segment and the ']' the byte right after the last.
func linkLabelSpan(n ast.Node) (openBracket, closeBracket int, ok bool) {
	first, last := -1, -1
	_ = ast.Walk(n, func(m ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		var segs []text.Segment
		switch t := m.(type) {
		case *ast.Text:
			segs = []text.Segment{t.Segment}
		case *ast.CodeSpan:
			if lines := t.Lines(); lines != nil {
				for i := range lines.Len() {
					segs = append(segs, lines.At(i))
				}
			}
		case *ast.RawHTML:
			for i := range t.Segments.Len() {
				segs = append(segs, t.Segments.At(i))
			}
		}
		for _, s := range segs {
			if first == -1 || s.Start < first {
				first = s.Start
			}
			if s.Stop > last {
				last = s.Stop
			}
		}
		return ast.WalkContinue, nil
	})
	if first == -1 {
		return 0, 0, false
	}
	return first - 1, last, true
}

// findLinkSpan locates the source byte span of an inline link starting at the
// ']' at closeBracket, mirroring goldmark's inline link grammar: optional
// angle-bracket destination, optional title, and the closing ')'. It returns
// the byte offsets of the opening '[', the destination path (end exclusive),
// and the position just past the closing ')'. ok is false when the construct
// does not parse as a valid inline link.
func findLinkSpan(content []byte, closeBracket int) (start, pathStart, pathEnd, end int, ok bool) {
	if closeBracket+1 >= len(content) || content[closeBracket+1] != '(' {
		return 0, 0, 0, 0, false
	}

	i := closeBracket + 2
	for i < len(content) && util.IsSpace(content[i]) {
		i++
	}
	pathStart = i

	terminated := false
	afterDest := 0
	if i < len(content) && content[i] == '<' {
		// Angle-bracket destination: ends at an unescaped '>'; cannot span
		// lines (goldmark scans a single line).
		pathStart++
		j := i + 1
		for j < len(content) {
			c := content[j]
			if c == '\\' && j+1 < len(content) && util.IsPunct(content[j+1]) {
				j += 2
				continue
			}
			if c == '>' {
				pathEnd = j
				j++
				terminated = true
				afterDest = j
				break
			}
			if c == '\n' {
				break
			}
			j++
		}
	} else {
		// Bare destination: ends at an unescaped space or an unescaped ')' at
		// depth 0 (nested parens are allowed, mirroring goldmark).
		opened := 0
		for i < len(content) {
			c := content[i]
			if c == '\\' && i+1 < len(content) && util.IsPunct(content[i+1]) {
				i += 2
				continue
			}
			if c == '(' {
				opened++
			} else if c == ')' {
				if opened == 0 {
					// The ')' itself closes the link, so the post-destination
					// scan resumes at it (unlike '<' in the angle form, which
					// is consumed as part of the destination syntax).
					pathEnd = i
					terminated = true
					afterDest = i
					break
				}
				opened--
			} else if util.IsSpace(c) {
				pathEnd = i
				terminated = true
				afterDest = i
				break
			}
			i++
		}
	}
	if !terminated {
		return 0, 0, 0, 0, false
	}

	// After the destination: optional whitespace (including newlines), then
	// ')' or a title, then whitespace and ')'.
	i = afterDest
	for i < len(content) && util.IsSpace(content[i]) {
		i++
	}
	if i < len(content) && content[i] == ')' {
		end = i + 1
	} else if i < len(content) && (content[i] == '"' || content[i] == '\'' || content[i] == '(') {
		opener := content[i]
		closer := opener
		if opener == '(' {
			closer = ')'
		}
		j := i + 1
		for j < len(content) {
			c := content[j]
			if c == '\\' && j+1 < len(content) {
				j += 2
				continue
			}
			if c == closer {
				j++
				break
			}
			j++
		}
		if j > 0 && j <= len(content) && content[j-1] == closer {
			for j < len(content) && util.IsSpace(content[j]) {
				j++
			}
			if j < len(content) && content[j] == ')' {
				end = j + 1
			}
		}
	}
	if end == 0 {
		return 0, 0, 0, 0, false
	}

	// Walk back to the opening '['.
	start = closeBracket
	for start > 0 && content[start] != '[' {
		start--
	}
	return start, pathStart, pathEnd, end, true
}

// utf16Len returns the number of UTF-16 code units in b, matching the LSP
// position encoding (astral runes count as two units).
func utf16Len(b []byte) uint32 {
	var n uint32
	for _, r := range string(b) {
		n += uint32(utf16.RuneLen(r))
	}
	return n
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
