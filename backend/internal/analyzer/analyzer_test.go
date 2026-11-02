package analyzer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/repoatlas/repoatlas/backend/internal/analyzer/graph"
	"github.com/repoatlas/repoatlas/backend/internal/domain"
)

func loadFixtureFiles(t *testing.T, fixtureDir string) []SourceFile {
	t.Helper()
	root := filepath.Join("..", "..", "testdata", fixtureDir)
	var files []SourceFile
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(p, ".py") {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		content, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		files = append(files, SourceFile{
			Path:    filepath.ToSlash(rel),
			Content: content,
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk fixture %s: %v", fixtureDir, err)
	}
	return files
}

func TestPythonAnalyzer_SmallFixture(t *testing.T) {
	files := loadFixtureFiles(t, "python_small")
	az := NewPythonAnalyzer()

	var stages []domain.AnalysisStage
	out, err := az.Analyze(context.Background(), files, func(stage domain.AnalysisStage) {
		stages = append(stages, stage)
	})
	if err != nil {
		t.Fatalf("Analyze error: %v", err)
	}

	if len(stages) != 3 || stages[0] != domain.StageIndexing || stages[1] != domain.StageParsing || stages[2] != domain.StageResolving {
		t.Fatalf("unexpected stages: %+v", stages)
	}

	if out.FilesCount != 4 {
		t.Fatalf("expected 4 files, got %d", out.FilesCount)
	}
	// Expected deduplicated internal edges (excluding self-import in models.py):
	// 1. app/main.py -> app/models.py
	// 2. app/main.py -> app/service.py
	// 3. app/service.py -> app/models.py (deduplicated from 3 imports!)
	// 4. app/service.py -> app/utils.py
	if out.InternalEdgesCount != 4 {
		t.Fatalf("expected 4 deduplicated internal edges, got %d: %+v", out.InternalEdgesCount, out.Edges)
	}
	// External imports: requests (main.py), os (utils.py) = 2
	if out.ExternalImportsCount != 2 {
		t.Fatalf("expected 2 external imports, got %d", out.ExternalImportsCount)
	}
	// Unresolved imports: from app.nonexistent import Ghost (utils.py) = 1
	if out.UnresolvedImportsCount != 1 {
		t.Fatalf("expected 1 unresolved import, got %d", out.UnresolvedImportsCount)
	}

	fileByPath := make(map[string]domain.SourceFile)
	for _, f := range out.Files {
		fileByPath[f.Path] = f
	}

	mainFile := fileByPath["app/main.py"]
	if !mainFile.IsEntrypoint || mainFile.FunctionsCount != 1 || mainFile.ClassesCount != 0 || mainFile.InDegree != 0 || mainFile.OutDegree != 2 {
		t.Fatalf("unexpected app/main.py stats: %+v", mainFile)
	}
	if mainFile.ImportanceScore != 4.0 {
		t.Fatalf("expected app/main.py importance_score=4.0, got %v", mainFile.ImportanceScore)
	}

	modelsFile := fileByPath["app/models.py"]
	// Self-import "from . import models" must NOT increase in_degree or out_degree
	if modelsFile.ClassesCount != 2 || modelsFile.InDegree != 2 || modelsFile.OutDegree != 0 {
		t.Fatalf("unexpected app/models.py stats: %+v", modelsFile)
	}

	serviceFile := fileByPath["app/service.py"]
	if serviceFile.ClassesCount != 1 || serviceFile.FunctionsCount != 2 || serviceFile.InDegree != 1 || serviceFile.OutDegree != 2 {
		t.Fatalf("unexpected app/service.py stats: %+v", serviceFile)
	}
}

func TestPythonAnalyzer_RelativeImportsAndSrcLayout(t *testing.T) {
	files := loadFixtureFiles(t, "python_relative_imports")
	az := NewPythonAnalyzer()

	out, err := az.Analyze(context.Background(), files, nil)
	if err != nil {
		t.Fatalf("Analyze error: %v", err)
	}

	if out.FilesCount != 4 {
		t.Fatalf("expected 4 files, got %d", out.FilesCount)
	}
	// Expected internal edges:
	// 1. src/pkg/__init__.py -> src/pkg/core.py
	// 2. src/pkg/core.py -> src/pkg/sub/worker.py
	// 3. src/pkg/sub/worker.py -> src/pkg/core.py (circular dependency)
	if out.InternalEdgesCount != 3 {
		t.Fatalf("expected 3 internal edges, got %d: %+v", out.InternalEdgesCount, out.Edges)
	}
	// Expected unresolved imports:
	// 1. from ..outside import something (beyond src/ root)
	// 2. from .missing_mod import helper (non-existent relative module)
	if out.UnresolvedImportsCount != 2 {
		t.Fatalf("expected 2 unresolved imports, got %d", out.UnresolvedImportsCount)
	}
}

func TestPythonAnalyzer_AmbiguousRootAndSrcCollision(t *testing.T) {
	az := NewPythonAnalyzer()
	files := []SourceFile{
		{Path: "foo.py", Content: []byte("def a(): pass\n")},
		{Path: "src/foo.py", Content: []byte("def b(): pass\n")},
		{Path: "main.py", Content: []byte("import foo\n")},
	}

	out, err := az.Analyze(context.Background(), files, nil)
	if err != nil {
		t.Fatalf("Analyze error: %v", err)
	}
	if out.InternalEdgesCount != 0 {
		t.Fatalf("expected 0 internal edges on ambiguous module match, got %d", out.InternalEdgesCount)
	}
	if out.UnresolvedImportsCount != 1 {
		t.Fatalf("expected 1 unresolved import on ambiguous module match, got %d", out.UnresolvedImportsCount)
	}
}

func TestSelectRecommendedFileIDs_TieBreakAndLimit(t *testing.T) {
	files := []domain.SourceFile{
		{Path: "z.py", ImportanceScore: 5.0},
		{Path: "a.py", ImportanceScore: 5.0},
		{Path: "m.py", ImportanceScore: 8.5},
		{Path: "b.py", ImportanceScore: 3.0},
		{Path: "c.py", ImportanceScore: 2.0},
		{Path: "d.py", ImportanceScore: 1.0},
	}

	got := graph.SelectRecommendedFileIDs(files, 5)
	want := []string{"file:m.py", "file:a.py", "file:z.py", "file:b.py", "file:c.py"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("at index %d: got %s, want %s", i, got[i], want[i])
		}
	}
}
