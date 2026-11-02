package graph

import (
	"fmt"
	"math/rand"
	"reflect"
	"testing"

	"github.com/repoatlas/repoatlas/backend/internal/domain"
)

func buildFilesAndEdges(rawFiles []domain.SourceFile, rawEdges []Edge) ([]domain.SourceFile, []Edge) {
	var imports []domain.ImportReference
	for _, e := range rawEdges {
		imports = append(imports, domain.ImportReference{
			SourcePath:     e.SourcePath,
			TargetPath:     e.TargetPath,
			Classification: domain.ImportInternal,
		})
	}
	out := BuildGraph(rawFiles, imports)
	return out.Files, out.Edges
}

// 1. 依存方向の正しさ & BFSの探索深度
func TestAnalyzeImpact_DirectionAndBFSDepth(t *testing.T) {
	// Chain:
	// app/cli.py --imports--> app/api.py --imports--> app/service.py --imports--> app/db.py --imports--> app/config.py
	// Also app/db.py --imports--> app/driver.py (downstream dependency of db.py, NOT dependent on db.py!)
	rawFiles := []domain.SourceFile{
		{Path: "app/cli.py", FunctionsCount: 2},
		{Path: "app/api.py", FunctionsCount: 4},
		{Path: "app/service.py", ClassesCount: 2},
		{Path: "app/db.py", ClassesCount: 1},
		{Path: "app/config.py", FunctionsCount: 1},
		{Path: "app/driver.py", FunctionsCount: 3},
	}
	rawEdges := []Edge{
		{SourcePath: "app/cli.py", TargetPath: "app/api.py"},
		{SourcePath: "app/api.py", TargetPath: "app/service.py"},
		{SourcePath: "app/service.py", TargetPath: "app/db.py"},
		{SourcePath: "app/db.py", TargetPath: "app/config.py"},
		{SourcePath: "app/db.py", TargetPath: "app/driver.py"},
	}
	files, edges := buildFilesAndEdges(rawFiles, rawEdges)

	// Target: app/db.py
	// Dependents on app/db.py:
	//   depth 1 (direct):   app/service.py
	//   depth 2 (indirect): app/api.py
	//   depth 3 (indirect): app/cli.py
	// NOT affected: app/config.py and app/driver.py (because db.py imports them, they do NOT import db.py!)
	resDepth1, ok := AnalyzeImpact(files, edges, "file:app/db.py", 1)
	if !ok {
		t.Fatal("expected AnalyzeImpact to find file:app/db.py")
	}
	if resDepth1.TotalAffected != 1 || resDepth1.DirectCount != 1 || resDepth1.IndirectCount != 0 {
		t.Fatalf("depth=1 expected 1 direct dependent, got %+v", resDepth1)
	}
	if resDepth1.AffectedFiles[0].Path != "app/service.py" || !resDepth1.AffectedFiles[0].Direct || resDepth1.AffectedFiles[0].Depth != 1 {
		t.Fatalf("unexpected depth=1 file: %+v", resDepth1.AffectedFiles[0])
	}

	resDepth2, ok := AnalyzeImpact(files, edges, "app/db.py", 2)
	if !ok {
		t.Fatal("expected AnalyzeImpact to find app/db.py")
	}
	if resDepth2.TotalAffected != 2 || resDepth2.DirectCount != 1 || resDepth2.IndirectCount != 1 {
		t.Fatalf("depth=2 expected 2 affected (1 direct, 1 indirect), got %+v", resDepth2)
	}
	if resDepth2.AffectedFiles[1].Path != "app/api.py" || resDepth2.AffectedFiles[1].Depth != 2 || resDepth2.AffectedFiles[1].ViaPath != "app/service.py" {
		t.Fatalf("unexpected depth=2 file: %+v", resDepth2.AffectedFiles[1])
	}

	resDepth3, _ := AnalyzeImpact(files, edges, "app/db.py", 3)
	if resDepth3.TotalAffected != 3 || resDepth3.DirectCount != 1 || resDepth3.IndirectCount != 2 {
		t.Fatalf("depth=3 expected 3 affected, got %+v", resDepth3)
	}
	for _, af := range resDepth3.AffectedFiles {
		if af.Path == "app/config.py" || af.Path == "app/driver.py" {
			t.Fatalf("downstream dependency %s must NOT be included in reverse impact of app/db.py", af.Path)
		}
	}
}

// 2. 循環依存 & 最短距離保証
func TestExploration_CyclicDependencies(t *testing.T) {
	// Cycle: a.py <-> b.py -> c.py -> a.py, plus d.py -> a.py
	rawFiles := []domain.SourceFile{
		{Path: "pkg/a.py", FunctionsCount: 2},
		{Path: "pkg/b.py", FunctionsCount: 2},
		{Path: "pkg/c.py", FunctionsCount: 2},
		{Path: "pkg/d.py", FunctionsCount: 1},
	}
	rawEdges := []Edge{
		{SourcePath: "pkg/a.py", TargetPath: "pkg/b.py"},
		{SourcePath: "pkg/b.py", TargetPath: "pkg/a.py"},
		{SourcePath: "pkg/b.py", TargetPath: "pkg/c.py"},
		{SourcePath: "pkg/c.py", TargetPath: "pkg/a.py"},
		{SourcePath: "pkg/d.py", TargetPath: "pkg/a.py"},
		{SourcePath: "pkg/a.py", TargetPath: "pkg/a.py"}, // self-loop must be ignored
	}
	files, edges := buildFilesAndEdges(rawFiles, rawEdges)

	// Impact on pkg/a.py with high maxDepth must terminate without infinite loop
	res, ok := AnalyzeImpact(files, edges, "pkg/a.py", 10)
	if !ok {
		t.Fatal("AnalyzeImpact failed on cyclic graph")
	}
	// b.py, c.py, d.py all directly import a.py (depth 1)
	if res.TotalAffected != 3 || res.DirectCount != 3 || res.IndirectCount != 0 {
		t.Fatalf("expected 3 direct dependents in cycle, got %+v", res)
	}

	// Guided Tour on cyclic graph without explicit entrypoint must still produce all 4 files deterministically
	tour := BuildGuidedTour(files, edges, 10)
	if len(tour) != 4 {
		t.Fatalf("expected 4 tour steps for 4-file cyclic repo, got %d", len(tour))
	}
}

// 3. 孤立ノード (Isolated Nodes)
func TestExploration_IsolatedNodes(t *testing.T) {
	rawFiles := []domain.SourceFile{
		{Path: "scripts/isolated_a.py", FunctionsCount: 1},
		{Path: "scripts/isolated_b.py", ClassesCount: 2},
		{Path: "scripts/isolated_c.py", FunctionsCount: 5},
		{Path: "scripts/isolated_d.py"},
		{Path: "scripts/isolated_e.py"},
		{Path: "scripts/isolated_f.py"},
	}
	files, edges := buildFilesAndEdges(rawFiles, nil)

	// Impact on isolated node should return 0 affected files
	impact, ok := AnalyzeImpact(files, edges, "scripts/isolated_a.py", 3)
	if !ok {
		t.Fatal("expected isolated_a.py to exist")
	}
	if impact.TotalAffected != 0 || len(impact.AffectedFiles) != 0 {
		t.Fatalf("expected 0 affected files for isolated node, got %+v", impact)
	}

	// Guided Tour should still select 5 deterministic steps ordered by symbols/path
	tour := BuildGuidedTour(files, edges, 10)
	if len(tour) < 5 {
		t.Fatalf("expected at least 5 tour steps for 6 isolated files, got %d", len(tour))
	}
	if tour[0].Path != "scripts/isolated_c.py" {
		t.Fatalf("expected isolated_c.py (5 functions) first among isolated files, got %s", tour[0].Path)
	}
}

// 4. 複数のエントリーポイントとGuided Tourの役割順序
func TestBuildGuidedTour_MultipleEntrypointsAndRoles(t *testing.T) {
	rawFiles := []domain.SourceFile{
		{Path: "src/app/main.py", FunctionsCount: 3},
		{Path: "src/app/cli.py", FunctionsCount: 2},
		{Path: "src/app/router.py", FunctionsCount: 6},
		{Path: "src/app/services/order.py", ClassesCount: 2, FunctionsCount: 4},
		{Path: "src/app/services/payment.py", ClassesCount: 1, FunctionsCount: 3},
		{Path: "src/app/models/entities.py", ClassesCount: 5},
		{Path: "src/app/common/config.py", ClassesCount: 1},
		{Path: "src/app/common/errors.py", ClassesCount: 4},
		{Path: "tests/test_main.py", FunctionsCount: 10},
	}
	rawEdges := []Edge{
		{SourcePath: "src/app/main.py", TargetPath: "src/app/router.py"},
		{SourcePath: "src/app/main.py", TargetPath: "src/app/common/config.py"},
		{SourcePath: "src/app/cli.py", TargetPath: "src/app/services/order.py"},
		{SourcePath: "src/app/cli.py", TargetPath: "src/app/common/config.py"},
		{SourcePath: "src/app/router.py", TargetPath: "src/app/services/order.py"},
		{SourcePath: "src/app/router.py", TargetPath: "src/app/services/payment.py"},
		{SourcePath: "src/app/services/order.py", TargetPath: "src/app/models/entities.py"},
		{SourcePath: "src/app/services/order.py", TargetPath: "src/app/common/errors.py"},
		{SourcePath: "src/app/services/payment.py", TargetPath: "src/app/models/entities.py"},
		{SourcePath: "src/app/services/payment.py", TargetPath: "src/app/common/errors.py"},
		{SourcePath: "src/app/services/payment.py", TargetPath: "src/app/common/config.py"},
		{SourcePath: "tests/test_main.py", TargetPath: "src/app/main.py"},
	}
	files, edges := buildFilesAndEdges(rawFiles, rawEdges)

	tour := BuildGuidedTour(files, edges, 10)
	if len(tour) < 5 || len(tour) > 10 {
		t.Fatalf("expected 5..10 tour steps, got %d", len(tour))
	}

	// Step 1 must be an entrypoint (not a leaf foundation like config.py, and not tests/test_main.py)
	if tour[0].Role != TourRoleEntrypoint {
		t.Fatalf("expected step 1 to have role entrypoint, got %+v", tour[0])
	}
	if tour[0].Path != "src/app/main.py" && tour[0].Path != "src/app/cli.py" {
		t.Fatalf("expected step 1 to be main.py or cli.py, got %s", tour[0].Path)
	}

	// Ensure tests/test_main.py is excluded when enough non-test files exist
	for _, s := range tour {
		if s.Path == "tests/test_main.py" {
			t.Fatalf("did not expect test file in guided tour when 8 production files exist: %+v", s)
		}
		if s.Reason == "" {
			t.Fatalf("expected non-empty reason for tour step %+v", s)
		}
	}
}

// 5. 同点スコアの安定ソート (Tie-Breaking Determinism under Random Shuffles)
func TestExploration_TieBreakStabilityUnderPermutation(t *testing.T) {
	// Construct 8 files with identical degrees and symbol counts in different directories
	rawFiles := []domain.SourceFile{
		{Path: "pkg_b/mod2.py", FunctionsCount: 2, ClassesCount: 1},
		{Path: "pkg_a/mod1.py", FunctionsCount: 2, ClassesCount: 1},
		{Path: "pkg_b/mod1.py", FunctionsCount: 2, ClassesCount: 1},
		{Path: "pkg_a/mod2.py", FunctionsCount: 2, ClassesCount: 1},
		{Path: "pkg_c/mod1.py", FunctionsCount: 2, ClassesCount: 1},
		{Path: "pkg_c/mod2.py", FunctionsCount: 2, ClassesCount: 1},
		{Path: "pkg_d/hub.py", FunctionsCount: 2, ClassesCount: 1},
		{Path: "pkg_d/base.py", FunctionsCount: 2, ClassesCount: 1},
	}
	// All mod* import pkg_d/hub.py, and pkg_d/hub.py imports pkg_d/base.py
	rawEdges := []Edge{
		{SourcePath: "pkg_a/mod1.py", TargetPath: "pkg_d/hub.py"},
		{SourcePath: "pkg_a/mod2.py", TargetPath: "pkg_d/hub.py"},
		{SourcePath: "pkg_b/mod1.py", TargetPath: "pkg_d/hub.py"},
		{SourcePath: "pkg_b/mod2.py", TargetPath: "pkg_d/hub.py"},
		{SourcePath: "pkg_c/mod1.py", TargetPath: "pkg_d/hub.py"},
		{SourcePath: "pkg_c/mod2.py", TargetPath: "pkg_d/hub.py"},
		{SourcePath: "pkg_d/hub.py", TargetPath: "pkg_d/base.py"},
	}

	baseFiles, baseEdges := buildFilesAndEdges(rawFiles, rawEdges)
	baseTour := BuildGuidedTour(baseFiles, baseEdges, 8)
	baseImpact, _ := AnalyzeImpact(baseFiles, baseEdges, "pkg_d/base.py", 5)
	baseArch := BuildArchitectureGraph(baseFiles, baseEdges)

	rng := rand.New(rand.NewSource(42))
	for iter := 0; iter < 20; iter++ {
		shuffledFiles := make([]domain.SourceFile, len(rawFiles))
		copy(shuffledFiles, rawFiles)
		rng.Shuffle(len(shuffledFiles), func(i, j int) {
			shuffledFiles[i], shuffledFiles[j] = shuffledFiles[j], shuffledFiles[i]
		})

		shuffledEdges := make([]Edge, len(rawEdges))
		copy(shuffledEdges, rawEdges)
		rng.Shuffle(len(shuffledEdges), func(i, j int) {
			shuffledEdges[i], shuffledEdges[j] = shuffledEdges[j], shuffledEdges[i]
		})

		f, e := buildFilesAndEdges(shuffledFiles, shuffledEdges)
		tour := BuildGuidedTour(f, e, 8)
		impact, _ := AnalyzeImpact(f, e, "pkg_d/base.py", 5)
		arch := BuildArchitectureGraph(f, e)

		if !reflect.DeepEqual(baseTour, tour) {
			t.Fatalf("iteration %d: BuildGuidedTour was not deterministic across shuffles", iter)
		}
		if !reflect.DeepEqual(baseImpact, impact) {
			t.Fatalf("iteration %d: AnalyzeImpact was not deterministic across shuffles", iter)
		}
		if !reflect.DeepEqual(baseArch, arch) {
			t.Fatalf("iteration %d: BuildArchitectureGraph was not deterministic across shuffles", iter)
		}
	}
}

// 6. 大規模グラフ (600 files across 15 directories, ~1700 edges)
func TestExploration_LargeGraphScalabilityAndArchitectureView(t *testing.T) {
	const numDirs = 15
	const filesPerDir = 40 // 600 files total
	var rawFiles []domain.SourceFile
	var rawEdges []Edge

	for d := 0; d < numDirs; d++ {
		dirName := fmt.Sprintf("src/module_%02d", d)
		for f := 0; f < filesPerDir; f++ {
			fileName := fmt.Sprintf("%s/file_%02d.py", dirName, f)
			if d == 0 && f == 0 {
				fileName = fmt.Sprintf("%s/main.py", dirName)
			}
			rawFiles = append(rawFiles, domain.SourceFile{
				Path:           fileName,
				FunctionsCount: (f % 5) + 1,
				ClassesCount:   f % 3,
			})
		}
	}

	// Connect files within each directory and across adjacent directories
	for d := 0; d < numDirs; d++ {
		dirName := fmt.Sprintf("src/module_%02d", d)
		for f := 0; f < filesPerDir-1; f++ {
			src := fmt.Sprintf("%s/file_%02d.py", dirName, f)
			if d == 0 && f == 0 {
				src = fmt.Sprintf("%s/main.py", dirName)
			}
			dst := fmt.Sprintf("%s/file_%02d.py", dirName, f+1)
			rawEdges = append(rawEdges, Edge{SourcePath: src, TargetPath: dst})

			if d+1 < numDirs {
				crossDst := fmt.Sprintf("src/module_%02d/file_%02d.py", d+1, f)
				rawEdges = append(rawEdges, Edge{SourcePath: src, TargetPath: crossDst})
			}
		}
	}

	files, edges := buildFilesAndEdges(rawFiles, rawEdges)
	if len(files) != 600 {
		t.Fatalf("expected 600 files, got %d", len(files))
	}

	// Guided Tour caps at 10 steps and starts with src/module_00/main.py
	tour := BuildGuidedTour(files, edges, 10)
	if len(tour) != 10 {
		t.Fatalf("expected 10 guided tour steps on 600-file graph, got %d", len(tour))
	}
	if tour[0].Path != "src/module_00/main.py" {
		t.Fatalf("expected step 1 to be src/module_00/main.py, got %s", tour[0].Path)
	}

	// Architecture View compresses 600 files into 15 directory nodes and 14 inter-directory edges
	arch := BuildArchitectureGraph(files, edges)
	if len(arch.Nodes) != numDirs {
		t.Fatalf("expected %d architecture nodes, got %d", numDirs, len(arch.Nodes))
	}
	if len(arch.Edges) != numDirs-1 {
		t.Fatalf("expected %d inter-directory edges, got %d", numDirs-1, len(arch.Edges))
	}
	for _, n := range arch.Nodes {
		if n.FileCount != filesPerDir {
			t.Fatalf("expected %d files in %s, got %d", filesPerDir, n.Directory, n.FileCount)
		}
		if n.InternalEdgeCount != filesPerDir-1 {
			t.Fatalf("expected %d internal edges in %s, got %d", filesPerDir-1, n.Directory, n.InternalEdgeCount)
		}
	}

	// Impact analysis on a deep node at maxDepth=2
	impact, ok := AnalyzeImpact(files, edges, "src/module_03/file_05.py", 2)
	if !ok {
		t.Fatal("AnalyzeImpact failed on large graph target")
	}
	if impact.TotalAffected == 0 {
		t.Fatal("expected non-zero affected files in large connected graph")
	}
	for _, af := range impact.AffectedFiles {
		if af.Depth < 1 || af.Depth > 2 {
			t.Fatalf("expected depth in [1,2], got %d for %s", af.Depth, af.Path)
		}
	}
}
