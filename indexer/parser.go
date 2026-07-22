package indexer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"

	"github.com/entreya/nativestudio/db"
	"github.com/entreya/nativestudio/knowledge"
)

// StructuralParser is the safe fallback for languages without a registered
// Tree-sitter grammar. Tree-sitter parsers can implement the same interface.
type StructuralParser struct{}

var declarationPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?m)^\s*(?:export\s+)?(?:public\s+|private\s+|protected\s+|static\s+|async\s+)*(class|interface|trait|function|func|type|def)\s+([A-Za-z_$][\w$]*)[^\n]*`),
	regexp.MustCompile(`(?m)^\s*(?:export\s+)?(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*=\s*(?:async\s*)?\([^\n]*=>`),
}

func (StructuralParser) Parse(ctx context.Context, path string, content []byte) (*knowledge.ParsedFile, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	text := string(content)
	hash := sha256.Sum256(content)
	parsed := &knowledge.ParsedFile{Path: path, Language: languageFor(path), Content: content, Hash: hex.EncodeToString(hash[:])}
	lines := strings.Split(text, "\n")
	for pi, pattern := range declarationPatterns {
		for _, loc := range pattern.FindAllStringSubmatchIndex(text, -1) {
			nameIndex := 4
			if pi == 1 {
				nameIndex = 2
			}
			if len(loc) <= nameIndex+1 {
				continue
			}
			name := text[loc[nameIndex]:loc[nameIndex+1]]
			kind := "function"
			if pi == 0 {
				kind = text[loc[2]:loc[3]]
			}
			start := 1 + strings.Count(text[:loc[0]], "\n")
			end := symbolEnd(lines, start)
			signature := strings.TrimSpace(strings.SplitN(text[loc[0]:loc[1]], "\n", 2)[0])
			parsed.Symbols = append(parsed.Symbols, knowledge.Symbol{ID: db.NewID(), Path: path, Name: name, QualifiedName: name, Type: kind, Signature: signature, StartLine: start, EndLine: end, ContentHash: parsed.Hash})
		}
	}
	return parsed, nil
}

func symbolEnd(lines []string, start int) int {
	if start < 1 || start > len(lines) {
		return start
	}
	depth := 0
	seenBrace := false
	for i := start - 1; i < len(lines); i++ {
		for _, r := range lines[i] {
			if r == '{' {
				depth++
				seenBrace = true
			}
			if r == '}' {
				depth--
			}
		}
		if seenBrace && depth <= 0 {
			return i + 1
		}
		if !seenBrace && i-(start-1) >= 40 {
			return i + 1
		}
	}
	return len(lines)
}

type SymbolChunker struct{ MaximumLines int }

func (c SymbolChunker) Chunk(parsed *knowledge.ParsedFile) ([]knowledge.Chunk, error) {
	max := c.MaximumLines
	if max <= 0 {
		max = 160
	}
	lines := strings.Split(string(parsed.Content), "\n")
	var chunks []knowledge.Chunk
	covered := make([]bool, len(lines))
	for _, symbol := range parsed.Symbols {
		start, end := symbol.StartLine, symbol.EndLine
		if start < 1 {
			start = 1
		}
		if end > len(lines) {
			end = len(lines)
		}
		for block := start; block <= end; block += max {
			blockEnd := block + max - 1
			if blockEnd > end {
				blockEnd = end
			}
			content := strings.Join(lines[block-1:blockEnd], "\n")
			sum := sha256.Sum256([]byte(content))
			chunks = append(chunks, knowledge.Chunk{ID: db.NewID(), SymbolID: symbol.ID, Path: parsed.Path, StartLine: block, EndLine: blockEnd, Content: content, ContentHash: hex.EncodeToString(sum[:]), TokenEstimate: (len(content) + 3) / 4})
			for i := block - 1; i < blockEnd; i++ {
				covered[i] = true
			}
		}
	}
	for start := 0; start < len(lines); {
		for start < len(lines) && covered[start] {
			start++
		}
		if start >= len(lines) {
			break
		}
		end := start
		for end < len(lines) && !covered[end] && end-start < max {
			end++
		}
		content := strings.Join(lines[start:end], "\n")
		if strings.TrimSpace(content) != "" {
			sum := sha256.Sum256([]byte(content))
			chunks = append(chunks, knowledge.Chunk{ID: db.NewID(), Path: parsed.Path, StartLine: start + 1, EndLine: end, Content: content, ContentHash: hex.EncodeToString(sum[:]), TokenEstimate: (len(content) + 3) / 4})
		}
		start = end
	}
	return chunks, nil
}
