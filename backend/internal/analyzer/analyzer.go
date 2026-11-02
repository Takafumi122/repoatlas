package analyzer

import (
	"context"
	"sort"

	"github.com/repoatlas/repoatlas/backend/internal/analyzer/graph"
	"github.com/repoatlas/repoatlas/backend/internal/analyzer/python"
	"github.com/repoatlas/repoatlas/backend/internal/domain"
)

type SourceFile struct {
	Path    string
	Content []byte
}

type StageCallback func(stage domain.AnalysisStage)

type Analyzer interface {
	Analyze(ctx context.Context, files []SourceFile, onStage StageCallback) (graph.BuildOutput, error)
}

type PythonAnalyzer struct {
	parser *python.Parser
}

func NewPythonAnalyzer() *PythonAnalyzer {
	return &PythonAnalyzer{
		parser: python.NewParser(),
	}
}

func (a *PythonAnalyzer) Analyze(ctx context.Context, files []SourceFile, onStage StageCallback) (graph.BuildOutput, error) {
	if onStage != nil {
		onStage(domain.StageIndexing)
	}

	sortedInputs := make([]SourceFile, len(files))
	copy(sortedInputs, files)
	sort.Slice(sortedInputs, func(i, j int) bool {
		return sortedInputs[i].Path < sortedInputs[j].Path
	})

	paths := make([]string, 0, len(sortedInputs))
	for _, f := range sortedInputs {
		paths = append(paths, f.Path)
	}
	modIndex := python.BuildModuleIndex(paths)

	if onStage != nil {
		onStage(domain.StageParsing)
	}

	parsedFiles := make([]python.ParsedFile, 0, len(sortedInputs))
	domainFiles := make([]domain.SourceFile, 0, len(sortedInputs))

	for _, f := range sortedInputs {
		if err := ctx.Err(); err != nil {
			return graph.BuildOutput{}, err
		}
		pf, err := a.parser.ParseFile(ctx, f.Path, f.Content)
		if err != nil {
			return graph.BuildOutput{}, err
		}
		parsedFiles = append(parsedFiles, pf)
		domainFiles = append(domainFiles, domain.SourceFile{
			Path:           f.Path,
			FunctionsCount: pf.FunctionsCount,
			ClassesCount:   pf.ClassesCount,
		})
	}

	if onStage != nil {
		onStage(domain.StageResolving)
	}

	var allImports []domain.ImportReference
	for _, pf := range parsedFiles {
		for _, rawImp := range pf.Imports {
			resolved := python.ResolveImport(pf.Path, rawImp, modIndex)
			allImports = append(allImports, resolved)
		}
	}

	out := graph.BuildGraph(domainFiles, allImports)
	return out, nil
}
