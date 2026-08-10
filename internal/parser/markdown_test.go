package parser

import (
	"testing"
)

func TestParseMarkdown(t *testing.T) {
	content := []byte(`# Note Title

Some text with a [link to test](docs/test.md) and another [broken link](docs/broken.md) on the next line.
`)

	uri := "file:///workspace/note.md"
	doc, links, title, hasH1 := ParseMarkdown(uri, content)

	if doc == nil {
		t.Fatal("expected non-nil AST document")
	}

	if title != "Note Title" {
		t.Errorf("expected title 'Note Title', got '%s'", title)
	}

	if !hasH1 {
		t.Errorf("expected hasH1 to be true")
	}

	if len(links) != 2 {
		t.Fatalf("expected 2 extracted links, got %d", len(links))
	}

	expectedLinks := []struct {
		path      string
		startLine uint32
		endLine   uint32
	}{
		{"docs/test.md", 2, 2},
		{"docs/broken.md", 2, 2},
	}

	for i, l := range links {
		exp := expectedLinks[i]
		if l.Path != exp.path {
			t.Errorf("link %d: expected path '%s', got '%s'", i, exp.path, l.Path)
		}
		if l.Range.Start.Line != exp.startLine || l.Range.End.Line != exp.endLine {
			t.Errorf("link %d: expected range lines %d-%d, got %d-%d", i, exp.startLine, exp.endLine, l.Range.Start.Line, l.Range.End.Line)
		}
	}
}

func TestParseMarkdownUTF16Positions(t *testing.T) {
	content := []byte("# T\n\nBroken é: [Missing](missing.md)\n")
	_, links, _, _ := ParseMarkdown("file:///t.md", content)
	if len(links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(links))
	}
	l := links[0]
	// LSP characters are UTF-16 code units: "Broken é: " is 10 units (é is 2
	// bytes but 1 unit) and "[Missing](" is 10 more, so the path spans units
	// 20-30. A byte-based count would report 22-32 instead.
	if l.PathRange.Start.Character != 20 || l.PathRange.End.Character != 30 {
		t.Errorf("expected PathRange units 20-30, got %d-%d", l.PathRange.Start.Character, l.PathRange.End.Character)
	}
	if l.PathRange.Start.Line != 2 || l.PathRange.End.Line != 2 {
		t.Errorf("expected PathRange on line 2, got lines %d-%d", l.PathRange.Start.Line, l.PathRange.End.Line)
	}
}

func TestParseMarkdownMultilineDestination(t *testing.T) {
	content := []byte("- [x] [execrising](/areas/health/fitness/running/exercising.mdd\n)\n\n- [ ] [next](/next.md)\n")
	_, links, _, _ := ParseMarkdown("file:///t.md", content)
	if len(links) != 2 {
		t.Fatalf("expected 2 links, got %d", len(links))
	}
	// Destination split across lines: goldmark ends the destination at the
	// newline and closes the link on the next line. The path range must stay
	// on the first line, exactly over the path.
	l0 := links[0]
	if l0.PathRange.Start.Line != 0 || l0.PathRange.Start.Character != 19 ||
		l0.PathRange.End.Line != 0 || l0.PathRange.End.Character != 63 {
		t.Errorf("multiline link: expected PathRange {0,19}->{0,63}, got %v->%v",
			l0.PathRange.Start, l0.PathRange.End)
	}
	if l0.Range.Start.Line != 0 || l0.Range.Start.Character != 6 ||
		l0.Range.End.Line != 1 || l0.Range.End.Character != 1 {
		t.Errorf("multiline link: expected Range {0,6}->{1,1}, got %v->%v",
			l0.Range.Start, l0.Range.End)
	}
	// The link after the multiline one must not be displaced by it.
	l1 := links[1]
	if l1.PathRange.Start.Line != 3 || l1.PathRange.Start.Character != 13 ||
		l1.PathRange.End.Line != 3 || l1.PathRange.End.Character != 21 {
		t.Errorf("link after multiline: expected PathRange {3,13}->{3,21}, got %v->%v",
			l1.PathRange.Start, l1.PathRange.End)
	}
}

func TestParseMarkdownNoCascadeAfterFallback(t *testing.T) {
	content := []byte("See [ref][r] and [real](/x.md).\n\n[r]: /y.md\n")
	_, links, _, _ := ParseMarkdown("file:///t.md", content)
	if len(links) != 2 {
		t.Fatalf("expected 2 links, got %d", len(links))
	}
	// Reference-style links have no "(dest)" in the source and fall back to a
	// coarse range; links after them must still get precise ranges.
	l1 := links[1]
	if l1.PathRange.Start.Line != 0 || l1.PathRange.Start.Character != 24 ||
		l1.PathRange.End.Line != 0 || l1.PathRange.End.Character != 29 {
		t.Errorf("link after reference link: expected PathRange {0,24}->{0,29}, got %v->%v",
			l1.PathRange.Start, l1.PathRange.End)
	}
}

func TestParseMarkdownFallbackTitle(t *testing.T) {
	content := []byte(`## Subheading Only

No H1 title here.
`)

	uri := "file:///workspace/folder/doc_name.md"
	_, _, title, hasH1 := ParseMarkdown(uri, content)

	if title != "doc_name.md" {
		t.Errorf("expected fallback title 'doc_name.md', got '%s'", title)
	}

	if hasH1 {
		t.Errorf("expected hasH1 to be false")
	}
}
