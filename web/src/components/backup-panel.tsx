"use client";
import { toast } from "sonner";
import { downloadExport } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Card,CardContent,CardHeader,CardTitle } from "@/components/ui/card";
export function BackupPanel(){return <Card><CardHeader><CardTitle>个人数据备份与恢复</CardTitle></CardHeader><CardContent className="space-y-3 text-sm">
 <p className="text-muted-foreground">完整备份包含 PostgreSQL、指定的数据目录及技能目录。数据库内凭据使用独立的 secrets.key 加密；备份时请将原密钥一起放入加密备份，或明确选择单独保管。配置文件和 JWT 签名密钥可另行选择。恢复先校验清单、完整性和密码，只写入新的空数据库及新的目录。</p>
 <ol className="list-decimal space-y-1 pl-5 text-xs"><li>下载本地工具，停止应用与录制代理后制作一致快照。</li><li>在 PowerShell 7 中通过环境变量设置数据库连接与至少 16 字符备份口令，保管口令。</li><li>先运行 Preview 检查范围；Restore 会拒绝覆盖现有数据库或目录。</li></ol>
 <Button variant="outline" onClick={()=>downloadExport("/system/backup-tool","diting-backup.ps1").catch(e=>toast.error(e.message))}>下载备份工具</Button>
 <details className="text-xs"><summary className="cursor-pointer py-2">查看本地使用示例</summary><pre className="overflow-auto whitespace-pre-wrap rounded-lg bg-muted p-3">{`# 数据库连接：环境变量 ARTEX_PG_DSN
# 备份口令：环境变量 DITING_BACKUP_PASSPHRASE
.\\diting-backup.ps1 -Mode Backup -Package backup.dbak -DataDir .\\data -SkillsDir .\\skills -IncludeLocalCredentials -EncryptionKey .\\secrets.key -OfflineAcknowledged
.\\diting-backup.ps1 -Mode Preview -Package backup.dbak
# 新的空库连接：环境变量 DITING_RESTORE_DSN
.\\diting-backup.ps1 -Mode Restore -Package backup.dbak -DestinationDir .\\restored-new -OfflineAcknowledged
# pg_dump / pg_restore 不在 PATH 时增加 -PgBin <PostgreSQL bin 目录>
# 单独备份加密密钥时使用 -KeyStoredSeparately，替代上面的密钥包含参数
# 恢复后将 local/secrets.key 放回应用配置目录，勿放进可浏览的 data 目录
# 可选本地凭据：-IncludeLocalCredentials -ConfigFile .\\config.json -SigningKey .\\jwt.key`}</pre></details>
 </CardContent></Card>}
