package golang

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"strconv"
	"strings"
)

// ParsedImport represents a single import specification extracted from a Go source file.
type ParsedImport struct {
	Path             string // unquoted import path, e.g. "github.com/example/myapp/internal/service"
	Alias            string // "", "_", ".", or custom alias
	Line             int
	Column           int
	ImportText       string // full statement or representation
	BuildConstraints string // constraint string if present
}

// ParsedGoFile contains the AST extraction results for a single .go file.
type ParsedGoFile struct {
	Path             string
	PackageName      string
	IsTest           bool
	FunctionsCount   int
	MethodsCount     int
	TypesCount       int
	Imports          []ParsedImport
	BuildConstraints string
}

// GoParser handles syntax parsing of Go files into AST without code execution.
type GoParser struct{}

func NewGoParser() *GoParser {
	return &GoParser{}
}

// ExtractBuildConstraints checks file comments and file naming conventions for build tags and OS/Arch conditions.
func ExtractBuildConstraints(filePath string, fileNode *ast.File) string {
	var constraints []string

	// Check file comments for //go:build or // +build directives
	if fileNode != nil {
		for _, cg := range fileNode.Comments {
			for _, c := range cg.List {
				text := strings.TrimSpace(c.Text)
				if strings.HasPrefix(text, "//go:build ") {
					constraints = append(constraints, strings.TrimPrefix(text, "//go:build "))
				} else if strings.HasPrefix(text, "// +build ") {
					constraints = append(constraints, strings.TrimPrefix(text, "// +build "))
				}
			}
		}
	}

	base := path.Base(filePath)
	nameWithoutExt := strings.TrimSuffix(base, ".go")
	parts := strings.Split(nameWithoutExt, "_")

	knownOS := map[string]bool{
		"linux": true, "darwin": true, "windows": true, "freebsd": true,
		"netbsd": true, "openbsd": true, "plan9": true, "solaris": true,
		"android": true, "ios": true, "js": true, "wasip1": true,
	}
	knownArch := map[string]bool{
		"386": true, "amd64": true, "arm": true, "arm64": true,
		"mips": true, "mipsle": true, "mips64": true, "mips64le": true,
		"ppc64": true, "ppc64le": true, "riscv64": true, "s390x": true, "wasm": true,
	}

	for _, p := range parts {
		if knownOS[p] {
			constraints = append(constraints, "os:"+p)
		} else if knownArch[p] {
			constraints = append(constraints, "arch:"+p)
		}
	}

	if strings.HasSuffix(base, "_test.go") {
		constraints = append(constraints, "test")
	}

	if len(constraints) == 0 {
		return ""
	}
	return strings.Join(constraints, "; ")
}

// ParseFile parses a single Go source file into ParsedGoFile.
func (p *GoParser) ParseFile(ctx context.Context, filePath string, content []byte) (ParsedGoFile, error) {
	if err := ctx.Err(); err != nil {
		return ParsedGoFile{}, err
	}

	fset := token.NewFileSet()
	fileNode, err := parser.ParseFile(fset, filePath, content, parser.ParseComments)
	if err != nil {
		// Tolerant fallback: try to parse without comments or return wrapped error
		return ParsedGoFile{}, fmt.Errorf("parse Go file %s: %w", filePath, err)
	}

	pkgName := ""
	if fileNode.Name != nil {
		pkgName = fileNode.Name.Name
	}

	buildConstraints := ExtractBuildConstraints(filePath, fileNode)
	isTest := strings.HasSuffix(filePath, "_test.go")

	var functionsCount int
	var methodsCount int
	var typesCount int
	var imports []ParsedImport

	ast.Inspect(fileNode, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.FuncDecl:
			if node.Recv == nil || len(node.Recv.List) == 0 {
				functionsCount++
			} else {
				methodsCount++
			}
		case *ast.TypeSpec:
			typesCount++
		case *ast.ImportSpec:
			rawPath := ""
			if node.Path != nil {
				unquoted, unqErr := strconv.Unquote(node.Path.Value)
				if unqErr == nil {
					rawPath = unquoted
				} else {
					rawPath = strings.Trim(node.Path.Value, `"`+"`")
				}
			}

			alias := ""
			if node.Name != nil {
				alias = node.Name.Name
			}

			pos := fset.Position(node.Pos())
			line := pos.Line
			col := pos.Column

			importStmt := ""
			if alias != "" {
				importStmt = fmt.Sprintf("import %s %q", alias, rawPath)
			} else {
				importStmt = fmt.Sprintf("import %q", rawPath)
			}

			imports = append(imports, ParsedImport{
				Path:             rawPath,
				Alias:            alias,
				Line:             line,
				Column:           col,
				ImportText:       importStmt,
				BuildConstraints: buildConstraints,
			})
		}
		return true
	})

	return ParsedGoFile{
		Path:             filePath,
		PackageName:      pkgName,
		IsTest:           isTest,
		FunctionsCount:   functionsCount,
		MethodsCount:     methodsCount,
		TypesCount:       typesCount,
		Imports:          imports,
		BuildConstraints: buildConstraints,
	}, nil
}
