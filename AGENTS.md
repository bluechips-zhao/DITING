# DITING 开发指南

Go 后端、PostgreSQL、Next.js 静态前端。基于 ARTEX；Go module/import 和 `ARTEX_*` 环境变量保留兼容名称。发布/更新源为 `bluechips-zhao/DITING`。

## 构建与配置

- Windows：`build.bat` 安装依赖、导出静态资源、内嵌生成 `diting.exe`。当前开发版包使用 `scripts/package-release.ps1 -Version 2.6.0`，完成与否以 RELEASE_NOTES.md 验收记录为准。
- Bash：`./build.sh --target linux/amd64`。使用 `ARTEX_BUILD_VERSION` 指定内嵌版本。
- Go 最低版本见 `go.mod`；前端使用 Node.js 22、npm 锁文件。
- 正式构建必须使用 `-tags embedui` 和已生成的静态资源。
- `config.example.json` 复制为 `config.json`，填写自己的 PostgreSQL；没有默认数据库密码。
- `ARTEX_PG_DSN` 优先于文件数据库配置；`ARTEX_CONFIG` 指定配置文件，否则先查工作目录，再查程序目录。
- `ARTEX_SKILL_DIR` 优先于文件 `skill_dir`，留空使用程序旁的 `skills/`。
- 模型在 UI 配置；API Key 与 `ARTEX_LLM_*` 环境变量是可选项。
- 开发后端 `go run ./cmd/artex`；前端 `cd web && npm run dev -- --port 5173`，API 默认反代 8787。
- 新数据库只初始化内置工具、Agent、提示词、拦截规则和系统设置；禁止初始化示例业务、预置模型或模拟认证。

## 修改入口

| 内容 | 位置 |
| --- | --- |
| 产品元数据 | `web/src/config/app-config.ts` |
| API 与类型 | `web/src/lib/api.ts`、`web/src/lib/types.ts` |
| 后端路由 | `server/server.go` 和对应 handler |
| 幂等迁移 | `db/schema.sql`、`db/db.go` |
| 提示词 | `agent/promptcatalog.go`、`agent/worker.go`、`server/orchestration.go` |
| 工具定义与绑定 | `agent/toolcatalog.go`、`agent/tools*.go`、`server/orchestration.go` |
| 发布与更新 | `build.sh`、`scripts/package-release.ps1`、`.github/workflows/release.yml`、`selfupdate/github.go` |

提示词种子只初始化空版本；更新默认提示词需对应迁移标记，保留用户定制。外部 Agent SDK norma 版本见 `go.mod`。

## 验证与发布

按改动运行类型检查、相关 Go 测试、构建及 vet。数据库测试使用隔离 PostgreSQL，禁止连接开发者业务数据库。正式包验证首次初始化、鉴权和空业务列表。

排除凭据、运行数据、日志、抓包、备份、内部交接记录和个人机器路径。README 可保留明确标注的模拟截图，截图数据不得导入应用。保留 LICENSE、字体许可与来源说明；不强制推送或改写已有远端历史。
