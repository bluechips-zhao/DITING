"use client";

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { CheckCircle2, Circle, ArrowRight, Loader2 } from "lucide-react";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { LoadError } from "@/components/load-error";

type Status = Awaited<ReturnType<typeof api.environment>>;

export default function SetupWizard() {
  const [status, setStatus] = useState<Status | null>(null);
  const [configured, setConfigured] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);
  const [savedId, setSavedId] = useState<number | null>(null);
  const [name, setName] = useState("");
  const [format, setFormat] = useState("openai");
  const [model, setModel] = useState("");
  const [url, setURL] = useState("");
  const [key, setKey] = useState("");
  const [saveError, setSaveError] = useState("");
  const load = useCallback(async () => {
    setLoading(true); setError("");
    try {
      const [env, llm] = await Promise.all([api.environment(), api.getLLM()]);
      setStatus(env); setConfigured(llm.configured);
    } catch (e) { setError(e instanceof Error ? e.message : "初始化状态读取失败"); }
    finally { setLoading(false); }
  }, []);
  useEffect(() => { void load(); }, [load]);
  const ready = status?.credential_key_ready && status.environment.checks.filter(c => c.required).every(c => c.ok);
  async function save(e: React.FormEvent) {
    e.preventDefault(); setSaving(true); setSaveError("");
    try {
      let id = savedId;
      if (!id) {
        const result = await api.saveLLMProfile({ name: name.trim(), format, model: model.trim(), base_url: url.trim(), api_key: key, streaming: true });
        id = result.id; setSavedId(id); setKey("");
      }
      await api.activateLLMProfile(String(id));
      setConfigured(true);
    } catch (e) { setSaveError(e instanceof Error ? e.message : "模型配置保存失败"); }
    finally { setSaving(false); }
  }
  return <div className="mx-auto w-full max-w-5xl space-y-6 py-4">
    <div><p className="text-xs uppercase tracking-[0.18em] text-primary">DITING · 开始使用</p><h1 className="mt-2 text-3xl font-semibold tracking-tight">完成你的工作环境</h1><p className="mt-2 text-sm text-muted-foreground">数据库与登录密码已就绪。检查运行环境，然后配置自己的模型。</p></div>
    <div className="flex flex-wrap gap-3 text-sm">{[{ name: "数据库与密码", done: true }, { name: "环境与密钥", done: !!ready }, { name: "模型配置", done: configured }].map(step => <span key={step.name} className="inline-flex items-center gap-2 rounded-full border px-3 py-1.5">{step.done ? <CheckCircle2 className="size-4 text-primary" /> : <Circle className="size-4 text-muted-foreground" />}{step.name}</span>)}</div>
    {error && <LoadError message={error} retry={load} busy={loading} />}
    {loading && !status && <p className="flex items-center gap-2 text-sm text-muted-foreground"><Loader2 className="size-4 animate-spin" />正在检查环境…</p>}
    <div className="grid gap-6 lg:grid-cols-2">
      <Card><CardHeader><CardTitle>本机环境</CardTitle><CardDescription>只检测本地状态，不调用模型或目标。</CardDescription></CardHeader><CardContent className="space-y-3">
        {status && <><p className="text-sm">{status.environment.os} / {status.environment.arch} · PostgreSQL {status.database_version}</p><div className="flex justify-between gap-3 border-b py-3 text-sm"><span>独立凭据加密密钥</span><span className={status.credential_key_ready ? "text-primary" : "text-destructive"}>{status.credential_key_ready ? "就绪" : "需检查 secrets.key"}</span></div>{status.environment.checks.map(c => <div key={c.name} className="flex justify-between gap-4 border-b py-3 text-sm"><div>{c.name}<p className="mt-1 text-xs text-muted-foreground">{c.detail}</p></div><span className={`shrink-0 ${c.ok ? "text-primary" : c.required ? "text-destructive" : "text-muted-foreground"}`}>{c.ok ? "通过" : c.required ? "需处理" : "可选"}</span></div>)}</>}
        <Button variant="outline" onClick={() => void load()} disabled={loading}>{loading ? "检测中…" : "重新检测"}</Button><p className="text-xs text-muted-foreground">Git、Python、Bash 和 Docker 只在对应功能中需要；没有这些工具也可以先配置模型。请备份应用配置目录中的 secrets.key。</p>
      </CardContent></Card>
      <Card><CardHeader><CardTitle>{configured ? "模型已配置" : "添加第一个模型"}</CardTitle><CardDescription>保存配置不会发送模型请求，不产生模型调用费用。</CardDescription></CardHeader><CardContent>
        {configured ? <div className="space-y-5"><p className="text-sm text-muted-foreground">已有默认模型可供应用使用。配置保存成功只证明本地配置就绪；真实连接测试可在 LLM 页面手动发起，可能产生费用。</p><Button asChild><Link href="/function/tasks">进入任务工作台<ArrowRight className="ml-2 size-4" /></Link></Button><Button asChild variant="outline" className="ml-2"><Link href="/system/llm">管理模型</Link></Button></div> : <form onSubmit={save} className="space-y-4">
          <fieldset disabled={saving || !!savedId} className="space-y-4">
            <div className="space-y-1.5"><Label htmlFor="setup-name">配置名称</Label><Input id="setup-name" required value={name} onChange={e => setName(e.target.value)} placeholder="例如：我的工作模型" /></div>
            <div className="space-y-1.5"><Label htmlFor="setup-format">接口格式</Label><select id="setup-format" value={format} onChange={e => setFormat(e.target.value)} className="h-10 w-full rounded-md border bg-background px-3 text-sm"><option value="openai">OpenAI Chat Completions 兼容</option><option value="openai-responses">OpenAI Responses 兼容</option><option value="anthropic">Anthropic Messages 兼容</option></select></div>
            <div className="space-y-1.5"><Label htmlFor="setup-model">模型标识</Label><Input id="setup-model" required value={model} onChange={e => setModel(e.target.value)} placeholder="填写供应商提供的完整模型标识" /></div>
            <div className="space-y-1.5"><Label htmlFor="setup-url">API Base URL</Label><Input id="setup-url" type="url" value={url} onChange={e => setURL(e.target.value)} placeholder="第三方接口请填写其 API 入口" /><p className="text-xs text-muted-foreground">使用兼容服务时填写供应商入口；留空采用所选接口格式的默认供应商入口。</p></div>
            <div className="space-y-1.5"><Label htmlFor="setup-key">API Key</Label><Input id="setup-key" type="password" autoComplete="new-password" value={key} onChange={e => setKey(e.target.value)} placeholder="供应商密钥；无需鉴权的本地服务可留空" /></div>
          </fieldset>
          {saveError && <p role="alert" className="text-sm text-destructive">{saveError}</p>}
          {savedId && <p className="text-xs text-muted-foreground">配置已保存，正在设为默认。重试只激活已保存的配置，不重复创建。</p>}
          <Button type="submit" disabled={saving || !ready}>{saving ? "保存中…" : savedId ? "重试设为默认模型" : "保存并设为默认模型"}</Button>
          <p className="text-xs text-muted-foreground">更详细的代理、限速和推理选项可稍后在 LLM 页面设置。</p>
        </form>}
      </CardContent></Card>
    </div>
    <div className="flex flex-wrap items-center justify-between gap-3 rounded-xl border bg-muted/30 p-4 text-sm"><p className="text-muted-foreground">也可以先浏览空白工作台，稍后返回本向导。</p><Button variant="outline" asChild><Link href="/function/tasks">稍后配置模型</Link></Button></div>
  </div>;
}
