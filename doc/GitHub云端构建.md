# GitHub 云端构建

源码仓库：[zwdotnet/Landlord](https://github.com/zwdotnet/Landlord)，公有，沿用上游 MIT 许可。构建入口是 `.github/workflows/cloud-build.yml`，使用 GitHub 托管的 `ubuntu-24.04`，不使用生产 VPS 或 NAS 作为 runner。

2026-11-11 [首次构建成功](https://github.com/zwdotnet/Landlord/actions/runs/43011871714)，对应源码提交 `e1d14c9`，部署制品约376MiB；构建、临时容器与PDF检查、打包及上传全部通过。详见 [云端构建验证记录](云端构建验证记录-20261111.json)。本次没有重新部署线上服务。

`main` 的后端、前端、部署配置或版本变动会触发构建，也可在 Actions → Build Linux deployment → Run workflow 手动触发。工作流只需要读取仓库，不需要 VPS 私钥、业务数据库或初始化邀请。

流水线执行前端类型检查及 `/landlord/` 生产构建、日期/PWA 测试，构建 `linux/amd64` 镜像，再用临时只读容器检查健康接口、版本、前端路径和 Chromium 中文 PDF。容器和数据随检查清理。它不是完整业务验收，也不替代 iPhone 系统推送实测。

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
