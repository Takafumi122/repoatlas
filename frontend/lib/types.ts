export type AnalysisStatus = "queued" | "running" | "completed" | "failed";

export type AnalysisStage =
  | "queued"
  | "fetching"
  | "indexing"
  | "parsing"
  | "resolving"
  | "persisting"
  | "completed"
  | "failed";

export type GraphViewMode = "file" | "architecture";

export type TourRole =
  | "entrypoint"
  | "orchestrator"
  | "core_module"
  | "foundation"
  | "module";

export interface GuidedTourStep {
  step: number;
  node_id: string;
  path: string;
  role: TourRole;
  role_label: string;
  score: number;
  reason: string;
  in_degree: number;
  out_degree: number;
  functions: number;
  classes: number;
  is_entrypoint: boolean;
}

export interface ImpactedFileData {
  node_id: string;
  path: string;
  depth: number;
  direct: boolean;
  via_node_id: string;
  via_path: string;
  in_degree: number;
  out_degree: number;
  importance_score: number;
}

export interface ImpactAnalysisResult {
  target_node_id: string;
  target_path: string;
  max_depth: number;
  total_files: number;
  total_affected: number;
  direct_count: number;
  indirect_count: number;
  affected_files: ImpactedFileData[];
  affected_edges: GraphEdgeData[];
}

export interface ArchitectureFileSummary {
  node_id: string;
  path: string;
  functions: number;
  classes: number;
  in_degree: number;
  out_degree: number;
  is_entrypoint: boolean;
  importance_score: number;
}

export interface ArchitectureNodeData {
  id: string;
  directory: string;
  file_count: number;
  functions_count: number;
  classes_count: number;
  internal_edge_count: number;
  in_degree: number;
  out_degree: number;
  incoming_weight: number;
  outgoing_weight: number;
  entrypoint_count: number;
  files: ArchitectureFileSummary[];
}

export interface ArchitectureEdgeData {
  id: string;
  source: string;
  target: string;
  source_directory: string;
  target_directory: string;
  weight: number;
  sample_pairs: string[];
}

export interface ArchitectureGraphData {
  nodes: ArchitectureNodeData[];
  edges: ArchitectureEdgeData[];
}

export interface RepositorySummary {
  owner: string;
  name: string;
  html_url: string;
}

export interface CreateAnalysisResponse {
  analysis_id: string;
  status: AnalysisStatus;
  cached: boolean;
}

export interface AnalysisErrorField {
  code: string | null;
  message: string | null;
}

export interface AnalysisStatusResponse {
  id: string;
  status: AnalysisStatus;
  stage: AnalysisStage;
  repository: RepositorySummary;
  commit_sha: string;
  error: AnalysisErrorField;
  created_at: string;
  completed_at: string | null;
}

export interface GraphSummary {
  files: number;
  internal_edges: number;
  external_imports: number;
  unresolved_imports: number;
}

export interface PackageFileInfo {
  path: string;
  functions: number;
  methods: number;
  types: number;
  build_tags?: string;
}

export interface GraphNodeData {
  id: string;
  path: string;
  functions: number;
  classes: number;
  in_degree: number;
  out_degree: number;
  is_entrypoint: boolean;
  importance_score: number;
  github_url: string;
  // Phase 6 Go extensions
  node_kind?: "file" | "package";
  language?: "python" | "go";
  module_path?: string;
  package_import_path?: string;
  directory_path?: string;
  package_name?: string;
  files_count?: number;
  methods_count?: number;
  types_count?: number;
  files?: PackageFileInfo[];
}

export interface GraphEdgeData {
  id?: string;
  source: string;
  target: string;
  kind?: string;
  evidence_count?: number;
  import_count?: number;
}

export type PRFileStatus = "added" | "modified" | "removed" | "renamed";

export type PRNodeVisualRole =
  | "changed"
  | "direct_impact"
  | "indirect_impact"
  | "unaffected"
  | "unanalyzed";

export interface PullRequestFile {
  id?: number;
  pr_analysis_id?: string;
  path: string;
  previous_path?: string;
  status: PRFileStatus;
  additions: number;
  deletions: number;
  changes: number;
  is_python: boolean;
  is_analyzed: boolean;
  unanalyzed_reason?: string;
}

export interface PRImpactSummary {
  changed_files_count: number;
  direct_impact_count: number;
  indirect_impact_count: number;
  total_affected_count: number;
  unanalyzed_files_count: number;
  analysis_completeness: "complete" | "partial";
}

export interface PRAffectedFile {
  node_id: string;
  path: string;
  depth: number;
  impact_type: "direct" | "indirect";
  source_changed_files: string[];
  representative_dependency_path: string[];
  in_degree: number;
  out_degree: number;
  importance_score: number;
  visual_role: PRNodeVisualRole;
}

export interface CreatePRAnalysisResponse {
  pr_analysis_id: string;
  status: AnalysisStatus;
  cached: boolean;
}

export interface PRAnalysisStatusResponse {
  id: string;
  status: AnalysisStatus;
  stage: AnalysisStage;
  repository: RepositorySummary;
  pull_number: number;
  title: string;
  state: string;
  base_sha: string;
  head_sha: string;
  base_ref: string;
  head_ref: string;
  head_analysis_id?: string;
  error: AnalysisErrorField;
  summary: PRImpactSummary;
  created_at: string;
  completed_at?: string | null;
}

export interface PRImpactResponse {
  pr_analysis_id: string;
  head_analysis_id: string;
  repository: RepositorySummary;
  pull_number: number;
  title: string;
  base_sha: string;
  head_sha: string;
  summary: PRImpactSummary;
  changed_files: PullRequestFile[];
  affected_files: PRAffectedFile[];
  affected_edges: GraphEdgeData[];
  unanalyzed_files: PullRequestFile[];
}

export interface ImportEvidence {
  id: number;
  analysis_id: string;
  source_path: string;
  target_path?: string | null;
  import_statement: string;
  module_name: string;
  start_line: number;
  start_column: number;
  end_line: number;
  end_column: number;
  import_kind: "absolute" | "relative";
  resolution_status: "internal" | "external" | "unresolved";
  resolution_reason: string;
  created_at: string;
}

export interface ImportReference {
  id?: number;
  analysis_id?: string;
  source_path: string;
  target_path?: string;
  import_text: string;
  module_name: string;
  line_number: number;
  classification: "internal" | "external" | "unresolved";
  resolution_note?: string;
  source_file?: string;
  source_package?: string;
  target_package?: string;
  alias?: string;
  column_number?: number;
  build_constraints?: string;
}

export interface EvidenceResponse {
  analysis_id: string;
  count?: number;
  evidences?: ImportEvidence[];
  imports?: ImportReference[];
}

export interface GraphResponse {
  demo?: boolean;
  analysis_id: string;
  repository: RepositorySummary;
  commit_sha: string;
  language?: "python" | "go" | string;
  graph_granularity?: "file" | "package" | string;
  summary: GraphSummary;
  nodes: GraphNodeData[];
  edges: GraphEdgeData[];
  recommended_file_ids: string[];
  guided_tour?: GuidedTourStep[];
  architecture?: ArchitectureGraphData;
  warnings: string[];
}

// Phase 6 Architecture Diff Types

export type DiffStatus = "queued" | "running" | "completed" | "failed";

export type NodeChangeType = "added" | "removed" | "modified" | "unchanged";

export type EdgeChangeType = "added" | "removed" | "unchanged";

export interface MetricValue {
  base: number;
  head: number;
  diff: number;
}

export interface ArchitectureMetricsDiff {
  total_nodes: MetricValue;
  total_edges: MetricValue;
  added_nodes: number;
  removed_nodes: number;
  modified_nodes: number;
  added_edges: number;
  removed_edges: number;
  cyclic_scc_count: MetricValue;
  cyclic_nodes_count: MetricValue;
  avg_in_degree: MetricValue;
  avg_out_degree: MetricValue;
  max_in_degree: MetricValue;
  max_out_degree: MetricValue;
}

export interface DiffSummary {
  base_commit_sha: string;
  head_commit_sha: string;
  language: string;
  granularity: string;
  total_base_nodes: number;
  total_head_nodes: number;
  total_base_edges: number;
  total_head_edges: number;
  added_nodes_count: number;
  removed_nodes_count: number;
  modified_nodes_count: number;
  added_edges_count: number;
  removed_edges_count: number;
  base_cyclic_sccs: number;
  head_cyclic_sccs: number;
  metrics: ArchitectureMetricsDiff;
}

export interface NodeMetricsSnapshot {
  functions: number;
  classes: number;
  in_degree: number;
  out_degree: number;
  importance: number;
  files_count?: number;
  methods_count?: number;
  types_count?: number;
}

export interface DiffNodeData {
  id: string;
  node_kind: string;
  language: string;
  path: string;
  change_type: NodeChangeType;
  base_metrics?: NodeMetricsSnapshot;
  head_metrics?: NodeMetricsSnapshot;
  change_details: string[];
  github_url: string;
  // UI helper state
  isSelected?: boolean;
  isDimmed?: boolean;
}

export interface DiffEdgeData {
  id: string;
  source: string;
  target: string;
  change_type: EdgeChangeType;
  is_cyclic: boolean;
  cycle_change_type?: "added" | "resolved" | "persistent";
}

export interface CycleChanges {
  added_cycles: string[][];
  resolved_cycles: string[][];
  persistent_cycles: string[][];
}

export interface CreateArchitectureDiffRequest {
  repository_url: string;
  base_sha: string;
  head_sha: string;
}

export interface CreateArchitectureDiffResponse {
  diff_id: string;
  status: DiffStatus;
  cached: boolean;
}

export interface ArchitectureDiffStatusResponse {
  id: string;
  status: DiffStatus;
  stage: string;
  repository: RepositorySummary;
  base_sha: string;
  head_sha: string;
  error: AnalysisErrorField;
  summary?: DiffSummary;
  created_at: string;
  completed_at?: string | null;
}

export interface ArchitectureDiffGraphResponse {
  diff_id: string;
  repository: RepositorySummary;
  base_sha: string;
  head_sha: string;
  language: string;
  granularity: string;
  status: DiffStatus;
  summary: DiffSummary;
  nodes: DiffNodeData[];
  edges: DiffEdgeData[];
  cycle_changes: CycleChanges;
  warnings: string[];
}

export interface APIErrorPayload {
  error?: {
    code?: string;
    message?: string;
  };
}
