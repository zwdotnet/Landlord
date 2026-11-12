# GitHub 云端构建

源码仓库：[zwdotnet/Landlord](https://github.com/zwdotnet/Landlord)，公有，沿用上游 MIT 许可。构建入口是 `.github/workflows/cloud-build.yml`，使用 GitHub 托管的 `ubuntu-24.04`，不使用生产 VPS 或 NAS 作为 runner。

2026-11-11 [首次构建成功](https://github.com/zwdotnet/Landlord/actions/runs/43011871714)，对应源码提交 `e1d14c9`，部署制品约376MiB；构建、临时容器与PDF检查、打包及上传全部通过。详见 [云端构建验证记录](云端构建验证记录-20261111.json)。该次记录仅为首次构建；当前线上已于22:10切换至 [43033817431](https://github.com/zwdotnet/Landlord/actions/runs/43033817431) 的 Headless Shell 制品（源码5323d74），详见 [上线验证](HeadlessShell上线与资源验证-20261111.md)。

`main` 的后端、前端、部署配置或版本变动会触发构建，也可在 Actions → Build Linux deployment → Run workflow 手动触发。工作流只需要读取仓库，不需要 VPS 私钥、业务数据库或初始化邀请。

流水线执行前端类型检查及 `/landlord/` 生产构建、日期/PWA 测试，构建 `linux/amd64` 镜像，再用临时只读容器检查健康接口、版本、前端路径和 Chromium Headless Shell 中文 PDF（沿用实际业务参数，断言完整 Chromium 不存在）。容器和数据随检查清理。它不是完整业务验收，也不替代 iPhone 系统推送实测。

构建成功后，在本次运行的 Artifacts 下载 `landlord-版本-提交-运行编号-尝试编号`，默认保留 7 天。解压后包含：

- `image.tar.gz`：可以用 `docker load` 导入的镜像。
- `release.tar.gz`：同一个镜像中提取的前端、固定镜像标签的 `compose.yml`、前端文件指纹、源码归档和构建记录。
- `build.json`：源码提交、运行编号、镜像 ID 和文件层等来源信息。
- `SHA256SUMS`：上述文件的 SHA256。

发布仍作为单独操作执行。先保存当前 SQLite 一致性备份和旧 release，再在新 release 目录核对并导入制品：

```sh
sha256sum -c SHA256SUMS
docker load -i image.tar.gz
tar -xzf release.tar.gz
find web -type d -exec chmod 0755 {} +
find web -type f -exec chmod 0644 {} +
docker compose -p landlord -f compose.yml config -q
```

`compose.yml` 延续 `/var/lib/landlord/data` 和 root 保护的 `/var/lib/landlord/bootstrap.env`，前端目录只读挂载；这些生产文件由服务器维护，制品中没有账号、数据库或密钥。核对备份、候选版本和制品后，再执行发布：

```sh
docker compose -p landlord -f compose.yml up -d --no-build --pull never
docker exec landlord test -r /app/web/index.html
curl --fail http://127.0.0.1:18930/ >/dev/null
```

静态文件仅供公开读取；业务数据和初始化环境文件保持原有受限权限。Python安全解包在受限umask下可能将目录创建为0700，因此必须显式核对web目录权限。容器healthy还需配合真实入口、脚本和样式请求验证。健康与公网验证通过后再更新 `/opt/landlord/current`。失败时恢复原 release 的 Compose 和前端；有数据库迁移时单独判断兼容性，不能盲目覆盖用户数据。构建工作流不会自行 SSH、重启服务或切换线上版本。

依据：[GitHub 托管 runner](https://docs.github.com/en/actions/reference/runners/github-hosted-runners)、[Docker Buildx Action](https://github.com/docker/build-push-action)、[构建制品](https://github.com/actions/upload-artifact)。当前真正运行的构建结果以仓库 Actions 为准。

## 2026-11-13 发布流程合并工具

`deploy/release.py`将制品核对、前端准备、镜像核对/按需加载、一致性备份、切换与回执放在同一个服务器进程中，减少逐条SSH往返。默认只读校验与输出具体计划，不构建、导入镜像或重启应用；对包校验、来源不匹配、归档穿越/链接和篡改、迁移凭证等有8个自动用例，已纳入完整`scripts/verify.ps1`。

以当前已发布制品为例，下面仅执行计划检查：

```sh
python3 deploy/release.py \
  --artifact-dir /opt/landlord/artifacts/github-43237672099 \
  --commit 65953074545c28d8ccb6429d099ad69e499af93b \
  --image landlord:0.10.0-65953074545c-43237672099-1 \
  --run-id 43237672099
```

部署者另行获准发布新候选后才加`--apply`，生成独立`run-运行号-commit`目录和published.json，记录分段耗时及备份/旧release。目标Linux服务器须有Python支持的tar数据过滤、Docker Compose与既有current；工具沿用本项目固定容器名、loopback端口和受限环境文件，不负责微信凭据或Caddy修改。切换前停止单个应用并保存最新数据/附件/环境/旧Compose，再启动预构建镜像；直接轮询应用健康，避免等待Docker的30秒健康检查周期。校验精确前端、运行限制、原业务字段/ID和物理文件后才切current；失败切回旧应用，不自动恢复旧库。

涉及模型/数据库结构、依赖或特殊配置变化时，仍先做匹配候选的迁移及旧版兼容预演，再提供`--require-migration-proof --migration-proof 文件路径`。JSON必须带`commit`、`pass:true`和`old_version_startup_compatible:true`；脚本不会凭字段存在自动认定迁移可回退。普通更新复用与候选源码指纹匹配的已通过门禁，不因文档收尾重复运行全部业务测试。

本轮已完成制品验证/默认计划模式及8项自动测试，尚未用新工具执行生产切换，不能将其视为经过生产演练的自动发布器。下一次获授权发布时需先按现场复核并验证实际应用/回退分支。公网页面、其他站点和真机验收仍按实际变更执行并记录；发布提速不代表省略备份或宣称固定总耗时。
