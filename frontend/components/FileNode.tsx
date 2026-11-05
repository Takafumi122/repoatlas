"use client";

import React, { memo } from "react";
import { Handle, Position, type NodeProps, type Node } from "@xyflow/react";
import type { FileNodeViewModel } from "@/lib/graph-layout";

function FileNodeInner({ data }: NodeProps<Node<FileNodeViewModel>>) {
  const {
    file,
    isRecommended,
    isSelected,
    isRelated,
    isDimmed,
    tourStep,
    impactDepth,
  } = data;
  const isPackage = file.node_kind === "package";
  const parts = file.path.split("/");
  const fileName = parts[parts.length - 1];
  const dirPath = parts.length > 1 ? parts.slice(0, -1).join("/") + "/" : "";

  const isDirectImpact = impactDepth === 1;
  const isIndirectImpact = impactDepth !== undefined && impactDepth >= 2;

  const classNames = [
    "rf-file-node",
    isPackage ? "rf-node-package" : "",
    file.is_entrypoint ? "rf-file-node-entrypoint" : "",
    !file.is_entrypoint && (isRecommended || tourStep !== undefined)
      ? "rf-file-node-tour"
      : "",
    isSelected ? "rf-file-node-selected" : "",
    isDirectImpact ? "rf-file-node-impact-direct" : "",
    isIndirectImpact ? "rf-file-node-impact-indirect" : "",
    !isSelected && !isDirectImpact && !isIndirectImpact && isRelated
      ? "rf-file-node-related"
      : "",
    isDimmed ? "rf-file-node-dimmed" : "",
  ]
    .filter(Boolean)
    .join(" ");

  const displayTitle = isPackage
    ? file.package_import_path || file.path
    : file.path;
  const displayMain = isPackage
    ? (file.package_name ? `${file.package_name}` : fileName)
    : fileName;
  const displaySub = isPackage
    ? (file.directory_path || dirPath || "(root)")
    : (dirPath || "(root)");

  return (
    <div className={classNames} data-testid={`graph-node-${file.path}`}>
      <Handle type="target" position={Position.Left} className="rf-handle" />
      <div className="rf-node-top">
        <span className="rf-node-dir" title={displayTitle}>
          {displaySub}
        </span>
        <div className="rf-node-badges">
          {isPackage && (
            <span className="badge badge-package" title="Go Package">
              Go Pkg
            </span>
          )}
          {tourStep !== undefined && (
            <span
              className="badge badge-tour"
              title={`Guided Code Tour Step #${tourStep}`}
            >
              Tour #{tourStep}
            </span>
          )}
          {file.is_entrypoint && (
            <span className="badge badge-entrypoint" title="エントリーポイント候補">
              Entry
            </span>
          )}
          {isRecommended && (
            <span className="badge badge-recommended" title="優先閲覧推奨ファイル">
              Top
            </span>
          )}
          {isDirectImpact && (
            <span className="badge badge-impact-direct" title="直接依存元 (Depth 1)">
              直接影響
            </span>
          )}
          {isIndirectImpact && (
            <span
              className="badge badge-impact-indirect"
              title={`間接依存元 (Depth ${impactDepth})`}
            >
              間接D{impactDepth}
            </span>
          )}
        </div>
      </div>

      <div className="rf-node-filename" title={displayTitle}>
        {isPackage ? `📦 ${displayMain}` : displayMain}
      </div>

      <div className="rf-node-metrics">
        <span title="内部被依存数 (in_degree)">In: {file.in_degree}</span>
        <span title="内部依存先数 (out_degree)">Out: {file.out_degree}</span>
        {isPackage ? (
          <span title={`構成ファイル: ${file.files_count || (file.files ? file.files.length : 1)} / 関数: ${file.functions} / 型: ${file.types_count ?? file.classes}`}>
            Files: {file.files_count || (file.files ? file.files.length : 1)}
          </span>
        ) : (
          <span title="重要度スコア (ヒューリスティック推定)">
            Score: {file.importance_score.toFixed(1)}
          </span>
        )}
      </div>
      <Handle type="source" position={Position.Right} className="rf-handle" />
    </div>
  );
}

export const FileNode = memo(FileNodeInner);
