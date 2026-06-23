package state

import (
	"testing"
)

func TestServerState_ParseAndIndexContent(t *testing.T) {
	s := NewServerState()
	uri := "file:///workspace/test.md"
	content := []byte("# Hello World\n[link](other.md)")

	err := s.ParseAndIndexContent(uri, content)
	if err != nil {
		t.Fatalf("unexpected error parsing and indexing content: %v", err)
	}

	docInfo, ok := s.Index[uri]
	if !ok {
		t.Fatalf("expected document %s to be indexed", uri)
	}

	if docInfo.Title != "Hello World" {
		t.Errorf("expected title 'Hello World', got '%s'", docInfo.Title)
	}

	if len(docInfo.Links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(docInfo.Links))
	}

	if docInfo.Links[0].Path != "other.md" {
		t.Errorf("expected link path 'other.md', got '%s'", docInfo.Links[0].Path)
	}
}
