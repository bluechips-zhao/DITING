"use client";

import * as React from "react";
import { api } from "@/lib/api";
import type { FindingRetestComparison as Comparison } from "@/lib/types";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";

const verdicts = { reproduced: "仍可复现", fixed: "已修复", inconclusive: "无法确认", "": "尚无结论" };

export function FindingRetestComparison({ findingId, retestId, onClose }: {
  findingId: string; retestId: number; onClose: () => void;
}) {
  const [data, setData] = React.useState<Comparison | null>(null);
  const [error, setError] = React.useState("");
  const [revision, setRevision] = React.useState(0);
  React.useEffect(() => {
    let active = true;
    setData(null); setError("");
    api.findingRetestComparison(findingId, retestId)
      .then(value => { if (active) setData(value); })
      .catch((e: unknown) => { if (active) setError(e instanceof Error ? e.message : "读取失败"); });
    return () => { active = false; };
  }, [findingId, retestId, revision]);

  return <Dialog open onOpenChange={open => { if (!open) onClose(); }}>
    <DialogContent className="max-h-[85vh] overflow-y-auto sm:max-w-4xl">
      <DialogHeader>
        <DialogTitle>复测 #{retestId} · 证据对比</DialogTitle>
        <DialogDescription>原始证据来自复测开始时的快照；当前记录的变化不覆盖历史结果。</DialogDescription>
      </DialogHeader>
      {error ? <div role="alert" className="text-destructive text-sm">{error} <Button variant="outline" size="sm" onClick={() => setRevision(v => v + 1)}>重试</Button></div> : null}
      {!data && !error ? <p role="status">正在读取对比…</p> : null}
      {data ? <>
        <p className="text-muted-foreground text-sm">发现 #{data.finding_id} · 快照时间 {new Date(data.created_at).toLocaleString("zh-CN")} · 当前证据版本 {data.current_evidence_version}</p>
        {data.source_evidence_changed === true ? <p role="note" className="rounded-md border p-3 text-sm">当前证据版本已变化。本次复测结论针对开始时的证据，不能自动代表最新证据已验证。</p> : null}
        {data.source_evidence_changed === null ? <p className="text-muted-foreground text-sm">历史记录未保存可比较的证据版本，版本变化无法确认。</p> : null}
        <div className="grid min-w-0 gap-4 md:grid-cols-2">
          <section className="min-w-0 rounded-md border p-3">
            <h3 className="mb-2 font-medium">复测开始时</h3>
            {data.baseline ? <>
              <p className="text-sm">{data.baseline.name || "未命名发现"} · {data.baseline.status || "状态未记录"}</p>
              <p className="text-muted-foreground text-xs">证据版本 {data.baseline.evidence_version ?? "未记录"} · 报告使用版本 {data.baseline.report_evidence_version ?? "未记录"}</p>
              <p className="my-3 whitespace-pre-wrap break-words text-sm">{data.baseline.summary || "未保存概述"}</p>
              <pre className="max-h-80 overflow-auto whitespace-pre-wrap break-words text-xs">{data.baseline.evidence || "未保存原始证据"}</pre>
            </> : <p className="text-muted-foreground text-sm">此历史记录没有原始快照，不能重建原始证据。</p>}
          </section>
          <section className="min-w-0 rounded-md border p-3">
            <h3 className="mb-2 font-medium">本次复测结果</h3>
            <p className="text-sm">{data.status === "completed" ? verdicts[data.verdict] : "复测尚未完成，不形成最终结论"}</p>
            <p className="my-3 whitespace-pre-wrap break-words text-sm">{data.summary || "尚无结果概述"}</p>
            <pre className="max-h-80 overflow-auto whitespace-pre-wrap break-words text-xs">{data.evidence || "尚无复测证据"}</pre>
          </section>
        </div>
      </> : null}
    </DialogContent>
  </Dialog>;
}
