package indexer

import (
	"context"
	"testing"
)

func TestTreeSitterParserExtractsGoSymbolsAndChunks(t *testing.T) {
	content := []byte("package sample\n\ntype Service struct{}\n\nfunc (s *Service) Run(name string) error {\n\treturn nil\n}\n")
	parsed, err := (TreeSitterParser{Fallback: StructuralParser{}}).Parse(context.Background(), "service.go", content)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Symbols) != 2 {
		t.Fatalf("expected type and method, got %#v", parsed.Symbols)
	}
	if parsed.Symbols[0].Name != "Service" || parsed.Symbols[1].Name != "Run" {
		t.Fatalf("unexpected symbols: %#v", parsed.Symbols)
	}
	chunks, err := (SymbolChunker{MaximumLines: 20}).Chunk(parsed)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) < 2 {
		t.Fatalf("expected symbol-aware chunks, got %#v", chunks)
	}
	for _, chunk := range chunks {
		if chunk.ContentHash == "" || chunk.StartLine < 1 || chunk.EndLine < chunk.StartLine {
			t.Fatalf("invalid chunk: %#v", chunk)
		}
	}
}
