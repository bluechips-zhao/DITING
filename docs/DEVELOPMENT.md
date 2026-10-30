# 源码构建与开发

[返回首页](../README.md) · [文档索引](README.md)

以下命令均在仓库根目录执行；前端命令按说明进入 web。

## 从源码构建

需要 Go **1.26.3 或更新版本**、Node.js **22**、npm 和 PostgreSQL。

```powershell
git clone https://github.com/bluechips-zhao/DITING.git
cd DITING
.\build.bat
Copy-Item config.example.json config.json
# 编辑 config.json 后启动
.\start.bat -addr 127.0.0.1:8787 -proxy 127.0.0.1:8788
```

Windows 可分发 zip：

```powershell
powershell -ExecutionPolicy Bypass -File scripts/package-release.ps1
```

打包脚本按固定文件清单组装，不复制配置、JWT 密钥、数据库、日志或工作区文件；输出到 `dist/`，拒绝覆盖已有同版本产物。

Linux/macOS 需要 Bash、rsync；打包还需 zip：

```bash
./build.sh --target linux/amd64
cp config.example.json config.json
# 编辑 config.json 后启动
./dist/artex-linux-amd64/artex -addr 127.0.0.1:8787 -proxy 127.0.0.1:8788
```

## 开发模式

后端 `go run ./cmd/artex`；另一终端进入 `web`，执行 `npm ci`、`npm run dev -- --port 5173`。前端 API 默认反代后端 8787。未配置模型时没有预置模型，也不会自动调用供应商。[开发指南](../AGENTS.md) 记录代码入口与配置优先级。


## 开发回归

常规回归使用本机模拟供应商，不调用真实 LLM。每次运行创建新的隔离 PostgreSQL 数据库，设置 `ARTEX_PG_DSN` 后执行 `go test -p 1 -count=1 ./...` 和 `go vet ./...`；不得连接正在使用的业务数据库。共享数据库的各包顺序执行，避免启动迁移与夹具清理互相干扰。

前端在 `web/` 执行 `npm test`、`npx tsc --noEmit` 和 `npm run build:static`。PR 工作流额外检查发行包的私有文件排除和嵌入前端的首次安装；Windows 作业验证守护启动脚本的升级、重启和回滚。工作流不发布版本。

打包后运行 `python scripts/check-package.py <发行包.zip>`。空库检查通过 `DITING_FRESH_PG_DSN` 明确指定全新隔离数据库，再执行 `python scripts/run-fresh-install.py <程序路径> <版本号>`；脚本复制程序到临时目录、移除继承的模型密钥，检查密码初始化、接口鉴权与空业务列表，然后停止测试程序。数据库向导回归另用全新本地测试库（名字须为 `diting_setup_<唯一后缀>`），设置 `DITING_SETUP_PG_DSN`，执行 `python scripts/run-setup-install.py <程序路径>`。它验证缺失配置、失败/成功连接测试、同进程启动、密码初始化、环境鉴权、空业务状态和模型保存/激活，使用不可用的本机模型入口，不发送模型测试请求。常规回归不调用真实模型或靶场；使用者的实际模型连接和目标任务需要自行明确配置、授权与预算，不能作为自动回归默认步骤。
