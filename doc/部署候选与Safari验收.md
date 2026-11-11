# 新加坡部署与 Safari 验收

记录日期：2026-11-11，Asia/Hong_Kong；实际当前状态以根目录 STATE.md 为准。按用户选择 Headless Shell 的授权，22:10 已切换 GitHub 最终制品，15 项公网验证通过，22:13 最终检查 healthy。19:44 的完整 Chromium 版为回退版本。MAIN自验，无独立验收。

## 正式运行

- URL：https://www.zoeng.site/landlord/；apex 沿原配置跳转到 www，主页与旧子站保留。
- 主机：singapore-vps / 111.91.27.81 / hcss-ecs-b4e4。沿用 SSH agent，未读取私钥、未改 SSH/PAM/2FA。
- release：`/opt/landlord/releases/20261111-0.10.0-shell-5323d74`；current指向它。后端镜像 `landlord:0.10.0-5323d747465d-43033817431-1`，应用源码5323d74。79份前端文件来自同一镜像，只读挂载到`/app/web`；目录0755、文件0644。
- 容器只监听127.0.0.1:18930，512MiB RAM / 768MiB内存+swap合计、1CPU、160进程，根目录只读、去全部capabilities；健康检查已通过。正式数据 `/var/lib/landlord/data`；初始化环境文件 `/var/lib/landlord/bootstrap.env` 权限0600。
- 镜像归档SHA256 `ddfc9b38e95c080ba79b5041ef31482c0f6b5c1f2d39460128f926712e3fb381`；原始GitHub制品SHA256 `64e2299cf9356a2c708cb39b46ef1b63c4ac8f7577b37d4c9408293843814120`。云端与VPS Docker显示ID不同，已核rootfs全部层、架构和源码revision。离线归档保存在`/opt/landlord/artifacts/github-43033817431`。
- 生产无模拟账号，首管理员需用户自行通过本机受限初始化文件创建；邀请只放URL fragment和请求头，不写访问日志。数据库/备份包含认证资料和VAPID身份，按业务数据保护。

## Swap 与构建

`/swapfile` 为4GiB、0600，已启用并加入fstab；swappiness=10，旧sysctl.conf的0覆盖也已同步修正。fstab解析无错误，swapfile.swap loaded/active，无需重启主机。原配置在 `/var/backups/landlord/swap-20261111-180034`。没有通过耗尽内存来测试swap。

当前使用[GitHub云端构建](GitHub云端构建.md)，托管ubuntu-24.04 runner，BuildKit 3GiB/2CPU/并行1。此次只下载、校验、加载镜像并启动，VPS和NAS均没有重新构建。18:58的NAS构建版保留为回退版本，NAS历史制品目录`/vol1/1000/NAS/Docker/builds/landlord-20261111-final`仍可追溯。

当前 Headless Shell 云端镜像已通过临时中文 PDF 及 15 项公网检查；隔离业务累计 30 项通过，其中最终镜像复验 16 项，覆盖合同、续签、收据、冲销、失败/超时回滚与重启持久化。本轮后端定点 3 项通过，未重跑全量。HOME/XDG 位于单次临时 profile，保持只读根目录；排队与转换总超时 20 秒。带周期约定的默认合同在既有Linux实测中为2页，签字区位于第2页。手机原生PDF预览/打印与iPhone系统推送仍待真机。

首次切换后前端目录因受限umask解包成0700，造成页面404，现已仅修正公开静态目录0755和文件0644。后续发布必须检查非root可读和真实页面，不能只看health接口。当前资源见 [Headless Shell 上线与资源验证](HeadlessShell上线与资源验证-20261111.md)：最终 Docker 口径16.57MiB，单次PDF峰值288.13MiB，内存上限事件/OOM/OOM kill均0。旧19:58资源报告保留为历史。

## 运维与回退

查看当前状态：

```sh
cd /opt/landlord/current
docker compose -p landlord -f compose.yml ps
docker compose -p landlord -f compose.yml logs --tail=80 landlord
```

同镜像恢复可用 `docker compose -p landlord -f compose.yml up -d --no-build --pull never`；不要使用down -v，不清空正式数据。升级前做SQLite一致性备份，并一起保存镜像、只读前端和compose配置。

此次一致性备份为`/var/backups/landlord/pre-shell-20261111-220921`，数据库quick_check=ok，含受限环境文件副本和上传资料。需要回退本次应用版本时，使用保留的上一版Compose：

```sh
cd /opt/landlord/releases/20261111-0.10.0-gh-e1d14c9
docker compose -p landlord -f compose.yml up -d --no-build --pull never --wait
curl --fail http://127.0.0.1:18930/ >/dev/null
ln -sfn /opt/landlord/releases/20261111-0.10.0-gh-e1d14c9 /opt/landlord/current
```

本次未改变Caddy，应用版本回退无需修改它。数据库回退需单独判断兼容性，不能用旧空库覆盖后续业务。本轮精确删除598badb中间镜像、release与压缩包及最终重复ZIP，VPS观察释放约2.19GiB；NAS合成数据卷与临时令牌已清理。当前和前一生产版镜像、发布目录、离线归档与备份均保留。此前22条项目缓存清理属于19:58历史操作，本轮未全局prune。

本次发布前Caddy完整原件在 `/etc/caddy/Caddyfile.before-landlord-20261111`，SHA256 `87264de97a283fc83c15ee6d368c8cbbe11578ee0644a18fa75ed1a151ed8f1b`。当前为 `47e1fa711faa085a5c864d0624278ff7743f62d014bfa5fa6e119e8150b05a24`。必要时先对比后续变更，恢复原文件、validate并reload即可撤回此次入口；不要覆盖以后其他任务的新配置或盲目回退数据库。

初始一致性数据库备份在 `/var/backups/landlord/initial-20261111/rental.db`，只代表18:58前的零账号状态，不能用来覆盖用户后来录入的资料。当前源码、制品指纹及验证见 [Headless Shell 上线记录](HeadlessShell上线验证记录-20261111.json)；[19:44再部署记录](云端再部署验证记录-20261111.json) 为历史；`doc/部署验证记录-20261111.json`保留为前次NAS部署的历史记录。

此前VPS原地构建发生全局OOM，Keepup退出137且重启网络失败；17:51已恢复原容器。本次发布前后7个原有站点入口均200，6个稳定响应指纹一致，邮件登录页仅核状态。事故原证据保留，不能以当前健康抹掉此前故障。

## iPhone Safari 真机

1. 使用初始化文件创建管理员，日常从正式HTTPS入口登录。
2. Safari分享→添加到主屏幕，从桌面图标打开。
3. 「消息与推送」点击「开启此设备通知」，用户同意授权，再点「发送测试通知」。
4. 关闭应用，确认实际出现系统通知；点击应打开已安装应用对应页面，详情仍需登录。
5. 退出/切换账号后不显示旧账号通知；关闭此设备通知后停止接收。升级由用户点「更新并重新打开」，断网显示重连页。

`accepted`表示推送服务接收，设备显示/点击才证明送达。Windows WebKit验证不能替代真机，当前为UNOBSERVED。依据：[WebKit Home Screen Web Push](https://webkit.org/blog/13878/web-push-for-web-apps-on-ios-and-ipados/)。

Swap配置依据 [swapon手册](https://man7.org/linux/man-pages/man8/swapon.8.html) 与 [Linux内核swappiness说明](https://docs.kernel.org/admin-guide/sysctl/vm.html)；构建限额依据 [Docker container driver](https://docs.docker.com/build/builders/drivers/docker-container/)。本文件的通过结论来自本任务实际运行结果。
