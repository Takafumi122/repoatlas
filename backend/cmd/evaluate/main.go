package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/repoatlas/repoatlas/backend/internal/analyzer"
	"github.com/repoatlas/repoatlas/backend/internal/domain"
)

type ExpectedEdge struct {
	Source string `json:"source"`
	Target string `json:"target"`
}

type DatasetExpected struct {
	Name              string         `json:"name"`
	Repo              string         `json:"repo,omitempty"`
	CommitSHA         string         `json:"commit_sha,omitempty"`
	Description       string         `json:"description"`
	InternalEdges     []ExpectedEdge `json:"internal_edges"`
	ExternalImports   []string       `json:"external_imports,omitempty"`
	UnresolvedImports []string       `json:"unresolved_imports,omitempty"`
}

type ProjectMetrics struct {
	Name             string         `json:"name"`
	DatasetType      string         `json:"dataset_type"`
	FilesCount       int            `json:"files_count"`
	TotalImports     int            `json:"total_imports"`
	GroundTruthEdges int            `json:"ground_truth_edges"`
	PredictedEdges   int            `json:"predicted_edges"`
	TruePositives    int            `json:"true_positives"`
	FalsePositives   int            `json:"false_positives"`
	FalseNegatives   int            `json:"false_negatives"`
	Precision        float64        `json:"precision"`
	Recall           float64        `json:"recall"`
	F1Score          float64        `json:"f1_score"`
	UnresolvedCount  int            `json:"unresolved_count"`
	UnresolvedRate   float64        `json:"unresolved_rate"`
	LatencyMs        float64        `json:"latency_ms"`
	HeapAllocBytes   uint64         `json:"heap_alloc_bytes"`
	TotalAllocBytes  uint64         `json:"total_alloc_bytes"`
	SysBytes         uint64         `json:"sys_bytes"`
	FPEdges          []ExpectedEdge `json:"false_positive_edges,omitempty"`
	FNEdges          []ExpectedEdge `json:"false_negative_edges,omitempty"`
	UnresolvedList   []string       `json:"unresolved_imports,omitempty"`
}

type EvaluationReport struct {
	GeneratedAt       time.Time        `json:"generated_at"`
	GoVersion         string           `json:"go_version"`
	NumCPU            int              `json:"num_cpu"`
	OverallPrecision  float64          `json:"overall_precision"`
	OverallRecall     float64          `json:"overall_recall"`
	OverallF1         float64          `json:"overall_f1"`
	AvgUnresolvedRate float64          `json:"avg_unresolved_rate"`
	TotalLatencyMs    float64          `json:"total_latency_ms"`
	PeakHeapAllocBytes uint64          `json:"peak_heap_alloc_bytes"`
	Projects          []ProjectMetrics `json:"projects"`
}

func main() {
	var (
		datasetFlag = flag.String("dataset", "all", "Dataset to evaluate: all, synthetic, real_repositories")
		repoFlag    = flag.String("repo", "", "Filter to evaluate single project or repository name")
		outDirFlag  = flag.String("out-dir", "evaluation/reports", "Directory to write evaluation reports")
		formatFlag  = flag.String("format", "all", "Output format: all, json, markdown")
	)
	flag.Parse()

	pyAnalyzer := analyzer.NewPythonAnalyzer()

	// Locate datasets root
	datasetsRoot := "evaluation/datasets"
	if _, err := os.Stat(datasetsRoot); err != nil {
		// Try relative to project root
		datasetsRoot = "../evaluation/datasets"
		if _, err := os.Stat(datasetsRoot); err != nil {
			log.Fatalf("Could not locate evaluation/datasets directory: %v", err)
		}
	}

	var targets []struct {
		datasetType string
		dirPath     string
	}

	if *datasetFlag == "all" || *datasetFlag == "synthetic" {
		synDir := filepath.Join(datasetsRoot, "synthetic")
		entries, err := os.ReadDir(synDir)
		if err == nil {
			for _, e := range entries {
				if e.IsDir() {
					targets = append(targets, struct {
						datasetType string
						dirPath     string
					}{"synthetic", filepath.Join(synDir, e.Name())})
				}
			}
		}
	}

	if *datasetFlag == "all" || *datasetFlag == "real_repositories" {
		realDir := filepath.Join(datasetsRoot, "real_repositories")
		entries, err := os.ReadDir(realDir)
		if err == nil {
			for _, e := range entries {
				if e.IsDir() {
					targets = append(targets, struct {
						datasetType string
						dirPath     string
					}{"real_repositories", filepath.Join(realDir, e.Name())})
				}
			}
		}
	}

	sort.Slice(targets, func(i, j int) bool {
		return targets[i].dirPath < targets[j].dirPath
	})

	var results []ProjectMetrics
	var peakHeap uint64
	var totalLatency float64

	for _, tgt := range targets {
		projName := filepath.Base(tgt.dirPath)
		if *repoFlag != "" && !strings.Contains(projName, *repoFlag) {
			continue
		}

		expectedPath := filepath.Join(tgt.dirPath, "expected.json")
		expBytes, err := os.ReadFile(expectedPath)
		if err != nil {
			log.Printf("Warning: skipping %s, expected.json not found: %v", tgt.dirPath, err)
			continue
		}

		var expected DatasetExpected
		if err := json.Unmarshal(expBytes, &expected); err != nil {
			log.Printf("Warning: skipping %s, invalid expected.json: %v", tgt.dirPath, err)
			continue
		}

		// Read all python files
		var sourceFiles []analyzer.SourceFile
		err = filepath.Walk(tgt.dirPath, func(p string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return err
			}
			if strings.HasSuffix(strings.ToLower(info.Name()), ".py") {
				rel, rErr := filepath.Rel(tgt.dirPath, p)
				if rErr != nil {
					return rErr
				}
				content, cErr := os.ReadFile(p)
				if cErr != nil {
					return cErr
				}
				sourceFiles = append(sourceFiles, analyzer.SourceFile{
					Path:    filepath.ToSlash(rel),
					Content: content,
				})
			}
			return nil
		})
		if err != nil {
			log.Printf("Error walking %s: %v", tgt.dirPath, err)
			continue
		}

		// Measure MemStats and Latency
		runtime.GC()
		var memBefore runtime.MemStats
		runtime.ReadMemStats(&memBefore)

		start := time.Now()
		output, err := pyAnalyzer.Analyze(context.Background(), sourceFiles, nil)
		duration := time.Since(start)

		var memAfter runtime.MemStats
		runtime.ReadMemStats(&memAfter)

		if err != nil {
			log.Printf("Error analyzing %s: %v", projName, err)
			continue
		}

		heapAlloc := memAfter.Alloc
		if heapAlloc > peakHeap {
			peakHeap = heapAlloc
		}
		latencyMs := float64(duration.Microseconds()) / 1000.0
		totalLatency += latencyMs

		// Evaluate Graph internal edges against Ground Truth
		expectedEdgeSet := make(map[[2]string]bool, len(expected.InternalEdges))
		for _, ee := range expected.InternalEdges {
			expectedEdgeSet[[2]string{ee.Source, ee.Target}] = true
		}

		predictedEdgeSet := make(map[[2]string]bool, len(output.Edges))
		for _, pe := range output.Edges {
			predictedEdgeSet[[2]string{pe.SourcePath, pe.TargetPath}] = true
		}

		var tp, fp, fn int
		var fpList, fnList []ExpectedEdge

		for pe := range predictedEdgeSet {
			if expectedEdgeSet[pe] {
				tp++
			} else {
				fp++
				fpList = append(fpList, ExpectedEdge{Source: pe[0], Target: pe[1]})
			}
		}

		for ee := range expectedEdgeSet {
			if !predictedEdgeSet[ee] {
				fn++
				fnList = append(fnList, ExpectedEdge{Source: ee[0], Target: ee[1]})
			}
		}

		// Safe metrics calculation with explicit zero handling
		var precision, recall, f1 float64
		if tp+fp == 0 {
			if len(expected.InternalEdges) == 0 {
				precision = 1.0
			} else {
				precision = 0.0
			}
		} else {
			precision = float64(tp) / float64(tp+fp)
		}

		if tp+fn == 0 {
			if len(predictedEdgeSet) == 0 {
				recall = 1.0
			} else {
				recall = 0.0
			}
		} else {
			recall = float64(tp) / float64(tp+fn)
		}

		if precision+recall > 0 {
			f1 = 2 * (precision * recall) / (precision + recall)
		} else {
			f1 = 0.0
		}

		var unresolvedList []string
		for _, imp := range output.Imports {
			if imp.Classification == domain.ImportUnresolved {
				unresolvedList = append(unresolvedList, fmt.Sprintf("%s:%d (%s) - %s", imp.SourcePath, imp.LineNumber, imp.ModuleName, imp.ResolutionReason))
			}
		}

		unresolvedCount := output.UnresolvedImportsCount
		totalImports := output.InternalEdgesCount + output.ExternalImportsCount + unresolvedCount
		var unresolvedRate float64
		if totalImports > 0 {
			unresolvedRate = float64(unresolvedCount) / float64(totalImports)
		}

		sort.Slice(fpList, func(i, j int) bool {
			return fpList[i].Source < fpList[j].Source
		})
		sort.Slice(fnList, func(i, j int) bool {
			return fnList[i].Source < fnList[j].Source
		})

		metrics := ProjectMetrics{
			Name:             projName,
			DatasetType:      tgt.datasetType,
			FilesCount:       len(sourceFiles),
			TotalImports:     totalImports,
			GroundTruthEdges: len(expected.InternalEdges),
			PredictedEdges:   len(output.Edges),
			TruePositives:    tp,
			FalsePositives:   fp,
			FalseNegatives:   fn,
			Precision:        precision,
			Recall:           recall,
			F1Score:          f1,
			UnresolvedCount:  unresolvedCount,
			UnresolvedRate:   unresolvedRate,
			LatencyMs:        latencyMs,
			HeapAllocBytes:   heapAlloc,
			TotalAllocBytes:  memAfter.TotalAlloc - memBefore.TotalAlloc,
			SysBytes:         memAfter.Sys,
			FPEdges:          fpList,
			FNEdges:          fnList,
			UnresolvedList:   unresolvedList,
		}
		results = append(results, metrics)
	}

	// Overall aggregation
	var sumP, sumR, sumF1, sumUnres float64
	for _, r := range results {
		sumP += r.Precision
		sumR += r.Recall
		sumF1 += r.F1Score
		sumUnres += r.UnresolvedRate
	}

	count := float64(len(results))
	report := EvaluationReport{
		GeneratedAt:       time.Now().UTC(),
		GoVersion:         runtime.Version(),
		NumCPU:            runtime.NumCPU(),
		OverallPrecision:  safeDiv(sumP, count),
		OverallRecall:     safeDiv(sumR, count),
		OverallF1:         safeDiv(sumF1, count),
		AvgUnresolvedRate: safeDiv(sumUnres, count),
		TotalLatencyMs:    totalLatency,
		PeakHeapAllocBytes: peakHeap,
		Projects:          results,
	}

	// Output results
	outDir := *outDirFlag
	if outDir == "evaluation/reports" {
		if _, err := os.Stat("evaluation"); err != nil {
			if _, pErr := os.Stat("../evaluation"); pErr == nil {
				outDir = "../evaluation/reports"
			}
		}
	}
	_ = os.MkdirAll(outDir, 0755)

	if *formatFlag == "all" || *formatFlag == "json" {
		jsonPath := filepath.Join(outDir, "evaluation_report.json")
		b, _ := json.MarshalIndent(report, "", "  ")
		if err := os.WriteFile(jsonPath, b, 0644); err != nil {
			log.Fatalf("Write JSON report failed: %v", err)
		}
		fmt.Printf("Saved JSON evaluation report: %s\n", jsonPath)
	}

	if *formatFlag == "all" || *formatFlag == "markdown" {
		mdPath := filepath.Join(outDir, "evaluation_report.md")
		mdContent := generateMarkdownReport(report)
		if err := os.WriteFile(mdPath, []byte(mdContent), 0644); err != nil {
			log.Fatalf("Write Markdown report failed: %v", err)
		}
		fmt.Printf("Saved Markdown evaluation report: %s\n", mdPath)
	}

	// Print quick terminal summary
	fmt.Println("\n==================================================")
	fmt.Println("RepoAtlas Static Analysis Evaluation Summary")
	fmt.Println("==================================================")
	fmt.Printf("Evaluated Projects : %d\n", len(report.Projects))
	fmt.Printf("Overall Precision  : %.2f%%\n", report.OverallPrecision*100)
	fmt.Printf("Overall Recall     : %.2f%%\n", report.OverallRecall*100)
	fmt.Printf("Overall F1-Score   : %.2f%%\n", report.OverallF1*100)
	fmt.Printf("Avg Unresolved Rate: %.2f%%\n", report.AvgUnresolvedRate*100)
	fmt.Printf("Total Latency      : %.2f ms\n", report.TotalLatencyMs)
	fmt.Printf("Peak Heap Alloc    : %.2f MB\n", float64(report.PeakHeapAllocBytes)/(1024*1024))
	fmt.Println("==================================================")
}

func safeDiv(a, b float64) float64 {
	if b == 0 {
		return 0
	}
	return a / b
}

func generateMarkdownReport(r EvaluationReport) string {
	var sb strings.Builder
	sb.WriteString("# RepoAtlas Static Analysis Evaluation Report\n\n")
	sb.WriteString(fmt.Sprintf("- **Generated At**: `%s`\n", r.GeneratedAt.Format(time.RFC3339)))
	sb.WriteString(fmt.Sprintf("- **Go Runtime**: `%s` (%d CPUs)\n", r.GoVersion, r.NumCPU))
	sb.WriteString(fmt.Sprintf("- **Total Projects Evaluated**: `%d`\n\n", len(r.Projects)))

	sb.WriteString("## Overall Benchmark Summary\n\n")
	sb.WriteString("| Metric | Value |\n")
	sb.WriteString("| :--- | :--- |\n")
	sb.WriteString(fmt.Sprintf("| **Overall Precision** | **%.2f%%** |\n", r.OverallPrecision*100))
	sb.WriteString(fmt.Sprintf("| **Overall Recall** | **%.2f%%** |\n", r.OverallRecall*100))
	sb.WriteString(fmt.Sprintf("| **Overall F1-Score** | **%.2f%%** |\n", r.OverallF1*100))
	sb.WriteString(fmt.Sprintf("| **Average Unresolved Rate** | **%.2f%%** |\n", r.AvgUnresolvedRate*100))
	sb.WriteString(fmt.Sprintf("| **Total Execution Time** | **%.2f ms** |\n", r.TotalLatencyMs))
	sb.WriteString(fmt.Sprintf("| **Peak Heap Allocated** | **%.2f MB** |\n\n", float64(r.PeakHeapAllocBytes)/(1024*1024)))

	sb.WriteString("## Project Breakdown\n\n")
	sb.WriteString("| Project | Dataset | Files | Imports | Precision | Recall | F1 | Unresolved | Latency (ms) | Peak Heap (KB) |\n")
	sb.WriteString("| :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- |\n")

	for _, p := range r.Projects {
		sb.WriteString(fmt.Sprintf("| `%s` | `%s` | %d | %d | %.1f%% | %.1f%% | %.1f%% | %.1f%% | %.2f | %.1f |\n",
			p.Name,
			p.DatasetType,
			p.FilesCount,
			p.TotalImports,
			p.Precision*100,
			p.Recall*100,
			p.F1Score*100,
			p.UnresolvedRate*100,
			p.LatencyMs,
			float64(p.HeapAllocBytes)/1024,
		))
	}

	sb.WriteString("\n## Dataset Scope & Limitations: Full vs Partial Repository Distinction\n\n")
	sb.WriteString("- **Synthetic Datasets (`01_stdlib` ~ `14_unresolved`)**: 完全に制御された構文網羅テストケース群。構文木抽出、相対import境界、エイリアス、循環依存、未解決エラーの検知精度を独立検証。\n")
	sb.WriteString("- **Real OSS Repositories (`click`, `itsdangerous`, `requests`)**: 本データセットは実リポジトリの全ファイル（テストスイート・ドキュメント・設定等を含む全体）ではなく、**コアパッケージディレクトリ（`src/` 配下）のモジュール群を対象とした部分評価（Partial Core Package Evaluation）** です。\n")
	sb.WriteString("  - `pallets/itsdangerous`: 全15ファイル中、コアパッケージ `src/itsdangerous/` の8ファイル（22内部エッジ）を評価対象として抽出（テスト群7ファイルを除外）。\n")
	sb.WriteString("  - `psf/requests`: 全37ファイル中、コアパッケージ `src/requests/` の主要13ファイル（20内部エッジ）を評価対象として抽出。\n")
	sb.WriteString("  - `pallets/click`: 全22ファイル中、中核モジュール `src/click/` の8ファイル（13内部エッジ）を評価対象として抽出。\n\n")

	sb.WriteString("## Error Pattern Categorization\n\n")
	for _, p := range r.Projects {
		if len(p.FPEdges) > 0 || len(p.FNEdges) > 0 || len(p.UnresolvedList) > 0 {
			sb.WriteString(fmt.Sprintf("### `%s` (`%s`)\n\n", p.Name, p.DatasetType))
			if len(p.FPEdges) > 0 {
				sb.WriteString("**False Positives (Predicted but not in Ground Truth):**\n")
				for _, fp := range p.FPEdges {
					sb.WriteString(fmt.Sprintf("- `%s` -> `%s`\n", fp.Source, fp.Target))
				}
				sb.WriteString("\n")
			}
			if len(p.FNEdges) > 0 {
				sb.WriteString("**False Negatives (Ground Truth but not found):**\n")
				for _, fn := range p.FNEdges {
					sb.WriteString(fmt.Sprintf("- `%s` -> `%s`\n", fn.Source, fn.Target))
				}
				sb.WriteString("\n")
			}
			if len(p.UnresolvedList) > 0 {
				sb.WriteString("**Unresolved Imports:**\n")
				for _, u := range p.UnresolvedList {
					sb.WriteString(fmt.Sprintf("- `%s`\n", u))
				}
				sb.WriteString("\n")
			}
		}
	}

	return sb.String()
}
