# Landlord 当前状态

更新日期：2026-11-11（Asia/Hong_Kong）。MAIN 直接实现与自验（SELF-ACCEPT），无独立验收。M1–M4 已完成本地及核心 Linux 验证；18:58 已部署新加坡 VPS，19:01 公网验证通过。iPhone 系统通知实际送达仍未验证。需求见 [项目启动与实施计划](doc/项目启动与实施计划.md)，当前事实以本文件为准。

## 当前交付

- 用户本轮明确授权“设置新加坡 VPS 的 swap，把本项目构建好了重新部署上去”，已完成；无需再次请求路由切换确认。没有 Git 提交、推送或外部消息。
- 正式入口：https://www.zoeng.site/landlord/ 。zoeng.site 沿原配置跳到 www。首个管理员尚未创建，正式用户/房源/租客/账单/收款/计划/任务均为 0；模拟数据仅在隔离环境。
- 首次初始化链接保存在本机受限文件 `.runtime/deploy-20261111/private/首次初始化.md`，已请求在Codex面板打开，供用户自行设置账号密码。邀请不写入本文件或访问日志；普通未持邀请的首次注册返回403。首个管理员注册后直接进入后台，修复了上游误要求隐藏入驻资料的流程及不适用的 NAS 文案。
- 目标 SSH `singapore-vps`，root@111.91.27.81，hostname hcss-ecs-b4e4；使用已有 agent/私钥，未读取私钥或改 SSH/PAM/2FA。用户已搁置2FA排查。
- `/swapfile` 4 GiB、0600 已启用，fstab 开机启用，swapfile.swap loaded/active；vm.swappiness=10，已同步修正旧 sysctl.conf 的0覆盖。原文件在 `/var/backups/landlord/swap-20261111-180034`，未重启主机。

## 已部署运行面

- 本地 Git main / 基线 a647d66，M1–M4及部署修改仍在工作区。版本0.10.0用于当前小范围部署，未发行GitHub版本；保留 rental v0.7.1 / 98e65d2c65c04a8e6bba321b6774ded72c5e812d 和 MIT LICENSE。
- 正式 release：`/opt/landlord/releases/20261111-0.10.0-final`，`/opt/landlord/current` 指向该目录。镜像 `landlord:0.10.0-20261111-final`；版本化前端在 release/web，只读挂载 `/app/web`，由 web-manifest.json 核对79文件。以后升级必须一起处理镜像和该前端制品。
- NAS Docker28镜像ID `sha256:530b058ac3bc23847dd54bad830f54967f5025074cd78530aaa8f24a9923d62a`；VPS Docker29 ID `sha256:ee53c9bc4a50643d8cacedde14a79925e69100e446dc749282300c20135c9c12`。归档SHA `8665e96d399ac3d2d30d84b7e49c14d101c0f164f55f8179d9ab2024b6b4372e`；7个rootfs层、linux/amd64与运行配置逐项相同，ID差异不作为传输损坏。
- 容器 `8c804d41d3a70cefdcb7cf899e82bc83cec2a518f8a9714416ff1159b9837fde`，19:00核实running/healthy；仅127.0.0.1:18930。512MiB RAM / 768MiB内存+swap合计、1CPU、160进程，根文件系统只读、cap_drop ALL、no-new-privileges；Go GOMEMLIMIT192MiB，单实例串行PDF。
- 正式数据 `/var/lib/landlord/data`，首次邀请环境 `/var/lib/landlord/bootstrap.env`（root0600）。未把本地试用数据或模拟账号导入正式库。初始一致性备份 `/var/backups/landlord/initial-20261111/rental.db`，quick_check=ok。
- Caddy 当前SHA `47e1fa711faa085a5c864d0624278ff7743f62d014bfa5fa6e119e8150b05a24`；发布前原件 `/etc/caddy/Caddyfile.before-landlord-20261111`，SHA `87264de97a283fc83c15ee6d368c8cbbe11578ee0644a18fa75ed1a151ed8f1b`。新增子路径/CSP/API no-store，主页与旧子站保留。
- 本地预览 http://127.0.0.1:18920 ，数据 `.runtime/dev-m2/data`，exec session59031；本轮再核0用户/业务数据。前端已同步最终构建，scripts/dev.ps1 -NoBuild可重启；旧M1实例18910保留，禁止并发写同库。

## 已实现业务

1. M1：明确租客ID归属、同房同月换租隔离、旧归属人工确认；按日期续租，按当月实际天数折算入住/退租/调价，历史出账不使用未来读数。
2. M2：平台模板新增版本、新签/续签PDF快照、逐笔实收、整笔冲销留原因和原件；文件/SHA/业务快照入SQLite。收款/PDF/余额/结转事务一致、幂等，越权/CSV/删除保护。
3. M3：主动启用房源/承租关系计划；沿用缴费日，抄表/出账提前3天（最早当月1日），提醒再提前3天；1–28日可修改，香港时间08:00起每分钟检查，启动补偿。
4. 缺抄表等待补录，历史账期按先后处理，重复复用原账单；沿用季度预付/押金/折算/自定义费用/结转。条款变化需再确认，权限撤销或账号停用阻止出账；重叠计划和同承租人重复计费受保护。
5. 站内待办给有权限的房东成员，新账单只通知绑定的对应租客；读取/已读/发送前重核权限，任务/账单/消息事务一致。
6. M4：子路径manifest/SW、安装指引、离线页和用户确认更新。仅缓存离线壳，不缓存API/PDF；VAPID身份持久化、订阅/取消/自测、重试退避、过期停用、账号切换取消旧投递。锁屏只提示新消息，点击限应用scope；accepted仅表示服务接收。
7. 容器Chromium156及Noto CJK已实测。修复Crashpad需要可写HOME/XDG的问题，目录放在每次PDF临时profile中并随转换清理；保留只读根目录。带自动化约定的Linux默认合同为2页，签字区在第2页；逐页核对中文与内容，不沿用Windows早期“默认1页”的观察。

## 验证事实

| 检查 | 本轮结论与证据 |
|---|---|
| 已有后端 | 去重501 PASS、0 FAIL、0测试跳过，17个测试包；沿用当日M3基线及定点日志，本次部署未重跑全量。见阶段三四验证记录 |
| 已有业务/迁移 | 本地真实应用26项、升级10项和M2 PDF原件保留证据沿用；未冒称本轮再次全跑 |
| 最终前端 | 根路径与/landlord/类型/Vite构建通过，部署目录web-final-build.txt / web-production-build.txt |
| Linux隔离 | 15项通过：中文合同/收据、冲销、缺读数/补录出账、租客通知、原件与VAPID重启保持、无重复账单。linux-acceptance/results.json，2页合同PNG已看 |
| 首管理员UI | 4项通过，first-owner-ui/results.json；真实注册、去邀请hash、直接进入管理员页，无隐藏申请阻断或脚本错误。只在隔离Windows数据库 |
| 公网 | 12项通过，public/results.json；HTTPS版本、manifest/SW、私有API401/no-store、未邀请注册403、Edge/WebKit手机页面、作用域与离线页，无脚本错误 |
| 原有站点 | 7个入口200；除动态邮件登录页外，6份响应指纹与发布前一致。siblings-before/after.json。Keepup持续healthy |
| 运行资源 | 19:00 Landlord约31.9MiB，Keepup约78.3MiB，VPS可用约878MiB，swap用约135MiB；仅该时刻观察，不代表峰值或长期监控 |
| 生产数据 | 0用户/业务对象，quick_check=ok，initial-data-check.json；未在生产创建验收账号 |
| Safari真机 | UNOBSERVED：需用户用iPhone添加主屏幕后主动授权、自测并确认系统显示/点击；Windows WebKit不替代真机 |

本轮证据统一在 `.runtime/deploy-20261111`，汇总见 [部署验证记录](doc/部署验证记录-20261111.json)。失败与修正保留：初次SSH转发被NAS策略拒绝（未修改SSH策略，改由NAS本机HTTP回传）；只读HOME导致PDF失败后已修复；首次UI载具定位错误后定点通过；Docker28/29显示ID不同已核层与配置；shell CRLF已统一LF。没有将旧失败删去或记成通过。

## 构建与事故记录

- 先前在VPS无swap时原地构建引发资源压力，内核有global OOM；Keepup退出137并因资源不足创建veth失败，OOMKilled=false，未证明被内核直接杀死。17:51已启动原容器恢复，详情见 Keepup-PM/STATE.md与Keepup/.runtime/vps-incident-20261111。
- 本次VPS只配置swap、接收/加载镜像和运行服务，没有编译/构建。实际构建在NAS WeisNAS，约4.8GiB可用；构建器最初768MiB/0.5CPU，按真实编译峰值调整到1.5GiB/1CPU、并行1，始终保留宿主余量。
- NAS使用任务专属镜像源/GOPROXY/Debian源，未改全局Docker配置。缓存保留在NAS专用BuildKit卷，构建器已停止移除；两套本轮模拟容器和数据卷已清理，源码/镜像/日志供追溯。
- NAS制品目录 `/vol1/1000/NAS/Docker/builds/landlord-20261111-final`；最终运行由Docker镜像+版本化只读前端共同组成。未来仍在独立Linux环境构建，不因有swap而在生产VPS编译。

## 下一步

用户随后指定新建公有 GitHub 仓库用于云端构建，已创建 `https://github.com/zwdotnet/Landlord`。本地新增 `.github/workflows/cloud-build.yml` 和云端镜像检查/打包脚本，使用 GitHub 托管 runner；正在准备首次上传与运行验证。此前“没有Git提交、推送”仅描述18:58那次部署，后续事实以本段及最终构建记录为准。此工作不切换现有线上版本，不使用生产SSH密钥。

用户打开本机首次初始化链接创建管理员，再添加房源/租客并确认周期计划。Safari按分享→添加到主屏幕→从桌面打开→消息与推送→开启并自测。首个自然账期、真实推送送达、完整签署状态机、押金实际退还/扣费和M5微信小程序仍属后续范围。未安排Codex自动跟进任务；swap与上线已完成，新增云端构建工作以上一段为准，不追加全量业务测试或生产发布。
