"use client";
import * as React from "react";
import { http } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Card,CardHeader,CardTitle,CardContent } from "@/components/ui/card";
type Migration={flag:string;state:string;error:string;updated_at:string};
export function MigrationPanel(){
 const [rows,setRows]=React.useState<Migration[]>([]),[error,setError]=React.useState("");
 const load=React.useCallback(()=>http<Migration[]>("/system/migration-health").then(r=>{setRows(r);setError("")}).catch(e=>setError(e.message)),[]);
 React.useEffect(()=>{void load()},[load]);const failed=rows.filter(r=>r.state==="failed");
 return <Card><CardHeader><CardTitle>初始化与迁移</CardTitle></CardHeader><CardContent className="space-y-3 text-sm"><p className="text-muted-foreground">{rows.length?`${rows.length} 项事务迁移已记录，${failed.length} 项失败。失败项将在下次启动重试。`:"正在读取迁移记录。"}</p>{failed.map(r=><p role="alert" className="text-destructive" key={r.flag}>{r.flag}：{r.error}</p>)}{error&&<p role="alert" className="text-destructive">{error}；保留上次记录。</p>}<details><summary className="cursor-pointer text-xs">查看迁移记录</summary><div className="max-h-52 overflow-auto text-xs">{rows.map(r=><p className="py-1" key={r.flag}>{r.flag} · {r.state==="ready"?"已完成或保留用户配置":"失败"}</p>)}</div></details><Button size="sm" variant="outline" onClick={()=>void load()}>刷新</Button></CardContent></Card>
}
