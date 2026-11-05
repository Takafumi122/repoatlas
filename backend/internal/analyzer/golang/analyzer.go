package golang

import (
	"context"
	"path"
	"sort"
	"strings"

	"github.com/repoatlas/repoatlas/backend/internal/analyzer/graph"
	"github.com/repoatlas/repoatlas/backend/internal/domain"
)

type SourceInput struct {
	Path    string
	Content []byte
}

type GoAnalyzer struct {
	parser *GoParser
}

func NewGoAnalyzer() *GoAnalyzer {
	return &GoAnalyzer{
		parser: NewGoParser(),
	}
}

type StageCallback func(stage domain.AnalysisStage)

// Analyze performs pure static AST analysis on Go repositories without executing any code.
func (a *GoAnalyzer) Analyze(ctx context.Context, files []SourceInput, onStage StageCallback) (graph.BuildOutput, error) {
	if onStage != nil {
		onStage(domain.StageIndexing)
	}

	// Sort input files deterministically by path
	sortedInputs := make([]SourceInput, len(files))
	copy(sortedInputs, files)
	sort.Slice(sortedInputs, func(i, j int) bool {
		return sortedInputs[i].Path < sortedInputs[j].Path
	})

	modIndex := NewModuleIndex()

	var goFiles []SourceInput

	// 1. Separate go.mod, go.work and .go files
	for _, f := range sortedInputs {
		base := path.Base(f.Path)
		if base == "go.mod" {
			_, _ = modIndex.ParseGoMod(f.Path, f.Content)
		} else if base == "go.work" {
			_ = modIndex.ParseGoWork(f.Path, f.Content)
		} else if strings.HasSuffix(f.Path, ".go") {
			goFiles = append(goFiles, f)
		}
	}

	if onStage != nil {
		onStage(domain.StageParsing)
	}

	// 2. Parse all .go files into AST
	parsedGoFiles := make([]ParsedGoFile, 0, len(goFiles))
	for _, f := range goFiles {
		if err := ctx.Err(); err != nil {
			return graph.BuildOutput{}, err
		}
		pf, err := a.parser.ParseFile(ctx, f.Path, f.Content)
		if err != nil {
			// For syntax errors in some files, skip or record as empty file to remain resilient
			parsedGoFiles = append(parsedGoFiles, ParsedGoFile{
				Path:        f.Path,
				PackageName: path.Base(path.Dir(f.Path)),
			})
			continue
		}
		parsedGoFiles = append(parsedGoFiles, pf)
	}

	if onStage != nil {
		onStage(domain.StageResolving)
	}

	// 3. Build package index and resolve package import paths
	pkgIndex := BuildPackageIndex(modIndex, parsedGoFiles)

	// 4. Construct domain.SourceFile for each Go Package node
	var packageList []*PackageData
	for _, p := range pkgIndex.PackagesByPath {
		packageList = append(packageList, p)
	}
	sort.Slice(packageList, func(i, j int) bool {
		return packageList[i].ImportPath < packageList[j].ImportPath
	})

	domainPackages := make([]domain.SourceFile, 0, len(packageList))
	for _, p := range packageList {
		var fileInfos []domain.PackageFileInfo
		for _, f := range p.Files {
			fileInfos = append(fileInfos, domain.PackageFileInfo{
				Path:      f.Path,
				Functions: f.FunctionsCount,
				Methods:   f.MethodsCount,
				Types:     f.TypesCount,
				IsTest:    f.IsTest,
			})
		}
		sort.Slice(fileInfos, func(i, j int) bool {
			return fileInfos[i].Path < fileInfos[j].Path
		})

		domainPackages = append(domainPackages, domain.SourceFile{
			Path:              p.ImportPath,
			FunctionsCount:    p.FunctionsCount,
			ClassesCount:      p.TypesCount, // maps types to classes for general summary compatibility
			IsEntrypoint:      p.IsEntrypoint,
			NodeKind:          "package",
			Language:          "go",
			ModulePath:        p.ModulePath,
			PackageImportPath: p.ImportPath,
			DirectoryPath:     p.Directory,
			PackageName:       p.PackageName,
			FilesCount:        len(p.Files),
			MethodsCount:      p.MethodsCount,
			TypesCount:        p.TypesCount,
			Files:             fileInfos,
		})
	}

	// 5. Resolve all import statements across all packages
	var allImports []domain.ImportReference
	for _, p := range packageList {
		for _, f := range p.Files {
			for _, imp := range f.Imports {
				res := pkgIndex.ResolveImport(p, imp)

				reason := res.Reason
				ref := domain.ImportReference{
					SourcePath:       p.ImportPath,
					TargetPath:       res.TargetPackage,
					ImportText:       imp.ImportText,
					ModuleName:       imp.Path,
					LineNumber:       imp.Line,
					ColumnNumber:     imp.Column,
					Classification:   res.Classification,
					ResolutionNote:   &reason,
					SourceFile:       f.Path,
					SourcePackage:    p.ImportPath,
					TargetPackage:    res.TargetPackage,
					Alias:            imp.Alias,
					BuildConstraints: imp.BuildConstraints,
				}
				allImports = append(allImports, ref)
			}
		}
	}

	out := graph.BuildGraph(domainPackages, allImports)
	out.Language = "go"
	out.GraphGranularity = "package"
	return out, nil
}
