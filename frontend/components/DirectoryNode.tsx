"use client";

import React, { memo } from "react";
import { Handle, Position, type NodeProps, type Node } from "@xyflow/react";
import type { DirectoryNodeViewModel } from "@/lib/graph-layout";

function DirectoryNodeInner({ data }: NodeProps<Node<DirectoryNodeViewModel>>) {
  const { dirNode, isSelected, isRelated, isDimmed } = data;

  const classNames = [
    "rf-dir-node",
    isSelected ? "rf-dir-node-selected" : "",
    !isSelected && isRelated ? "rf-dir-node-related" : "",
    isDimmed ? "rf-dir-node-dimmed" : "",
  ]
    .filter(Boolean)
    .join(" ");

  const previewFiles = dirNode.files.slice(0, 3);

  return (
    <div
      className={classNames}
      data-testid={`arch-node-${dirNode.directory}`}
    >
      <Handle type="target" position={Position.Left} className="rf-handle" />
      <div className="rf-node-top">
        <span className="badge badge-arch-dir">Directory Group</span>
        <span className="rf-dir-filecount">{dirNode.file_count} files</span>
      </div>

      <div className="rf-dir-title" title={dirNode.directory}>
        📁 {dirNode.directory}
      </div>

      <div className="rf-dir-preview">
        {previewFiles.map((f) => {
          const shortName = f.path.split("/").pop() || f.path;
          return (
            <span key={f.node_id} className="rf-dir-file-pill">
              {shortName}
            </span>
          );
        })}
        {dirNode.file_count > previewFiles.length && (
          <span className="rf-dir-file-more">
            +{dirNode.file_count - previewFiles.length}
          </span>
        )}
      </div>

      <div className="rf-node-metrics">
        <span title="内部凝集import数">内部: {dirNode.internal_edge_count}</span>
        <span title="他ディレクトリからの被依存数">
          In: {dirNode.in_degree} ({dirNode.incoming_weight})
        </span>
        <span title="他ディレクトリへの依存先数">
          Out: {dirNode.out_degree} ({dirNode.outgoing_weight})
        </span>
      </div>
      <Handle type="source" position={Position.Right} className="rf-handle" />
    </div>
  );
}

export const DirectoryNode = memo(DirectoryNodeInner);
