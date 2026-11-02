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
}

export interface GraphEdgeData {
  source: string;
  target: string;
  kind: string;
}

export interface GraphResponse {
  demo?: boolean;
  analysis_id: string;
  repository: RepositorySummary;
  commit_sha: string;
  summary: GraphSummary;
  nodes: GraphNodeData[];
  edges: GraphEdgeData[];
  recommended_file_ids: string[];
  guided_tour?: GuidedTourStep[];
  architecture?: ArchitectureGraphData;
  warnings: string[];
}

export interface APIErrorPayload {
  error?: {
    code?: string;
    message?: string;
  };
}
