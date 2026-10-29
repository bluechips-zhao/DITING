"use client";

import * as React from "react";
import { api } from "@/lib/api";
import type { CompanyReportVersion } from "@/lib/types";
import { Button } from "@/components/ui/button";

export function CompanyReportHistory({ companyId, revision }: { companyId: string; revision: number }) {
  const [rows, setRows] = React.useState<CompanyReportVersion[]>([]);
  const [before, setBefore] = React.useState<string>();
  const [cursors, setCursors] = React.useState<(string | undefined)[]>([]);
  const [selected, setSelected] = React.useState("");
  const [detail, setDetail] = React.useState<CompanyReportVersion>();
  const [loading, setLoading] = React.useState(false);
  const [error, setError] = React.useState("");
  const [refresh, setRefresh] = React.useState(0);
  const [deleting, setDeleting] = React.useState(false);
  const [confirmDelete, setConfirmDelete] = React.useState(false);

  React.useEffect(() => {
    const controller = new AbortController();
    let active = true;
    setLoading(true); setError(""); setSelected(""); setDetail(undefined); setRows([]);
    api.companyReportHistory(companyId, before, controller.signal)
      .then(value => { if (active) setRows(value); })
      .catch((e: unknown) => { if (active) setError(e instanceof Error ? e.message : "读取历史失败"); })
      .finally(() => { if (active) setLoading(false); });
    return () => { active = false; controller.abort(); };
  }, [companyId, before, revision, refresh]);

  React.useEffect(() => {
    setDetail(undefined); setConfirmDelete(false);
    if (!selected) return;
    const controller = new AbortController();
    let active = true;
    setError("");
    api.companyReportVersion(companyId, selected, controller.signal)
      .then(value => { if (active) setDetail(value); })
      .catch((e: unknown) => { if (active) setError(e instanceof Error ? e.message : "读取版本失败"); });
    return () => { active = false; controller.abort(); };
  }, [companyId, selected, refresh]);

  function download() {
    if (!detail?.content) return;
    const url = URL.createObjectURL(new Blob([detail.content], { type: "text/markdown;charset=utf-8" }));
    const link = document.createElement("a");
    link.href = url; link.download = `company-${companyId}-report-${detail.id}-redacted.md`;
    document.body.appendChild(link); link.click(); link.remove();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  }

  async function remove() {
    if (!detail || deleting) return;
    setDeleting(true); setError("");
    try {
      await api.deleteCompanyReportVersion(companyId, detail.id);
      setSelected(""); setDetail(undefined); setConfirmDelete(false); setRefresh(v => v + 1);
    } catch (e) { setError(e instanceof Error ? e.message : "删除失败"); }
    finally { setDeleting(false); }
  }

  return <section className="space-y-3 border-t pt-4" aria-label="企业报告历史">
    <h3 className="text-sm font-medium">已保存的脱敏版本</h3>
    <p className="text-muted-foreground text-xs">保存当时的数据与模板，后续记录变化不会覆盖。企业删除时其报告历史一并删除。</p>
    {loading ? <p role="status">正在读取历史…</p> : null}
    {error ? <p role="alert" className="text-destructive text-sm">{error} <Button size="sm" variant="outline" onClick={() => setRefresh(v => v + 1)}>重试</Button></p> : null}
    <label className="block text-sm">历史版本
      <select className="mt-1 w-full rounded-md border bg-background p-2" value={selected} disabled={deleting || loading} onChange={e => { setDetail(undefined); setSelected(e.target.value); }}>
        <option value="">{rows.length ? "选择一个版本" : "本页暂无已保存版本"}</option>
        {rows.map(row => <option key={row.id} value={row.id}>#{row.id} · {row.template === "technical" ? "技术明细" : "管理摘要"} · {new Date(row.snapshot_at).toLocaleString()}</option>)}
      </select>
    </label>
    <div className="flex gap-2">
      <Button size="sm" variant="outline" disabled={loading || deleting || cursors.length === 0} onClick={() => { setBefore(cursors[cursors.length - 1]); setCursors(v => v.slice(0,-1)); }}>上一页</Button>
      <Button size="sm" variant="outline" disabled={loading || deleting || rows.length < 50} onClick={() => { setCursors(v => [...v,before]); setBefore(rows[rows.length - 1].id); }}>下一页</Button>
      <Button size="sm" variant="outline" disabled={!detail?.content || deleting} onClick={download}>下载此版本</Button>
      <Button size="sm" variant="outline" disabled={!detail || deleting} onClick={() => setConfirmDelete(v => !v)}>删除此版本</Button>
    </div>
    {confirmDelete ? <div className="flex items-center gap-2 text-sm" role="alert">删除后无法恢复该历史副本。<Button size="sm" variant="destructive" disabled={deleting} onClick={remove}>确认删除</Button><Button size="sm" variant="outline" disabled={deleting} onClick={() => setConfirmDelete(false)}>取消</Button></div> : null}
    {detail ? <pre className="max-h-[40vh] overflow-auto whitespace-pre-wrap break-words rounded-md border p-3 text-xs">{detail.content}</pre> : null}
  </section>;
}
