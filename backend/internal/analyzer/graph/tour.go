package graph

import (
	"fmt"
	"math"
	"path"
	"sort"
	"strings"

	"github.com/repoatlas/repoatlas/backend/internal/domain"
)

// TourRole identifies why a file was chosen in the Guided Code Tour.
type TourRole string

const (
	TourRoleEntrypoint   TourRole = "entrypoint"
	TourRoleOrchestrator TourRole = "orchestrator"
	TourRoleCoreModule   TourRole = "core_module"
	TourRoleFoundation   TourRole = "foundation"
	TourRoleModule       TourRole = "module"
)

// GuidedTourStep represents a single step in the recommended reading order.
type GuidedTourStep struct {
	Step         int      `json:"step"`
	NodeID       string   `json:"node_id"`
	Path         string   `json:"path"`
	Role         TourRole `json:"role"`
	RoleLabel    string   `json:"role_label"`
	Score        float64  `json:"score"`
	Reason       string   `json:"reason"`
	InDegree     int      `json:"in_degree"`
	OutDegree    int      `json:"out_degree"`
	Functions    int      `json:"functions"`
	Classes      int      `json:"classes"`
	IsEntrypoint bool     `json:"is_entrypoint"`
}

var extendedEntrypointNames = map[string]bool{
	"main.py":     true,
	"app.py":      true,
	"__main__.py": true,
	"cli.py":      true,
	"server.py":   true,
	"run.py":      true,
	"wsgi.py":     true,
	"asgi.py":     true,
	"manage.py":   true,
}

func isTestOrAuxPath(filePath string) bool {
	lower := strings.ToLower(filePath)
	segments := strings.Split(lower, "/")
	for _, seg := range segments {
		if seg == "tests" || seg == "test" || seg == "testing" || seg == "docs" || seg == "examples" || seg == "benchmarks" {
			return true
		}
	}
	base := path.Base(lower)
	return strings.HasPrefix(base, "test_") ||
		strings.HasSuffix(base, "_test.py") ||
		base == "conftest.py" ||
		base == "setup.py" ||
		base == "noxfile.py" ||
		base == "fabfile.py"
}

func directoryDepth(filePath string) int {
	return strings.Count(filePath, "/")
}

func isFacadeInit(filePath string) bool {
	if path.Base(filePath) != "__init__.py" {
		return false
	}
	// Root or top-level package __init__.py (e.g. pkg/__init__.py or src/pkg/__init__.py)
	depth := directoryDepth(filePath)
	return depth <= 2
}

// DetermineTargetTourSize returns a deterministic target tour length in [5, 10]
// (or len(files) when the repository has fewer than 5 files).
func DetermineTargetTourSize(totalFiles int, nonTestFiles int, maxSteps int) int {
	if totalFiles == 0 {
		return 0
	}
	if maxSteps <= 0 {
		maxSteps = 10
	}
	if maxSteps < 5 {
		maxSteps = 5
	}
	if maxSteps > 10 {
		maxSteps = 10
	}
	if totalFiles <= 5 {
		return totalFiles
	}

	effective := nonTestFiles
	if effective < 5 {
		effective = totalFiles
	}
	// Scale between 5 and 10 based on repository size
	scaled := 5 + (effective-5)/4
	if scaled < 5 {
		scaled = 5
	}
	if scaled > maxSteps {
		scaled = maxSteps
	}
	if scaled > totalFiles {
		scaled = totalFiles
	}
	return scaled
}

// BuildGuidedTour generates a deterministic reading order of 5–10 files
// prioritizing:
//  1. Entrypoints & public package facades (where execution or API usage begins)
//  2. Orchestrators & workflow coordinators reachable from entrypoints
//  3. Core domain & shared foundation modules heavily imported across the codebase
func BuildGuidedTour(files []domain.SourceFile, edges []Edge, maxSteps int) []GuidedTourStep {
	if len(files) == 0 {
		return []GuidedTourStep{}
	}

	// Copy and sort files deterministically by path ASC
	sortedFiles := make([]domain.SourceFile, len(files))
	copy(sortedFiles, files)
	sort.Slice(sortedFiles, func(i, j int) bool {
		return sortedFiles[i].Path < sortedFiles[j].Path
	})

	fileByPath := make(map[string]domain.SourceFile, len(sortedFiles))
	nonTestCount := 0
	for _, f := range sortedFiles {
		fileByPath[f.Path] = f
		if !isTestOrAuxPath(f.Path) {
			nonTestCount++
		}
	}

	targetSize := DetermineTargetTourSize(len(sortedFiles), nonTestCount, maxSteps)

	// Build forward adjacency list (source imports target: source -> target)
	outgoing := make(map[string][]string, len(sortedFiles))
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
		outgoing[e.SourcePath] = append(outgoing[e.SourcePath], e.TargetPath)
	}
	for k := range outgoing {
		sort.Strings(outgoing[k])
	}

	selectedPaths := make(map[string]bool, targetSize)
	var steps []GuidedTourStep

	addStep := func(f domain.SourceFile, role TourRole, roleLabel string, score float64, reason string) {
		selectedPaths[f.Path] = true
		nodeID := domain.NodeIDForPath(f.Path)
		if f.Language == "go" || f.NodeKind == "package" {
			if f.PackageImportPath != "" {
				nodeID = domain.NodeIDForPackage(f.PackageImportPath)
			} else {
				nodeID = domain.NodeIDForPackage(f.Path)
			}
		}
		steps = append(steps, GuidedTourStep{
			Step:         len(steps) + 1,
			NodeID:       nodeID,
			Path:         f.Path,
			Role:         role,
			RoleLabel:    roleLabel,
			Score:        math.Round(score*100) / 100,
			Reason:       reason,
			InDegree:     f.InDegree,
			OutDegree:    f.OutDegree,
			Functions:    f.FunctionsCount,
			Classes:      f.ClassesCount,
			IsEntrypoint: f.IsEntrypoint || extendedEntrypointNames[path.Base(f.Path)],
		})
	}

	// -------------------------------------------------------------------------
	// Phase 1: Entrypoint / Public API Facade Selection (up to 2 files)
	// -------------------------------------------------------------------------
	type entryCandidate struct {
		file   domain.SourceFile
		score  float64
		reason string
	}
	var entryCandidates []entryCandidate

	for _, f := range sortedFiles {
		if isTestOrAuxPath(f.Path) && nonTestCount > 0 {
			continue
		}
		base := path.Base(f.Path)
		isExplicitEntry := f.IsEntrypoint || extendedEntrypointNames[base]
		if f.Language == "go" || f.NodeKind == "package" {
			if f.PackageName == "main" || strings.HasPrefix(f.DirectoryPath, "cmd/") || f.DirectoryPath == "cmd" {
				isExplicitEntry = true
			}
		}
		isFacade := isFacadeInit(f.Path) && (f.OutDegree > 0 || f.InDegree > 0)
		isRootOrchestrator := f.InDegree == 0 && f.OutDegree > 0

		if !isExplicitEntry && !isFacade && !isRootOrchestrator {
			continue
		}

		depthPenalty := 0.35 * float64(directoryDepth(f.Path))
		symbolBonus := math.Min(2.0, 0.15*float64(f.FunctionsCount+f.ClassesCount))
		score := 0.9*float64(f.OutDegree) + 0.4*float64(f.InDegree) + symbolBonus - depthPenalty

		var reasonParts []string
		if isExplicitEntry {
			score += 6.0
			if f.Language == "go" || f.NodeKind == "package" {
				reasonParts = append(reasonParts, fmt.Sprintf("Go実行起点パッケージ (%s: %s)", f.PackageName, f.DirectoryPath))
			} else {
				reasonParts = append(reasonParts, fmt.Sprintf("実行の起点となるエントリーポイント (%s)", base))
			}
		} else if isFacade {
			score += 4.2
			reasonParts = append(reasonParts, "パッケージの公開API境界 (__init__.py)")
		} else if isRootOrchestrator {
			score += 3.2
			reasonParts = append(reasonParts, "被依存0で内部モジュールを呼び出す起点モジュール")
		}

		if f.OutDegree > 0 {
			reasonParts = append(reasonParts, fmt.Sprintf("内部モジュール %d件をimport", f.OutDegree))
		}
		if f.InDegree > 0 {
			reasonParts = append(reasonParts, fmt.Sprintf("被依存 %d件", f.InDegree))
		}

		entryCandidates = append(entryCandidates, entryCandidate{
			file:   f,
			score:  score,
			reason: strings.Join(reasonParts, " / "),
		})
	}

	sort.Slice(entryCandidates, func(i, j int) bool {
		if math.Abs(entryCandidates[i].score-entryCandidates[j].score) > 1e-9 {
			return entryCandidates[i].score > entryCandidates[j].score
		}
		return entryCandidates[i].file.Path < entryCandidates[j].file.Path
	})

	maxEntrySteps := 1
	if targetSize >= 6 && len(entryCandidates) >= 2 && entryCandidates[1].score >= 4.0 {
		maxEntrySteps = 2
	}
	for i := 0; i < len(entryCandidates) && len(steps) < maxEntrySteps && len(steps) < targetSize; i++ {
		c := entryCandidates[i]
		addStep(c.file, TourRoleEntrypoint, "Entrypoint / 起点", c.score, c.reason)
	}

	// Compute shortest forward import distance from selected entrypoints via BFS
	entryDist := make(map[string]int, len(sortedFiles))
	var bfsQueue []string
	for _, s := range steps {
		entryDist[s.Path] = 0
		bfsQueue = append(bfsQueue, s.Path)
	}
	for len(bfsQueue) > 0 {
		curr := bfsQueue[0]
		bfsQueue = bfsQueue[1:]
		d := entryDist[curr]
		for _, nxt := range outgoing[curr] {
			if _, visited := entryDist[nxt]; !visited {
				entryDist[nxt] = d + 1
				bfsQueue = append(bfsQueue, nxt)
			}
		}
	}

	// -------------------------------------------------------------------------
	// Phase 2: Orchestrators & Main Workflow Coordinators
	// -------------------------------------------------------------------------
	orchestratorQuota := (targetSize - len(steps)) / 2
	if orchestratorQuota < 2 && targetSize-len(steps) >= 2 {
		orchestratorQuota = 2
	}

	type orchCandidate struct {
		file   domain.SourceFile
		dist   int
		score  float64
		reason string
	}
	var orchCandidates []orchCandidate

	for _, f := range sortedFiles {
		if selectedPaths[f.Path] {
			continue
		}
		if isTestOrAuxPath(f.Path) && nonTestCount > len(steps) {
			continue
		}
		if f.OutDegree == 0 {
			continue
		}

		dist, reachable := entryDist[f.Path]
		if !reachable {
			dist = 999
		}

		score := 1.4*float64(f.OutDegree) + 1.1*float64(f.InDegree) +
			math.Min(2.5, 0.2*float64(f.FunctionsCount+f.ClassesCount)) -
			0.25*float64(directoryDepth(f.Path))

		var reasons []string
		if dist == 1 {
			score += 3.5
			reasons = append(reasons, "起点ファイルから直接呼び出される主要処理")
		} else if dist == 2 {
			score += 1.8
			reasons = append(reasons, "起点から2ホップで到達する主要フロー")
		}
		if f.InDegree > 0 && f.OutDegree >= 2 {
			score += 2.0
			reasons = append(reasons, fmt.Sprintf("%d個のモジュールを束ねて%d個から参照されるハブ", f.OutDegree, f.InDegree))
		} else {
			reasons = append(reasons, fmt.Sprintf("内部依存先 %d件 / 被依存 %d件", f.OutDegree, f.InDegree))
		}
		if f.FunctionsCount+f.ClassesCount > 0 {
			reasons = append(reasons, fmt.Sprintf("関数%d・クラス%dを定義", f.FunctionsCount, f.ClassesCount))
		}

		orchCandidates = append(orchCandidates, orchCandidate{
			file:   f,
			dist:   dist,
			score:  score,
			reason: strings.Join(reasons, " / "),
		})
	}

	sort.Slice(orchCandidates, func(i, j int) bool {
		if math.Abs(orchCandidates[i].score-orchCandidates[j].score) > 1e-9 {
			return orchCandidates[i].score > orchCandidates[j].score
		}
		if orchCandidates[i].dist != orchCandidates[j].dist {
			return orchCandidates[i].dist < orchCandidates[j].dist
		}
		return orchCandidates[i].file.Path < orchCandidates[j].file.Path
	})

	for i := 0; i < len(orchCandidates) && i < orchestratorQuota && len(steps) < targetSize; i++ {
		c := orchCandidates[i]
		addStep(c.file, TourRoleOrchestrator, "Workflow / 主要制御", c.score, c.reason)
	}

	// Track which files are directly imported by already-selected tour steps
	importedByTour := make(map[string]int)
	for _, s := range steps {
		for _, target := range outgoing[s.Path] {
			importedByTour[target]++
		}
	}

	// -------------------------------------------------------------------------
	// Phase 3: Core Domain & Shared Foundation Modules
	// -------------------------------------------------------------------------
	type coreCandidate struct {
		file   domain.SourceFile
		role   TourRole
		label  string
		score  float64
		reason string
	}
	var coreCandidates []coreCandidate

	for _, f := range sortedFiles {
		if selectedPaths[f.Path] {
			continue
		}
		if isTestOrAuxPath(f.Path) && len(steps) < nonTestCount {
			continue
		}

		tourRefCount := importedByTour[f.Path]
		symbolScore := math.Min(3.0, 0.25*float64(f.FunctionsCount+f.ClassesCount))
		score := 2.2*float64(f.InDegree) + 0.6*float64(f.OutDegree) +
			1.2*float64(tourRefCount) + symbolScore -
			0.2*float64(directoryDepth(f.Path))

		role := TourRoleCoreModule
		label := "Core Domain / 中核モデル"
		var reasons []string

		if f.InDegree >= 2 && f.OutDegree == 0 {
			role = TourRoleFoundation
			label = "Foundation / 共有基盤"
			reasons = append(reasons, fmt.Sprintf("%d個の内部ファイルから共通参照される基盤モジュール", f.InDegree))
		} else if f.InDegree >= 1 {
			reasons = append(reasons, fmt.Sprintf("被依存 %d件の中心モジュール", f.InDegree))
		} else {
			role = TourRoleModule
			label = "Module / 構成要素"
			reasons = append(reasons, "独立した構成モジュール")
		}

		if tourRefCount > 0 {
			reasons = append(reasons, fmt.Sprintf("前半ステップの%dファイルから直接import", tourRefCount))
		}
		if f.ClassesCount > 0 || f.FunctionsCount > 0 {
			reasons = append(reasons, fmt.Sprintf("クラス%d・関数%dを定義", f.ClassesCount, f.FunctionsCount))
		}

		coreCandidates = append(coreCandidates, coreCandidate{
			file:   f,
			role:   role,
			label:  label,
			score:  score,
			reason: strings.Join(reasons, " / "),
		})
	}

	sort.Slice(coreCandidates, func(i, j int) bool {
		if math.Abs(coreCandidates[i].score-coreCandidates[j].score) > 1e-9 {
			return coreCandidates[i].score > coreCandidates[j].score
		}
		if coreCandidates[i].file.ImportanceScore != coreCandidates[j].file.ImportanceScore {
			return coreCandidates[i].file.ImportanceScore > coreCandidates[j].file.ImportanceScore
		}
		return coreCandidates[i].file.Path < coreCandidates[j].file.Path
	})

	for i := 0; i < len(coreCandidates) && len(steps) < targetSize; i++ {
		c := coreCandidates[i]
		addStep(c.file, c.role, c.label, c.score, c.reason)
	}

	// -------------------------------------------------------------------------
	// Phase 4: Fallback if fewer than targetSize (e.g., repo with mostly tests)
	// -------------------------------------------------------------------------
	if len(steps) < targetSize {
		var remaining []domain.SourceFile
		for _, f := range sortedFiles {
			if !selectedPaths[f.Path] {
				remaining = append(remaining, f)
			}
		}
		sort.Slice(remaining, func(i, j int) bool {
			if remaining[i].ImportanceScore != remaining[j].ImportanceScore {
				return remaining[i].ImportanceScore > remaining[j].ImportanceScore
			}
			iSyms := remaining[i].FunctionsCount + remaining[i].ClassesCount
			jSyms := remaining[j].FunctionsCount + remaining[j].ClassesCount
			if iSyms != jSyms {
				return iSyms > jSyms
			}
			return remaining[i].Path < remaining[j].Path
		})
		for _, f := range remaining {
			if len(steps) >= targetSize {
				break
			}
			reason := fmt.Sprintf("補助モジュール (被依存 %d件 / 依存先 %d件 / 関数%d・クラス%d)",
				f.InDegree, f.OutDegree, f.FunctionsCount, f.ClassesCount)
			addStep(f, TourRoleModule, "Module / 構成要素", f.ImportanceScore, reason)
		}
	}

	return steps
}
