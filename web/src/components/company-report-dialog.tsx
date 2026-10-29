"use client";

import * as React from "react";
import { FileTextIcon, DownloadIcon } from "lucide-react";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { CompanyReportHistory } from "@/components/company-report-history";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";

export function CompanyReportDialog({ companyId, companyName }: { companyId: string; companyName: string }) {
  const [open, setOpen] = React.useState(false);
  const [template, setTemplate] = React.useState<"technical" | "executive">("technical");
  const [redacted, setRedacted] = React.useState(true);
  const [content, setContent] = React.useState("");
  const [loading, setLoading] = React.useState(false);
  const [error, setError] = React.useState("");
  const [revision, setRevision] = React.useState(0);
  const [historyRevision, setHistoryRevision] = React.useState(0);
  const [saving, setSaving] = React.useState(false);
  const [saveMessage, setSaveMessage] = React.useState("");

  async function save() {
    if (saving) return;
    setSaving(true); setSaveMessage("");
    try {
      const saved = await api.saveCompanyReport(companyId, template);
      setHistoryRevision(v => v + 1);
      setSaveMessage(`已保存脱敏版本 #${saved.id}，内容以保存时的数据为准。`);
    } catch (e) { setSaveMessage(e instanceof Error ? e.message : "保存失败"); }
    finally { setSaving(false); }
  }

  React.useEffect(() => {
    if (!open) return;
    const controller = new AbortController();
    let active = true;
    setLoading(true); setError(""); setContent("");
    api.companyReport(companyId, template, redacted, controller.signal)
      .then(value => { if (active) setContent(value); })
      .catch((e: unknown) => { if (active) setError(e instanceof Error ? e.message : "生成报告失败"); })
      .finally(() => { if (active) setLoading(false); });
    return () => { active = false; controller.abort(); };
  }, [open, companyId, template, redacted, revision]);

  function download() {
    if (!content || loading || error) return;
    const url = URL.createObjectURL(new Blob([content], { type: "text/markdown;charset=utf-8" }));
    const link = document.createElement("a");
    link.href = url;
    link.download = `company-${companyId}-${template}${redacted ? "-redacted" : ""}.md`;
    document.body.appendChild(link); link.click(); link.remove();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  }

  return <>
    <Button variant="outline" size="sm" onClick={() => { setContent(""); setSaveMessage(""); setRedacted(true); setOpen(true); }}><FileTextIcon />报告</Button>
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogContent className="max-h-[85vh] overflow-y-auto sm:max-w-4xl">
        <DialogHeader>
          <DialogTitle>{companyName} · 企业报告</DialogTitle>
          <DialogDescription>汇总本企业当前归属的资产与发现，保留跨任务来源、证据版本和最新复测记录。生成过程不调用模型。</DialogDescription>
        </DialogHeader>
        <div className="flex flex-wrap items-center gap-4">
          <label className="flex items-center gap-2 text-sm">模板
            <select className="rounded-md border bg-background p-2" value={template} onChange={event => { setContent(""); setSaveMessage(""); setTemplate(event.target.value as typeof template); }}>
              <option value="technical">技术明细</option><option value="executive">管理摘要</option>
            </select>
          </label>
          <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={redacted} onChange={event => { setContent(""); setSaveMessage(""); setRedacted(event.target.checked); }} />脱敏副本</label>
          <Button className="ml-auto" size="sm" onClick={download} disabled={!content || loading || !!error}><DownloadIcon />下载 Markdown</Button>
          <Button variant="outline" size="sm" onClick={save} disabled={saving || loading || !content || !!error}> {saving ? "保存中…" : "保存脱敏版本"}</Button>
        </div>
        {saveMessage ? <p role="status" className="text-sm">{saveMessage}</p> : null}
        {loading ? <p role="status" className="text-muted-foreground text-sm">正在汇总数据快照…</p> : null}
        {error ? <div role="alert" className="text-destructive text-sm">{error} <Button variant="outline" size="sm" onClick={() => setRevision(v => v + 1)}>重试</Button></div> : null}
        {content ? <pre className="max-h-[55vh] overflow-auto whitespace-pre-wrap break-words rounded-md border bg-muted/20 p-4 text-xs">{content}</pre> : null}
        {open ? <CompanyReportHistory key={`${companyId}:${historyRevision}`} companyId={companyId} revision={historyRevision} /> : null}
      </DialogContent>
    </Dialog>
  </>;
}
