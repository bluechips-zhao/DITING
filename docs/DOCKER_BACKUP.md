# Docker 备份与恢复

2.5.0 已在 Windows Docker Desktop 的 Linux amd64 引擎完成实际停机备份、错误口令拒绝、新项目恢复、2.4.0 到 2.5.0 升级及升级失败后恢复旧版演练。源数据库、密钥及四个应用卷均验证保留。验收使用隔离数据，不包含真实模型或目标调用；正式部署仍应按自身数据量、磁盘空间和业务文件布局演练恢复。

## 准备

在可信宿主机安装 Python 3.12 或以上、Docker Compose，并执行：

```sh
python -m pip install -r scripts/requirements-backup.txt
```

使用 `DITING_BACKUP_PASSPHRASE` 环境变量传入至少 16 字符的独立备份口令。通过自己的秘密管理方式设置，避免写入命令参数、源码、共享终端记录或 Git。遗失口令无法解密。下面使用 `diting` 作为源 Compose 项目名，实际命令必须匹配部署项目名及其环境文件。

## 停机备份

应用完成首次初始化后才能备份。先停止应用，保持 PostgreSQL 运行：

```sh
docker compose -p diting --env-file .env stop artex
python scripts/docker_backup.py backup --project diting --env-file .env --file backups/diting-before-upgrade.dbak --ack-stopped
python scripts/docker_backup.py preview --file backups/diting-before-upgrade.dbak
docker compose -p diting --env-file .env start artex
```

备份包含 PostgreSQL 逻辑转储，以及 runtime、workspace、skills、toolhome 四个独立数据卷。runtime 包含登录签名密钥及凭据加密密钥；工具会检查凭据密钥能解密数据库中的加密字段。业务文件、密钥及转储均在一个 AES-256-GCM 加密归档内，分块认证并校验结束标记。重复文件名、路径越界、校验和错误和错误口令会拒绝恢复。

工具不会覆盖已有备份文件。宿主机临时目录会在执行期间存放明文中间文件，应运行在受控主机上并保证足够可用空间。加密归档及展开内容上限为 50 GiB。支持普通文件、目录和指向同一卷内的安全相对软链接、硬链接；越界链接、循环链接、链接下的归档成员以及特殊文件会被拒绝。卷内已有指向镜像程序或其他卷的绝对链接时，需要先调整该部署的文件布局，不能将失败的备份视为成功。

同时保留对应的应用镜像：

```sh
docker image save -o backups/diting-image.tar diting:2.5.0-local
```

此镜像归档不包含业务数据卷。备份清单记录原应用的精确 image ID；恢复前必须在本地加载该镜像，不能只依赖同名可变标签。

## 恢复到新项目

为恢复项目准备单独的环境文件，例如 `recovery.env`，设置新的 PostgreSQL 密码和未占用的回环端口。不能复用原项目名、原数据库或已有数据卷。

```sh
docker image load -i backups/diting-image.tar
python scripts/docker_backup.py restore --project diting-recovery --env-file recovery.env --file backups/diting-before-upgrade.dbak --ack-stopped
```

工具在创建任何容器前验证加密归档、库存、校验和及文件路径。存在目标项目容器或数据卷时拒绝操作。恢复配置须保留标准的两个服务及五个项目专属本地卷；自定义 Compose 中的外部卷、共享卷、宿主机绑定、卷子路径及继承挂载会被拒绝，防止新项目名指向旧数据。恢复使用原应用精确 image ID，新建空 PostgreSQL，导入转储及四个卷后保持应用停止。失败时保留新目标用于排查，原部署不被覆盖或删除。

从恢复输出取 `image_id`，将其保存到恢复专用环境文件的 `DITING_IMAGE`，例如 `DITING_IMAGE=sha256:<恢复输出的完整哈希>`。随后启动：

```sh
docker compose -p diting-recovery --env-file recovery.env up -d --no-build
docker compose -p diting-recovery --env-file recovery.env ps
```

检查健康状态、登录、模型配置、业务记录和工作区文件后，再决定是否切换访问端口。不要让原项目和恢复项目同时执行相同的定时任务或目标任务。切换与清理旧部署不由恢复工具自动执行。

## 升级

升级前按上述流程制作并预览完整备份、保存旧镜像。将部署环境文件中的 `DITING_IMAGE` 修改为已验证的新镜像，再执行 `docker compose up -d --no-build`。容器内二进制自动更新和回滚被禁用。

数据库迁移后，不能只换回旧镜像来宣称完成回滚。应恢复升级前完整备份到新项目，检查旧镜像与原数据库、密钥和文件的一致性，再切换访问入口。


## 带预置卷内容的旧镜像

部分旧镜像会在创建容器时向空应用卷复制内置文件。恢复工具会在导入数据库前检查四个应用卷为空；触发该检查时，不要删除检查或向已有部署强行恢复。对单独的新恢复项目，可在专用 Compose 文件的四个应用卷挂载中设置 `volume: {nocopy: true}`，并通过 `--compose-file` 显式传入；保持两个服务和五个项目专属卷。恢复工具写回归档内容并设置应用卷 UID/GID 10001。此设置只用于恢复，不应照搬到首次安装的标准 Compose，后者依赖卷初始化建立正确权限。
