package knowledge

import "context"

type ParsedFile struct {
	Path     string
	Language string
	Content  []byte
	Hash     string
	Symbols  []Symbol
	Imports  []string
}

type CodeParser interface {
	Parse(ctx context.Context, path string, content []byte) (*ParsedFile, error)
}

type Chunker interface {
	Chunk(parsed *ParsedFile) ([]Chunk, error)
}

type EmbeddingProvider interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

type SummaryGenerator interface {
	SummarizeFile(ctx context.Context, file ParsedFile, chunks []Chunk) (string, error)
	SummarizeProject(ctx context.Context, projectName string, files []FileSummary) (string, error)
}

type Retriever interface {
	Retrieve(ctx context.Context, workspaceID, query string) ([]Candidate, error)
}
