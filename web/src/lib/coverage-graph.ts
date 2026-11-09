import type { CoverageGraphNode, CoverageGraphEdge } from "./types";

export const FOLD_LIMIT = 20;
export const FOLD_STEP = 20;
type Kind = CoverageGraphNode["kind"];
export type FoldNode = {
  fold: true; key: string; groupId: string; parentKey: string;
  kind: Kind; hidden: CoverageGraphNode[];
};
export type AssetRenderNode = { fold: false; key: string; kind: Kind; node: CoverageGraphNode };
export type RenderNode = AssetRenderNode | FoldNode;

export function computeVisible(
  nodes: CoverageGraphNode[], edges: CoverageGraphEdge[], expanded: Map<string, number>,
): { renderNodes: RenderNode[]; renderEdges: CoverageGraphEdge[] } {
  const byKey = new Map(nodes.map(node => [node.key, node]));
  const uniqueNodes = [...byKey.values()];
  const parents = new Set<string>();
  const children = new Map<string, string[]>();
  const pairs = new Map<string, Set<string>>();
  const validEdges: CoverageGraphEdge[] = [];
  for (const edge of edges) {
    if (!byKey.has(edge.src) || !byKey.has(edge.dst)) continue;
    let destinations = pairs.get(edge.src);
    if (!destinations) { destinations = new Set(); pairs.set(edge.src, destinations); }
    if (destinations.has(edge.dst)) continue;
    destinations.add(edge.dst);
    validEdges.push(edge);
    parents.add(edge.src);
    const siblings = children.get(edge.dst);
    if (siblings) siblings.push(edge.src);
    else children.set(edge.dst, [edge.src]);
  }

  const roots = uniqueNodes.filter(node => !parents.has(node.key)).map(node => node.key);
  const covered = new Set<string>();
  const cover = (root: string) => {
    const pending = [root];
    covered.add(root);
    for (let i = 0; i < pending.length; i++) {
      for (const child of children.get(pending[i]) ?? []) {
        if (covered.has(child)) continue;
        covered.add(child); pending.push(child);
      }
    }
  };
  for (const root of roots) cover(root);
  // A disconnected cycle has no parentless root. Add one representative, then
  // apply the same fold rules; do not silently lose the entire component.
  for (const node of uniqueNodes) {
    if (!covered.has(node.key)) { roots.push(node.key); cover(node.key); }
  }

  const queue = [...roots];
  const visible = new Set(roots);
  const folds: FoldNode[] = [];
  for (let i = 0; i < queue.length; i++) {
    const parent = queue[i];
    const groups = new Map<Kind, CoverageGraphNode[]>();
    for (const key of children.get(parent) ?? []) {
      const node = byKey.get(key)!;
      const siblings = groups.get(node.kind);
      if (siblings) siblings.push(node);
      else groups.set(node.kind, [node]);
    }
    for (const [kind, siblings] of groups) {
      siblings.sort((a,b) => a.tested !== b.tested ? (a.tested ? -1 : 1) : (a.label || "").localeCompare(b.label || ""));
      const groupId = `${parent}::${kind}`;
      const shown = expanded.get(groupId) ?? FOLD_LIMIT;
      for (const node of siblings.slice(0,shown)) {
        if (!visible.has(node.key)) { visible.add(node.key); queue.push(node.key); }
      }
      const hidden = siblings.slice(shown);
      if (hidden.length) folds.push({ fold:true,key:`fold:${groupId}`,groupId,parentKey:parent,kind,hidden });
    }
  }

  const renderNodes: RenderNode[] = uniqueNodes.filter(node=>visible.has(node.key)).map(node=>({fold:false,key:node.key,kind:node.kind,node}));
  const renderEdges = validEdges.filter(edge=>visible.has(edge.src)&&visible.has(edge.dst));
  for (const fold of folds) {
    // A multi-parent asset may already be visible through another parent.
    fold.hidden = fold.hidden.filter(node=>!visible.has(node.key));
    if (!fold.hidden.length) continue;
    renderNodes.push(fold); renderEdges.push({src:fold.key,dst:fold.parentKey});
  }
  return { renderNodes, renderEdges };
}
