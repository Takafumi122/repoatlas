package python

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_python "github.com/tree-sitter/tree-sitter-python/bindings/go"
)

type RawImport struct {
	ImportText   string
	Module       string // e.g. "pkg.sub" or "" for "from . import x"
	ImportedName string // e.g. "x" for "from pkg import x", "" for "import pkg"
	Level        int    // 0 for absolute, >=1 for relative ("." = 1, ".." = 2)
	LineNumber   int    // 1-based line number
	StartLine    int    // 1-based start line
	StartColumn  int    // 1-based start column (Unicode rune count in line)
	EndLine      int    // 1-based end line
	EndColumn    int    // 1-based end column (Unicode rune count in line)
	ImportKind   string // "absolute" or "relative"
}

type ParsedFile struct {
	Path           string
	FunctionsCount int
	ClassesCount   int
	Imports        []RawImport
}

type Parser struct {
	lang *tree_sitter.Language
}

func NewParser() *Parser {
	return &Parser{
		lang: tree_sitter.NewLanguage(tree_sitter_python.Language()),
	}
}

func (p *Parser) ParseFile(ctx context.Context, filePath string, content []byte) (ParsedFile, error) {
	tsParser := tree_sitter.NewParser()
	defer tsParser.Close()

	if err := tsParser.SetLanguage(p.lang); err != nil {
		return ParsedFile{}, fmt.Errorf("set python language: %w", err)
	}

	tree := tsParser.ParseCtx(ctx, content, nil)
	if tree == nil {
		return ParsedFile{}, fmt.Errorf("parse python file %s failed or cancelled", filePath)
	}
	defer tree.Close()

	result := ParsedFile{
		Path: filePath,
	}

	root := tree.RootNode()
	if root == nil {
		return result, nil
	}

	walkAST(root, content, &result)
	return result, nil
}

func walkAST(node *tree_sitter.Node, source []byte, out *ParsedFile) {
	if node == nil {
		return
	}

	switch node.Kind() {
	case "function_definition":
		out.FunctionsCount++
	case "class_definition":
		out.ClassesCount++
	case "import_statement":
		out.Imports = append(out.Imports, extractImportStatement(node, source)...)
	case "import_from_statement":
		out.Imports = append(out.Imports, extractImportFromStatement(node, source)...)
	}

	childCount := node.ChildCount()
	for i := uint(0); i < childCount; i++ {
		child := node.Child(i)
		walkAST(child, source, out)
	}
}

func computeRunePosition(source []byte, point tree_sitter.Point) (int, int) {
	line := int(point.Row) + 1
	currRow := uint(0)
	lineStart := 0
	for i := 0; i < len(source); i++ {
		if currRow == point.Row {
			lineStart = i
			break
		}
		if source[i] == '\n' {
			currRow++
			if currRow == point.Row {
				lineStart = i + 1
				break
			}
		}
	}
	byteCol := int(point.Column)
	endOffset := lineStart + byteCol
	if endOffset > len(source) {
		endOffset = len(source)
	}
	if lineStart > endOffset {
		return line, byteCol + 1
	}
	col := utf8.RuneCount(source[lineStart:endOffset]) + 1
	return line, col
}

func extractImportStatement(node *tree_sitter.Node, source []byte) []RawImport {
	stmtText := sanitizeImportText(node.Utf8Text(source))
	sLine, sCol := computeRunePosition(source, node.StartPosition())
	eLine, eCol := computeRunePosition(source, node.EndPosition())

	var imports []RawImport
	count := node.NamedChildCount()
	for i := uint(0); i < count; i++ {
		child := node.NamedChild(i)
		if child == nil {
			continue
		}
		switch child.Kind() {
		case "dotted_name":
			mod := strings.TrimSpace(child.Utf8Text(source))
			if mod != "" {
				imports = append(imports, RawImport{
					ImportText:  stmtText,
					Module:      mod,
					Level:       0,
					LineNumber:  sLine,
					StartLine:   sLine,
					StartColumn: sCol,
					EndLine:     eLine,
					EndColumn:   eCol,
					ImportKind:  "absolute",
				})
			}
		case "aliased_import":
			nameNode := child.ChildByFieldName("name")
			if nameNode == nil && child.NamedChildCount() > 0 {
				nameNode = child.NamedChild(0)
			}
			if nameNode != nil {
				mod := strings.TrimSpace(nameNode.Utf8Text(source))
				if mod != "" {
					imports = append(imports, RawImport{
						ImportText:  stmtText,
						Module:      mod,
						Level:       0,
						LineNumber:  sLine,
						StartLine:   sLine,
						StartColumn: sCol,
						EndLine:     eLine,
						EndColumn:   eCol,
						ImportKind:  "absolute",
					})
				}
			}
		}
	}
	return imports
}

func extractImportFromStatement(node *tree_sitter.Node, source []byte) []RawImport {
	stmtText := sanitizeImportText(node.Utf8Text(source))
	sLine, sCol := computeRunePosition(source, node.StartPosition())
	eLine, eCol := computeRunePosition(source, node.EndPosition())

	var module string
	var level int
	var importedNames []string

	seenImportKeyword := false
	childCount := node.ChildCount()
	for i := uint(0); i < childCount; i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		kind := child.Kind()

		if !seenImportKeyword {
			if kind == "import" {
				seenImportKeyword = true
				continue
			}
			switch kind {
			case "dotted_name":
				module = strings.TrimSpace(child.Utf8Text(source))
			case "relative_import":
				level, module = parseRelativeImportNode(child, source)
			}
			continue
		}

		// After the "import" keyword: collect imported targets
		switch kind {
		case "dotted_name":
			name := strings.TrimSpace(child.Utf8Text(source))
			if name != "" {
				importedNames = append(importedNames, name)
			}
		case "aliased_import":
			nameNode := child.ChildByFieldName("name")
			if nameNode == nil && child.NamedChildCount() > 0 {
				nameNode = child.NamedChild(0)
			}
			if nameNode != nil {
				name := strings.TrimSpace(nameNode.Utf8Text(source))
				if name != "" {
					importedNames = append(importedNames, name)
				}
			}
		case "wildcard_import":
			importedNames = append(importedNames, "*")
		}
	}

	importKind := "absolute"
	if level > 0 {
		importKind = "relative"
	}

	if len(importedNames) == 0 {
		return []RawImport{
			{
				ImportText:  stmtText,
				Module:      module,
				Level:       level,
				LineNumber:  sLine,
				StartLine:   sLine,
				StartColumn: sCol,
				EndLine:     eLine,
				EndColumn:   eCol,
				ImportKind:  importKind,
			},
		}
	}

	imports := make([]RawImport, 0, len(importedNames))
	for _, imported := range importedNames {
		imports = append(imports, RawImport{
			ImportText:   stmtText,
			Module:       module,
			ImportedName: imported,
			Level:        level,
			LineNumber:   sLine,
			StartLine:    sLine,
			StartColumn:  sCol,
			EndLine:      eLine,
			EndColumn:    eCol,
			ImportKind:   importKind,
		})
	}
	return imports
}

func parseRelativeImportNode(node *tree_sitter.Node, source []byte) (int, string) {
	var level int
	var module string
	count := node.ChildCount()
	for i := uint(0); i < count; i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		switch child.Kind() {
		case "import_prefix":
			dots := strings.TrimSpace(child.Utf8Text(source))
			level += strings.Count(dots, ".")
		case "dotted_name":
			module = strings.TrimSpace(child.Utf8Text(source))
		}
	}
	return level, module
}

func sanitizeImportText(raw string) string {
	collapsed := strings.Join(strings.Fields(raw), " ")
	if len(collapsed) > 240 {
		return collapsed[:240] + "..."
	}
	return collapsed
}
