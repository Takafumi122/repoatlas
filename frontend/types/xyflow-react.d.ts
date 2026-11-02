declare module "@xyflow/react" {
  import type React from "react";

  export enum Position {
    Left = "left",
    Top = "top",
    Right = "right",
    Bottom = "bottom",
  }

  export enum MarkerType {
    Arrow = "arrow",
    ArrowClosed = "arrowclosed",
  }

  export interface EdgeMarker {
    type: MarkerType | "arrow" | "arrowclosed";
    color?: string;
    width?: number;
    height?: number;
  }

  export interface XYPosition {
    x: number;
    y: number;
  }

  export interface Node<
    TData extends Record<string, unknown> = Record<string, unknown>,
    TType extends string | undefined = string | undefined
  > {
    id: string;
    position: XYPosition;
    data: TData;
    type?: TType;
    style?: React.CSSProperties;
    className?: string;
  }

  export interface Edge<
    TData extends Record<string, unknown> = Record<string, unknown>
  > {
    id: string;
    source: string;
    target: string;
    type?: string;
    label?: React.ReactNode;
    labelStyle?: React.CSSProperties;
    labelBgStyle?: React.CSSProperties;
    animated?: boolean;
    markerEnd?: EdgeMarker | string;
    style?: React.CSSProperties;
    data?: TData;
  }

  export interface NodeProps<TNode extends Node = Node> {
    id: string;
    data: TNode["data"];
    selected?: boolean;
    type?: string;
  }

  export type NodeMouseHandler<TNode extends Node = Node> = (
    event: React.MouseEvent,
    node: TNode
  ) => void;

  export interface HandleProps {
    type: "source" | "target";
    position: Position;
    id?: string;
    className?: string;
    style?: React.CSSProperties;
  }

  export const Handle: React.ComponentType<HandleProps>;

  export interface BackgroundProps {
    color?: string;
    gap?: number | [number, number];
    size?: number;
    className?: string;
  }

  export const Background: React.ComponentType<BackgroundProps>;

  export interface ControlsProps {
    showZoom?: boolean;
    showFitView?: boolean;
    showInteractive?: boolean;
    className?: string;
  }

  export const Controls: React.ComponentType<ControlsProps>;

  export interface MiniMapProps<TNode extends Node = Node> {
    pannable?: boolean;
    zoomable?: boolean;
    nodeColor?: string | ((node: TNode) => string);
    className?: string;
  }

  export const MiniMap: React.ComponentType<MiniMapProps<any>>;

  export interface ReactFlowProps<
    TNode extends Node = Node<any>,
    TEdge extends Edge = Edge<any>
  > {
    nodes?: TNode[];
    edges?: TEdge[];
    nodeTypes?: Record<string, React.ComponentType<any>>;
    onNodeClick?: NodeMouseHandler<TNode>;
    onPaneClick?: (event: React.MouseEvent) => void;
    fitView?: boolean;
    minZoom?: number;
    maxZoom?: number;
    proOptions?: { hideAttribution?: boolean };
    children?: React.ReactNode;
  }

  export function ReactFlow<
    TNode extends Node = Node<any>,
    TEdge extends Edge = Edge<any>
  >(props: ReactFlowProps<TNode, TEdge>): React.ReactElement | null;
}
