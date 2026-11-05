package graph

import (
	"sort"
	"strings"

	"github.com/repoatlas/repoatlas/backend/internal/domain"
)

type Edge struct {
	SourcePath       string `json:"source"`
	TargetPath       string `json:"target"`
	Kind             string `json:"kind"`
	EvidenceCount    int    `json:"evidence_count,omitempty"`
	SourcePackage    string `json:"source_package,omitempty"`
	TargetPackage    string `json:"target_package,omitempty"`
	BuildConstraints string `json:"build_constraints,omitempty"`
	IsConditional    bool   `json:"is_conditional,omitempty"`
}

type BuildOutput struct {
	Language               string
	GraphGranularity       string
	Files                  []domain.SourceFile
	Edges                  []Edge
	Imports                []domain.ImportReference
	RecommendedFileIDs     []string
	FilesCount             int
	InternalEdgesCount     int
	ExternalImportsCount   int
	UnresolvedImportsCount int
}

// BuildGraph computes deduplicated internal edges (excluding self-edges),
// calculates in/out degrees and importance scores for each file, and sorts outputs deterministically.
func BuildGraph(files []domain.SourceFile, imports []domain.ImportReference) BuildOutput {
	fileMap := make(map[string]*domain.SourceFile, len(files))
	sortedFiles := make([]domain.SourceFile, len(files))
	copy(sortedFiles, files)
	sort.Slice(sortedFiles, func(i, j int) bool {
		return sortedFiles[i].Path < sortedFiles[j].Path
	})
	for i := range sortedFiles {
		sortedFiles[i].InDegree = 0
		sortedFiles[i].OutDegree = 0
		fileMap[sortedFiles[i].Path] = &sortedFiles[i]
	}

	type edgeKey struct {
		src string
		dst string
	}
	seenEdges := make(map[edgeKey]bool)
	evidenceCounts := make(map[edgeKey]int)
	var edges []Edge

	externalCount := 0
	unresolvedCount := 0

	for _, imp := range imports {
		switch imp.Classification {
		case domain.ImportInternal:
			if imp.SourcePath == "" || imp.TargetPath == "" {
				continue
			}
			// Exclude self-imports from graph edges and degree counts
			if imp.SourcePath == imp.TargetPath {
				continue
			}
			if _, ok := fileMap[imp.SourcePath]; !ok {
				continue
			}
			if _, ok := fileMap[imp.TargetPath]; !ok {
				continue
			}
			k := edgeKey{src: imp.SourcePath, dst: imp.TargetPath}
			evidenceCounts[k]++
			if !seenEdges[k] {
				seenEdges[k] = true
				edges = append(edges, Edge{
					SourcePath:       imp.SourcePath,
					TargetPath:       imp.TargetPath,
					Kind:             "import",
					SourcePackage:    imp.SourcePackage,
					TargetPackage:    imp.TargetPackage,
					BuildConstraints: imp.BuildConstraints,
					IsConditional:    imp.BuildConstraints != "",
				})
				fileMap[imp.SourcePath].OutDegree++
				fileMap[imp.TargetPath].InDegree++
			}
		case domain.ImportExternal:
			externalCount++
		case domain.ImportUnresolved:
			unresolvedCount++
		}
	}

	for i := range edges {
		k := edgeKey{src: edges[i].SourcePath, dst: edges[i].TargetPath}
		edges[i].EvidenceCount = evidenceCounts[k]
	}

	sort.Slice(edges, func(i, j int) bool {
		if edges[i].SourcePath != edges[j].SourcePath {
			return edges[i].SourcePath < edges[j].SourcePath
		}
		return edges[i].TargetPath < edges[j].TargetPath
	})

	lang := "python"
	granularity := "file"
	if len(sortedFiles) > 0 && sortedFiles[0].Language == "go" {
		lang = "go"
		granularity = "package"
	}

	for i := range sortedFiles {
		f := &sortedFiles[i]
		if f.Language == "go" || f.NodeKind == "package" {
			if f.PackageName == "main" || strings.HasPrefix(f.DirectoryPath, "cmd/") || f.DirectoryPath == "cmd" {
				f.IsEntrypoint = true
			}
		} else {
			f.IsEntrypoint = IsEntrypointFile(f.Path)
		}
		f.ImportanceScore = ComputeImportanceScore(f.InDegree, f.OutDegree, f.IsEntrypoint)
	}

	recommended := SelectRecommendedFileIDs(sortedFiles, 5)

	return BuildOutput{
		Language:               lang,
		GraphGranularity:       granularity,
		Files:                  sortedFiles,
		Edges:                  edges,
		Imports:                imports,
		RecommendedFileIDs:     recommended,
		FilesCount:             len(sortedFiles),
		InternalEdgesCount:     len(edges),
		ExternalImportsCount:   externalCount,
		UnresolvedImportsCount: unresolvedCount,
	}
}
