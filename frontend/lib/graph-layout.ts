import dagre from "@dagrejs/dagre";
import type { Edge, Node } from "@xyflow/react";
import type {
  ArchitectureEdgeData,
  ArchitectureGraphData,
  ArchitectureNodeData,
  GraphEdgeData,
  GraphNodeData,
  GuidedTourStep,
  ImpactAnalysisResult,
  ImpactedFileData,
  TourRole,
} from "./types";

export const MAX_RENDERED_NODES = 300;
const NODE_WIDTH = 240;
const NODE_HEIGHT = 86;
const ARCH_NODE_WIDTH = 264;
const ARCH_NODE_HEIGHT = 112;

export interface FileNodeViewModel extends Record<string, unknown> {
  file: GraphNodeData;
  isRecommended: boolean;
  isSelected: boolean;
  isRelated: boolean;
  isDimmed: boolean;
  tourStep?: number;
  impactDepth?: number;
}

export interface DirectoryNodeViewModel extends Record<string, unknown> {
  dirNode: ArchitectureNodeData;
  isSelected: boolean;
  isRelated: boolean;
  isDimmed: boolean;
}

export interface FilteredGraphResult {
  visibleNodes: GraphNodeData[];
  visibleEdges: GraphEdgeData[];
  totalNodes: number;
  hiddenByLimitCount: number;
  directories: string[];
}

export function extractTopDirectory(filePath: string): string {
  const idx = filePath.indexOf("/");
  if (idx === -1) {
    return "(root)";
  }
  return filePath.slice(0, idx);
}

export function extractDirectoryPath(filePath: string): string {
  const idx = filePath.lastIndexOf("/");
  if (idx <= 0) {
    return "(root)";
  }
  return filePath.slice(0, idx);
}

const ENTRYPOINT_FILENAMES = new Set([
  "main.py",
  "app.py",
  "__main__.py",
  "cli.py",
  "server.py",
  "run.py",
  "wsgi.py",
  "asgi.py",
  "manage.py",
]);

function isTestOrAuxPath(filePath: string): boolean {
  const lower = filePath.toLowerCase();
  const segments = lower.split("/");
  for (const seg of segments) {
    if (
      seg === "tests" ||
      seg === "test" ||
      seg === "testing" ||
      seg === "docs" ||
      seg === "examples" ||
      seg === "benchmarks"
    ) {
      return true;
    }
  }
  const base = segments[segments.length - 1] || "";
  return (
    base.startsWith("test_") ||
    base.endsWith("_test.py") ||
    base === "conftest.py" ||
    base === "setup.py"
  );
}

/**
 * Generates or normalizes the Guided Code Tour (5–10 steps) deterministically.
 */
export function buildGuidedTour(
  nodes: GraphNodeData[],
  edges: GraphEdgeData[],
  providedTour?: GuidedTourStep[]
): GuidedTourStep[] {
  if (providedTour && providedTour.length > 0) {
    return providedTour;
  }
  if (nodes.length === 0) {
    return [];
  }

  const sortedNodes = [...nodes].sort((a, b) => a.path.localeCompare(b.path));
  const nodeById = new Map(sortedNodes.map((n) => [n.id, n]));
  const nonTestCount = sortedNodes.filter((n) => !isTestOrAuxPath(n.path)).length;

  let targetSize = sortedNodes.length;
  if (sortedNodes.length > 5) {
    const effective = nonTestCount >= 5 ? nonTestCount : sortedNodes.length;
    targetSize = Math.min(
      10,
      Math.max(5, 5 + Math.floor((effective - 5) / 4))
    );
  }

  const outgoing = new Map<string, string[]>();
  for (const e of edges) {
    if (e.source === e.target || !nodeById.has(e.source) || !nodeById.has(e.target)) {
      continue;
    }
    const list = outgoing.get(e.source) || [];
    list.push(e.target);
    outgoing.set(e.source, list);
  }
  for (const [k, list] of outgoing.entries()) {
    list.sort();
    outgoing.set(k, list);
  }

  const selectedIds = new Set<string>();
  const steps: GuidedTourStep[] = [];

  const addStep = (
    n: GraphNodeData,
    role: TourRole,
    roleLabel: string,
    score: number,
    reason: string
  ) => {
    selectedIds.add(n.id);
    const base = n.path.split("/").pop() || "";
    steps.push({
      step: steps.length + 1,
      node_id: n.id,
      path: n.path,
      role,
      role_label: roleLabel,
      score: Math.round(score * 100) / 100,
      reason,
      in_degree: n.in_degree,
      out_degree: n.out_degree,
      functions: n.functions,
      classes: n.classes,
      is_entrypoint: n.is_entrypoint || ENTRYPOINT_FILENAMES.has(base),
    });
  };

  // Phase 1: Entrypoints & Package Facades
  const entryCandidates: { node: GraphNodeData; score: number; reason: string }[] = [];
  for (const n of sortedNodes) {
    if (isTestOrAuxPath(n.path) && nonTestCount > 0) continue;
    const parts = n.path.split("/");
    const base = parts[parts.length - 1] || "";
    const depth = parts.length - 1;
    const isExplicit = n.is_entrypoint || ENTRYPOINT_FILENAMES.has(base);
    const isFacade = base === "__init__.py" && depth <= 2 && (n.out_degree > 0 || n.in_degree > 0);
    const isRootOrch = n.in_degree === 0 && n.out_degree > 0;

    if (!isExplicit && !isFacade && !isRootOrch) continue;

    let score =
      0.9 * n.out_degree +
      0.4 * n.in_degree +
      Math.min(2.0, 0.15 * (n.functions + n.classes)) -
      0.35 * depth;
    const reasons: string[] = [];
    if (isExplicit) {
      score += 6.0;
      reasons.push(`実行の起点となるエントリーポイント (${base})`);
    } else if (isFacade) {
      score += 4.2;
      reasons.push("パッケージの公開API境界 (__init__.py)");
    } else {
      score += 3.2;
      reasons.push("被依存0で内部モジュールを呼び出す起点モジュール");
    }
    if (n.out_degree > 0) reasons.push(`内部モジュール ${n.out_degree}件をimport`);
    if (n.in_degree > 0) reasons.push(`被依存 ${n.in_degree}件`);

    entryCandidates.push({ node: n, score, reason: reasons.join(" / ") });
  }

  entryCandidates.sort((a, b) => {
    if (Math.abs(a.score - b.score) > 1e-9) return b.score - a.score;
    return a.node.path.localeCompare(b.node.path);
  });

  const maxEntry = targetSize >= 6 && entryCandidates.length >= 2 && entryCandidates[1].score >= 4.0 ? 2 : 1;
  for (let i = 0; i < entryCandidates.length && steps.length < maxEntry && steps.length < targetSize; i++) {
    const c = entryCandidates[i];
    addStep(c.node, "entrypoint", "Entrypoint / 起点", c.score, c.reason);
  }

  // Forward BFS distance from entrypoints
  const entryDist = new Map<string, number>();
  const queue: string[] = [];
  for (const s of steps) {
    entryDist.set(s.node_id, 0);
    queue.push(s.node_id);
  }
  while (queue.length > 0) {
    const curr = queue.shift()!;
    const d = entryDist.get(curr)!;
    for (const nxt of outgoing.get(curr) || []) {
      if (!entryDist.has(nxt)) {
        entryDist.set(nxt, d + 1);
        queue.push(nxt);
      }
    }
  }

  // Phase 2: Orchestrators
  const orchQuota = Math.max(2, Math.floor((targetSize - steps.length) / 2));
  const orchCandidates: { node: GraphNodeData; dist: number; score: number; reason: string }[] = [];
  for (const n of sortedNodes) {
    if (selectedIds.has(n.id)) continue;
    if (isTestOrAuxPath(n.path) && nonTestCount > steps.length) continue;
    if (n.out_degree === 0) continue;

    const dist = entryDist.get(n.id) ?? 999;
    const depth = n.path.split("/").length - 1;
    let score =
      1.4 * n.out_degree +
      1.1 * n.in_degree +
      Math.min(2.5, 0.2 * (n.functions + n.classes)) -
      0.25 * depth;
    const reasons: string[] = [];
    if (dist === 1) {
      score += 3.5;
      reasons.push("起点ファイルから直接呼び出される主要処理");
    } else if (dist === 2) {
      score += 1.8;
      reasons.push("起点から2ホップで到達する主要フロー");
    }
    if (n.in_degree > 0 && n.out_degree >= 2) {
      score += 2.0;
      reasons.push(`${n.out_degree}個のモジュールを束ねて${n.in_degree}個から参照されるハブ`);
    } else {
      reasons.push(`内部依存先 ${n.out_degree}件 / 被依存 ${n.in_degree}件`);
    }
    if (n.functions + n.classes > 0) {
      reasons.push(`関数${n.functions}・クラス${n.classes}を定義`);
    }
    orchCandidates.push({ node: n, dist, score, reason: reasons.join(" / ") });
  }

  orchCandidates.sort((a, b) => {
    if (Math.abs(a.score - b.score) > 1e-9) return b.score - a.score;
    if (a.dist !== b.dist) return a.dist - b.dist;
    return a.node.path.localeCompare(b.node.path);
  });

  for (let i = 0; i < orchCandidates.length && i < orchQuota && steps.length < targetSize; i++) {
    const c = orchCandidates[i];
    addStep(c.node, "orchestrator", "Workflow / 主要制御", c.score, c.reason);
  }

  // Phase 3: Core Domain & Shared Foundations
  const importedByTour = new Map<string, number>();
  for (const s of steps) {
    for (const dst of outgoing.get(s.node_id) || []) {
      importedByTour.set(dst, (importedByTour.get(dst) || 0) + 1);
    }
  }

  const coreCandidates: {
    node: GraphNodeData;
    role: TourRole;
    label: string;
    score: number;
    reason: string;
  }[] = [];

  for (const n of sortedNodes) {
    if (selectedIds.has(n.id)) continue;
    if (isTestOrAuxPath(n.path) && steps.length < nonTestCount) continue;

    const tourRefs = importedByTour.get(n.id) || 0;
    const depth = n.path.split("/").length - 1;
    const score =
      2.2 * n.in_degree +
      0.6 * n.out_degree +
      1.2 * tourRefs +
      Math.min(3.0, 0.25 * (n.functions + n.classes)) -
      0.2 * depth;

    let role: TourRole = "core_module";
    let label = "Core Domain / 中核モデル";
    const reasons: string[] = [];

    if (n.in_degree >= 2 && n.out_degree === 0) {
      role = "foundation";
      label = "Foundation / 共有基盤";
      reasons.push(`${n.in_degree}個の内部ファイルから共通参照される基盤モジュール`);
    } else if (n.in_degree >= 1) {
      reasons.push(`被依存 ${n.in_degree}件の中心モジュール`);
    } else {
      role = "module";
      label = "Module / 構成要素";
      reasons.push("独立した構成モジュール");
    }
    if (tourRefs > 0) {
      reasons.push(`前半ステップの${tourRefs}ファイルから直接import`);
    }
    if (n.classes + n.functions > 0) {
      reasons.push(`クラス${n.classes}・関数${n.functions}を定義`);
    }
    coreCandidates.push({ node: n, role, label, score, reason: reasons.join(" / ") });
  }

  coreCandidates.sort((a, b) => {
    if (Math.abs(a.score - b.score) > 1e-9) return b.score - a.score;
    if (a.node.importance_score !== b.node.importance_score) {
      return b.node.importance_score - a.node.importance_score;
    }
    return a.node.path.localeCompare(b.node.path);
  });

  for (let i = 0; i < coreCandidates.length && steps.length < targetSize; i++) {
    const c = coreCandidates[i];
    addStep(c.node, c.role, c.label, c.score, c.reason);
  }

  return steps;
}

/**
 * Deterministic BFS over reverse import edges to compute upstream files impacted
 * if `targetNodeId` is modified. Handles cycles safely via visitedDepth.
 */
export function computeImpactAnalysis(
  nodes: GraphNodeData[],
  edges: GraphEdgeData[],
  targetNodeId: string,
  maxDepth: number
): ImpactAnalysisResult | null {
  const normalizedId = targetNodeId.startsWith("file:")
    ? targetNodeId
    : `file:${targetNodeId}`;
  const nodeById = new Map(nodes.map((n) => [n.id, n]));
  const targetNode = nodeById.get(normalizedId);
  if (!targetNode) {
    return null;
  }

  const effectiveMaxDepth = maxDepth > 0 ? maxDepth : Math.max(1, nodes.length);

  // Reverse adjacency list: target -> sorted list of sources that import target
  const dependentsMap = new Map<string, string[]>();
  const seenEdge = new Set<string>();
  for (const e of edges) {
    if (e.source === e.target) continue;
    if (!nodeById.has(e.source) || !nodeById.has(e.target)) continue;
    const key = `${e.source}->${e.target}`;
    if (seenEdge.has(key)) continue;
    seenEdge.add(key);

    const list = dependentsMap.get(e.target) || [];
    list.push(e.source);
    dependentsMap.set(e.target, list);
  }
  for (const [k, list] of dependentsMap.entries()) {
    list.sort((a, b) => {
      const pa = nodeById.get(a)?.path || a;
      const pb = nodeById.get(b)?.path || b;
      return pa.localeCompare(pb);
    });
    dependentsMap.set(k, list);
  }

  const visitedDepth = new Map<string, number>();
  const viaNodeMap = new Map<string, string>();
  visitedDepth.set(normalizedId, 0);

  const queue: string[] = [normalizedId];
  while (queue.length > 0) {
    const curr = queue.shift()!;
    const currDepth = visitedDepth.get(curr)!;
    if (currDepth >= effectiveMaxDepth) {
      continue;
    }

    for (const depId of dependentsMap.get(curr) || []) {
      if (visitedDepth.has(depId)) {
        continue;
      }
      visitedDepth.set(depId, currDepth + 1);
      viaNodeMap.set(depId, curr);
      queue.push(depId);
    }
  }

  const affectedFiles: ImpactedFileData[] = [];
  let directCount = 0;
  let indirectCount = 0;

  for (const [id, depth] of visitedDepth.entries()) {
    if (id === normalizedId || depth <= 0) continue;
    const n = nodeById.get(id)!;
    const viaId = viaNodeMap.get(id) || normalizedId;
    const viaNode = nodeById.get(viaId);
    const direct = depth === 1;
    if (direct) {
      directCount++;
    } else {
      indirectCount++;
    }
    affectedFiles.push({
      node_id: n.id,
      path: n.path,
      depth,
      direct,
      via_node_id: viaId,
      via_path: viaNode ? viaNode.path : viaId.replace(/^file:/, ""),
      in_degree: n.in_degree,
      out_degree: n.out_degree,
      importance_score: n.importance_score,
    });
  }

  affectedFiles.sort((a, b) => {
    if (a.depth !== b.depth) return a.depth - b.depth;
    if (a.importance_score !== b.importance_score) {
      return b.importance_score - a.importance_score;
    }
    return a.path.localeCompare(b.path);
  });

  const affectedEdges: GraphEdgeData[] = [];
  for (const e of edges) {
    if (e.source === e.target) continue;
    const srcDepth = visitedDepth.get(e.source);
    const dstDepth = visitedDepth.get(e.target);
    if (srcDepth !== undefined && dstDepth !== undefined && srcDepth >= 1 && dstDepth >= 0) {
      affectedEdges.push(e);
    }
  }

  return {
    target_node_id: normalizedId,
    target_path: targetNode.path,
    max_depth: effectiveMaxDepth,
    total_files: nodes.length,
    total_affected: affectedFiles.length,
    direct_count: directCount,
    indirect_count: indirectCount,
    affected_files: affectedFiles,
    affected_edges: affectedEdges,
  };
}

/**
 * Aggregates file-level graph into directory-level Architecture Graph.
 */
export function buildArchitectureGraph(
  nodes: GraphNodeData[],
  edges: GraphEdgeData[],
  providedArch?: ArchitectureGraphData
): ArchitectureGraphData {
  if (providedArch && providedArch.nodes && providedArch.nodes.length > 0) {
    return providedArch;
  }

  const nodeById = new Map(nodes.map((n) => [n.id, n]));
  const dirMap = new Map<string, ArchitectureNodeData>();

  for (const n of nodes) {
    const dir = extractDirectoryPath(n.path);
    const dirId = `dir:${dir}`;
    let group = dirMap.get(dir);
    if (!group) {
      group = {
        id: dirId,
        directory: dir,
        file_count: 0,
        functions_count: 0,
        classes_count: 0,
        internal_edge_count: 0,
        in_degree: 0,
        out_degree: 0,
        incoming_weight: 0,
        outgoing_weight: 0,
        entrypoint_count: 0,
        files: [],
      };
      dirMap.set(dir, group);
    }
    group.file_count++;
    group.functions_count += n.functions;
    group.classes_count += n.classes;
    if (n.is_entrypoint) group.entrypoint_count++;
    group.files.push({
      node_id: n.id,
      path: n.path,
      functions: n.functions,
      classes: n.classes,
      in_degree: n.in_degree,
      out_degree: n.out_degree,
      is_entrypoint: n.is_entrypoint,
      importance_score: n.importance_score,
    });
  }

  const pairSamples = new Map<string, { srcDir: string; dstDir: string; samples: string[] }>();
  const seenFileEdge = new Set<string>();

  for (const e of edges) {
    if (e.source === e.target) continue;
    const srcNode = nodeById.get(e.source);
    const dstNode = nodeById.get(e.target);
    if (!srcNode || !dstNode) continue;

    const edgeKey = `${srcNode.id}->${dstNode.id}`;
    if (seenFileEdge.has(edgeKey)) continue;
    seenFileEdge.add(edgeKey);

    const srcDir = extractDirectoryPath(srcNode.path);
    const dstDir = extractDirectoryPath(dstNode.path);
    if (srcDir === dstDir) {
      const grp = dirMap.get(srcDir);
      if (grp) grp.internal_edge_count++;
      continue;
    }

    const pairKey = `${srcDir}|||${dstDir}`;
    let bucket = pairSamples.get(pairKey);
    if (!bucket) {
      bucket = { srcDir, dstDir, samples: [] };
      pairSamples.set(pairKey, bucket);
    }
    bucket.samples.push(`${srcNode.path} -> ${dstNode.path}`);
  }

  const archEdges: ArchitectureEdgeData[] = [];
  for (const bucket of pairSamples.values()) {
    bucket.samples.sort();
    const weight = bucket.samples.length;
    const srcGroup = dirMap.get(bucket.srcDir)!;
    const dstGroup = dirMap.get(bucket.dstDir)!;
    srcGroup.out_degree++;
    srcGroup.outgoing_weight += weight;
    dstGroup.in_degree++;
    dstGroup.incoming_weight += weight;

    archEdges.push({
      id: `dir:${bucket.srcDir}->dir:${bucket.dstDir}`,
      source: `dir:${bucket.srcDir}`,
      target: `dir:${bucket.dstDir}`,
      source_directory: bucket.srcDir,
      target_directory: bucket.dstDir,
      weight,
      sample_pairs: bucket.samples.slice(0, 5),
    });
  }

  archEdges.sort((a, b) => {
    if (a.source_directory !== b.source_directory) {
      return a.source_directory.localeCompare(b.source_directory);
    }
    return a.target_directory.localeCompare(b.target_directory);
  });

  const archNodes = Array.from(dirMap.values());
  for (const grp of archNodes) {
    grp.files.sort((a, b) => {
      if (a.importance_score !== b.importance_score) {
        return b.importance_score - a.importance_score;
      }
      return a.path.localeCompare(b.path);
    });
  }
  archNodes.sort((a, b) => a.directory.localeCompare(b.directory));

  return { nodes: archNodes, edges: archEdges };
}

export function filterAndCapGraph(
  nodes: GraphNodeData[],
  edges: GraphEdgeData[],
  recommendedIds: string[],
  searchQuery: string,
  selectedDirectory: string,
  maxNodes: number = MAX_RENDERED_NODES
): FilteredGraphResult {
  const dirSet = new Set<string>();
  for (const n of nodes) {
    dirSet.add(extractTopDirectory(n.path));
    const fullDir = extractDirectoryPath(n.path);
    if (fullDir !== "(root)") {
      dirSet.add(fullDir);
    }
  }
  const directories = Array.from(dirSet).sort();

  const normalizedQuery = searchQuery.trim().toLowerCase();
  let matching = nodes.filter((n) => {
    if (selectedDirectory && selectedDirectory !== "ALL") {
      const topDir = extractTopDirectory(n.path);
      const fullDir = extractDirectoryPath(n.path);
      if (
        topDir !== selectedDirectory &&
        fullDir !== selectedDirectory &&
        !fullDir.startsWith(selectedDirectory + "/")
      ) {
        return false;
      }
    }
    if (normalizedQuery && !n.path.toLowerCase().includes(normalizedQuery)) {
      return false;
    }
    return true;
  });

  let hiddenByLimitCount = 0;
  if (matching.length > maxNodes) {
    const recSet = new Set(recommendedIds);
    const prioritized = [...matching].sort((a, b) => {
      const aRec = recSet.has(a.id) ? 1 : 0;
      const bRec = recSet.has(b.id) ? 1 : 0;
      if (aRec !== bRec) return bRec - aRec;
      if (a.importance_score !== b.importance_score) {
        return b.importance_score - a.importance_score;
      }
      return a.path.localeCompare(b.path);
    });
    hiddenByLimitCount = matching.length - maxNodes;
    matching = prioritized.slice(0, maxNodes).sort((a, b) =>
      a.path.localeCompare(b.path)
    );
  }

  const visibleIdSet = new Set(matching.map((n) => n.id));
  const visibleEdges = edges.filter(
    (e) => visibleIdSet.has(e.source) && visibleIdSet.has(e.target)
  );

  return {
    visibleNodes: matching,
    visibleEdges,
    totalNodes: nodes.length,
    hiddenByLimitCount,
    directories,
  };
}

export function computeDagreNodePositions(
  nodes: GraphNodeData[],
  edges: GraphEdgeData[]
): Map<string, { x: number; y: number }> {
  const g = new dagre.graphlib.Graph();
  g.setDefaultEdgeLabel(() => ({}));
  g.setGraph({
    rankdir: "LR",
    nodesep: 44,
    ranksep: 96,
    marginx: 28,
    marginy: 28,
    acyclicer: "greedy",
    ranker: "tight-tree",
  });

  for (const n of nodes) {
    g.setNode(n.id, { width: NODE_WIDTH, height: NODE_HEIGHT });
  }

  // For large graphs (> 80 nodes), prevent pathological 100+ rank horizontal chains
  // in Dagre by tracking approximate hop depth and skipping layout edges that would
  // push rank depth past 8 columns (all visual edges are still rendered in React Flow).
  if (nodes.length > 80) {
    const approxRank = new Map<string, number>();
    for (const e of edges) {
      if (e.source === e.target) continue;
      const srcRank = approxRank.get(e.source) ?? 0;
      const dstRank = approxRank.get(e.target) ?? 0;
      if (srcRank >= 7) {
        continue;
      }
      if (dstRank <= srcRank) {
        approxRank.set(e.target, srcRank + 1);
      }
      g.setEdge(e.source, e.target);
    }
  } else {
    for (const e of edges) {
      g.setEdge(e.source, e.target);
    }
  }

  dagre.layout(g);

  const positions = new Map<string, { x: number; y: number }>();
  nodes.forEach((n, idx) => {
    const pos = g.node(n.id);
    const x =
      pos && Number.isFinite(pos.x)
        ? pos.x - NODE_WIDTH / 2
        : (idx % 4) * 280;
    const y =
      pos && Number.isFinite(pos.y)
        ? pos.y - NODE_HEIGHT / 2
        : Math.floor(idx / 4) * 120;
    positions.set(n.id, { x, y });
  });
  return positions;
}

export function applyGraphVisualState(
  nodes: GraphNodeData[],
  edges: GraphEdgeData[],
  positions: Map<string, { x: number; y: number }>,
  recommendedIds: string[],
  selectedNodeId: string | null,
  impactResult?: ImpactAnalysisResult | null,
  tourStepById?: Map<string, number>
): { flowNodes: Node<FileNodeViewModel>[]; flowEdges: Edge[] } {
  const recSet = new Set(recommendedIds);
  const impactDepthMap = new Map<string, number>();
  const impactEdgeSet = new Set<string>();

  if (impactResult && selectedNodeId === impactResult.target_node_id) {
    impactDepthMap.set(impactResult.target_node_id, 0);
    for (const af of impactResult.affected_files) {
      impactDepthMap.set(af.node_id, af.depth);
    }
    for (const ae of impactResult.affected_edges) {
      impactEdgeSet.add(`${ae.source}->${ae.target}`);
    }
  }

  const relatedNodeIds = new Set<string>();
  if (selectedNodeId) {
    relatedNodeIds.add(selectedNodeId);
    if (impactDepthMap.size > 0) {
      for (const id of impactDepthMap.keys()) {
        relatedNodeIds.add(id);
      }
    }
    for (const e of edges) {
      if (e.source === selectedNodeId) {
        relatedNodeIds.add(e.target);
      }
      if (e.target === selectedNodeId) {
        relatedNodeIds.add(e.source);
      }
    }
  }

  const flowNodes: Node<FileNodeViewModel>[] = nodes.map((n, idx) => {
    const pos = positions.get(n.id) || {
      x: (idx % 4) * 280,
      y: Math.floor(idx / 4) * 120,
    };

    const isSelected = selectedNodeId === n.id;
    const isRelated = selectedNodeId ? relatedNodeIds.has(n.id) : false;
    const isDimmed = Boolean(selectedNodeId && !isRelated);
    const impactDepth = impactDepthMap.get(n.id);
    const tourStep = tourStepById?.get(n.id);

    return {
      id: n.id,
      type: "fileNode",
      position: pos,
      data: {
        file: n,
        isRecommended: recSet.has(n.id),
        isSelected,
        isRelated,
        isDimmed,
        tourStep,
        impactDepth,
      },
    };
  });

  const flowEdges: Edge[] = edges.map((e) => {
    const edgeKey = `${e.source}->${e.target}`;
    const isImpactEdge = impactEdgeSet.has(edgeKey);
    const isDirectEdge = Boolean(
      selectedNodeId &&
        (e.source === selectedNodeId || e.target === selectedNodeId)
    );
    const isHighlighted = isImpactEdge || isDirectEdge;
    const isDimmed = Boolean(selectedNodeId && !isHighlighted);

    let strokeColor = "#64748b";
    if (isImpactEdge) {
      strokeColor = "#f97316";
    } else if (isDirectEdge) {
      strokeColor = "#38bdf8";
    } else if (isDimmed) {
      strokeColor = "#334155";
    }

    return {
      id: edgeKey,
      source: e.source,
      target: e.target,
      animated: isHighlighted,
      markerEnd: {
        type: "arrowclosed",
        color: strokeColor,
        width: 15,
        height: 15,
      },
      style: {
        stroke: strokeColor,
        strokeWidth: isHighlighted ? 2.4 : 1.5,
        opacity: isDimmed ? 0.22 : 0.92,
      },
    };
  });

  return { flowNodes, flowEdges };
}

export function buildLayoutedElements(
  nodes: GraphNodeData[],
  edges: GraphEdgeData[],
  recommendedIds: string[],
  selectedNodeId: string | null,
  impactResult?: ImpactAnalysisResult | null,
  tourStepById?: Map<string, number>
): { flowNodes: Node<FileNodeViewModel>[]; flowEdges: Edge[] } {
  const positions = computeDagreNodePositions(nodes, edges);
  return applyGraphVisualState(
    nodes,
    edges,
    positions,
    recommendedIds,
    selectedNodeId,
    impactResult,
    tourStepById
  );
}

export function buildArchitectureLayoutElements(
  arch: ArchitectureGraphData,
  selectedDirId: string | null
): { flowNodes: Node<DirectoryNodeViewModel>[]; flowEdges: Edge[] } {
  const g = new dagre.graphlib.Graph();
  g.setDefaultEdgeLabel(() => ({}));
  g.setGraph({
    rankdir: "LR",
    nodesep: 54,
    ranksep: 120,
    marginx: 32,
    marginy: 32,
    acyclicer: "greedy",
  });

  for (const n of arch.nodes) {
    g.setNode(n.id, { width: ARCH_NODE_WIDTH, height: ARCH_NODE_HEIGHT });
  }
  for (const e of arch.edges) {
    g.setEdge(e.source, e.target);
  }

  dagre.layout(g);

  const relatedDirIds = new Set<string>();
  if (selectedDirId) {
    relatedDirIds.add(selectedDirId);
    for (const e of arch.edges) {
      if (e.source === selectedDirId) relatedDirIds.add(e.target);
      if (e.target === selectedDirId) relatedDirIds.add(e.source);
    }
  }

  const flowNodes: Node<DirectoryNodeViewModel>[] = arch.nodes.map((n, idx) => {
    const pos = g.node(n.id);
    const x =
      pos && Number.isFinite(pos.x) ? pos.x - ARCH_NODE_WIDTH / 2 : (idx % 3) * 320;
    const y =
      pos && Number.isFinite(pos.y)
        ? pos.y - ARCH_NODE_HEIGHT / 2
        : Math.floor(idx / 3) * 150;

    const isSelected = selectedDirId === n.id;
    const isRelated = selectedDirId ? relatedDirIds.has(n.id) : false;
    const isDimmed = Boolean(selectedDirId && !isRelated);

    return {
      id: n.id,
      type: "directoryNode",
      position: { x, y },
      data: {
        dirNode: n,
        isSelected,
        isRelated,
        isDimmed,
      },
    };
  });

  const flowEdges: Edge[] = arch.edges.map((e) => {
    const isHighlighted = Boolean(
      selectedDirId && (e.source === selectedDirId || e.target === selectedDirId)
    );
    const isDimmed = Boolean(selectedDirId && !isHighlighted);
    const width = Math.min(5, 1.5 + Math.log2(e.weight + 1));
    const strokeColor = isHighlighted
      ? "#38bdf8"
      : isDimmed
      ? "#334155"
      : "#64748b";

    return {
      id: e.id,
      source: e.source,
      target: e.target,
      label: `${e.weight} imports`,
      animated: isHighlighted,
      markerEnd: {
        type: "arrowclosed",
        color: strokeColor,
        width: 15,
        height: 15,
      },
      style: {
        stroke: strokeColor,
        strokeWidth: isHighlighted ? width + 0.8 : width,
        opacity: isDimmed ? 0.22 : 0.92,
      },
    };
  });

  return { flowNodes, flowEdges };
}
