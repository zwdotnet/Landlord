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
  --artifact-dir /opt/landlord/artifacts/github-43263322293 \
  --commit 17532776461d772d2683a1fdd2910680824db006 \
  --image landlord:0.10.0-17532776461d-43263322293-1 \
  --run-id 43263322293
```

部署者另行获准发布新候选后才加`--apply`，生成独立`run-运行号-commit`目录和published.json，记录分段耗时及备份/旧release。目标Linux服务器须有Python支持的tar数据过滤、Docker Compose与既有current；工具沿用本项目固定容器名、loopback端口和受限环境文件，不负责微信凭据或Caddy修改。切换前停止单个应用并保存最新数据/附件/环境/旧Compose，再启动预构建镜像；直接轮询应用健康，避免等待Docker的30秒健康检查周期。校验精确前端、运行限制、原业务字段/ID和物理文件后才切current；失败切回旧应用，不自动恢复旧库。

涉及模型/数据库结构、依赖或特殊配置变化时，仍先做匹配候选的迁移及旧版兼容预演，再提供`--require-migration-proof --migration-proof 文件路径`。JSON必须带`commit`、`pass:true`和`old_version_startup_compatible:true`；脚本不会凭字段存在自动认定迁移可回退。普通更新复用与候选源码指纹匹配的已通过门禁，不因文档收尾重复运行全部业务测试。

2026-11-13经用户明确授权，首次用此工具实际发布1753277成功：工具总计28.99秒，包校验3.09秒、冻结备份/切换/核对段1.44秒；VPS直下345085602字节制品并校验89.59秒，含解包93.65秒，均单独计时。随后服务器及公网38项复核通过，旧release/镜像和冻结备份保留；失败回退分支未触发，不能宣称已实演。回执和详细时间边界见[部署记录末节](部署候选与Safari验收.md)及测试报告§12。公网页面、其他站点和真机验收仍按实际变更执行并记录；发布提速不代表省略备份或保证固定总耗时。

## 2026-11-15 审查修复后的构建约定

本节描述本地已修改的工作流，不表示新workflow已经上传或在GitHub运行。统一`scripts/verify.py`在PR、main相关路径改动和手动触发时执行；覆盖Go全量、Web/小程序Node、发布/小程序准备/备份与门禁Python、客户端语法和Web类型/构建。Go工具链固定1.26.5，下载文件与官方发行清单SHA256核对；不依赖微信登录或根.env。原生开发工具/真机单独验收。

`build`依赖`verify`成功，PR不生成镜像/部署包。验证日志和summary按commit/run attempt保存；打包必须读到该commit的完整成功回执，包内附verification.json，build.json记录回执SHA、各步计数和二进制version/gitCommit/buildTime/appName。打包与发布共用严格回执校验：完整候选、精确七步名称/顺序、退出码0、非零测试计数/零失败；空或不完整的新二进制身份拒绝。旧制品校验能力保留用于既有回退，不将旧包记成新完整候选。远端required checks未读未改，不能声称已阻止所有绕过分支保护的合入。

Docker构建现在必须传入`GIT_COMMIT`和UTC RFC3339 `BUILD_TIME`，两者注入Go链接参数；云工作流和`deploy/build-image.sh`已同步。镜像冒烟读取实际`/api/version`并与构建输入核对；启动时SQLite实际参数回读失败或升级前快照失败均停止启动，防止无保护迁移。相同版本号但不同构建身份会重新快照；本机未注入元数据的构建退回实际可执行文件SHA256判据。

完整备份恢复操作见[README](../README.md#备份恢复与上传边界)。本轮仅在新合成实例验证，未停止生产、安装Linux/Docker环境、触发云运行、推送、部署或改微信平台；Windows自验不能代替Linux镜像的实际检查。

## 2026-11-14 后续发布记录

本节为历史生产发布记录；最新候选见下方11月16日补充。

用户“好，接下来部署”授权源码623a21d，推送后触发[云构建43592881908](https://github.com/zwdotnet/Landlord/actions/runs/43592881908)，成功完成前端/日期/PWA、Linux镜像和只读中文PDF检查。制品`landlord-0.10.0-623a21d30366-43592881908-1`为345224737字节，原ZIP SHA256 `d24bbeca48b66caf0d71ee585c5b6445ce78d31e64d936e5f99eadfdc09dbe1d`；VPS直下核对后用相同release.py发布，未在VPS构建。

current为`/opt/landlord/releases/run-43592881908-623a21d30366`，冻结备份`/var/backups/landlord/pre-run-43592881908-20261114-121157`；原1753277应用/镜像和业务保留。20:11:59发布回执、20:13服务器和20:14公网47项复核通过。本次补齐完整检查各组成项，PowerShell5对Python stderr误报及单独补验事实见报告§19，不把原脚本退出1改写为0。分段耗时、回退和当前边界见[部署记录末节](部署候选与Safari验收.md)及STATE.md；小程序客户端未上传发布。

## 2026-11-16 云端验证候选（未部署生产）

用户授权推送GitHub并运行构建验证，仍不部署生产。首轮c9472b4的Linux门禁暴露客户端单测依赖被忽略config.local.js，Go通过但Node失败，build被阻断；437b961改为每次缓存重置后注入合成配置，本机与干净副本Node104通过。没有读取/创建原本机配置，原失败证据保留。

[云运行43784715179](https://github.com/zwdotnet/Landlord/actions/runs/43784715179)的verify/build成功，完整Linux Go489个顶层/572事件、Node104、Python12＋4＋12、小程序33份JS及Web构建通过；精确身份、只读容器和中文PDF冒烟通过。候选`landlord-0.10.0-437b961d85da-43784715179-1`共350961751字节，ZIP SHA256为`2fb52d82fc1618a02a9ff60cbdf241287d2da00b8c9273e0f89655b8258e82b3`，GitHub留存7天，以实际artifact过期时间为准。

候选已下载并核对外层ZIP/包内SHA、78份前端文件、完整回执和二进制身份，运行release.py默认只读检查通过；没有加载镜像、执行--apply或创建发布目录。工作流没有自动部署路径，生产仍沿用原发布；最终文档提交不改变候选源代码。证据与残余检查见[审查报告§10](项目审查报告与改进建议-20261115.md)及STATE；本轮API只读确认main未启用必需状态检查，未更改仓库保护。

## 2026-11-16 独立隔离演练

`.github/workflows/release-rehearsal.yml`在一次性GitHub runner中复用上述既有437b961制品，校验包、回执及产品源码无差异后，仅加载到runner自己的Docker。`tools/release-rehearsal.py`使用新合成目录、随机凭据、随机容器名和loopback端口，测试30MiB上传＋4笔实收PDF＋数据库快照并发，停写后完整备份及新目录恢复；不构建新镜像，不使用生产数据，不部署生产。该工作流只响应自身/工具路径或手动运行，不因文档变化重复测试，也不触发原Go/Web全门禁。

最终[43789242881](https://github.com/zwdotnet/Landlord/actions/runs/43789242881)通过；容器内核内存峰值约127MiB（包含cache/tmpfs）、临时盘采样峰值29.4MiB、OOM0，完整恢复38表/9份鉴权原件一致。维护暂存采样62.1MiB位于宿主维护目录，不能与容器256MiB tmpfs混为一谈。前两轮测试夹具/权限失败原件保留；这些数值仅适用于该有界样本，不是生产容量或RTO承诺。只上传脱敏JSON结果，所有测试容器移除；候选和回执保留7天，过期须先更新明确候选参数，不静默选择其他镜像。

本轮另只读核实生产Caddy2.6.2→127.0.0.1:18930，应用真实来源为172.19.0.1容器网关。未来部署需先重核网关，再按实际路径精确设置`TRUSTED_PROXIES=172.19.0.1/32`，不能照抄127.0.0.1或整个私网；本轮未修改环境文件/Caddy、重启容器或切current。真实脱敏副本/维护安排和平台手机条件仍见[审查报告§11](项目审查报告与改进建议-20261115.md)及STATE。
