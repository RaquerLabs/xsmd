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
