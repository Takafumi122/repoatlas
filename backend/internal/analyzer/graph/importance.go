package graph

import (
	"math"
	"path"
	"sort"

	"github.com/repoatlas/repoatlas/backend/internal/domain"
)

var entrypointFilenames = map[string]bool{
	"main.py":     true,
	"app.py":      true,
	"__main__.py": true,
	"cli.py":      true,
}

func IsEntrypointFile(filePath string) bool {
	return entrypointFilenames[path.Base(filePath)]
}

// ComputeImportanceScore calculates the v1 heuristic importance score:
// min(10, 2*in_degree + 0.5*out_degree + entrypoint_bonus)
func ComputeImportanceScore(inDegree, outDegree int, isEntrypoint bool) float64 {
	bonus := 0.0
	if isEntrypoint {
		bonus = 3.0
	}
	raw := 2.0*float64(inDegree) + 0.5*float64(outDegree) + bonus
	if raw > 10.0 {
		raw = 10.0
	}
	return math.Round(raw*100) / 100
}

// SelectRecommendedFileIDs returns up to topN (default 5) file node IDs
// ordered by importance_score DESC, then path ASC.
func SelectRecommendedFileIDs(files []domain.SourceFile, topN int) []string {
	if topN <= 0 {
		topN = 5
	}
	if len(files) == 0 {
		return []string{}
	}

	sorted := make([]domain.SourceFile, len(files))
	copy(sorted, files)

	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].ImportanceScore != sorted[j].ImportanceScore {
			return sorted[i].ImportanceScore > sorted[j].ImportanceScore
		}
		return sorted[i].Path < sorted[j].Path
	})

	if len(sorted) > topN {
		sorted = sorted[:topN]
	}

	ids := make([]string, len(sorted))
	for i, f := range sorted {
		ids[i] = domain.NodeIDForPath(f.Path)
	}
	return ids
}
