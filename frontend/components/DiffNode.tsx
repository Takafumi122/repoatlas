"use client";

import React, { memo } from "react";
import { Handle, Position, type NodeProps, type Node } from "@xyflow/react";
import type { DiffNodeData } from "@/lib/types";

export interface DiffNodeViewModel extends Record<string, unknown> {
  diffNode: DiffNodeData;
}

function DiffNodeInner({ data }: NodeProps<Node<DiffNodeViewModel>>) {
  const { diffNode } = data;
  const isPackage = diffNode.node_kind === "package";
  const parts = diffNode.path.split("/");
  const fileName = parts[parts.length - 1];
  const dirPath = parts.length > 1 ? parts.slice(0, -1).join("/") + "/" : "";

  let changeClass = "rf-diff-node-unchanged";
  let changeBadge = (
    <span className="badge badge-diff-unchanged" title="変更なし">
      =
    </span>
  );

  switch (diffNode.change_type) {
    case "added":
      changeClass = "rf-diff-node-added";
      changeBadge = (
        <span className="badge badge-diff-added" title="新規追加">
          + 追加
        </span>
      );
      break;
    case "removed":
      changeClass = "rf-diff-node-removed";
      changeBadge = (
        <span className="badge badge-diff-removed" title="削除">
          - 削除
        </span>
      );
      break;
    case "modified":
      changeClass = "rf-diff-node-modified";
      changeBadge = (
        <span className="badge badge-diff-modified" title="変更あり">
          ~ 変更
        </span>
      );
      break;
  }

  const classNames = [
    "rf-diff-node",
    changeClass,
    diffNode.isSelected ? "rf-diff-node-selected" : "",
    diffNode.isDimmed ? "rf-diff-node-dimmed" : "",
  ]
    .filter(Boolean)
    .join(" ");

  // Metrics to show
  const metrics = diffNode.head_metrics || diffNode.base_metrics;

  return (
    <div className={classNames} data-testid={`diff-node-${diffNode.path}`}>
      <Handle type="target" position={Position.Left} className="rf-handle" />
      <div className="rf-node-top">
        <span className="rf-node-dir" title={diffNode.path}>
          {dirPath || "(root)"}
        </span>
        <div className="rf-node-badges">
          {isPackage && (
            <span className="badge badge-package" title="Go Package">
              Go Pkg
            </span>
          )}
          {changeBadge}
        </div>
      </div>

      <div className="rf-node-filename" title={diffNode.path}>
        {isPackage ? `📦 ${fileName}` : fileName}
      </div>

      {metrics && (
        <div className="rf-node-metrics">
          <span title="In-degree (被依存数)">In: {metrics.in_degree}</span>
          <span title="Out-degree (依存先数)">Out: {metrics.out_degree}</span>
          {diffNode.change_details.length > 0 && (
            <span
              style={{ color: "#fbbf24", fontWeight: 600 }}
              title={diffNode.change_details.join("\n")}
            >
              Δ {diffNode.change_details.length}
            </span>
          )}
        </div>
      )}
      <Handle type="source" position={Position.Right} className="rf-handle" />
    </div>
  );
}

export const DiffNode = memo(DiffNodeInner);
