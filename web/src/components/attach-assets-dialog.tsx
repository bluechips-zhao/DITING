"use client";

import * as React from "react";
import { toast } from "sonner";
import { api } from "@/lib/api";
import type { Task,Asset } from "@/lib/types";
import { Button } from "@/components/ui/button";
import { Dialog,DialogContent,DialogHeader,DialogTitle,DialogDescription,DialogFooter,DialogTrigger } from "@/components/ui/dialog";
import { Select,SelectContent,SelectItem,SelectTrigger,SelectValue } from "@/components/ui/select";

export function AttachAssetsDialog({ids,assets,onDone}:{ids:number[];assets:Asset[];onDone:()=>void}){
 const[open,setOpen]=React.useState(false),[tasks,setTasks]=React.useState<Task[]>([]),[task,setTask]=React.useState(""),[error,setError]=React.useState(""),[busy,setBusy]=React.useState(false);
 React.useEffect(()=>{if(!open)return;let alive=true;api.tasks().then(r=>{if(alive){setTasks(r.tasks);setError("")}}).catch(e=>{if(alive)setError(e.message)});return()=>{alive=false}},[open]);
 return <Dialog open={open} onOpenChange={setOpen}><DialogTrigger asChild><Button size="sm" variant="outline" disabled={!ids.length}>关联任务 ({ids.length})</Button></DialogTrigger>
 <DialogContent><DialogHeader><DialogTitle>将现有资产关联到任务</DialogTitle><DialogDescription>仅新增任务关联，不修改企业归属、覆盖范围或执行授权。继承任务资产遵守只读规则。</DialogDescription></DialogHeader>
 <Select value={task} onValueChange={setTask}><SelectTrigger aria-label="选择目标任务"><SelectValue placeholder="选择目标任务"/></SelectTrigger><SelectContent>{tasks.map(t=><SelectItem key={t.id} value={t.id}>{t.name||t.description||`任务 #${t.id}`}</SelectItem>)}</SelectContent></Select>
 <div className="max-h-52 overflow-auto rounded-lg bg-muted p-3 text-xs">{ids.map(id=>{const a=assets.find(a=>a.id===id);return <p key={id}>#{id} · {a?.domain||a?.url||a?.ip||a?.app_name||"已选择资产"}</p>})}</div>
 {error&&<p role="alert" className="text-sm text-destructive">{error}</p>}
 <DialogFooter><Button disabled={!task||busy} onClick={async()=>{setBusy(true);try{const r=await api.attachTaskAssets(task,ids,"用户从资产页批量关联");toast.success(`新增 ${r.attached} 项，已有 ${r.existing} 项`);setOpen(false);onDone()}catch(e){setError(e instanceof Error?e.message:"关联失败")}finally{setBusy(false)}}}>{busy?"关联中…":"确认关联"}</Button></DialogFooter>
 </DialogContent></Dialog>;
}
