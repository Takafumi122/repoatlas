"use client";

import React, { useEffect, useState } from "react";
import { FALLBACK_DEMO_GRAPH, getDemoGraph } from "@/lib/api";
import type { GraphResponse } from "@/lib/types";
import { RepositoryGraph } from "@/components/RepositoryGraph";

export default function DemoPage() {
  const [graph, setGraph] = useState<GraphResponse>(FALLBACK_DEMO_GRAPH);

  useEffect(() => {
    const ctrl = new AbortController();
    getDemoGraph(ctrl.signal)
      .then((data) => {
        setGraph({ ...data, demo: true });
      })
      .catch(() => {
        // Keep bundled fallback demo graph if offline
      });
    return () => ctrl.abort();
  }, []);

  return <RepositoryGraph graph={graph} />;
}
