package graph

import (
	"sort"
	"strings"

	"github.com/repoatlas/repoatlas/backend/internal/domain"
)

// ImpactedFile represents a file that directly or transitively depends on the target file.
type ImpactedFile struct {
	NodeID          string  `json:"node_id"`
	Path            string  `json:"path"`
	Depth           int     `json:"depth"`
	Direct          bool    `json:"direct"`
	ViaNodeID       string  `json:"via_node_id"`
	ViaPath         string  `json:"via_path"`
	InDegree        int     `json:"in_degree"`
	OutDegree       int     `json:"out_degree"`
	ImportanceScore float64 `json:"importance_score"`
}

// ImpactAnalysisResult holds the BFS reverse-dependency impact analysis for a target file.
type ImpactAnalysisResult struct {
	TargetNodeID  string         `json:"target_node_id"`
	TargetPath    string         `json:"target_path"`
	MaxDepth      int            `json:"max_depth"`
	TotalFiles    int            `json:"total_files"`
	TotalAffected int            `json:"total_affected"`
	DirectCount   int            `json:"direct_count"`
	IndirectCount int            `json:"indirect_count"`
	AffectedFiles []ImpactedFile `json:"affected_files"`
	AffectedEdges []Edge         `json:"affected_edges"`
}

// NormalizeFilePath strips an optional "file:" or "pkg:" prefix so callers can pass either a node ID or a path.
func NormalizeFilePath(input string) string {
	input = strings.TrimSpace(input)
	input = strings.TrimPrefix(input, "file:")
	input = strings.TrimPrefix(input, "pkg:")
	return input
}

// findMatchingTarget searches for a matching file or package node by:
// 1) exact Path match (with/without leading slash or ./ or prefix)
// 2) PackageImportPath or NodeID match (for Go packages)
// 3) DirectoryPath match (for Go packages where target is a directory like "internal/store")
// 4) Contained File match (for Go packages where target is a file inside the package like "internal/store/postgres.go")
func findMatchingTarget(files []domain.SourceFile, targetInput string) (string, domain.SourceFile, bool) {
	targetNorm := NormalizeFilePath(targetInput)
	trimmedTarget := strings.Trim(targetNorm, "/")
	trimmedTarget = strings.TrimPrefix(trimmedTarget, "./")

	// 1. Direct match on f.Path
	for _, f := range files {
		if f.Path == targetNorm || f.Path == trimmedTarget {
			return f.Path, f, true
		}
	}

	// 2. Match on PackageImportPath or NodeID
	for _, f := range files {
		if f.PackageImportPath != "" {
			if f.PackageImportPath == targetNorm || f.PackageImportPath == trimmedTarget {
				return f.Path, f, true
			}
			if domain.NodeIDForPackage(f.PackageImportPath) == targetInput {
				return f.Path, f, true
			}
		}
		if domain.NodeIDForPath(f.Path) == targetInput {
			return f.Path, f, true
		}
	}

	// 3. Match on DirectoryPath
	for _, f := range files {
		if f.DirectoryPath != "" {
			dirTrim := strings.Trim(f.DirectoryPath, "/")
			dirTrim = strings.TrimPrefix(dirTrim, "./")
			if dirTrim == trimmedTarget || f.DirectoryPath == targetNorm {
				return f.Path, f, true
			}
		}
	}

	// 4. Match on contained files in package
	for _, f := range files {
		for _, fi := range f.Files {
			fiTrim := strings.Trim(fi.Path, "/")
			fiTrim = strings.TrimPrefix(fiTrim, "./")
			if fiTrim == trimmedTarget || fi.Path == targetNorm {
				return f.Path, f, true
			}
		}
	}

	return "", domain.SourceFile{}, false
}

// AnalyzeImpact performs a deterministic Breadth-First Search (BFS) over reverse import edges
// (finding files that import targetPath directly at depth=1 or transitively at depth>=2 up to maxDepth).
// If maxDepth <= 0, it defaults to len(files) (unbounded within the finite graph).
// Cycles (e.g. A <-> B or A -> B -> C -> A) are safely handled via the visited set.
func AnalyzeImpact(files []domain.SourceFile, edges []Edge, targetInput string, maxDepth int) (ImpactAnalysisResult, bool) {
	targetPath, targetFile, exists := findMatchingTarget(files, targetInput)
	if !exists {
		return ImpactAnalysisResult{}, false
	}
	fileByPath := make(map[string]domain.SourceFile, len(files))
	for _, f := range files {
		fileByPath[f.Path] = f
	}

	effectiveMaxDepth := maxDepth
	if effectiveMaxDepth <= 0 {
		effectiveMaxDepth = len(files)
		if effectiveMaxDepth == 0 {
			effectiveMaxDepth = 1
		}
	}

	// Build reverse adjacency list: imported_file -> []importer_file
	// Because Edge{SourcePath: S, TargetPath: T} means S imports T,
	// if T changes, S is impacted.
	dependentsMap := make(map[string][]string, len(files))
	seenPair := make(map[[2]string]bool, len(edges))
	for _, e := range edges {
		if e.SourcePath == "" || e.TargetPath == "" || e.SourcePath == e.TargetPath {
			continue
		}
		if _, ok := fileByPath[e.SourcePath]; !ok {
			continue
		}
		if _, ok := fileByPath[e.TargetPath]; !ok {
			continue
		}
		pair := [2]string{e.SourcePath, e.TargetPath}
		if seenPair[pair] {
			continue
		}
		seenPair[pair] = true
		dependentsMap[e.TargetPath] = append(dependentsMap[e.TargetPath], e.SourcePath)
	}
	for k := range dependentsMap {
		sort.Strings(dependentsMap[k])
	}

	visitedDepth := make(map[string]int, len(files))
	viaPathMap := make(map[string]string, len(files))
	visitedDepth[targetPath] = 0

	queue := []string{targetPath}
	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]

		currDepth := visitedDepth[curr]
		if currDepth >= effectiveMaxDepth {
			continue
		}

		for _, dep := range dependentsMap[curr] {
			if _, seen := visitedDepth[dep]; seen {
				continue
			}
			visitedDepth[dep] = currDepth + 1
			viaPathMap[dep] = curr
			queue = append(queue, dep)
		}
	}

	var affected []ImpactedFile
	directCount := 0
	indirectCount := 0

	for p, d := range visitedDepth {
		if p == targetPath || d <= 0 {
			continue
		}
		f := fileByPath[p]
		via := viaPathMap[p]
		isDirect := d == 1
		if isDirect {
			directCount++
		} else {
			indirectCount++
		}
		nodeID := domain.NodeIDForPath(p)
		viaNodeID := domain.NodeIDForPath(via)
		if f.Language == "go" || f.NodeKind == "package" {
			nodeID = domain.NodeIDForPackage(p)
			if via != "" {
				viaNodeID = domain.NodeIDForPackage(via)
			}
		}
		affected = append(affected, ImpactedFile{
			NodeID:          nodeID,
			Path:            p,
			Depth:           d,
			Direct:          isDirect,
			ViaNodeID:       viaNodeID,
			ViaPath:         via,
			InDegree:        f.InDegree,
			OutDegree:       f.OutDegree,
			ImportanceScore: f.ImportanceScore,
		})
	}

	// Deterministic ordering: Depth ASC, ImportanceScore DESC, Path ASC
	sort.Slice(affected, func(i, j int) bool {
		if affected[i].Depth != affected[j].Depth {
			return affected[i].Depth < affected[j].Depth
		}
		if affected[i].ImportanceScore != affected[j].ImportanceScore {
			return affected[i].ImportanceScore > affected[j].ImportanceScore
		}
		return affected[i].Path < affected[j].Path
	})

	// Collect edges that participate in the impact subgraph
	var affectedEdges []Edge
	for _, e := range edges {
		if e.SourcePath == e.TargetPath {
			continue
		}
		srcDepth, srcIn := visitedDepth[e.SourcePath]
		dstDepth, dstIn := visitedDepth[e.TargetPath]
		if !srcIn || !dstIn {
			continue
		}
		// Edge S -> T is part of the impact propagation if S is an impacted node (srcDepth >= 1)
		// and T is either the target (dstDepth == 0) or another impacted node.
		if srcDepth >= 1 && dstDepth >= 0 {
			affectedEdges = append(affectedEdges, Edge{
				SourcePath: e.SourcePath,
				TargetPath: e.TargetPath,
				Kind:       "import",
			})
		}
	}
	sort.Slice(affectedEdges, func(i, j int) bool {
		if affectedEdges[i].SourcePath != affectedEdges[j].SourcePath {
			return affectedEdges[i].SourcePath < affectedEdges[j].SourcePath
		}
		return affectedEdges[i].TargetPath < affectedEdges[j].TargetPath
	})

	if affected == nil {
		affected = []ImpactedFile{}
	}
	if affectedEdges == nil {
		affectedEdges = []Edge{}
	}

	targetNodeID := domain.NodeIDForPath(targetPath)
	if targetFile.Language == "go" || targetFile.NodeKind == "package" {
		targetNodeID = domain.NodeIDForPackage(targetPath)
	}

	return ImpactAnalysisResult{
		TargetNodeID:  targetNodeID,
		TargetPath:    targetPath,
		MaxDepth:      effectiveMaxDepth,
		TotalFiles:    len(files),
		TotalAffected: len(affected),
		DirectCount:   directCount,
		IndirectCount: indirectCount,
		AffectedFiles: affected,
		AffectedEdges: affectedEdges,
	}, true
}

type MultiSourceImpactResult struct {
	Summary       domain.PRImpactSummary  `json:"summary"`
	AffectedFiles []domain.PRAffectedFile `json:"affected_files"`
	AffectedEdges []Edge                  `json:"affected_edges"`
}

// AnalyzeMultiSourceImpact performs a deterministic Multi-Source Breadth-First Search (BFS)
// over reverse import edges from all changed Python/Go files in a pull request.
// If A imports B and B is changed, A is impacted.
// Reached files with depth=1 are classified as Direct, depth>=2 as Indirect.
func AnalyzeMultiSourceImpact(
	files []domain.SourceFile,
	edges []Edge,
	changedFiles []domain.PullRequestFile,
	maxDepth int,
) MultiSourceImpactResult {
	fileByPath := make(map[string]domain.SourceFile, len(files))
	for _, f := range files {
		fileByPath[f.Path] = f
	}

	effectiveMaxDepth := maxDepth
	if effectiveMaxDepth <= 0 {
		effectiveMaxDepth = len(files)
		if effectiveMaxDepth == 0 {
			effectiveMaxDepth = 1
		}
	}

	// 1. Identify starting changed files and count unanalyzed files
	startSet := make(map[string]bool)
	unanalyzedCount := 0
	for _, cf := range changedFiles {
		if !cf.IsAnalyzed {
			unanalyzedCount++
			continue
		}
		// Match against HEAD graph files (handling file or package matching)
		if matchedPath, _, ok := findMatchingTarget(files, cf.Path); ok {
			startSet[matchedPath] = true
		} else {
			unanalyzedCount++
		}
	}

	// 2. Build reverse dependency graph (imported_target -> []importing_sources)
	dependentsMap := make(map[string][]string, len(files))
	seenPair := make(map[[2]string]bool, len(edges))
	for _, e := range edges {
		if e.SourcePath == "" || e.TargetPath == "" || e.SourcePath == e.TargetPath {
			continue
		}
		if _, ok := fileByPath[e.SourcePath]; !ok {
			continue
		}
		if _, ok := fileByPath[e.TargetPath]; !ok {
			continue
		}
		pair := [2]string{e.SourcePath, e.TargetPath}
		if seenPair[pair] {
			continue
		}
		seenPair[pair] = true
		dependentsMap[e.TargetPath] = append(dependentsMap[e.TargetPath], e.SourcePath)
	}
	for k := range dependentsMap {
		sort.Strings(dependentsMap[k])
	}

	// 3. Multi-source BFS
	dist := make(map[string]int, len(files))
	parent := make(map[string]string, len(files))
	sourceRoots := make(map[string][]string, len(files))

	var startList []string
	for p := range startSet {
		startList = append(startList, p)
	}
	sort.Strings(startList)

	queue := make([]string, 0, len(files))
	for _, s := range startList {
		dist[s] = 0
		parent[s] = ""
		sourceRoots[s] = []string{s}
		queue = append(queue, s)
	}

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]

		currDist := dist[curr]
		if currDist >= effectiveMaxDepth {
			continue
		}

		for _, dep := range dependentsMap[curr] {
			if startSet[dep] {
				continue
			}

			prevDist, seen := dist[dep]
			if !seen {
				dist[dep] = currDist + 1
				parent[dep] = curr
				sourceRoots[dep] = append([]string(nil), sourceRoots[curr]...)
				queue = append(queue, dep)
			} else if prevDist == currDist+1 {
				sourceRoots[dep] = mergeSortedStrings(sourceRoots[dep], sourceRoots[curr])
				if curr < parent[dep] {
					parent[dep] = curr
				}
			}
		}
	}

	// 4. Construct affected files
	var affected []domain.PRAffectedFile
	directCount := 0
	indirectCount := 0

	for p, d := range dist {
		if startSet[p] || d <= 0 {
			continue
		}
		f := fileByPath[p]
		isDirect := d == 1
		var impactType string
		var visualRole domain.PRNodeVisualRole
		if isDirect {
			directCount++
			impactType = "direct"
			visualRole = domain.PRVisualRoleDirectImpact
		} else {
			indirectCount++
			impactType = "indirect"
			visualRole = domain.PRVisualRoleIndirectImpact
		}

		var revPath []string
		curr := p
		for curr != "" {
			revPath = append(revPath, curr)
			curr = parent[curr]
		}
		repPath := make([]string, len(revPath))
		for i := range revPath {
			repPath[i] = revPath[len(revPath)-1-i]
		}

		roots := sourceRoots[p]
		sort.Strings(roots)

		nodeID := domain.NodeIDForPath(p)
		if f.Language == "go" || f.NodeKind == "package" {
			nodeID = domain.NodeIDForPackage(p)
		}

		affected = append(affected, domain.PRAffectedFile{
			NodeID:             nodeID,
			Path:               p,
			Depth:              d,
			ImpactType:         impactType,
			SourceChangedFiles: roots,
			RepresentativePath: repPath,
			InDegree:           f.InDegree,
			OutDegree:          f.OutDegree,
			ImportanceScore:    f.ImportanceScore,
			VisualRole:         visualRole,
		})
	}

	sort.Slice(affected, func(i, j int) bool {
		if affected[i].Depth != affected[j].Depth {
			return affected[i].Depth < affected[j].Depth
		}
		if affected[i].ImportanceScore != affected[j].ImportanceScore {
			return affected[i].ImportanceScore > affected[j].ImportanceScore
		}
		return affected[i].Path < affected[j].Path
	})

	var affectedEdges []Edge
	for _, e := range edges {
		if e.SourcePath == e.TargetPath {
			continue
		}
		srcDist, srcIn := dist[e.SourcePath]
		dstDist, dstIn := dist[e.TargetPath]
		if !srcIn || !dstIn {
			continue
		}
		if srcDist >= 1 && dstDist >= 0 {
			affectedEdges = append(affectedEdges, Edge{
				SourcePath:    e.SourcePath,
				TargetPath:    e.TargetPath,
				Kind:          "import",
				EvidenceCount: e.EvidenceCount,
			})
		}
	}
	sort.Slice(affectedEdges, func(i, j int) bool {
		if affectedEdges[i].SourcePath != affectedEdges[j].SourcePath {
			return affectedEdges[i].SourcePath < affectedEdges[j].SourcePath
		}
		return affectedEdges[i].TargetPath < affectedEdges[j].TargetPath
	})

	if affected == nil {
		affected = []domain.PRAffectedFile{}
	}
	if affectedEdges == nil {
		affectedEdges = []Edge{}
	}

	completeness := "complete"
	if unanalyzedCount > 0 {
		completeness = "partial"
	}

	return MultiSourceImpactResult{
		Summary: domain.PRImpactSummary{
			ChangedFilesCount:    len(changedFiles),
			DirectImpactCount:    directCount,
			IndirectImpactCount:  indirectCount,
			TotalAffectedCount:   len(affected),
			UnanalyzedFilesCount: unanalyzedCount,
			AnalysisCompleteness: completeness,
		},
		AffectedFiles: affected,
		AffectedEdges: affectedEdges,
	}
}

func mergeSortedStrings(a, b []string) []string {
	seen := make(map[string]bool, len(a)+len(b))
	var res []string
	for _, s := range a {
		if !seen[s] {
			seen[s] = true
			res = append(res, s)
		}
	}
	for _, s := range b {
		if !seen[s] {
			seen[s] = true
			res = append(res, s)
		}
	}
	sort.Strings(res)
	return res
}
