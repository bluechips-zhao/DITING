"use client";

import * as React from "react";
import { http } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";

type Candidate = {
  id: string; intent_id: string; activity_id: string; tool: string;
  tool_use_id: string; reason: string; state: "pending" | "reviewed" | "dismissed";
};
type Page = { candidates: Candidate[]; has_more: boolean };

export function TaskEvidenceCandidates({ taskId }: { taskId: string }) {
  const [rows, setRows] = React.useState<Candidate[] | null>(null);
  const [hasMore, setHasMore] = React.useState(false);
  const [error, setError] = React.useState("");
  const [busy, setBusy] = React.useState(false);
  const [source, setSource] = React.useState<{ id: string; output: string; truncated: boolean } | null>(null);
  const generation = React.useRef(0);
  const load = React.useCallback(async (before?: string) => {
    const current = generation.current;
    setBusy(true);
    try {
      const page = await http<Page>(`/tasks/${taskId}/evidence-candidates${before ? `?before=${before}` : ""}`);
      if (current !== generation.current) return;
      setRows(old => before ? [...(old ?? []), ...page.candidates.filter(row => !old?.some(v => v.id === row.id))] : page.candidates);
      setHasMore(page.has_more); setError("");
    } catch (e) {
      if (current === generation.current) setError(e instanceof Error ? e.message : "候选读取失败");
    } finally { if (current === generation.current) setBusy(false); }
  }, [taskId]);
  React.useEffect(() => {
    generation.current++; setRows(null); setSource(null); setError(""); setHasMore(false);
    void load();
    return () => { generation.current++; };
  }, [load]);
  const act = async (row: Candidate, state?: "reviewed" | "dismissed") => {
    const current = generation.current; setBusy(true);
    try {
      const path = `/tasks/${taskId}/evidence-candidates/${row.id}`;
      if (state) {
        await http(path, { method: "PATCH", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ state }) });
        if (current === generation.current) setRows(old => old?.map(v => v.id === row.id ? { ...v, state } : v) ?? null);
      } else {
        const result = await http<{ output: string; truncated: boolean }>(`${path}/source`);
        if (current === generation.current) setSource({ id: row.activity_id, ...result });
      }
      if (current === generation.current) setError("");
    } catch (e) {
      if (current === generation.current) setError(e instanceof Error ? e.message : "操作失败");
    } finally { if (current === generation.current) setBusy(false); }
  };
  return <Card>
    <CardHeader className="flex flex-row items-center justify-between"><CardTitle className="text-sm">待核对的证据候选</CardTitle><Button size="sm" variant="outline" disabled={busy} onClick={() => void load()}>刷新候选</Button></CardHeader>
    <CardContent className="space-y-3 text-sm">
      <p className="text-xs text-muted-foreground">工具命中信号或零写回方向的输出仅作为线索。请检查活动保存的工具输出、目标和对照结果；标记已核对不会创建确认漏洞或发送漏洞通知。确认漏洞仍需通过漏洞登记流程保存完整证据。</p>
      {error && <p role="alert" className="text-destructive">{error}；保留上次候选。</p>}
      {!rows && !error && <p className="text-muted-foreground">正在读取候选…</p>}
      {rows?.length === 0 && <p className="text-muted-foreground">暂无候选；这不代表测试覆盖完整。</p>}
      {rows?.map(row => <div key={row.id} className="space-y-2 rounded-md border p-3">
        <p>{row.reason}</p>
        <p className="text-xs text-muted-foreground">方向 #{row.intent_id} · 来源活动 #{row.activity_id} · {row.tool || "工具"} · {({ pending: "待核对", reviewed: "已核对", dismissed: "已忽略" })[row.state]}</p>
        <div className="flex flex-wrap gap-2"><Button size="sm" variant="outline" disabled={busy} onClick={() => void act(row)}>查看来源输出</Button>
          {row.state === "pending" && <><Button size="sm" disabled={busy} onClick={() => void act(row, "reviewed")}>标记已核对</Button><Button size="sm" variant="ghost" disabled={busy} onClick={() => void act(row, "dismissed")}>忽略</Button></>}
        </div>
      </div>)}
      {hasMore && rows && <Button variant="outline" disabled={busy} onClick={() => void load(rows.at(-1)?.id)}>加载更多候选</Button>}
      <Dialog open={source !== null} onOpenChange={open => { if (!open) setSource(null); }}>
        <DialogContent className="sm:max-w-2xl">
          <DialogHeader>
            <DialogTitle>候选来源输出</DialogTitle>
            <DialogDescription>来源活动 #{source?.id}{source?.truncated ? "（仅显示前 32768 字符）" : ""}。这是活动保存的工具输出，内容尚需核对。</DialogDescription>
          </DialogHeader>
          <pre className="max-h-[60vh] overflow-auto whitespace-pre-wrap break-words text-xs">{source?.output}</pre>
        </DialogContent>
      </Dialog>
    </CardContent>
  </Card>;
}
