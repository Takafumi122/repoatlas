import {
  APIErrorPayload,
  AnalysisStatusResponse,
  ArchitectureDiffGraphResponse,
  ArchitectureDiffStatusResponse,
  CreateAnalysisResponse,
  CreateArchitectureDiffResponse,
  CreatePRAnalysisResponse,
  EvidenceResponse,
  GraphResponse,
  PRAnalysisStatusResponse,
  PRImpactResponse,
} from "./types";

export function resolveApiBaseUrl(
  configuredPublicUrl: string | undefined = process.env.NEXT_PUBLIC_API_BASE_URL,
  isBrowser: boolean = typeof window !== "undefined"
): string {
  const trimmed = (configuredPublicUrl ?? "").trim().replace(/\/$/, "");

  // When configured as "same-origin" or targeting an internal Railway domain from a browser,
  // route through Next.js same-origin rewrites (/api/v1/*) so the browser never calls .railway.internal directly.
  if (trimmed === "same-origin" || (isBrowser && trimmed.includes(".railway.internal"))) {
    return isBrowser
      ? ""
      : (process.env.INTERNAL_API_BASE_URL || "http://localhost:8080").replace(/\/$/, "");
  }

  if (trimmed.length > 0) {
    return trimmed;
  }

  // Default: in browser, if NEXT_PUBLIC_API_BASE_URL is omitted in production, use same-origin proxy ("")
  // while in local development/test default to http://localhost:8080 if not using same-origin.
  if (isBrowser && process.env.NODE_ENV === "production") {
    return "";
  }

  return (process.env.INTERNAL_API_BASE_URL || "http://localhost:8080").replace(/\/$/, "");
}

const API_BASE_URL = resolveApiBaseUrl();

export class APIClientError extends Error {
  code: string;
  status: number;

  constructor(code: string, message: string, status: number) {
    super(message);
    this.name = "APIClientError";
    this.code = code;
    this.status = status;
  }
}

async function parseErrorResponse(res: Response): Promise<APIClientError> {
  try {
    const body = (await res.json()) as APIErrorPayload;
    const code = body?.error?.code || "INTERNAL_ERROR";
    const message =
      body?.error?.message ||
      `APIエラーが発生しました（HTTP ${res.status}）`;
    return new APIClientError(code, message, res.status);
  } catch {
    return new APIClientError(
      "INTERNAL_ERROR",
      `サーバー通信エラーが発生しました（HTTP ${res.status}）`,
      res.status
    );
  }
}

export async function createAnalysis(
  repositoryUrl: string,
  signal?: AbortSignal
): Promise<CreateAnalysisResponse> {
  const res = await fetch(`${API_BASE_URL}/api/v1/analyses`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Accept: "application/json",
    },
    body: JSON.stringify({ repository_url: repositoryUrl }),
    signal,
  });

  if (!res.ok) {
    throw await parseErrorResponse(res);
  }
  return (await res.json()) as CreateAnalysisResponse;
}

export async function getAnalysisStatus(
  id: string,
  signal?: AbortSignal
): Promise<AnalysisStatusResponse> {
  const res = await fetch(
    `${API_BASE_URL}/api/v1/analyses/${encodeURIComponent(id)}`,
    {
      method: "GET",
      headers: { Accept: "application/json" },
      cache: "no-store",
      signal,
    }
  );

  if (!res.ok) {
    throw await parseErrorResponse(res);
  }
  return (await res.json()) as AnalysisStatusResponse;
}

export async function getAnalysisGraph(
  id: string,
  signal?: AbortSignal
): Promise<GraphResponse> {
  const res = await fetch(
    `${API_BASE_URL}/api/v1/analyses/${encodeURIComponent(id)}/graph`,
    {
      method: "GET",
      headers: { Accept: "application/json" },
      cache: "no-store",
      signal,
    }
  );

  if (!res.ok) {
    throw await parseErrorResponse(res);
  }
  return (await res.json()) as GraphResponse;
}

export const FALLBACK_DEMO_GRAPH: GraphResponse = {
  demo: true,
  analysis_id: "demo-python-web-starter",
  repository: {
    owner: "repoatlas-demo",
    name: "python-web-starter",
    html_url: "https://github.com/repoatlas-demo/python-web-starter",
  },
  commit_sha: "8f4b2c9e1a3d5f7b9c0e2a4d6f8b0c2e4a6d8f0a",
  summary: {
    files: 7,
    internal_edges: 11,
    external_imports: 4,
    unresolved_imports: 1,
  },
  nodes: [
    {
      id: "file:app/__init__.py",
      path: "app/__init__.py",
      functions: 1,
      classes: 0,
      in_degree: 1,
      out_degree: 2,
      is_entrypoint: false,
      importance_score: 3.0,
      github_url:
        "https://github.com/repoatlas-demo/python-web-starter/blob/8f4b2c9e1a3d5f7b9c0e2a4d6f8b0c2e4a6d8f0a/app/__init__.py",
    },
    {
      id: "file:app/auth.py",
      path: "app/auth.py",
      functions: 3,
      classes: 1,
      in_degree: 2,
      out_degree: 2,
      is_entrypoint: false,
      importance_score: 5.0,
      github_url:
        "https://github.com/repoatlas-demo/python-web-starter/blob/8f4b2c9e1a3d5f7b9c0e2a4d6f8b0c2e4a6d8f0a/app/auth.py",
    },
    {
      id: "file:app/config.py",
      path: "app/config.py",
      functions: 2,
      classes: 1,
      in_degree: 3,
      out_degree: 0,
      is_entrypoint: false,
      importance_score: 6.0,
      github_url:
        "https://github.com/repoatlas-demo/python-web-starter/blob/8f4b2c9e1a3d5f7b9c0e2a4d6f8b0c2e4a6d8f0a/app/config.py",
    },
    {
      id: "file:app/db.py",
      path: "app/db.py",
      functions: 4,
      classes: 1,
      in_degree: 2,
      out_degree: 1,
      is_entrypoint: false,
      importance_score: 4.5,
      github_url:
        "https://github.com/repoatlas-demo/python-web-starter/blob/8f4b2c9e1a3d5f7b9c0e2a4d6f8b0c2e4a6d8f0a/app/db.py",
    },
    {
      id: "file:app/main.py",
      path: "app/main.py",
      functions: 2,
      classes: 0,
      in_degree: 0,
      out_degree: 3,
      is_entrypoint: true,
      importance_score: 4.5,
      github_url:
        "https://github.com/repoatlas-demo/python-web-starter/blob/8f4b2c9e1a3d5f7b9c0e2a4d6f8b0c2e4a6d8f0a/app/main.py",
    },
    {
      id: "file:app/models.py",
      path: "app/models.py",
      functions: 1,
      classes: 3,
      in_degree: 2,
      out_degree: 1,
      is_entrypoint: false,
      importance_score: 4.5,
      github_url:
        "https://github.com/repoatlas-demo/python-web-starter/blob/8f4b2c9e1a3d5f7b9c0e2a4d6f8b0c2e4a6d8f0a/app/models.py",
    },
    {
      id: "file:app/routes.py",
      path: "app/routes.py",
      functions: 5,
      classes: 0,
      in_degree: 1,
      out_degree: 3,
      is_entrypoint: false,
      importance_score: 3.5,
      github_url:
        "https://github.com/repoatlas-demo/python-web-starter/blob/8f4b2c9e1a3d5f7b9c0e2a4d6f8b0c2e4a6d8f0a/app/routes.py",
    },
  ],
  edges: [
    { source: "file:app/__init__.py", target: "file:app/config.py", kind: "import" },
    { source: "file:app/__init__.py", target: "file:app/routes.py", kind: "import" },
    { source: "file:app/auth.py", target: "file:app/config.py", kind: "import" },
    { source: "file:app/auth.py", target: "file:app/models.py", kind: "import" },
    { source: "file:app/db.py", target: "file:app/config.py", kind: "import" },
    { source: "file:app/main.py", target: "file:app/__init__.py", kind: "import" },
    { source: "file:app/main.py", target: "file:app/auth.py", kind: "import" },
    { source: "file:app/main.py", target: "file:app/db.py", kind: "import" },
    { source: "file:app/models.py", target: "file:app/db.py", kind: "import" },
    { source: "file:app/routes.py", target: "file:app/auth.py", kind: "import" },
    { source: "file:app/routes.py", target: "file:app/models.py", kind: "import" },
  ],
  recommended_file_ids: [
    "file:app/config.py",
    "file:app/auth.py",
    "file:app/db.py",
    "file:app/main.py",
    "file:app/models.py",
  ],
  warnings: [
    "Demo dataset (bundled static fixture). Real GitHub analysis results may vary.",
    "Static import graph; dynamic imports are not included.",
  ],
};

export async function getDemoGraph(signal?: AbortSignal): Promise<GraphResponse> {
  try {
    const res = await fetch(`${API_BASE_URL}/api/v1/demo/graph`, {
      method: "GET",
      headers: { Accept: "application/json" },
      cache: "no-store",
      signal,
    });
    if (!res.ok) {
      return FALLBACK_DEMO_GRAPH;
    }
    return (await res.json()) as GraphResponse;
  } catch {
    return FALLBACK_DEMO_GRAPH;
  }
}



export async function createArchitectureDiff(
  repositoryUrl: string,
  baseSha: string,
  headSha: string,
  signal?: AbortSignal
): Promise<CreateArchitectureDiffResponse> {
  const res = await fetch(`${API_BASE_URL}/api/v1/architecture-diffs`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Accept: "application/json",
    },
    body: JSON.stringify({
      repository_url: repositoryUrl,
      base_sha: baseSha,
      head_sha: headSha,
    }),
    signal,
  });

  if (!res.ok) {
    throw await parseErrorResponse(res);
  }
  return (await res.json()) as CreateArchitectureDiffResponse;
}

export async function getArchitectureDiffStatus(
  id: string,
  signal?: AbortSignal
): Promise<ArchitectureDiffStatusResponse> {
  const res = await fetch(
    `${API_BASE_URL}/api/v1/architecture-diffs/${encodeURIComponent(id)}`,
    {
      method: "GET",
      headers: { Accept: "application/json" },
      cache: "no-store",
      signal,
    }
  );

  if (!res.ok) {
    throw await parseErrorResponse(res);
  }
  return (await res.json()) as ArchitectureDiffStatusResponse;
}

export async function getArchitectureDiffGraph(
  id: string,
  signal?: AbortSignal
): Promise<ArchitectureDiffGraphResponse> {
  const res = await fetch(
    `${API_BASE_URL}/api/v1/architecture-diffs/${encodeURIComponent(id)}/graph`,
    {
      method: "GET",
      headers: { Accept: "application/json" },
      cache: "no-store",
      signal,
    }
  );

  if (!res.ok) {
    throw await parseErrorResponse(res);
  }
  return (await res.json()) as ArchitectureDiffGraphResponse;
}

export async function createPRAnalysis(
  url: string,
  signal?: AbortSignal
): Promise<CreatePRAnalysisResponse> {
  const res = await fetch(`${API_BASE_URL}/api/v1/pull-requests/analyses`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Accept: "application/json",
    },
    body: JSON.stringify({ url }),
    signal,
  });

  if (!res.ok) {
    throw await parseErrorResponse(res);
  }
  return (await res.json()) as CreatePRAnalysisResponse;
}

export async function getPRAnalysisStatus(
  id: string,
  signal?: AbortSignal
): Promise<PRAnalysisStatusResponse> {
  const res = await fetch(
    `${API_BASE_URL}/api/v1/pull-requests/analyses/${encodeURIComponent(id)}`,
    {
      method: "GET",
      headers: { Accept: "application/json" },
      cache: "no-store",
      signal,
    }
  );

  if (!res.ok) {
    throw await parseErrorResponse(res);
  }
  return (await res.json()) as PRAnalysisStatusResponse;
}

export async function getPRAnalysisImpact(
  id: string,
  signal?: AbortSignal
): Promise<PRImpactResponse> {
  const res = await fetch(
    `${API_BASE_URL}/api/v1/pull-requests/analyses/${encodeURIComponent(id)}/impact`,
    {
      method: "GET",
      headers: { Accept: "application/json" },
      cache: "no-store",
      signal,
    }
  );

  if (!res.ok) {
    throw await parseErrorResponse(res);
  }
  return (await res.json()) as PRImpactResponse;
}

export async function getAnalysisEvidence(
  analysisId: string,
  options?: {
    sourcePath?: string;
    targetPath?: string;
    unresolvedOnly?: boolean;
  },
  signal?: AbortSignal
): Promise<EvidenceResponse> {
  const params = new URLSearchParams();
  if (options?.sourcePath) params.set("source_path", options.sourcePath);
  if (options?.targetPath) params.set("target_path", options.targetPath);
  if (options?.unresolvedOnly) params.set("unresolved_only", "true");

  const qs = params.toString();
  const url = `${API_BASE_URL}/api/v1/analyses/${encodeURIComponent(analysisId)}/evidence${qs ? `?${qs}` : ""}`;

  const res = await fetch(url, {
    method: "GET",
    headers: { Accept: "application/json" },
    cache: "no-store",
    signal,
  });

  if (!res.ok) {
    throw await parseErrorResponse(res);
  }
  return (await res.json()) as EvidenceResponse;
}


