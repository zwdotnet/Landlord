# Landlord 当前状态

更新日期：2026-11-11（Asia/Hong_Kong），最终快照 2026-11-11T22:13:08.023727+08:00。用户选择 Headless Shell 后，22:10 已完成云端构建、隔离业务验证、VPS 切换和收尾。MAIN 直接实现与自验（SELF-ACCEPT），无独立验收。需求见 [项目启动与实施计划](doc/项目启动与实施计划.md)。

## 当前交付

- 正式入口：<https://www.zoeng.site/landlord/>；仓库 <https://github.com/zwdotnet/Landlord>。源码提交 `5323d747465d3a0aa4d55b25565108644485a71f`，云端 [Actions 43033817431](https://github.com/zwdotnet/Landlord/actions/runs/43033817431) 成功。后续文档提交不改变该镜像源码。
- 生产账号及业务对象仍全部为 0，SQLite quick_check=ok。首个管理员由用户使用本机受限文件 `.runtime/deploy-20261111/private/首次初始化.md` 的邀请自行创建，邀请和环境值未变；未创建生产测试账号。
- Linux PDF 已由完整 Chromium 改为 Headless Shell，保留 Noto CJK、原 HTML、归档指纹和串行转换。排队与转换总超时由 45 秒改为 20 秒，解决超过 HTTP 30 秒写限额后无法返回错误的问题；超时不落收款和余额。
- iPhone 主屏幕安装和系统通知实际送达仍未验证（UNOBSERVED），微信小程序未实现。

## 已部署运行面

- SSH `singapore-vps` → root@111.91.27.81，hostname hcss-ecs-b4e4，沿用既有身份；未读取私钥、未改 SSH/PAM/2FA。
- 当前 release：`/opt/landlord/releases/20261111-0.10.0-shell-5323d74`，`/opt/landlord/current` 指向它；镜像 `landlord:0.10.0-5323d747465d-43033817431-1`，VPS ID `sha256:daace17a0dac1a5e7c3c3d58e9e99bef6975730fc0458d4c423de501a1bf8988`。云端 ID `sha256:cd7959c521572a252203e3f94735c5b6ab4d35dbca4cd766f0c3058f508b5e97`，rootfs 全部层、架构及 revision 一致。
- 容器 `d7692279b7984f23d78f7447c342f4b80f95b4665cb9731259722b88f6ad59a2` healthy，仅 127.0.0.1:18930；512 MiB RAM、768 MiB 内存+swap 合计、1 CPU、160 任务。只读根、移除全部 capabilities、GOMEMLIMIT192MiB。同镜像 79 份前端文件只读挂载 `/app/web`，目录 0755、文件 0644。
- 正式数据 `/var/lib/landlord/data`；初始化环境 `/var/lib/landlord/bootstrap.env` 仍 root0600。VAPID、邀请及数据沿用，用户/房源/租客/账单/收款/计划/任务仍全 0。
- 切换前 SQLite 一致性备份 `/var/backups/landlord/pre-shell-20261111-220921`，含受限环境和上传资料；回退 release `/opt/landlord/releases/20261111-0.10.0-gh-e1d14c9`、对应完整 Chromium 镜像及归档均保留。更早 18:58 版本也保留；不得用旧空库覆盖后来业务。
- 最终 ZIP SHA256 `64e2299cf9356a2c708cb39b46ef1b63c4ac8f7577b37d4c9408293843814120`；离线镜像、release、源码来源与操作记录保存在 `/opt/landlord/artifacts/github-43033817431`，重复 ZIP 已删除，不依赖 GitHub 7 天保留期。
- Caddy SHA `47e1fa711faa085a5c864d0624278ff7743f62d014bfa5fa6e119e8150b05a24` 不变，无 reload；原有 7 入口均 200，6 稳定内容指纹不变，Keepup healthy，其他容器未重启。
- 4 GiB `/swapfile` 0600、fstab 持久启用，swappiness=10；配置备份 `/var/backups/landlord/swap-20261111-180034`。VPS 不构建，GitHub 托管 runner 构建，NAS 只承载隔离业务验证。

## 已实现业务

1. M1：明确租客ID归属、同房同月换租隔离、旧归属人工确认；按日期续租，按当月实际天数折算入住/退租/调价，历史出账不使用未来读数。
2. M2：平台模板新增版本、新签/续签PDF快照、逐笔实收、整笔冲销留原因和原件；文件/SHA/业务快照入SQLite。收款/PDF/余额/结转事务一致、幂等，越权/CSV/删除保护。
3. M3：主动启用房源/承租关系计划；沿用缴费日，抄表/出账提前3天（最早当月1日），提醒再提前3天；1–28日可修改，香港时间08:00起每分钟检查，启动补偿。
4. 缺抄表等待补录，历史账期按先后处理，重复复用原账单；沿用季度预付/押金/折算/自定义费用/结转。条款变化需再确认，权限撤销或账号停用阻止出账；重叠计划和同承租人重复计费受保护。
5. 站内待办给有权限的房东成员，新账单只通知绑定的对应租客；读取/已读/发送前重核权限，任务/账单/消息事务一致。
6. M4：子路径manifest/SW、安装指引、离线页和用户确认更新。仅缓存离线壳，不缓存API/PDF；VAPID身份持久化、订阅/取消/自测、重试退避、过期停用、账号切换取消旧投递。锁屏只提示新消息，点击限应用scope；accepted仅表示服务接收。
7. 容器 Chromium Headless Shell 156 及 Noto CJK 已实测，完整 Chromium 已移除。修复Crashpad需要可写HOME/XDG的问题，目录放在每次PDF临时profile中并随转换清理；保留只读根目录。带自动化约定的 Linux 默认合同为 2 页，签字区在第2页；逐页核对中文与内容，不沿用Windows早期“默认1页”的观察。

## 验证事实

| 检查 | 当前结论 |
|---|---|
| 当前公网 | 15 项 PASS：入口与资源指纹、Edge/WebKit、SW scope、离线页、401/no-store、无脚本错误 |
| PDF 业务 | 累计 30 项通过，最终镜像复验 16 项，覆盖合同/续签/收据/冲销/幂等/长模板/失败与超时回滚/重启 |
| PDF 质量 | 15 份 SHA 与文字检查通过，最终 7 页抽样版面目视通过；长合同 31 页 |
| 本轮后端 | 定点 3 项 PASS；此前 501 项全量是历史基线，本轮未重跑 |
| 云端构建 | 43033817431 成功；前端类型/生产构建、日期/PWA 5 项、Linux Shell 实际参数中文 PDF 检查通过 |
| 当前资源 | 最终 Docker 约 16.57 MiB；原始 cgroup 187.79 MiB 含缓存，不能相加；空闲 CPU 均值 0.13% |
| 当前 PDF 峰值 | 288.13 MiB，约 1.41 秒；max/oom/oom_kill 均 0，本项目 swap=0 |
| 主机余量 | MemAvailable 979.82 MiB，根盘剩余 16.76 GiB |
| Safari 真机 | UNOBSERVED，Windows WebKit 不替代 iPhone 系统通知实测 |

资源口径和比较见 [Headless Shell 上线与资源验证](doc/HeadlessShell上线与资源验证-20261111.md)，机器记录见 [上线验证记录](doc/HeadlessShell上线验证记录-20261111.json)。104 MiB 为先前 NAS 独立渲染器样本，不是生产完整服务指标；旧 VPS 约 512 MiB 诊断样本与本轮条件不同，不据此计算节省率。

## 历史与收尾

VPS 无 swap 时原地构建曾引发全局 OOM，Keepup 退出137并因资源不足创建veth失败，OOMKilled=false，未证明被内核直接杀死；17:51 已恢复。原记录在 Keepup-PM/STATE.md 与 Keepup/.runtime/vps-incident-20261111。此前 NAS 构建及 19:44 GitHub 版部署见 [部署文档](doc/部署候选与Safari验收.md)、[19:58资源记录](doc/新加坡VPS资源检查-20261111.md)；本轮没有在 VPS 或 NAS 构建。

已精确删除本轮 `598badb` 中间镜像、release 与压缩包、最终重复 ZIP；VPS 观察释放约 2.19 GiB。NAS 临时容器、合成数据卷与测试令牌已清理。保留当前/前一生产版镜像与 release、离线归档、一致性备份及失败证据；无全局 prune、无其他服务重启。先前 0700 静态目录导致页面404的教训已纳入发布检查，当前 0755/0644 验证通过。

本轮优化、部署和收尾完成。生产 PDF 继续串行，资源限额保持 512 MiB；自然账期、iPhone 推送送达、手机 PDF 打印、完整签署、押金退还/扣费及 M5 微信小程序仍待后续。未安排 Codex 自动跟进任务。
