package diff

import (
	"math"
	"sort"
	"strings"

	"github.com/repoatlas/repoatlas/backend/internal/analyzer/graph"
	"github.com/repoatlas/repoatlas/backend/internal/domain"
)

// DiffNode represents a node in the comparison graph with its diff status and metadata.
type DiffNode struct {
	ID                 string                `json:"id"`
	Path               string                `json:"path"`
	NodeKind           string                `json:"node_kind"` // "file" or "package"
	Language           string                `json:"language"`
	ChangeType         domain.NodeChangeType `json:"change_type"` // added, removed, modified, unchanged
	InCycleBase        bool                  `json:"in_cycle_base"`
	InCycleHead        bool                  `json:"in_cycle_head"`
	BaseFunctions      int                   `json:"base_functions"`
	HeadFunctions      int                   `json:"head_functions"`
	FunctionsDiff      int                   `json:"functions_diff"`
	BaseClasses        int                   `json:"base_classes"`
	HeadClasses        int                   `json:"head_classes"`
	ClassesDiff        int                   `json:"classes_diff"`
	BaseInDegree       int                   `json:"base_in_degree"`
	HeadInDegree       int                   `json:"head_in_degree"`
	BaseOutDegree      int                   `json:"base_out_degree"`
	HeadOutDegree      int                   `json:"head_out_degree"`
	IsEntrypointBase   bool                  `json:"is_entrypoint_base"`
	IsEntrypointHead   bool                  `json:"is_entrypoint_head"`
	ImportanceScore    float64               `json:"importance_score"`
	FilesCount         int                   `json:"files_count,omitempty"`
	PackageImportPath  string                `json:"package_import_path,omitempty"`
	DirectoryPath      string                `json:"directory_path,omitempty"`
	PackageName        string                `json:"package_name,omitempty"`
	BaseFiles          []domain.PackageFileInfo `json:"base_files,omitempty"`
	HeadFiles          []domain.PackageFileInfo `json:"head_files,omitempty"`
}

// DiffEdge represents a dependency edge in the comparison graph with its diff status.
type DiffEdge struct {
	Source     string                `json:"source"`
	Target     string                `json:"target"`
	SourcePath string                `json:"source_path"`
	TargetPath string                `json:"target_path"`
	ChangeType domain.EdgeChangeType `json:"change_type"` // added, removed, unchanged
	Kind       string                `json:"kind"`
}

// CycleChanges details newly introduced, resolved, and persistent cyclic components.
type CycleChanges struct {
	AddedCycles      [][]string `json:"added_cycles"`
	ResolvedCycles   [][]string `json:"resolved_cycles"`
	PersistentCycles [][]string `json:"persistent_cycles"`
}

// GraphDiffResult holds the full comparison result between BASE and HEAD dependency graphs.
type GraphDiffResult struct {
	Summary      domain.DiffSummary `json:"summary"`
	Nodes        []DiffNode         `json:"nodes"`
	Edges        []DiffEdge         `json:"edges"`
	CycleChanges CycleChanges       `json:"cycle_changes"`
	Warnings     []string           `json:"warnings"`
}

// ComputeGraphDiff performs a deterministic comparison between BASE and HEAD graph snapshots.
func ComputeGraphDiff(
	baseSHA, headSHA string,
	language, granularity string,
	baseFiles []domain.SourceFile, baseEdges []graph.Edge,
	headFiles []domain.SourceFile, headEdges []graph.Edge,
) GraphDiffResult {
	if language == "" {
		language = "python"
	}
	if granularity == "" {
		if language == "go" {
			granularity = "package"
		} else {
			granularity = "file"
		}
	}

	nodeIDFor := func(f domain.SourceFile) string {
		if f.Language == "go" || f.NodeKind == "package" {
			if f.PackageImportPath != "" {
				return domain.NodeIDForPackage(f.PackageImportPath)
			}
			return domain.NodeIDForPackage(f.Path)
		}
		return domain.NodeIDForPath(f.Path)
	}

	baseMap := make(map[string]domain.SourceFile, len(baseFiles))
	for _, f := range baseFiles {
		baseMap[f.Path] = f
	}

	headMap := make(map[string]domain.SourceFile, len(headFiles))
	for _, f := range headFiles {
		headMap[f.Path] = f
	}

	// 1. Detect cycles in BASE and HEAD using Tarjan SCC
	baseSCC := DetectCycles(baseFiles, baseEdges)
	headSCC := DetectCycles(headFiles, headEdges)

	// Compare cyclic SCCs
	sccKey := func(scc []string) string {
		sorted := make([]string, len(scc))
		copy(sorted, scc)
		sort.Strings(sorted)
		return strings.Join(sorted, "||")
	}

	baseSCCMap := make(map[string][]string, len(baseSCC.CyclicSCCs))
	for _, scc := range baseSCC.CyclicSCCs {
		baseSCCMap[sccKey(scc)] = scc
	}

	headSCCMap := make(map[string][]string, len(headSCC.CyclicSCCs))
	for _, scc := range headSCC.CyclicSCCs {
		headSCCMap[sccKey(scc)] = scc
	}

	var addedCycles [][]string
	var resolvedCycles [][]string
	var persistentCycles [][]string

	for k, scc := range headSCCMap {
		if _, exists := baseSCCMap[k]; exists {
			persistentCycles = append(persistentCycles, scc)
		} else {
			addedCycles = append(addedCycles, scc)
		}
	}

	for k, scc := range baseSCCMap {
		if _, exists := headSCCMap[k]; !exists {
			resolvedCycles = append(resolvedCycles, scc)
		}
	}

	sort.Slice(addedCycles, func(i, j int) bool {
		if len(addedCycles[i]) == 0 || len(addedCycles[j]) == 0 {
			return len(addedCycles[i]) < len(addedCycles[j])
		}
		return addedCycles[i][0] < addedCycles[j][0]
	})
	sort.Slice(resolvedCycles, func(i, j int) bool {
		if len(resolvedCycles[i]) == 0 || len(resolvedCycles[j]) == 0 {
			return len(resolvedCycles[i]) < len(resolvedCycles[j])
		}
		return resolvedCycles[i][0] < resolvedCycles[j][0]
	})
	sort.Slice(persistentCycles, func(i, j int) bool {
		if len(persistentCycles[i]) == 0 || len(persistentCycles[j]) == 0 {
			return len(persistentCycles[i]) < len(persistentCycles[j])
		}
		return persistentCycles[i][0] < persistentCycles[j][0]
	})

	// 2. Node Diff
	allKeys := make(map[string]bool)
	for k := range baseMap {
		allKeys[k] = true
	}
	for k := range headMap {
		allKeys[k] = true
	}

	var sortedKeys []string
	for k := range allKeys {
		sortedKeys = append(sortedKeys, k)
	}
	sort.Strings(sortedKeys)

	var diffNodes []DiffNode
	addedNodesCount := 0
	removedNodesCount := 0
	modifiedNodesCount := 0

	for _, k := range sortedKeys {
		bf, inBase := baseMap[k]
		hf, inHead := headMap[k]

		var changeType domain.NodeChangeType
		var n DiffNode

		switch {
		case inHead && !inBase:
			changeType = domain.NodeChangeAdded
			addedNodesCount++
			n = DiffNode{
				ID:                nodeIDFor(hf),
				Path:              hf.Path,
				NodeKind:          hf.NodeKind,
				Language:          hf.Language,
				ChangeType:        changeType,
				InCycleHead:       headSCC.NodeInCycle[k],
				HeadFunctions:     hf.FunctionsCount,
				FunctionsDiff:     hf.FunctionsCount,
				HeadClasses:       hf.ClassesCount,
				ClassesDiff:       hf.ClassesCount,
				HeadInDegree:      hf.InDegree,
				HeadOutDegree:     hf.OutDegree,
				IsEntrypointHead:  hf.IsEntrypoint,
				ImportanceScore:   hf.ImportanceScore,
				FilesCount:        hf.FilesCount,
				PackageImportPath: hf.PackageImportPath,
				DirectoryPath:     hf.DirectoryPath,
				PackageName:       hf.PackageName,
				HeadFiles:         hf.Files,
			}
		case inBase && !inHead:
			changeType = domain.NodeChangeRemoved
			removedNodesCount++
			n = DiffNode{
				ID:                nodeIDFor(bf),
				Path:              bf.Path,
				NodeKind:          bf.NodeKind,
				Language:          bf.Language,
				ChangeType:        changeType,
				InCycleBase:       baseSCC.NodeInCycle[k],
				BaseFunctions:     bf.FunctionsCount,
				FunctionsDiff:     -bf.FunctionsCount,
				BaseClasses:       bf.ClassesCount,
				ClassesDiff:       -bf.ClassesCount,
				BaseInDegree:      bf.InDegree,
				BaseOutDegree:     bf.OutDegree,
				IsEntrypointBase:  bf.IsEntrypoint,
				ImportanceScore:   bf.ImportanceScore,
				FilesCount:        bf.FilesCount,
				PackageImportPath: bf.PackageImportPath,
				DirectoryPath:     bf.DirectoryPath,
				PackageName:       bf.PackageName,
				BaseFiles:         bf.Files,
			}
		default: // in both
			isModified := bf.FunctionsCount != hf.FunctionsCount ||
				bf.ClassesCount != hf.ClassesCount ||
				bf.InDegree != hf.InDegree ||
				bf.OutDegree != hf.OutDegree ||
				bf.IsEntrypoint != hf.IsEntrypoint ||
				bf.FilesCount != hf.FilesCount

			if isModified {
				changeType = domain.NodeChangeModified
				modifiedNodesCount++
			} else {
				changeType = domain.NodeChangeUnchanged
			}

			n = DiffNode{
				ID:                nodeIDFor(hf),
				Path:              hf.Path,
				NodeKind:          hf.NodeKind,
				Language:          hf.Language,
				ChangeType:        changeType,
				InCycleBase:       baseSCC.NodeInCycle[k],
				InCycleHead:       headSCC.NodeInCycle[k],
				BaseFunctions:     bf.FunctionsCount,
				HeadFunctions:     hf.FunctionsCount,
				FunctionsDiff:     hf.FunctionsCount - bf.FunctionsCount,
				BaseClasses:       bf.ClassesCount,
				HeadClasses:       hf.ClassesCount,
				ClassesDiff:       hf.ClassesCount - bf.ClassesCount,
				BaseInDegree:      bf.InDegree,
				HeadInDegree:      hf.InDegree,
				BaseOutDegree:     bf.OutDegree,
				HeadOutDegree:     hf.OutDegree,
				IsEntrypointBase:  bf.IsEntrypoint,
				IsEntrypointHead:  hf.IsEntrypoint,
				ImportanceScore:   hf.ImportanceScore,
				FilesCount:        hf.FilesCount,
				PackageImportPath: hf.PackageImportPath,
				DirectoryPath:     hf.DirectoryPath,
				PackageName:       hf.PackageName,
				BaseFiles:         bf.Files,
				HeadFiles:         hf.Files,
			}
		}

		if n.NodeKind == "" {
			if language == "go" {
				n.NodeKind = "package"
			} else {
				n.NodeKind = "file"
			}
		}
		if n.Language == "" {
			n.Language = language
		}

		diffNodes = append(diffNodes, n)
	}

	// 3. Edge Diff
	type edgeKey struct {
		src string
		dst string
	}
	baseEdgeMap := make(map[edgeKey]graph.Edge, len(baseEdges))
	for _, e := range baseEdges {
		baseEdgeMap[edgeKey{src: e.SourcePath, dst: e.TargetPath}] = e
	}

	headEdgeMap := make(map[edgeKey]graph.Edge, len(headEdges))
	for _, e := range headEdges {
		headEdgeMap[edgeKey{src: e.SourcePath, dst: e.TargetPath}] = e
	}

	allEdgeKeys := make(map[edgeKey]bool)
	for k := range baseEdgeMap {
		allEdgeKeys[k] = true
	}
	for k := range headEdgeMap {
		allEdgeKeys[k] = true
	}

	var sortedEdgeKeys []edgeKey
	for k := range allEdgeKeys {
		sortedEdgeKeys = append(sortedEdgeKeys, k)
	}
	sort.Slice(sortedEdgeKeys, func(i, j int) bool {
		if sortedEdgeKeys[i].src != sortedEdgeKeys[j].src {
			return sortedEdgeKeys[i].src < sortedEdgeKeys[j].src
		}
		return sortedEdgeKeys[i].dst < sortedEdgeKeys[j].dst
	})

	var diffEdges []DiffEdge
	addedEdgesCount := 0
	removedEdgesCount := 0

	for _, k := range sortedEdgeKeys {
		_, inBase := baseEdgeMap[k]
		_, inHead := headEdgeMap[k]

		var changeType domain.EdgeChangeType
		switch {
		case inHead && !inBase:
			changeType = domain.EdgeChangeAdded
			addedEdgesCount++
		case inBase && !inHead:
			changeType = domain.EdgeChangeRemoved
			removedEdgesCount++
		default:
			changeType = domain.EdgeChangeUnchanged
		}

		var srcID, dstID string
		if language == "go" {
			srcID = domain.NodeIDForPackage(k.src)
			dstID = domain.NodeIDForPackage(k.dst)
		} else {
			srcID = domain.NodeIDForPath(k.src)
			dstID = domain.NodeIDForPath(k.dst)
		}

		diffEdges = append(diffEdges, DiffEdge{
			Source:     srcID,
			Target:     dstID,
			SourcePath: k.src,
			TargetPath: k.dst,
			ChangeType: changeType,
			Kind:       "import",
		})
	}

	// 4. Metrics Diff Calculation
	calcMetrics := func(files []domain.SourceFile) (avgIn, avgOut, maxIn, maxOut float64) {
		if len(files) == 0 {
			return 0, 0, 0, 0
		}
		var sumIn, sumOut int
		var mIn, mOut int
		for _, f := range files {
			sumIn += f.InDegree
			sumOut += f.OutDegree
			if f.InDegree > mIn {
				mIn = f.InDegree
			}
			if f.OutDegree > mOut {
				mOut = f.OutDegree
			}
		}
		avgIn = math.Round((float64(sumIn)/float64(len(files)))*100) / 100
		avgOut = math.Round((float64(sumOut)/float64(len(files)))*100) / 100
		maxIn = float64(mIn)
		maxOut = float64(mOut)
		return
	}

	bAvgIn, bAvgOut, bMaxIn, bMaxOut := calcMetrics(baseFiles)
	hAvgIn, hAvgOut, hMaxIn, hMaxOut := calcMetrics(headFiles)

	round2 := func(val float64) float64 {
		return math.Round(val*100) / 100
	}

	metrics := domain.ArchitectureMetricsDiff{
		TotalNodes: domain.MetricValue{
			Base: float64(len(baseFiles)),
			Head: float64(len(headFiles)),
			Diff: float64(len(headFiles) - len(baseFiles)),
		},
		TotalEdges: domain.MetricValue{
			Base: float64(len(baseEdges)),
			Head: float64(len(headEdges)),
			Diff: float64(len(headEdges) - len(baseEdges)),
		},
		AddedNodes:    addedNodesCount,
		RemovedNodes:  removedNodesCount,
		ModifiedNodes: modifiedNodesCount,
		AddedEdges:    addedEdgesCount,
		RemovedEdges:  removedEdgesCount,
		CyclicSCCCount: domain.MetricValue{
			Base: float64(baseSCC.CyclicSCCCount),
			Head: float64(headSCC.CyclicSCCCount),
			Diff: float64(headSCC.CyclicSCCCount - baseSCC.CyclicSCCCount),
		},
		CyclicNodesCount: domain.MetricValue{
			Base: float64(baseSCC.CyclicNodesCount),
			Head: float64(headSCC.CyclicNodesCount),
			Diff: float64(headSCC.CyclicNodesCount - baseSCC.CyclicNodesCount),
		},
		AvgInDegree: domain.MetricValue{
			Base: bAvgIn,
			Head: hAvgIn,
			Diff: round2(hAvgIn - bAvgIn),
		},
		AvgOutDegree: domain.MetricValue{
			Base: bAvgOut,
			Head: hAvgOut,
			Diff: round2(hAvgOut - bAvgOut),
		},
		MaxInDegree: domain.MetricValue{
			Base: bMaxIn,
			Head: hMaxIn,
			Diff: hMaxIn - bMaxIn,
		},
		MaxOutDegree: domain.MetricValue{
			Base: bMaxOut,
			Head: hMaxOut,
			Diff: hMaxOut - bMaxOut,
		},
	}

	summary := domain.DiffSummary{
		BaseCommitSHA:      baseSHA,
		HeadCommitSHA:      headSHA,
		Language:           language,
		Granularity:        granularity,
		TotalBaseNodes:     len(baseFiles),
		TotalHeadNodes:     len(headFiles),
		TotalBaseEdges:     len(baseEdges),
		TotalHeadEdges:     len(headEdges),
		AddedNodesCount:    addedNodesCount,
		RemovedNodesCount:  removedNodesCount,
		ModifiedNodesCount: modifiedNodesCount,
		AddedEdgesCount:    addedEdgesCount,
		RemovedEdgesCount:  removedEdgesCount,
		BaseCyclicSCCs:     baseSCC.CyclicSCCCount,
		HeadCyclicSCCs:     headSCC.CyclicSCCCount,
		Metrics:            metrics,
	}

	if addedCycles == nil {
		addedCycles = [][]string{}
	}
	if resolvedCycles == nil {
		resolvedCycles = [][]string{}
	}
	if persistentCycles == nil {
		persistentCycles = [][]string{}
	}

	return GraphDiffResult{
		Summary: summary,
		Nodes:   diffNodes,
		Edges:   diffEdges,
		CycleChanges: CycleChanges{
			AddedCycles:      addedCycles,
			ResolvedCycles:   resolvedCycles,
			PersistentCycles: persistentCycles,
		},
		Warnings: []string{
			"Deterministic AST dependency diff; dynamic calls and reflection are not reflected in graph changes.",
		},
	}
}
