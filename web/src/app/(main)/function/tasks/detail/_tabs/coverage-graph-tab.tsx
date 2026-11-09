"use client";

import * as React from "react";
import type { Graph as G6Graph } from "@antv/g6";
import {
  AppWindow,
  Building2,
  Globe,
  Link2,
  type LucideIcon,
  Radio,
  RefreshCw,
  Server,
  Waypoints,
} from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Card, CardContent } from "@/components/ui/card";
import { ScrollArea } from "@/components/ui/scroll-area";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { api } from "@/lib/api";
import { usePreferencesStore } from "@/stores/preferences/preferences-provider";
import type { CoverageAssetRef, CoverageAssetRefs, CoverageGraphEdge, CoverageGraphNode } from "@/lib/types";
import { cn } from "@/lib/utils";

// 每个父节点下、同一类型的子节点默认展示的数量；超出折叠，"展示更多"每次再拉这么多。
import { computeVisible, FOLD_LIMIT, FOLD_STEP, type FoldNode, type RenderNode } from "@/lib/coverage-graph";

type Kind = CoverageGraphNode["kind"];

type KindMeta = { label: string; icon: LucideIcon; iconBg: string; hex: string; size: number };

const kindMeta: Record<Kind, KindMeta> = {
  company: { label: "公司", icon: Building2, iconBg: "bg-slate-500", hex: "#64748b", size: 46 },
  root_domain: { label: "根域名", icon: Globe, iconBg: "bg-indigo-500", hex: "#6366f1", size: 38 },
  subdomain: { label: "子域名", icon: Waypoints, iconBg: "bg-blue-500", hex: "#3b82f6", size: 30 },
  ip: { label: "IP", icon: Server, iconBg: "bg-cyan-600", hex: "#0891b2", size: 28 },
  service: { label: "服务", icon: Radio, iconBg: "bg-amber-500", hex: "#f59e0b", size: 26 },
  app: { label: "App", icon: AppWindow, iconBg: "bg-fuchsia-500", hex: "#d946ef", size: 26 },
  endpoint: { label: "端点", icon: Link2, iconBg: "bg-rose-500", hex: "#f43f5e", size: 20 },
};

// G6 节点图标用平台一致的 lucide 图标：把 lucide 的 SVG 路径（v1.22）渲染成白色描边的
// data URI，作为节点 iconSrc（白色在实色/灰色底上都清晰）。手写内嵌，避免 react-dom/server
// 在 React19/Next 客户端打包的问题。
function svgUri(inner: string, filled = false): string {
  const attrs = filled
    ? 'fill="#fff" stroke="none"'
    : 'fill="none" stroke="#fff" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"';
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" ${attrs}>${inner}</svg>`;
  return `data:image/svg+xml,${encodeURIComponent(svg)}`;
}

const KIND_ICON: Record<Kind, string> = {
  company: svgUri(
    '<path d="M10 12h4"/><path d="M10 8h4"/><path d="M14 21v-3a2 2 0 0 0-4 0v3"/><path d="M6 10H4a2 2 0 0 0-2 2v7a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2V9a2 2 0 0 0-2-2h-2"/><path d="M6 21V5a2 2 0 0 1 2-2h8a2 2 0 0 1 2 2v16"/>',
  ),
  root_domain: svgUri(
    '<circle cx="12" cy="12" r="10"/><path d="M12 2a14.5 14.5 0 0 0 0 20 14.5 14.5 0 0 0 0-20"/><path d="M2 12h20"/>',
  ),
  subdomain: svgUri(
    '<path d="m10.586 5.414-5.172 5.172"/><path d="m18.586 13.414-5.172 5.172"/><path d="M6 12h12"/><circle cx="12" cy="20" r="2"/><circle cx="12" cy="4" r="2"/><circle cx="20" cy="12" r="2"/><circle cx="4" cy="12" r="2"/>',
  ),
  ip: svgUri(
    '<rect width="20" height="8" x="2" y="2" rx="2" ry="2"/><rect width="20" height="8" x="2" y="14" rx="2" ry="2"/><line x1="6" x2="6.01" y1="6" y2="6"/><line x1="6" x2="6.01" y1="18" y2="18"/>',
  ),
  service: svgUri(
    '<path d="M16.247 7.761a6 6 0 0 1 0 8.478"/><path d="M19.075 4.933a10 10 0 0 1 0 14.134"/><path d="M4.925 19.067a10 10 0 0 1 0-14.134"/><path d="M7.753 16.239a6 6 0 0 1 0-8.478"/><circle cx="12" cy="12" r="2"/>',
  ),
  app: svgUri(
    '<rect x="2" y="4" width="20" height="16" rx="2"/><path d="M10 4v4"/><path d="M2 8h20"/><path d="M6 4v4"/>',
  ),
  endpoint: svgUri(
    '<path d="M9 17H7A5 5 0 0 1 7 7h2"/><path d="M15 7h2a5 5 0 1 1 0 10h-2"/><line x1="8" x2="16" y1="12" y2="12"/>',
  ),
};

const FOLD_ICON = svgUri(
  '<circle cx="5" cy="12" r="1.6"/><circle cx="12" cy="12" r="1.6"/><circle cx="19" cy="12" r="1.6"/>',
  true,
);

// ---------------------------------------------------------------------------
// Folding: full graph → currently-visible node/edge set (自顶向下级联折叠)。
// ---------------------------------------------------------------------------
// G6 数据映射。自定义字段放节点顶层（G6 v5 官方 force 示例约定：style/layout 回调直接读 d.<field>）。
// ---------------------------------------------------------------------------
type G6NodeDatum = {
  id: string;
  kind: Kind;
  fold: boolean;
  tested: boolean;
  inScope: boolean;
  lbl: string;
  size: number;
};

function trunc(s: string, n = 26): string {
  return s.length > n ? `${s.slice(0, n - 1)}…` : s;
}

// 图里的节点文案：服务不显示完整 URL，只显示 端口·标题·状态码；端点只显示 path。
// 其余类型沿用后端给的 label。
function graphLabel(n: CoverageGraphNode): string {
  if (n.kind === "service") {
    const parts: string[] = [];
    if (n.port) parts.push(`:${n.port}`);
    if (n.page_title) parts.push(n.page_title);
    if (n.status_code) parts.push(String(n.status_code));
    return parts.length ? parts.join(" · ") : n.domain || n.ip || n.label;
  }
  if (n.kind === "endpoint" && n.url) {
    try {
      return new URL(n.url).pathname || "/";
    } catch {
      /* 非法 URL：回退到完整 label */
    }
  }
  return n.label;
}

function toG6Nodes(renderNodes: RenderNode[]): G6NodeDatum[] {
  return renderNodes.map((rn) => {
    if (rn.fold) {
      return {
        id: rn.key,
        kind: rn.kind,
        fold: true,
        tested: false,
        inScope: false,
        lbl: `还有 ${rn.hidden.length} 个${kindMeta[rn.kind].label}`,
        size: 24,
      };
    }
    return {
      id: rn.key,
      kind: rn.kind,
      fold: false,
      tested: rn.node.tested,
      inScope: rn.node.in_scope,
      lbl: trunc(graphLabel(rn.node) || kindMeta[rn.kind].label),
      size: kindMeta[rn.kind].size,
    };
  });
}

// G6 的类型把自定义字段归在 data 下，但官方 force 示例（及运行时）按顶层读 d.<field>。
// 回调形参用 unknown 满足 G6 签名，内部用 nd() 强转回我们的顶层结构。
const nd = (d: unknown) => d as G6NodeDatum;

// 已测=实色高亮；范围内未测=灰；范围外/折叠=更淡的灰 + 虚线描边。
function nodeFill(d: G6NodeDatum): string {
  if (d.fold) return "#f1f5f9";
  if (!d.inScope) return "#e2e8f0";
  if (d.tested) return kindMeta[d.kind].hex;
  return "#94a3b8";
}
function nodeStroke(d: G6NodeDatum): string {
  if (d.fold || !d.inScope) return "#94a3b8";
  if (d.tested) return "#0f766e";
  return "#64748b";
}

// ---------------------------------------------------------------------------
// 抽屉：资产节点看详情；折叠节点看隐藏列表 + "展示更多"。
// ---------------------------------------------------------------------------
function DetailRow({ label, children }: { label: string; children: React.ReactNode }) {
  if (children === undefined || children === null || children === "") return null;
  return (
    <div className="flex items-start gap-3 py-1 text-sm">
      <span className="text-muted-foreground w-16 shrink-0">{label}</span>
      <span className="text-foreground min-w-0 flex-1 break-words">{children}</span>
    </div>
  );
}

function RefList({ title, items }: { title: string; items: CoverageAssetRef[] }) {
  if (items.length === 0) return null;
  return (
    <div>
      <h4 className="text-muted-foreground mb-1 text-xs font-medium">
        {title}（{items.length}）
      </h4>
      <div className="flex flex-col gap-1">
        {items.map((r) => (
          <div
            key={`${r.kind}-${r.id}`}
            className="flex flex-wrap items-start gap-2 rounded-md bg-muted/50 px-2 py-1.5 text-xs"
          >
            <span className="shrink-0 font-mono text-muted-foreground">#{r.id}</span>
            {r.state && <span className="shrink-0 text-muted-foreground">{r.state}</span>}
            <span className="min-w-32 flex-1 break-words text-foreground">{r.summary || "—"}</span>
            {r.inherited && r.source_task_id && (
              <Badge variant="outline" className="shrink-0">
                来源 #{r.source_task_id} · 只读
              </Badge>
            )}
          </div>
        ))}
      </div>
    </div>
  );
}

function AssetSheet({
  node,
  taskId,
  onOpenChange,
  onReturnFocus,
}: {
  node: CoverageGraphNode | null;
  taskId: string;
  onOpenChange: (open: boolean) => void;
  onReturnFocus: () => void;
}) {
  const meta = node ? kindMeta[node.kind] : null;
  const Icon = meta?.icon;
  const raw = node ? JSON.stringify(node, null, 2) : "";
  const [refs, setRefs] = React.useState<CoverageAssetRefs | null>(null);

  React.useEffect(() => {
    setRefs(null);
    if (!node?.asset_id) return;
    let cancelled = false;
    api
      .taskAssetRefs(taskId, node.asset_id)
      .then((r) => {
        if (!cancelled) setRefs(r);
      })
      .catch(() => {
        /* 无关联或出错：不展示该区块 */
      });
    return () => {
      cancelled = true;
    };
  }, [node, taskId]);
  return (
    <Sheet open={node !== null} onOpenChange={onOpenChange}>
      <SheetContent onCloseAutoFocus={event => {event.preventDefault();onReturnFocus();}} side="right" className="flex w-full flex-col gap-0 p-0 data-[side=right]:sm:max-w-md">
        {node && meta && Icon && (
          <>
            <SheetHeader className="border-b p-4">
              <div className="flex items-center gap-2.5 pr-8">
                <span className={cn("flex size-8 shrink-0 items-center justify-center rounded-lg shadow-sm", meta.iconBg)}>
                  <Icon className="size-4 text-white" />
                </span>
                <div className="min-w-0">
                  <SheetTitle className="leading-tight">{meta.label}</SheetTitle>
                  <span className="text-muted-foreground truncate font-mono text-xs" title={node.label}>
                    {node.label}
                  </span>
                </div>
              </div>
            </SheetHeader>
            <ScrollArea className="min-h-0 flex-1">
              <div className="flex w-full min-w-0 flex-col gap-4 p-4">
                <section>
                  <h4 className="text-muted-foreground mb-1 text-xs font-medium">属性</h4>
                  <DetailRow label="类型">{meta.label}</DetailRow>
                  <DetailRow label="测试状态">
                    {node.in_scope ? (
                      node.tested ? (
                        <span className="text-emerald-600 dark:text-emerald-400">已测试</span>
                      ) : (
                        <span className="text-neutral-500">未测试</span>
                      )
                    ) : (
                      <span className="text-neutral-400">范围外（连接节点）</span>
                    )}
                  </DetailRow>
                  <DetailRow label="域名">{node.domain}</DetailRow>
                  <DetailRow label="根域名">{node.root_domain}</DetailRow>
                  <DetailRow label="IP">{node.ip}</DetailRow>
                  <DetailRow label="端口">{node.port ? node.port : undefined}</DetailRow>
                  <DetailRow label="URL">
                    {node.url ? <span className="font-mono text-xs break-all">{node.url}</span> : undefined}
                  </DetailRow>
                  <DetailRow label="标题">{node.page_title}</DetailRow>
                  <DetailRow label="状态码">{node.status_code ? node.status_code : undefined}</DetailRow>
                  <DetailRow label="App">{node.app_name}</DetailRow>
                  <DetailRow label="资产ID">
                    {node.asset_id ? <span className="font-mono text-xs">{node.asset_id}</span> : undefined}
                  </DetailRow>
                </section>
                {refs && (refs.intents.length > 0 || refs.facts.length > 0 || refs.findings.length > 0) && (
                  <section className="flex flex-col gap-3 border-t pt-3">
                    <RefList title="关联意图" items={refs.intents} />
                    <RefList title="关联事实" items={refs.facts} />
                    <RefList title="关联发现" items={refs.findings} />
                  </section>
                )}
                <section className="border-t pt-3">
                  <h4 className="text-muted-foreground mb-1.5 text-xs font-medium">原始数据</h4>
                  <pre className="bg-muted/50 text-foreground max-w-full overflow-hidden rounded-md border p-3 font-mono text-xs leading-relaxed break-all whitespace-pre-wrap">
                    {raw}
                  </pre>
                </section>
              </div>
            </ScrollArea>
          </>
        )}
      </SheetContent>
    </Sheet>
  );
}

function CoverageNodePicker({ nodes, onPick }: { nodes: CoverageGraphNode[]; onPick: (node: CoverageGraphNode, target: HTMLButtonElement) => void }) {
  const [query, setQuery] = React.useState("");
  const [page, setPage] = React.useState(0);
  const filtered = React.useMemo(() => {
    const needle = query.trim().toLocaleLowerCase();
    return needle ? nodes.filter(node => node.label.toLocaleLowerCase().includes(needle)) : nodes;
  }, [nodes, query]);
  const pages = Math.max(1, Math.ceil(filtered.length / 50));
  const current = Math.min(page, pages - 1);
  return <div className="flex flex-col gap-2 p-3">
    <Input aria-label="搜索节点名称" placeholder="按名称搜索全部节点" value={query} onChange={event => {setQuery(event.target.value);setPage(0);}} />
    <div className="flex items-center justify-between gap-2 text-xs">
      <span role="status">共 {filtered.length} 个节点 · 第 {current + 1}/{pages} 页</span>
      <div className="flex gap-1">
        <Button size="sm" variant="outline" disabled={current === 0} onClick={() => setPage(current - 1)}>上一页</Button>
        <Button size="sm" variant="outline" disabled={current + 1 >= pages} onClick={() => setPage(current + 1)}>下一页</Button>
      </div>
    </div>
    {!filtered.length && <p className="text-muted-foreground py-3 text-sm">没有匹配的节点</p>}
    {filtered.slice(current * 50, (current + 1) * 50).map(node => {
      const meta = kindMeta[node.kind]; const Icon = meta.icon;
      return <button key={node.key} data-coverage-node type="button" onClick={event => onPick(node,event.currentTarget)} className="hover:bg-accent flex items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm">
        <span className={cn("flex size-5 shrink-0 items-center justify-center rounded",node.tested?meta.iconBg:"bg-neutral-400 dark:bg-neutral-600")}><Icon className="size-3 text-white"/></span>
        <span className="min-w-0 flex-1 truncate font-mono text-xs" title={node.label}>{node.label}</span>
        {node.tested && <span className="size-1.5 shrink-0 rounded-full bg-emerald-500"/>}
      </button>;
    })}
  </div>;
}

function FoldSheet({
  fold,
  onOpenChange,
  onShowMore,
  onPick,
  onReturnFocus,
}: {
  fold: FoldNode | null;
  onOpenChange: (open: boolean) => void;
  onShowMore: (groupId: string) => void;
  onPick: (n: CoverageGraphNode) => void;
  onReturnFocus: () => void;
}) {
  const meta = fold ? kindMeta[fold.kind] : null;
  const Icon = meta?.icon;
  return (
    <Sheet open={fold !== null} onOpenChange={onOpenChange}>
      <SheetContent onCloseAutoFocus={event => {event.preventDefault();onReturnFocus();}} side="right" className="flex w-full flex-col gap-0 p-0 data-[side=right]:sm:max-w-md">
        {fold && meta && Icon && (
          <>
            <SheetHeader className="border-b p-4">
              <SheetTitle className="text-base">
                未展示的{meta.label}（{fold.hidden.length}）
              </SheetTitle>
              <p className="text-muted-foreground text-xs">已测优先展示。点「展示更多」把下一批拉进图里。</p>
            </SheetHeader>
            <ScrollArea className="min-h-0 flex-1">
              <CoverageNodePicker key={fold.key} nodes={fold.hidden} onPick={onPick}/>
            </ScrollArea>
            <div className="border-t p-3">
              <Button className="w-full" variant="outline" onClick={() => onShowMore(fold.groupId)}>
                展示更多（+{FOLD_STEP}）
              </Button>
            </div>
          </>
        )}
      </SheetContent>
    </Sheet>
  );
}

// ---------------------------------------------------------------------------
function GraphInner({ taskId, coverageEnabled = true }: { taskId: string; coverageEnabled?: boolean }) {
  const [data, setData] = React.useState<{
    nodes: CoverageGraphNode[];
    edges: CoverageGraphEdge[];
  } | null>(null);
  const [expanded, setExpanded] = React.useState<Map<string, number>>(new Map());
  const [selectedAsset, setSelectedAsset] = React.useState<CoverageGraphNode | null>(null);
  const [selectedFoldId, setSelectedFoldId] = React.useState<string | null>(null);
  const [nodeListOpen, setNodeListOpen] = React.useState(false);
  const [zoomPercent, setZoomPercent] = React.useState(100);
  const dark = usePreferencesStore(state => state.resolvedThemeMode === "dark");
  const darkRef = React.useRef(dark);
  darkRef.current = dark;
  const [loading, setLoading] = React.useState(true);
  const [dataError, setDataError] = React.useState<string | null>(null);
  const requestVersion = React.useRef(0);
  const [rendering, setRendering] = React.useState(false);
  const [graphError, setGraphError] = React.useState<string | null>(null);
  const renderQueues = React.useRef(new WeakMap<G6Graph, { running: boolean; pending: boolean; topology?: string }>());

  const containerRef = React.useRef<HTMLDivElement>(null);
  const graphRef = React.useRef<G6Graph | null>(null);
  const focusReturn = React.useRef<HTMLElement | null>(null);
  const nodeListButton = React.useRef<HTMLButtonElement | null>(null);
  const restoreFocus = React.useCallback(() => {
    const target = focusReturn.current;
    (target?.isConnected ? target : nodeListButton.current)?.focus();
  }, []);
  // click 处理需要最新的 key→RenderNode 映射（G6 事件回调闭包外读 ref）。
  const renderMapRef = React.useRef<Map<string, RenderNode>>(new Map());
  const gDataRef = React.useRef<{ nodes: G6NodeDatum[]; edges: { source: string; target: string }[] }>({
    nodes: [],
    edges: [],
  });

  const fetchGraph = React.useCallback(() => {
    const version = ++requestVersion.current;
    setLoading(true);
    api
      .taskCoverageGraph(taskId)
      .then((g) => {
        if (version !== requestVersion.current) return;
        setDataError(null);
        // 资产覆盖度功能关闭(B1)：仍出图(范围内资产/company 关联依旧可见)，但抹平
        // tested 状态——不显示测试进度、不做已测高亮。
        const nodes = coverageEnabled ? (g.nodes ?? []) : (g.nodes ?? []).map((n) => ({ ...n, tested: false }));
        setData({ nodes, edges: g.edges ?? [] });
      })
      .catch(() => {if(version === requestVersion.current)setDataError("资产图读取失败，请重试；已保留上次数据。");})
      .finally(() => {if(version === requestVersion.current)setLoading(false);});
  }, [taskId, coverageEnabled]);

  React.useEffect(() => {
    fetchGraph();
    return () => {requestVersion.current++;};
  }, [fetchGraph]);
  const sourceNodesByKey = React.useMemo(() => new Map((data?.nodes ?? []).map(node => [node.key,node])), [data]);
  React.useEffect(() => {
    setSelectedAsset(current => current ? sourceNodesByKey.get(current.key) ?? null : null);
  }, [sourceNodesByKey]);

  const { renderNodes, renderEdges } = React.useMemo(
    () =>
      data
        ? computeVisible(data.nodes, data.edges, expanded)
        : { renderNodes: [] as RenderNode[], renderEdges: [] as CoverageGraphEdge[] },
    [data, expanded],
  );

  const graphData = React.useMemo(() => ({
    nodes: toG6Nodes(renderNodes),
    edges: renderEdges.map((edge) => ({ source: edge.src, target: edge.dst })),
  }), [renderNodes, renderEdges]);
  // Include labels, scope, hidden counts and exact edge endpoints. The previous
  // key/edge-count signature missed changes that kept the same item counts.
  const sig = React.useMemo(() => JSON.stringify({graphData,dark}), [graphData,dark]);
  gDataRef.current = graphData;
  const rmap = new Map<string, RenderNode>();
  for (const rn of renderNodes) rmap.set(rn.key, rn);
  renderMapRef.current = rmap;

  const applyData = React.useCallback(() => {
    const graph = graphRef.current;
    if (!graph || graph.destroyed) return;
    let queue = renderQueues.current.get(graph);
    if (!queue) {
      queue = { running: false, pending: false };
      renderQueues.current.set(graph, queue);
    }
    queue.pending = true;
    if (queue.running) return;
    queue.running = true;
    const state = queue;
    setRendering(true);
    void (async () => {
      try {
        while (state.pending && !graph.destroyed && graphRef.current === graph) {
          state.pending = false;
          const snapshot = gDataRef.current;
          const topology = JSON.stringify({nodes:snapshot.nodes.map(node => [node.id,node.kind,node.size]),edges:snapshot.edges});
          graph.setData(snapshot);
          try {
            // Compute the same 300 force iterations before drawing, instead of
            // repainting thousands of labels on every simulation tick.
            if (snapshot.nodes.length && state.topology !== topology) await graph.render();
            else await graph.draw();
            state.topology = topology;
            if (!graph.destroyed && graphRef.current === graph) setGraphError(null);
          } catch {
            if (!graph.destroyed && graphRef.current === graph) setGraphError("资产图布局失败，请刷新重试。");
          }
        }
      } finally {
        state.running = false;
        if (!graph.destroyed && graphRef.current === graph) setRendering(false);
      }
    })();
  }, []);

  // 建图（一次）。动态 import 避开 SSR/静态导出期的 window 依赖。
  React.useEffect(() => {
    let destroyed = false;
    let graph: G6Graph | null = null;
    void (async () => {
      const { Graph } = await import("@antv/g6");
      if (destroyed || !containerRef.current) return;
      graph = new Graph({
        container: containerRef.current,
        autoResize: true,
        autoFit: { type: "view", options: { when: "always" }, animation: false },
        padding: 24,
        animation: false,
        background: "transparent",
        node: {
          style: {
            size: (d: unknown) => nd(d).size,
            fill: (d: unknown) => nodeFill(nd(d)),
            stroke: (d: unknown) => nodeStroke(nd(d)),
            lineWidth: (d: unknown) => (nd(d).fold || !nd(d).inScope ? 1 : 1.5),
            lineDash: (d: unknown) => (nd(d).fold || !nd(d).inScope ? [3, 3] : [0]),
            iconSrc: (d: unknown) => (nd(d).fold ? FOLD_ICON : KIND_ICON[nd(d).kind]),
            iconWidth: (d: unknown) => Math.max(12, nd(d).size * 0.55),
            iconHeight: (d: unknown) => Math.max(12, nd(d).size * 0.55),
            labelText: (d: unknown) => nd(d).lbl,
            labelFontSize: 10,
            labelPlacement: "bottom",
            labelFill: () => darkRef.current ? "#cbd5e1" : "#475569",
            labelBackground: true,
            labelBackgroundFill: () => darkRef.current ? "rgba(17,20,27,0.85)" : "rgba(255,255,255,0.75)",
            labelBackgroundRadius: 3,
            labelPadding: [1, 3],
          },
        },
        edge: {
          style: { stroke: "#cbd5e1", lineWidth: 1, endArrow: false },
        },
        layout: {
          type: "d3-force",
          animation: false,
          preLayout: true,
          iterations: 300,
          collide: { radius: (d: unknown) => (nd(d).size ? nd(d).size : 20) + 8 },
          link: {
            distance: (edge: unknown) => {
              // 顶层（公司/根域名）离子节点远一点，叶子近一点。edge.source 可能是 id 或已解析节点。
              const s = (edge as { source: string | { id?: string } }).source;
              const srcId = typeof s === "string" ? s : (s?.id ?? "");
              const src = renderMapRef.current.get(srcId);
              const k = src && !src.fold ? src.kind : "endpoint";
              return k === "company" || k === "root_domain" ? 120 : 60;
            },
          },
          manyBody: {
            strength: (d: unknown) => (nd(d).kind === "endpoint" || nd(d).fold ? -60 : -200),
          },
        },
        behaviors: [{type:"drag-element",animation:false}, {type:"drag-canvas",animation:false}, {type:"zoom-canvas",animation:false}],
      });
      graph.on("aftertransform", () => {
        if (graph && !graph.destroyed) setZoomPercent(Math.round(graph.getZoom() * 100));
      });
      graph.on("node:click", (evt: unknown) => {
        const id = (evt as { target?: { id?: string } }).target?.id;
        if (!id) return;
        const rn = renderMapRef.current.get(id);
        if (!rn) return;
        if (rn.fold) setSelectedFoldId(rn.key);
        else {focusReturn.current = nodeListButton.current;setSelectedAsset(rn.node);}
      });
      graphRef.current = graph;
      applyData();
    })();
    return () => {
      destroyed = true;
      // 先停布局再销毁:尽量缩短"布局在飞、context 被清空"的竞态窗口。
      try {
        graph?.stopLayout();
      } catch {
        /* 图可能尚未建成或已无布局上下文 */
      }
      graph?.destroy();
      graphRef.current = null;
    };
  }, [applyData]);

  // 可见集合变化 → 重新灌数据 + 布局。sig 只作为重排触发器（applyData 读 gDataRef）。
  // biome-ignore lint/correctness/useExhaustiveDependencies: sig 是刻意的重排触发依赖
  React.useEffect(() => {
    applyData();
  }, [sig, applyData]);

  const showMore = React.useCallback((groupId: string) => {
    setExpanded((prev) => {
      const m = new Map(prev);
      m.set(groupId, (m.get(groupId) ?? FOLD_LIMIT) + FOLD_STEP);
      return m;
    });
  }, []);

  const selectedFold =
    selectedFoldId != null
      ? ((renderNodes.find((n) => n.fold && n.key === selectedFoldId) as FoldNode | undefined) ?? null)
      : null;
  React.useEffect(() => {
    if (selectedFoldId != null && !selectedFold) setSelectedFoldId(null);
  }, [selectedFoldId, selectedFold]);

  const total = data?.nodes.length ?? 0;
  const tested = data?.nodes.filter((n) => n.in_scope && n.tested).length ?? 0;
  const inScope = data?.nodes.filter((n) => n.in_scope).length ?? 0;

  return (
    <div className="flex h-full w-full flex-col">

      {/* 图例 + 统计 + 刷新（叠加层） */}
      <div className="bg-card flex shrink-0 flex-col gap-2 border-b p-3 text-xs">
        <div className="flex flex-wrap items-center justify-between gap-2">
          {total > 0 ? (
            <span className="text-muted-foreground">
              范围内 <span className="text-foreground font-semibold tabular-nums">{inScope}</span>
              {coverageEnabled && (
                <>
                  {" "}
                  · 已测{" "}
                  <span className="font-semibold tabular-nums text-emerald-600 dark:text-emerald-400">{tested}</span>
                </>
              )}
            </span>
          ) : (
            <span className="text-muted-foreground">{loading ? "加载中…" : "暂无范围内资产（先锚定任务范围）"}</span>
          )}
          <div className="ml-auto flex flex-wrap items-center gap-1">
            <Button variant="outline" size="sm" ref={nodeListButton} aria-expanded={nodeListOpen} aria-controls="coverage-node-list" onClick={event => {focusReturn.current=event.currentTarget;setNodeListOpen(value => !value);}}>节点列表</Button>
            <Button variant="outline" size="sm" onClick={() => {const graph=graphRef.current;if(graph&&!graph.destroyed)void graph.fitView({when:"always"},false).catch(()=>setGraphError("画布适应失败，请重试。"));}}>适应画布</Button>
            <Button variant="outline" size="sm" aria-label="放大资产图" onClick={() => {const graph=graphRef.current;if(graph&&!graph.destroyed)void graph.zoomBy(1.25,false).catch(()=>setGraphError("缩放失败，请重试。"));}}>＋</Button>
            <Button variant="outline" size="sm" aria-label="缩小资产图" onClick={() => {const graph=graphRef.current;if(graph&&!graph.destroyed)void graph.zoomBy(0.8,false).catch(()=>setGraphError("缩放失败，请重试。"));}}>－</Button>
            <span aria-label="画布缩放比例" className="min-w-10 text-center tabular-nums text-muted-foreground">{zoomPercent}%</span>
          <Button variant="ghost" size="icon" className="size-6" onClick={fetchGraph} title="刷新" aria-label="刷新资产图" disabled={loading}>
            <RefreshCw className={cn("size-3.5", loading && "animate-spin")} />
          </Button>
          </div>
        </div>
        {rendering && <span role="status" className="text-muted-foreground">正在布局资产图…</span>}
        {dataError && <p role="alert" className="text-destructive">{dataError}</p>}
        {graphError && <p role="alert" className="text-destructive">{graphError}</p>}
        <div className="flex flex-wrap gap-x-3 gap-y-1.5">
          {(Object.keys(kindMeta) as Kind[]).map((k) => {
            const m = kindMeta[k];
            const Icon = m.icon;
            return (
              <span key={k} className="text-foreground inline-flex items-center gap-1.5">
                <span className={cn("flex size-4 items-center justify-center rounded", m.iconBg)}>
                  <Icon className="size-2.5 text-white" />
                </span>
                {m.label}
              </span>
            );
          })}
        </div>
        <div className="border-border/60 text-muted-foreground flex flex-wrap gap-x-3 gap-y-1.5 border-t pt-2">
          {coverageEnabled && (
            <>
              <span className="inline-flex items-center gap-1.5">
                <span className="size-3 rounded-full bg-emerald-500" /> 已测（高亮）
              </span>
              <span className="inline-flex items-center gap-1.5">
                <span className="size-3 rounded-full bg-neutral-400" /> 未测
              </span>
            </>
          )}
          <span className="inline-flex items-center gap-1.5">
            <span className="size-3 rounded-full border border-dashed border-neutral-400 bg-neutral-200" /> 范围外
          </span>
        </div>
        {renderNodes.some(node => node.fold) && <select aria-label="查看折叠分组" className="max-w-full rounded-md border bg-background px-2 py-1.5 text-xs" value={selectedFoldId ?? ""} onChange={event => {if(event.target.value){focusReturn.current=event.currentTarget;setSelectedFoldId(event.target.value);}}}>
          <option value="">查看折叠分组</option>
          {renderNodes.filter((node):node is FoldNode => node.fold).map(fold => <option key={fold.key} value={fold.key}>{sourceNodesByKey.get(fold.parentKey)?.label ?? "未命名分组"} · {kindMeta[fold.kind].label}（{fold.hidden.length}）</option>)}
        </select>}
        <p className="text-muted-foreground/80 border-border/60 border-t pt-2 leading-relaxed">
          力导向布局，计算完成后显示；可拖拽节点、滚轮缩放；灰色「⋯」是折叠节点，点开可展开更多。
        </p>
      </div>

      {nodeListOpen && <section id="coverage-node-list" aria-label="全部图节点" className="max-h-56 shrink-0 overflow-auto border-b"><CoverageNodePicker nodes={data?.nodes ?? []} onPick={(node,target) => {focusReturn.current=target;setSelectedAsset(node);}}/></section>}
      <div ref={containerRef} className="min-h-0 flex-1 bg-muted/30" />
      <AssetSheet node={selectedAsset} taskId={taskId} onReturnFocus={restoreFocus} onOpenChange={(o) => !o && setSelectedAsset(null)} />
      <FoldSheet
        fold={selectedFold}
        onReturnFocus={() => {if(!selectedAsset)restoreFocus();}}
        onOpenChange={(o) => !o && setSelectedFoldId(null)}
        onShowMore={showMore}
        onPick={(n) => {
          setSelectedFoldId(null);
          setSelectedAsset(n);
        }}
      />
    </div>
  );
}

export function CoverageGraphTab({ taskId, coverageEnabled = true }: { taskId: string; coverageEnabled?: boolean }) {
  return (
    <Card>
      <CardContent className="p-0">
        <div className="h-[72vh] w-full overflow-hidden rounded-xl">
          <GraphInner taskId={taskId} coverageEnabled={coverageEnabled} />
        </div>
      </CardContent>
    </Card>
  );
}
