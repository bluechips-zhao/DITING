"use client";

import * as React from "react";

import { CheckIcon, CopyIcon, FileTextIcon, DownloadIcon, RefreshCwIcon } from "lucide-react";
import { toast } from "sonner";

import { Markdown } from "@/components/markdown";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { api } from "@/lib/api";
import { copyText } from "@/lib/utils";

export function ReportTab({ taskId }: { taskId: string }) {
  const [report, setReport] = React.useState<string>("");
  const [loading, setLoading] = React.useState(true);
  const [copied, setCopied] = React.useState(false);
	const [error, setError] = React.useState("");
	const [updatedAt, setUpdatedAt] = React.useState<Date | null>(null);
	const [revision, setRevision] = React.useState(0);

  React.useEffect(() => {
    let active = true;
    setLoading(true);
	setError("");
    api
      .report(taskId)
      .then((text) => {
        if (active) { setReport(text); setUpdatedAt(new Date()); }
      })
      .catch((err: unknown) => {
        if (active) setError(err instanceof Error ? err.message : "报告读取失败");
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [taskId, revision]);
	React.useEffect(() => { setReport(""); setUpdatedAt(null); }, [taskId]);
	function download() {
		const url = URL.createObjectURL(new Blob([report], { type: "text/markdown;charset=utf-8" }));
		const a = document.createElement("a"); a.href = url; a.download = `task-${taskId}-report.md`; a.click();
		URL.revokeObjectURL(url);
	}

  async function copy() {
    if (!report) return;
    const ok = await copyText(report);
    if (ok) {
      setCopied(true);
      toast.success("已复制 Markdown");
      setTimeout(() => setCopied(false), 1500);
    } else {
      toast.error("复制失败，请手动选择文本复制");
    }
  }

  let content: React.ReactNode;
  if (loading) {
    content = (
      <div className="flex flex-col items-center justify-center gap-2 rounded-md border border-dashed py-16 text-muted-foreground text-sm">
        <FileTextIcon className="size-8 opacity-40" />
        加载中…
      </div>
    );
  } else if (report) {
    content = (
      <div className="max-h-[60vh] overflow-auto rounded-md border bg-muted/20 p-4">
        <Markdown text={report} />
      </div>
    );
  } else {
    content = (
      <div className="flex flex-col items-center justify-center gap-2 rounded-md border border-dashed py-16 text-muted-foreground text-sm">
        <FileTextIcon className="size-8 opacity-40" />
        暂无报告
      </div>
    );
  }

  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between">
        <CardTitle className="flex items-center gap-2 text-sm">
          <FileTextIcon className="size-4" /> 渗透测试报告（Markdown）
        </CardTitle>
        <div className="flex gap-2">
		  <Button size="sm" variant="outline" disabled={loading} onClick={() => setRevision(v => v + 1)}><RefreshCwIcon />刷新</Button>
		  {report && <Button size="sm" variant="outline" onClick={download}><DownloadIcon />下载</Button>}
          {report && (
            <Button size="sm" variant="outline" onClick={copy}>
              {copied ? <CheckIcon /> : <CopyIcon />} 复制
            </Button>
          )}
        </div>
      </CardHeader>
      <CardContent>
		{error && <p role="alert" className="mb-3 text-sm text-destructive">{error}。点击刷新重试{report ? "；当前显示上次成功读取的报告。" : "。"}</p>}
		{updatedAt && <p className="mb-3 text-xs text-muted-foreground">读取于 {updatedAt.toLocaleString()}</p>}
		{!error || report ? content : null}
	  </CardContent>
    </Card>
  );
}
