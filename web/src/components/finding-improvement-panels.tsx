"use client";
import * as React from "react";
import Link from "next/link";
import { toast } from "sonner";
import { http } from "@/lib/api";
import { Markdown } from "@/components/markdown";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Card,CardContent,CardHeader,CardTitle } from "@/components/ui/card";

type ReportState={has_more?:boolean;jobs:{id:number;state:string;error:string;conversation_id:number|null;evidence_version:number}[];versions:{id:number;report:string;created_at:string;evidence_version:number}[]};
export function FindingReportPanel({id,readOnly,onDone}:{id:string;readOnly?:boolean;onDone:()=>void}){
 const[data,setData]=React.useState<ReportState>({jobs:[],versions:[]}),[error,setError]=React.useState(""),[busy,setBusy]=React.useState(false),[version,setVersion]=React.useState<number|null>(null);
 const [before,setBefore]=React.useState(0);const doneRef=React.useRef(onDone);doneRef.current=onDone;const lastState=React.useRef("");
 const load=React.useCallback(async()=>{try{const r=await http<ReportState>(`/exploration/findings/${id}/report-status?${before?`before=${before}`:""}`);const state=r.jobs[0]?`${r.jobs[0].id}:${r.jobs[0].state}`:"";if(lastState.current&&lastState.current!==state&&r.jobs[0]?.state==="ready")doneRef.current();lastState.current=state;setData(r);setError("");}catch(e){setError(e instanceof Error?e.message:"状态加载失败")}},[id,before]);
 React.useEffect(()=>{void load();const t=setInterval(()=>void load(),5000);return()=>clearInterval(t)},[load]);
 const job=data.jobs[0],old=data.versions.find(v=>v.id===version),active=job&&(job.state==="running"||job.state==="queued");
 return <Card><CardHeader><CardTitle className="text-sm">报告生成记录</CardTitle></CardHeader><CardContent className="space-y-3">
 <p className="text-xs text-muted-foreground">{job?`状态 ${job.state} · 证据版本 ${job.evidence_version}`:"尚无生成记录；可根据已有证据生成。"}</p>
 {job?.error&&<p role="alert" className="text-xs text-destructive">{job.error}</p>}{error&&<p role="alert" className="text-xs text-destructive">{error}；保留上次记录。</p>}
 <div className="flex flex-wrap gap-2"><Button size="sm" variant="outline" onClick={()=>void load()}>刷新状态</Button><Button size="sm" disabled={readOnly||busy||!!active} onClick={async()=>{setBusy(true);try{await http(`/exploration/findings/${id}/regenerate-report`,{method:"POST"});toast.success("报告已排队，使用已有证据");await load();onDone()}catch(e){toast.error(e instanceof Error?e.message:"生成失败")}finally{setBusy(false)}}}>重新生成</Button>{job?.conversation_id&&<Button size="sm" variant="ghost" asChild><Link href={`/chat?c=${job.conversation_id}`}>查看生成过程</Link></Button>}</div>
 {data.versions.length>0&&<><label className="block text-xs" htmlFor={`report-versions-${id}`}>历史报告（每页 20 版）</label><select id={`report-versions-${id}`} className="w-full rounded-lg border bg-background p-2 text-xs" value={version??""} onChange={e=>setVersion(e.target.value?Number(e.target.value):null)}><option value="">选择历史版本</option>{data.versions.map(v=><option key={v.id} value={v.id}>#{v.id} · 证据 v{v.evidence_version} · {new Date(v.created_at).toLocaleString("zh-CN")}</option>)}</select><div className="flex gap-2">{before>0&&<Button size="sm" variant="outline" onClick={()=>{setBefore(0);setVersion(null)}}>最新版本</Button>}{data.has_more&&<Button size="sm" variant="outline" onClick={()=>{setBefore(data.versions[data.versions.length-1].id);setVersion(null)}}>更早版本</Button>}</div>{old&&<div className="max-h-80 overflow-auto"><Markdown text={old.report}/></div>}</>}
 </CardContent></Card>;
}

type DuplicateState={relation:{primary_id:number;reason:string}|null;candidates:{id:number;name:string;summary:string;reason:string}[]};
export function FindingDuplicatePanel({id,readOnly,onDone}:{id:string;readOnly?:boolean;onDone:()=>void}){
 const[data,setData]=React.useState<DuplicateState>({relation:null,candidates:[]}),[primary,setPrimary]=React.useState(""),[reason,setReason]=React.useState(""),[error,setError]=React.useState(""),[busy,setBusy]=React.useState(false);
 const load=React.useCallback(()=>http<DuplicateState>(`/exploration/findings/${id}/duplicates`).then(r=>{setData(r);setError("")}).catch(e=>setError(e.message)),[id]);React.useEffect(()=>{void load()},[load]);
 const save=async(primaryId:number)=>{if(!reason.trim()){setError("请填写人工确认理由");return};setBusy(true);try{const r=await http<DuplicateState>(`/exploration/findings/${id}/duplicates`,{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({primary_id:primaryId,reason})});setData(r);setError("");toast.success(primaryId?"重复关系已关联，原记录与证据保留":"已解除重复关系并恢复原处置状态");onDone()}catch(e){setError(e instanceof Error?e.message:"关联失败")}finally{setBusy(false)}};
 return <Card><CardHeader><CardTitle className="text-sm">重复发现关联</CardTitle></CardHeader><CardContent className="space-y-3">{data.relation&&<p className="text-sm">主记录 <Link className="underline" href={`/function/findings/detail?id=${data.relation.primary_id}`}>#{data.relation.primary_id}</Link> · {data.relation.reason}</p>}<p className="text-xs text-muted-foreground">候选按类别、资产或证据交集提供，需人工确认，不自动合并或删除。</p>{data.candidates.map(c=><button key={c.id} disabled={readOnly} type="button" className="block w-full rounded-lg border p-2 text-left text-xs hover:bg-muted" onClick={()=>setPrimary(String(c.id))}>#{c.id} · {c.name||c.summary}<span className="block text-muted-foreground">{c.reason}</span></button>)}{!readOnly&&<><Input aria-label="主发现编号" placeholder="主发现编号" value={primary} onChange={e=>setPrimary(e.target.value)}/><Input aria-label="人工确认理由" placeholder="人工确认理由" value={reason} onChange={e=>setReason(e.target.value)}/><div className="flex gap-2"><Button size="sm" disabled={busy||!Number(primary)} onClick={()=>void save(Number(primary))}>关联主记录</Button>{data.relation&&<Button variant="outline" size="sm" disabled={busy} onClick={()=>void save(0)}>解除关联</Button>}</div></>}{error&&<p role="alert" className="text-xs text-destructive">{error}</p>}</CardContent></Card>;
}
