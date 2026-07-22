package indexer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	golang "github.com/smacker/go-tree-sitter/golang"
	javascript "github.com/smacker/go-tree-sitter/javascript"
	php "github.com/smacker/go-tree-sitter/php"
	python "github.com/smacker/go-tree-sitter/python"
	tsx "github.com/smacker/go-tree-sitter/typescript/tsx"
	typescript "github.com/smacker/go-tree-sitter/typescript/typescript"

	"github.com/entreya/nativestudio/db"
	"github.com/entreya/nativestudio/knowledge"
)

// TreeSitterParser extracts syntax-aware symbol ranges for supported source
// languages and delegates unsupported text formats to the structural fallback.
type TreeSitterParser struct{ Fallback knowledge.CodeParser }

func (p TreeSitterParser) Parse(ctx context.Context, path string, content []byte) (*knowledge.ParsedFile, error) {
	language, ok := treeSitterLanguage(path)
	if !ok {
		fallback := p.Fallback
		if fallback == nil {
			fallback = StructuralParser{}
		}
		return fallback.Parse(ctx, path, content)
	}
	parser := sitter.NewParser()
	defer parser.Close()
	parser.SetLanguage(language)
	tree, err := parser.ParseCtx(ctx, nil, content)
	if err != nil {
		return nil, err
	}
	defer tree.Close()
	hash := sha256.Sum256(content)
	parsed := &knowledge.ParsedFile{Path: path, Language: languageFor(path), Content: content, Hash: hex.EncodeToString(hash[:])}
	var visit func(*sitter.Node)
	visit = func(node *sitter.Node) {
		kind, keep := symbolKind(node.Type())
		if keep {
			nameNode := node.ChildByFieldName("name")
			if nameNode == nil {
				nameNode = firstIdentifier(node)
			}
			if nameNode != nil {
				name := nameNode.Content(content)
				signature := strings.TrimSpace(strings.SplitN(node.Content(content), "\n", 2)[0])
				parsed.Symbols = append(parsed.Symbols, knowledge.Symbol{ID: db.NewID(), Path: path, Name: name, QualifiedName: name, Type: kind, Signature: signature, StartLine: int(node.StartPoint().Row) + 1, EndLine: int(node.EndPoint().Row) + 1, ContentHash: parsed.Hash})
			}
		}
		if isImportNode(node.Type()) {
			value := strings.TrimSpace(node.Content(content))
			if len(value) > 500 {
				value = value[:500]
			}
			parsed.Imports = append(parsed.Imports, value)
		}
		for i := uint32(0); i < node.NamedChildCount(); i++ {
			visit(node.NamedChild(int(i)))
		}
	}
	visit(tree.RootNode())
	return parsed, nil
}

func treeSitterLanguage(path string) (*sitter.Language, bool) {
	lower := strings.ToLower(path)
	switch {
	case strings.HasSuffix(lower, ".go"):
		return golang.GetLanguage(), true
	case strings.HasSuffix(lower, ".js"), strings.HasSuffix(lower, ".jsx"):
		return javascript.GetLanguage(), true
	case strings.HasSuffix(lower, ".ts"):
		return typescript.GetLanguage(), true
	case strings.HasSuffix(lower, ".tsx"):
		return tsx.GetLanguage(), true
	case strings.HasSuffix(lower, ".php"):
		return php.GetLanguage(), true
	case strings.HasSuffix(lower, ".py"):
		return python.GetLanguage(), true
	default:
		return nil, false
	}
}
func symbolKind(nodeType string) (string, bool) {
	switch nodeType {
	case "class_declaration", "class_definition":
		return "class", true
	case "interface_declaration":
		return "interface", true
	case "trait_declaration":
		return "trait", true
	case "function_declaration", "function_definition":
		return "function", true
	case "method_declaration", "method_definition":
		return "method", true
	case "type_declaration", "type_alias_declaration":
		return "type", true
	case "enum_declaration":
		return "enum", true
	default:
		return "", false
	}
}
func isImportNode(nodeType string) bool {
	switch nodeType {
	case "import_declaration", "import_statement", "use_declaration":
		return true
	default:
		return false
	}
}
func firstIdentifier(node *sitter.Node) *sitter.Node {
	for i := uint32(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(int(i))
		if child.Type() == "identifier" || child.Type() == "name" || child.Type() == "type_identifier" {
			return child
		}
		if nested := firstIdentifier(child); nested != nil {
			return nested
		}
	}
	return nil
}
