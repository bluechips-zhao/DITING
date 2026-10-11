"use client";

import * as React from "react";
import { DownloadIcon } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { downloadExport } from "@/lib/api";

export function DataExport({path,params,formats,label="导出筛选结果"}:{path:string;params:Record<string,string|undefined>;formats:string[];label?:string}){
 const [busy,setBusy]=React.useState(false);
 return <div className="flex flex-wrap items-center gap-1" aria-label={label}>
 {formats.map(format=><Button key={format} variant="outline" size="sm" disabled={busy} title={`${label}，包含全部匹配页`} onClick={async()=>{
  setBusy(true);try{const q=new URLSearchParams({format});Object.entries(params).forEach(([k,v])=>{if(v!==undefined&&v!=="")q.set(k,v)});await downloadExport(`${path}?${q}`,`export.${format}`);toast.success("完整导出已下载");}catch(e){toast.error(e instanceof Error?e.message:"导出失败");}finally{setBusy(false);}
 }}><DownloadIcon className="size-3.5"/>{busy?"导出中":format.toUpperCase()}</Button>)}
 </div>;
}
