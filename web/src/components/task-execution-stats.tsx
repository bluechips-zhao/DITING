"use client";

import * as React from "react";
import { http } from "@/lib/api";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";

type Direction = {
  intent_id: string; summary: string; state: string;
  model_calls: number; model_running: number; model_failed: number;
  model_interrupted: number; model_elapsed_ms: number; unknown_elapsed_calls: number;
  input_tokens: number; output_tokens: number; unknown_usage_calls: number;
  tool_results: number; tool_errors: number; confirmed_facts: number;
  confirmed_findings: number; candidate_findings: number;
};

export function TaskExecutionStats({ taskId }: { taskId: string }) {
  const [rows, setRows] = React.useState<Direction[] | null>(null);
  const [error, setError] = React.useState("");
  React.useEffect(() => {
    let active = true;
    let loading = false;
    setRows(null);
    setError("");
    const load = async () => {
      if (loading) return;
      loading = true;
      try {
        const result = await http<{ directions: Direction[] }>(`/tasks/${taskId}/execution-stats`);
        if (active) { setRows(result.directions); setError(""); }
      } catch (e) {
        if (active) setError(e instanceof Error ? e.message : "统计读取失败");
      } finally { loading = false; }
    };
    void load();
    const timer = setInterval(() => void load(), 5000);
    return () => { active = false; clearInterval(timer); };
  }, [taskId]);
  return <Card>
    <CardHeader><CardTitle className="text-sm">方向执行统计</CardTitle></CardHeader>
    <CardContent className="space-y-3 text-sm">
      <p className="text-xs text-muted-foreground">模型调用从 2.6.0 开始记录，按供应商 SDK 方法调用计数，不含 SDK 内部的 HTTP 重试。累计模型耗时可包含重叠调用，不代表方向的实际经过时间。部分用量仅是已观察下限；未知用量请求的预算估算见任务累计预算。确认事实、确认漏洞和待确认候选按当前实际记录分别统计。</p>
      {error && <p role="alert" className="text-destructive">{error}；保留上次统计。</p>}
      {!rows && !error && <p className="text-muted-foreground">正在读取统计…</p>}
      {rows?.length === 0 && <p className="text-muted-foreground">尚无方向或模型调用记录。</p>}
      {rows && rows.length > 0 && <div className="overflow-x-auto"><table className="w-full text-left text-xs">
        <caption className="sr-only">各方向模型调用、耗时和已写回产出</caption>
        <thead><tr>{["方向", "模型调用", "累计模型耗时", "已观察 Token", "工具结果", "确认事实 / 漏洞 / 候选"].map(label => <th key={label} scope="col" className="p-2 font-medium">{label}</th>)}</tr></thead>
        <tbody>{rows.map(row => <tr key={row.intent_id} className="border-t">
          <th scope="row" className="max-w-64 p-2 font-normal">{row.intent_id !== "0" && `#${row.intent_id} `}{row.summary || "未命名方向"}</th>
          <td className="p-2">{row.model_calls}（进行中 {row.model_running} / 失败 {row.model_failed} / 中断 {row.model_interrupted}）</td>
          <td className="p-2">{(row.model_elapsed_ms / 1000).toFixed(1)}s；未知 {row.unknown_elapsed_calls}</td>
          <td className="p-2">{(row.input_tokens + row.output_tokens).toLocaleString()}；未知 {row.unknown_usage_calls}</td>
          <td className="p-2">{row.tool_results} / 错误 {row.tool_errors}</td>
          <td className="p-2">{row.confirmed_facts} / {row.confirmed_findings} / {row.candidate_findings}</td>
        </tr>)}</tbody>
      </table></div>}
    </CardContent>
  </Card>;
}
