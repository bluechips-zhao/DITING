"use client";
import * as React from "react";
import { toast } from "sonner";
import { http } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Card,CardContent,CardHeader,CardTitle } from "@/components/ui/card";
type Budget={token_limit:number;tool_limit:number;reserve_tokens:number;reserve_tools:number;tokens_used:number;tokens_reserved:number;tools_used:number;unreported_requests:number;state:string};
const fields=[['token_limit','累计 Token 上限'],['reserve_tokens','Token 收尾预留'],['tool_limit','累计工具调用上限'],['reserve_tools','工具收尾预留']] as const;
export function TaskBudgetPanel({taskId}:{taskId:string}){
 const[data,setData]=React.useState<Budget|null>(null),[draft,setDraft]=React.useState<Record<string,string>>({}),[editing,setEditing]=React.useState(false),[error,setError]=React.useState(""),[busy,setBusy]=React.useState(false);
 const load=React.useCallback(async()=>{try{setData(await http<Budget>(`/tasks/${taskId}/budget`));setError("")}catch(e){setError(e instanceof Error?e.message:"预算读取失败")}},[taskId]);
 React.useEffect(()=>{void load();const t=setInterval(()=>void load(),5000);return()=>clearInterval(t)},[load]);
 return <Card><CardHeader><CardTitle className="text-sm">任务累计预算</CardTitle></CardHeader><CardContent className="space-y-3">
 <p className="text-xs text-muted-foreground">0 表示不限。包含规划、Worker、主对话和带任务归属的报告会话。请求前按上下文估算预留，完整结束后按提供方用量结算；断流、提前停止或缺失用量时保守计入预估额度，已返回的部分用量更高时保留较高值。这些估算不是精确账单。达到阈值停止新增工作，预留用于已有证据写回和后台收割。</p>
 {data&&<div className="flex flex-wrap gap-3 text-sm"><span>状态 {data.state}</span><span>Token {data.tokens_used.toLocaleString()} / {data.token_limit||"不限"}</span><span>请求预留 {data.tokens_reserved.toLocaleString()}</span><span>工具 {data.tools_used} / {data.tool_limit||"不限"}</span>{data.unreported_requests>0&&<span>未知用量请求 {data.unreported_requests}</span>}</div>}
 {error&&<p role="alert" className="text-xs text-destructive">{error}；保留上次记录。</p>}
 {editing?<><div className="grid gap-3 sm:grid-cols-2">{fields.map(([key,label])=><label key={key} className="space-y-1 text-xs">{label}<Input type="number" min="0" step="1" value={draft[key]??"0"} onChange={e=>setDraft(d=>({...d,[key]:e.target.value}))}/></label>)}</div><p className="text-xs text-muted-foreground">调整前先暂停并等待当前请求结束。保存会保留累计用量，增加预算后由你决定是否恢复任务。</p><div className="flex gap-2"><Button size="sm" disabled={busy} onClick={async()=>{setBusy(true);try{const input=Object.fromEntries(fields.map(([key])=>[key,Number(draft[key]||0)]));await http(`/tasks/${taskId}/budget`,{method:"PUT",headers:{"Content-Type":"application/json"},body:JSON.stringify(input)});toast.success("预算已保存，累计用量保留");setEditing(false);await load()}catch(e){setError(e instanceof Error?e.message:"保存失败")}finally{setBusy(false)}}}>保存预算</Button><Button size="sm" variant="ghost" onClick={()=>setEditing(false)}>取消</Button></div></>:<Button variant="outline" size="sm" disabled={!data} onClick={()=>{setDraft(Object.fromEntries(fields.map(([key])=>[key,String(data?.[key]??0)])));setEditing(true)}}>调整预算</Button>}
 </CardContent></Card>;
}
