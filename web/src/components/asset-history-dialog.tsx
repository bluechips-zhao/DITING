"use client";

import * as React from "react";
import { HistoryIcon } from "lucide-react";
import { api } from "@/lib/api";
import type { AssetChange } from "@/lib/types";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";

const fieldLabels: Record<string, string> = {
  type: "资产类型", company_id: "所属企业 ID", company_source: "归属方式", task_ids: "关联任务 ID",
  domain: "域名", root_domain: "根域名", ip: "IP 地址", port: "端口", bound_domains: "绑定域名",
  open_ports: "开放端口", record_type: "解析类型", record_value: "解析值", bundle_id: "应用标识",
  app_name: "应用名称", category: "应用分类", url: "地址", service_type: "服务类型",
  service_name: "服务名称", status_code: "响应状态码", technologies: "技术栈", method: "请求方法",
};
const typeLabels: Record<string, string> = { root_domain: "根域名", subdomain: "子域名", ip: "IP", app: "应用", service: "服务", endpoint: "接口" };
function value(value: unknown, key: string): string {
  if (value === undefined || value === null || value === "" || (Array.isArray(value) && !value.length)) return "未记录";
  if (Array.isArray(value)) return value.map(item => typeof item === "object" ? JSON.stringify(item) : String(item)).join("、");
  if (key === "type" && typeof value === "string") return typeLabels[value] ?? value;
  if (key === "company_source") return value === "explicit" ? "手动指定" : value === "scope" ? "范围匹配" : String(value);
  return typeof value === "string" ? value : JSON.stringify(value);
}
function meaningful(value: unknown) { return value !== null && value !== undefined && value !== "" && (!Array.isArray(value) || value.length > 0); }

export function AssetHistoryDialog({ assetId }: { assetId: string }) {
  const [open, setOpen] = React.useState(false);
  const [rows, setRows] = React.useState<AssetChange[]>([]);
  const [before, setBefore] = React.useState<string>();
  const [cursors, setCursors] = React.useState<(string | undefined)[]>([]);
  const [loading, setLoading] = React.useState(false);
  const [error, setError] = React.useState("");
  const [revision, setRevision] = React.useState(0);

  React.useEffect(() => {
    if (!open) return;
    const controller = new AbortController();
    let active = true;
    setRows([]); setLoading(true); setError("");
    api.assetHistory(assetId, before, controller.signal)
      .then(value => { if (active) setRows(value); })
      .catch((e: unknown) => { if (active) setError(e instanceof Error ? e.message : "读取变化失败"); })
      .finally(() => { if (active) setLoading(false); });
    return () => { active = false; controller.abort(); };
  }, [open, assetId, before, revision]);

  return <>
    <Button variant="ghost" size="icon" className="size-7" aria-label={`查看资产 ${assetId} 的变化记录`} onClick={() => { setBefore(undefined); setCursors([]); setOpen(true); }}><HistoryIcon className="size-3.5" /></Button>
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogContent className="max-h-[85vh] overflow-y-auto sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>资产 #{assetId} · 变化记录</DialogTitle>
          <DialogDescription>按数据库观察时间倒序。任务 ID 是当时的关联集合，不代表该任务造成变化。仅记录启用后的所选资产字段，不保存认证信息容器；已识别的凭据已脱敏，自定义敏感内容仍需人工核对。删除资产会清理其历史。</DialogDescription>
        </DialogHeader>
        {loading ? <p role="status">正在读取…</p> : null}
        {error ? <p role="alert" className="text-destructive">{error} <Button variant="outline" size="sm" onClick={() => setRevision(v => v + 1)}>重试</Button></p> : null}
        {!loading && !error && !rows.length ? <p className="text-muted-foreground text-sm">暂无变化记录。</p> : null}
        {rows.map(row => {
          const previous = row.previous ?? {};
          const keys = Object.keys(row.current).filter(key => row.previous
            ? JSON.stringify(previous[key]) !== JSON.stringify(row.current[key])
            : meaningful(row.current[key])).sort();
          return <section key={row.id} className="space-y-2 rounded-md border p-3">
            <h3 className="text-sm font-medium">#{row.id} · {row.kind === "baseline" ? "启用时基线（更早历史未知）" : row.kind === "created" ? "新增" : "字段变化"} · {new Date(row.observed_at).toLocaleString()}</h3>
            <div className="overflow-x-auto"><table className="w-full text-left text-xs"><thead><tr><th className="p-1">字段</th><th className="p-1">此前</th><th className="p-1">此次观察</th></tr></thead><tbody>
              {keys.map(key => <tr key={key} className="border-t"><td className="p-1">{fieldLabels[key] ?? key}</td><td className="max-w-64 break-words whitespace-pre-wrap p-1">{row.previous ? value(previous[key], key) : "无历史快照"}</td><td className="max-w-64 break-words whitespace-pre-wrap p-1">{value(row.current[key], key)}</td></tr>)}
            </tbody></table></div>
          </section>;
        })}
        <div className="flex gap-2">
          <Button variant="outline" size="sm" disabled={loading || !cursors.length} onClick={() => { setBefore(cursors[cursors.length-1]); setCursors(v => v.slice(0,-1)); }}>上一页</Button>
          <Button variant="outline" size="sm" disabled={loading || rows.length < 50} onClick={() => { setCursors(v => [...v,before]); setBefore(rows[rows.length-1].id); }}>下一页</Button>
        </div>
      </DialogContent>
    </Dialog>
  </>;
}
