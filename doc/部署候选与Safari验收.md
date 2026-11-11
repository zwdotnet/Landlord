# 新加坡部署与 Safari 验收

记录日期：2026-11-11，Asia/Hong_Kong；实际当前状态以根目录 STATE.md 为准。用户本轮明确授权配置 swap、完成构建并部署，18:58 上线，19:01 公网验证通过；MAIN 自验，无独立验收。

## 正式运行

- URL：https://www.zoeng.site/landlord/；apex 沿原配置跳转到 www，主页与旧子站保留。
- 主机：singapore-vps / 111.91.27.81 / hcss-ecs-b4e4。沿用 SSH agent，未读取私钥、未改 SSH/PAM/2FA。
- release：`/opt/landlord/releases/20261111-0.10.0-final`；current 指向它。后端镜像 `landlord:0.10.0-20261111-final`，前端 release/web 只读挂载到 `/app/web`。镜像与前端清单一起构成此次发布。
- 容器只监听127.0.0.1:18930，512MiB RAM / 768MiB内存+swap合计、1CPU、160进程，根目录只读、去全部capabilities；健康检查已通过。正式数据 `/var/lib/landlord/data`；初始化环境文件 `/var/lib/landlord/bootstrap.env` 权限0600。
- 镜像归档SHA256 `8665e96d399ac3d2d30d84b7e49c14d101c0f164f55f8179d9ab2024b6b4372e`。Docker28/29显示ID不同，已核rootfs各层、架构与运行配置完全相同，详见部署验证记录。
- 生产无模拟账号，首管理员需用户自行通过本机受限初始化文件创建；邀请只放URL fragment和请求头，不写访问日志。数据库/备份包含认证资料和VAPID身份，按业务数据保护。

## Swap 与构建

`/swapfile` 为4GiB、0600，已启用并加入fstab；swappiness=10，旧sysctl.conf的0覆盖也已同步修正。fstab解析无错误，swapfile.swap loaded/active，无需重启主机。原配置在 `/var/backups/landlord/swap-20261111-180034`。没有通过耗尽内存来测试swap。

构建在家中NAS WeisNAS完成，初始可用约4.8GiB，构建器最终限制1.5GiB/1CPU、并行1。`deploy/build-image.sh` 要求至少3GiB可用内存。NAS使用任务级镜像源、GOPROXY与Debian源，未改全局Docker配置；源码、镜像、日志在 `/vol1/1000/NAS/Docker/builds/landlord-20261111-final`。专用BuildKit缓存保留，构建器和两套模拟应用已停止清理，生产VPS没有编译或构建。

Linux容器已实测合同PDF、收据、冲销、自动出账和重启持久化。Chromium156的Crashpad需要可写HOME/XDG，wrapper将其放在单次临时profile中，保持只读根目录。带周期约定的默认合同在Linux为2页，签字区整块在第2页；已查看两页，无中文乱码或裁切。手机原生PDF预览/打印与iPhone系统推送仍待真机。

## 运维与回退

查看当前状态：

```sh
cd /opt/landlord/current
docker compose -p landlord -f compose.yml ps
docker compose -p landlord -f compose.yml logs --tail=80 landlord
```

同镜像恢复可用 `docker compose -p landlord -f compose.yml up -d --no-build --pull never`；不要使用down -v，不清空正式数据。升级前做SQLite一致性备份，并一起保存镜像、只读前端和compose配置。

本次发布前Caddy完整原件在 `/etc/caddy/Caddyfile.before-landlord-20261111`，SHA256 `87264de97a283fc83c15ee6d368c8cbbe11578ee0644a18fa75ed1a151ed8f1b`。当前为 `47e1fa711faa085a5c864d0624278ff7743f62d014bfa5fa6e119e8150b05a24`。必要时先对比后续变更，恢复原文件、validate并reload即可撤回此次入口；不要覆盖以后其他任务的新配置或盲目回退数据库。

初始一致性数据库备份在 `/var/backups/landlord/initial-20261111/rental.db`，只代表零账号初始状态，不能用来覆盖用户后来录入的资料。源码与制品指纹、各项结果见 `doc/部署验证记录-20261111.json`。

此前VPS原地构建发生全局OOM，Keepup退出137且重启网络失败；17:51已恢复原容器。本次发布前后7个原有站点入口均200，6个稳定响应指纹一致，邮件登录页仅核状态。事故原证据保留，不能以当前健康抹掉此前故障。

## iPhone Safari 真机

1. 使用初始化文件创建管理员，日常从正式HTTPS入口登录。
2. Safari分享→添加到主屏幕，从桌面图标打开。
3. 「消息与推送」点击「开启此设备通知」，用户同意授权，再点「发送测试通知」。
4. 关闭应用，确认实际出现系统通知；点击应打开已安装应用对应页面，详情仍需登录。
5. 退出/切换账号后不显示旧账号通知；关闭此设备通知后停止接收。升级由用户点「更新并重新打开」，断网显示重连页。

`accepted`表示推送服务接收，设备显示/点击才证明送达。Windows WebKit验证不能替代真机，当前为UNOBSERVED。依据：[WebKit Home Screen Web Push](https://webkit.org/blog/13878/web-push-for-web-apps-on-ios-and-ipados/)。

Swap配置依据 [swapon手册](https://man7.org/linux/man-pages/man8/swapon.8.html) 与 [Linux内核swappiness说明](https://docs.kernel.org/admin-guide/sysctl/vm.html)；构建限额依据 [Docker container driver](https://docs.docker.com/build/builders/drivers/docker-container/)。本文件的通过结论来自本任务实际运行结果。
