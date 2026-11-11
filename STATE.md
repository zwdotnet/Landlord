# Landlord 当前状态

更新日期：2026-11-11（Asia/Hong_Kong）。MAIN 直接实现与自验（SELF-ACCEPT），无独立验收。M1–M4 已完成本地及核心 Linux 验证；19:44已重新部署GitHub云端制品，静态目录权限问题已修复，19:58最终检查healthy。iPhone 系统通知实际送达仍未验证。需求见 [项目启动与实施计划](doc/项目启动与实施计划.md)，当前事实以本文件为准。

## 当前交付

- 用户最新授权“重新部署，做好收尾工作，最后检查资源占用”，已完成云端制品部署、备份、验证及精确缓存清理；项目已上传公有GitHub仓库，收尾文档随main提交。不发送外部消息，不触碰SSH/PAM/2FA。
- 正式入口：https://www.zoeng.site/landlord/ 。zoeng.site 沿原配置跳到 www。首个管理员尚未创建，正式用户/房源/租客/账单/收款/计划/任务均为 0；模拟数据仅在隔离环境。
- 首次初始化链接保存在本机受限文件 `.runtime/deploy-20261111/private/首次初始化.md`，已请求在Codex面板打开，供用户自行设置账号密码。邀请不写入本文件或访问日志；普通未持邀请的首次注册返回403。首个管理员注册后直接进入后台，修复了上游误要求隐藏入驻资料的流程及不适用的 NAS 文案。
- 目标 SSH `singapore-vps`，root@111.91.27.81，hostname hcss-ecs-b4e4；使用已有 agent/私钥，未读取私钥或改 SSH/PAM/2FA。用户已搁置2FA排查。
- `/swapfile` 4 GiB、0600 已启用，fstab 开机启用，swapfile.swap loaded/active；vm.swappiness=10，已同步修正旧 sysctl.conf 的0覆盖。原文件在 `/var/backups/landlord/swap-20261111-180034`，未重启主机。

## 已部署运行面

- 正式入口：https://www.zoeng.site/landlord/；仓库https://github.com/zwdotnet/Landlord，main已上传。应用源码e1d14c9，后续文档提交不改变该镜像对应的源码。
- 当前release：`/opt/landlord/releases/20261111-0.10.0-gh-e1d14c9`，current已指向该目录；镜像`landlord:0.10.0-e1d14c9baf3d-43011871714-1`，VPS image ID `sha256:453cac95caea2bfe273f83bf57b2a4bd19bb612f09de0b72ee7f6a7cea063da8`。云端原ID `sha256:44f1dd409e456b056b485080f1c79e9273872cbcda2143dc07e7509485c99e03`，Docker版本显示不同，已核所有rootfs层、架构及源码revision一致。
- 容器`e8c20f4a499edeee0535d8fe8c8d276c96f0e46ec5123aa144849266ac45e58c`，healthy，仅127.0.0.1:18930。512MiB RAM / 768MiB内存+swap合计、1CPU、160任务；只读根、全部capabilities移除、Go GOMEMLIMIT192MiB。79份前端文件和镜像一致，只读挂载/app/web，静态目录0755、文件0644。
- 正式数据`/var/lib/landlord/data`；初始化环境`/var/lib/landlord/bootstrap.env`仍root0600且值未变。用户/房源/租客/账单/收款/计划/任务仍全部0，quick_check=ok。首管理员邀请沿用原本机受限文件，未创建生产测试账号。
- 此次一致性备份`/var/backups/landlord/pre-github-20261111-194350`；回退release`/opt/landlord/releases/20261111-0.10.0-final`和镜像landlord:0.10.0-20261111-final均保留。旧初始空库备份只供追溯，不覆盖日后用户数据。
- Caddy SHA仍`47e1fa711faa085a5c864d0624278ff7743f62d014bfa5fa6e119e8150b05a24`，本次未改路由或reload；7个原有站点200，Keepup healthy。详细回退步骤见部署文档。
- 云端制品SHA256 `063f6bc8b443558fd33b900e4b31f395fb9b0d7f0b6f73d8106683048d58d92f`；校验后拆出的镜像、release、源码和来源记录保存在`/opt/landlord/artifacts/github-43011871714`，不依赖GitHub制品7天保留期。

## 已实现业务

1. M1：明确租客ID归属、同房同月换租隔离、旧归属人工确认；按日期续租，按当月实际天数折算入住/退租/调价，历史出账不使用未来读数。
2. M2：平台模板新增版本、新签/续签PDF快照、逐笔实收、整笔冲销留原因和原件；文件/SHA/业务快照入SQLite。收款/PDF/余额/结转事务一致、幂等，越权/CSV/删除保护。
3. M3：主动启用房源/承租关系计划；沿用缴费日，抄表/出账提前3天（最早当月1日），提醒再提前3天；1–28日可修改，香港时间08:00起每分钟检查，启动补偿。
4. 缺抄表等待补录，历史账期按先后处理，重复复用原账单；沿用季度预付/押金/折算/自定义费用/结转。条款变化需再确认，权限撤销或账号停用阻止出账；重叠计划和同承租人重复计费受保护。
5. 站内待办给有权限的房东成员，新账单只通知绑定的对应租客；读取/已读/发送前重核权限，任务/账单/消息事务一致。
6. M4：子路径manifest/SW、安装指引、离线页和用户确认更新。仅缓存离线壳，不缓存API/PDF；VAPID身份持久化、订阅/取消/自测、重试退避、过期停用、账号切换取消旧投递。锁屏只提示新消息，点击限应用scope；accepted仅表示服务接收。
7. 容器Chromium156及Noto CJK已实测。修复Crashpad需要可写HOME/XDG的问题，目录放在每次PDF临时profile中并随转换清理；保留只读根目录。带自动化约定的Linux默认合同为2页，签字区在第2页；逐页核对中文与内容，不沿用Windows早期“默认1页”的观察。

## 验证事实

| 检查 | 当前结论 |
|---|---|
| 当前公网 | 15项PASS，入口/关键资源对应云端制品，Edge/WebKit手机页、真实SW scope、离线页、401/no-store、无脚本错误；本机public/results.json |
| 当前PDF | 一次临时中文PDF成功，约3.22秒，无生产业务写入；峰值约512MiB，memory.events max=196、oom=0、oom_kill=0 |
| 资源 | 19:58 Docker口径54.25MiB，原始cgroup 316.86MiB含缓存；空闲CPU均值0.10%，主机可用908.66MiB；详见资源报告 |
| 原有站点 | 7入口200，6稳定指纹不变；Keepup healthy，其他容器未重启 |
| 既有基线 | 后端501 PASS、Linux业务15项、首管理员UI4项及前次部署验证沿用当日证据，本次没有重跑全量后端或冒称独立验收 |
| 云端构建 | Actions43011871714成功；前端类型/生产构建、日期/PWA5项、Linux镜像、只读容器/PDF及打包上传通过 |
| Safari真机 | UNOBSERVED，iPhone主屏幕安装和系统通知显示仍需用户实际设备验证 |

初次切换后安全解包产生0700静态目录，API正常但页面404；已确认并修复为0755/0644，增加非root可读和入口验证，15项最终通过。原始失败保留在`.runtime/redeploy-github-20261111/public/first-attempt.json`，未删除失败证据。当前记录见`doc/云端再部署验证记录-20261111.json`。

## 构建与事故记录

- 先前在VPS无swap时原地构建引发资源压力，内核有global OOM；Keepup退出137并因资源不足创建veth失败，OOMKilled=false，未证明被内核直接杀死。17:51已启动原容器恢复，详情见 Keepup-PM/STATE.md与Keepup/.runtime/vps-incident-20261111。
- 本次VPS只配置swap、接收/加载镜像和运行服务，没有编译/构建。实际构建在NAS WeisNAS，约4.8GiB可用；构建器最初768MiB/0.5CPU，按真实编译峰值调整到1.5GiB/1CPU、并行1，始终保留宿主余量。
- NAS使用任务专属镜像源/GOPROXY/Debian源，未改全局Docker配置。缓存保留在NAS专用BuildKit卷，构建器已停止移除；两套本轮模拟容器和数据卷已清理，源码/镜像/日志供追溯。
- 上述NAS构建是18:58历史版本；当前已改用GitHub托管runner云端构建。VPS只下载、校验、加载并运行制品，4GiB swap不改变生产机不构建的约定。

## 收尾与后续

已备份并保留当前/上一版镜像及release，归档旧候选源码后删除目录，删除candidate镜像和重复ZIP；按精确ID清理22条本项目构建缓存，其他118条保留，约释放4.00GiB。当前没有临时容器或残留Chromium。最新主机剩余磁盘18.39GiB。数据/初始化邀请未改变，代码和收尾文档同步GitHub。

用户用本机首次初始化文件创建管理员，再添加房源、租客和周期计划。PDF短时峰值接近512MiB上限，后续优化应针对实际PDF场景；保持串行与资源限额，不因空闲占用低而外推多人负载。自然账期、iPhone推送送达、完整签署、押金退还/扣费及M5微信小程序仍待后续。未安排Codex自动跟进任务；本轮部署与收尾完成，不追加构建、全量业务测试或发布。
