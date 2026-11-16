# 新加坡部署与 Safari 验收

> 最新追加：2026-11-13 01:00已发布源码1753277，01:02服务器及公网38项复核通过；分段耗时、当前版本、备份和回退见本文件末节。以下旧记录保留原观察时间。

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

## 2026-11-12 QA修复版发布

用户在修复自验完成后明确授权“提交并部署”。源码`65953074545c28d8ccb6429d099ad69e499af93b`已提交并推送main，完整保留此前待发布内容；镜像仍为0.10.0，以commit/run区分制品。小程序源码入Git，客户端未上传或发布，多房东接入/一次性登记链接/管理员重构均未启动。

- 正式入口：<https://www.zoeng.site/landlord/>；原首页及其他子站保留。22:45:41（香港时间）发布，22:48服务器最终复核通过。
- [云端构建43237672099](https://github.com/zwdotnet/Landlord/actions/runs/43237672099)成功，GitHub runner执行Web检查、Linux镜像构建及只读中文PDF冒烟；VPS没有构建。发布前本地完整门禁Go493/15包、Web及Node5、独立HTTP52、桌面/390×844各12闭环、原生兼容8均通过，详见[测试报告§9—10](系统性测试报告-20261112.md)。
- current：`/opt/landlord/releases/20261112-0.10.0-qa-6595307`；镜像`landlord:0.10.0-65953074545c-43237672099-1`；VPS image ID `sha256:7153b465f4353e9d085140aac0056fd554642285c1fef2f54b62c8239ee67a2c`，核对了OCI源码revision及rootfs层。78份静态文件来自同一制品，目录0755/文件0644、只读挂载。
- 制品`/opt/landlord/artifacts/github-43237672099`；原GitHub ZIP SHA256 `31515fe64904065f032c9e58f73a7a95d84bcdcc54398dd300ce4a4d58f152ca`。应用512MiB RAM/768MiB内存加swap/1CPU/160pids、只读根、127.0.0.1:18930、Headless Shell与PDF串行20秒保持，healthy且未OOM。最终单点Docker8.305MiB为空闲样本，不作为峰值/压力结论。
- 正式数据仍`/var/lib/landlord/data`。受限`/var/lib/landlord/bootstrap.env`安全追加WX_APP_ID/WX_APP_SECRET，其他键保留、0600；值未写日志或Git。公网空code请求400/no-store，证明配置与路由生效，未外调微信、未建微信身份或生产测试账号。
- 发布前在线一致性备份`/var/backups/landlord/pre-qa-20261112-223009`；停应用后再次备份`/var/backups/landlord/pre-qa-switch-20261112-224535`，含DB、上传资料、环境、旧Compose及Caddy。先在不联网/不挂生产数据的副本验证新增微信表和账单4个快照字段，并验证上一应用可读迁移后副本，再切换；无批量更正历史金额。
- 最终quick_check=ok，原13业务表旧字段/旧ID匹配最新冻结备份，原附件/备份/导出静态文件指纹保持，5份旧日志前缀完整。users3/rooms1/tenants1/bills1/items2/attachments1、payments/contracts/issued_documents0、微信身份0是当时只读观察，不假定以后仍相同。
- 公网38项通过：HTTPS健康、精确index和有关JS/SW/manifest指纹、匿名接口保护、已删除接口404、微信空code拒绝、Edge桌面与WebKit390×844登录页渲染/宽度、SW作用域，无页面脚本异常。未登录生产账号做业务验收。物理手机、图片/账单等登录后WebKit兼容、打印/推送及正式小程序仍未验。
- Caddy SHA256 `31bc98fc5356bc66a170e9ad493da041c5742de72c990fd0b75149ebd9762f5c`未改；原首页SHA256 `fdc6985aedfe85031c31a9caa0ec4b6a03f1e5cad89fa41be461199236378096`保持。其他3容器ID和启动时间未变，7个原站点入口200/6个稳定响应指纹相同；邮件页动态内容只核状态。

上一版本release`/opt/landlord/releases/20261112-0.10.0-apply-ef3cbee`及镜像`landlord:0.10.0-ef3cbee57268-43161630747-1`保留。需要回退应用时，先核对后续变更和当前业务，再使用匹配的旧Compose/前端：

```sh
docker compose -p landlord -f /opt/landlord/releases/20261112-0.10.0-apply-ef3cbee/compose.yml up -d --no-build --pull never --wait
curl --fail http://127.0.0.1:18930/api/health
curl --fail http://127.0.0.1:18930/ >/dev/null
ln -sfn /opt/landlord/releases/20261112-0.10.0-apply-ef3cbee /opt/landlord/current
```

这只切应用/前端。旧应用启动迁移后副本已通过，但后续数据仍需另判；不能恢复旧数据库覆盖新业务，不回退正确的`/landlord`基址/CSP或其他站点配置，不用down -v。WX受限配置可保留，旧代码不使用它。

发布证据在本机`.runtime/deploy-qa-20261112/`与服务器`/opt/landlord/operations/qa-20261112/`，包含预检、备份、制品、迁移预演、发布、最终服务器和公网回执；公开截图仅本机。脚本含固定旧状态/独占目录，仅作模板，不能原样重跑。初轮静态文件断言因正常日志追加失败，已核实原前缀保持并修正检查，详见测试报告§10；未因此改业务或恢复旧库。上述为SELF-ACCEPT，无独立/真机验收，未安排后台监控。

## 2026-11-13 可靠性修复发布与耗时

用户明确授权“提交 部署，看这次部署会不会快一点”。源码`17532776461d772d2683a1fdd2910680824db006`已提交/推送main，含统计区间、损坏备份拒绝和房源请求异常修复，以及测试/发布工具/备案前材料；无数据库结构或依赖版本变化。已通过的完整门禁Go501项与候选源码指纹匹配，本次复用其结果，不因文档提交再跑全量业务。

- 构建：[Actions43263322293](https://github.com/zwdotnet/Landlord/actions/runs/43263322293)，成功；云端前端、只读运行与中文PDF检查通过。VPS直下345085602字节制品，ZIP SHA256 `2f07cb82be73f2cc8830247cda122ec74b0df3def8060bff963d9e33216ae22d`匹配GitHub；三归档校验、内外来源、78份前端和Docker rootfs/revision均匹配。
- 当前：`/opt/landlord/releases/run-43263322293-17532776461d`；镜像`landlord:0.10.0-17532776461d-43263322293-1`，实际镜像ID `sha256:9b47f43ca2594e06eaab14e7b6ec0a1f8674dcba2d89d0ccf66793fa4bd0fa36`。01:00:39切换完成，01:02复核通过，均为香港时间。生产未构建、未创建测试业务、未真实交换微信code。
- 冻结备份：`/var/backups/landlord/pre-run-43263322293-20261112-170037`（UTC名）；最新旧版为`/opt/landlord/releases/20261112-0.10.0-qa-6595307`，镜像`landlord:0.10.0-65953074545c-43237672099-1`，均保留。
- 服务/资料：healthy，无OOM；512MiB/768MiB/1CPU/160pids、只读根与前端、loopback18930保持。quick_check=ok，数据库结构及14个业务表原字段/ID与冻结备份一致，5份非数据库文件哈希及6份日志旧前缀保持；bootstrap字节/0600和Caddy未改。
- 公网38项通过、脚本异常0，Edge桌面及WebKit390×844登录可用；根首页和其他站点保持，3个其他容器未重建/重启、7个入口200。WebKit业务的SW限制、物理手机/打印/通知、正式小程序域名/审核和完整自然月账期未验，不因发布成功视为解决。小程序客户端未上传。

首次使用`deploy/release.py --apply`成功，服务器工具28.99秒，其中包校验3.09秒、备份/切换/核对段1.44秒；VPS直下与ZIP校验89.59秒，含解包93.65秒。云构建用时4分33秒，云构建起到上线8分31秒，到复核记录10分32秒；最初阅读准备、此前完整门禁、后续文档收尾不计入此口径，上次33分钟未按相同边界埋点，不给出精确提速比例。详细分段见[测试报告第12节](系统性测试报告-20261112.md)。失败回退分支未触发实演。

发布收尾文档提交`ca04b2d51fa5397fc6d3861b4b0c50c6f6baa729`已推送；从发布秒表起点00:51:59到01:07文档推送收尾15分21秒，仍不含最初阅读准备及此前完整门禁。此后文档整理仅核对既有回执，没有重新访问生产或触发发布；线上源码仍为1753277。

需要回退时先核对现场及后续业务兼容性，再仅切旧应用与前端，健康/精确入口通过后更新current：

```sh
docker compose -p landlord -f /opt/landlord/releases/20261112-0.10.0-qa-6595307/compose.yml up -d --no-build --pull never --wait
curl --fail http://127.0.0.1:18930/api/health
curl --fail http://127.0.0.1:18930/ >/dev/null
ln -sfn /opt/landlord/releases/20261112-0.10.0-qa-6595307 /opt/landlord/current
```

以上为回退参考，**本次未执行回退**；不恢复旧数据库覆盖后续业务，不回退Caddy或WX配置，不用down -v。证据位于本机`.runtime/deploy-readiness-20261113/`，服务器`/opt/landlord/operations/readiness-20261113/`及`/opt/landlord/artifacts/github-43263322293/`，新release内有published.json。所有旧release、镜像、备份和失败证据保留；后续单独文档提交不改变本次部署的源码revision1753277。执行者SELF-ACCEPT，无独立验收，未安排后台监控。

## 2026-11-14 房东小程序接口与SW修复发布

用户在本地提交623a21d后明确指令“好，接下来部署”。本轮推送该源码并发布Web/API，包含SW订阅权限保护、房东微信绑定/快捷登录、抄表/账单/实收接口；小程序客户端仍未上传/发布。多房东来源、一次性登记链接、管理员重构及小程序冲销均未启动，没有生产测试业务。

- [云构建43592881908](https://github.com/zwdotnet/Landlord/actions/runs/43592881908)成功，源码`623a21d30366cd95d6590fe7b1ef1284fd4dba64`，GitHub完成Web检查、Linux镜像构建、只读运行与中文PDF冒烟。VPS只下载/加载制品，没有构建。镜像标签仍以0.10.0加commit/run区分。
- 当前：`/opt/landlord/releases/run-43592881908-623a21d30366`；镜像`landlord:0.10.0-623a21d30366-43592881908-1`，实际ID`sha256:a777001b5e2eff27a63bfc4ddc365d46ca8a51671241050e4d87542aa5e6a6a5`。核对源码revision、rootfs层与78份前端。20:11:59写出发布回执，20:13服务器、20:14公网复核通过，均为香港时间。
- 制品：`/opt/landlord/artifacts/github-43592881908/`，原GitHub ZIP为345224737字节，SHA256 `d24bbeca48b66caf0d71ee585c5b6445ce78d31e64d936e5f99eadfdc09dbe1d`匹配。内外来源、三文件校验和、归档成员/前端完整性由现有release.py核对，未改发布工具代码。
- 冻结备份：`/var/backups/landlord/pre-run-43592881908-20261114-121157`（UTC名），含一致性SQLite、附件/日志、受限环境、旧Compose与Caddy。旧release`/opt/landlord/releases/run-43263322293-17532776461d`及其镜像保留。current在健康、精确前端、数据/配置核对通过后切换；失败回退分支未触发。
- 原数据：quick_check=ok、结构不变，14个旧业务表原字段/ID与冻结库一致；8份非数据库文件哈希及11份旧日志前缀保持。users3、rooms1、tenants1、bills1、items2、attachments1，payments/documents/contracts/plans/tasks和微信身份0；没有新建生产账号或录入业务。
- 容器healthy、无OOM，512MiB RAM/768MiB内存加swap、1CPU、160pids、只读根和前端、loopback18930保持；bootstrap原字节/0600、WX键存在及Caddy保持。其他3容器未重建/重启、7个站点入口200、6个稳定响应指纹保持（邮件页动态只断言状态）。
- 发布前完成Go553/15含测试包、Web类型/构建、Node8及发布Python8组成项；PowerShell5将unittest正常stderr误判为NativeCommandError，原入口退出1，已单独复核Python8项为0/OK，Go/Web未重跑。原错误保留，不声称verify.ps1整体退出0。公网47项、脚本异常0，覆盖精确静态文件、登录页、新旧API匿名鉴权、空微信code交换前拒绝和SW作用域；物理手机、生产真实微信交换与登录后业务未验。详细证据见测试报告§19。

云构建5分21秒；VPS直下并校验102.73秒，含解包/包验证109.16秒；发布工具28.12秒，其中包校验0.99秒、冻结备份/切换/核对段1.66秒。构建起至发布回执9分3.54秒，至公网验证11分41.01秒；本次完整检查与云构建并行，初读和最终文档另计，不能与此前复用门禁或未埋点的33分钟估计直接比较。

回退仍先核对现场和后续业务兼容性，再仅切回前一应用/前端，验证后更新current：

```sh
docker compose -p landlord -f /opt/landlord/releases/run-43263322293-17532776461d/compose.yml up -d --no-build --pull never --wait
curl --fail http://127.0.0.1:18930/api/health
curl --fail http://127.0.0.1:18930/ >/dev/null
ln -sfn /opt/landlord/releases/run-43263322293-17532776461d /opt/landlord/current
```

本轮未执行回退，不恢复旧数据库覆盖后续业务，不用down -v或回退Caddy/WX。证据在本机`.runtime/deploy-owner-20261114/raw/`，服务器`/opt/landlord/operations/owner-20261114/`和制品目录，新release内有published.json。凭据和短时签名下载位置仅在进程中使用，未落盘/入日志；没有后台监控。执行者SELF-ACCEPT，无独立/真机验收。发布收尾文档另行提交推送，应用源码revision保持623a21d。

## 2026-11-16 审查修复版Web/API发布

用户最新指令“部署”明确授权生产Web/API，替代前轮这部分“不部署”限制；微信后台、体验版/正式版上传仍未授权，也未执行。使用已验证候选437b961，不重复构建产品、不在VPS构建，不替换原19032预览。

- 香港时间11:13:12发布，current为`/opt/landlord/releases/run-43784715179-437b961d85da`，镜像`landlord:0.10.0-437b961d85da-43784715179-1`；公网`/api/version`四项构建身份与云包精确一致。当前版本仍0.10.0，以完整revision、run和构建时间区分。
- 复用[云43784715179](https://github.com/zwdotnet/Landlord/actions/runs/43784715179)及[隔离组合演练43789242881](https://github.com/zwdotnet/Landlord/actions/runs/43789242881)。350961751字节原ZIP SHA256 `2fb52d82fc1618a02a9ff60cbdf241287d2da00b8c9273e0f89655b8258e82b3`；GitHub元数据、包内三份SHA、回执、revision/RootFS及78份Web清单均核对。
- VPS Docker29.1.3使用containerd存储，加载后image ID为OCI manifest摘要`sha256:ee5a96235c3f509713cd490ef323e05ed0f5285f688239def4fcbd108d41c198`，归档config摘要为云build.json记录的`sha256:c0768926c38192d6befba8c701c1905d37af4cd17a390868262aa46c004ad3c1`。已核对OCI index→manifest→config完整摘要链及层/revision；二者口径不同，不能直接判为加载错误。首次额外预演的简单ID相等断言失败发生于停写前，原失败保留，产品未改。

### 真实资料保护及服务器内预演

数据与配置只在原服务器受限目录处理，没有复制至本机/GitHub，也不对外开放测试服务。先停全部应用写入、创建并校验完整`/var/backups/landlord/full-pre-review-20261116-01.zip`，随后立即恢复原服务，停写到健康1.406s。归档包含一致性DB、完整文件及受限环境，SHA256 `95ddde6ee7bb5cdf794be5c53a719e6783c62943d382fa1c05ced59ae5a20785`；备份0.201s、恢复0.042s，仅为当次真实小体量观察，不作长期RTO。

恢复至服务器全新目录后，38张业务表/结构及24份原非主库文件一致；依次以实际新旧Linux镜像、network=none、只读根/512MiB/1CPU/160pids运行并停止，再核对原业务/结构及原件。没有挂生产data或加载生产环境到测试容器，不发送登录/收款等业务请求；原样DB中的必要配置仍属受限副本，未当作已脱敏资料上传。既有自动计划/周期任务均0，外发通路由network=none封闭。测试容器已移除，受限备份/预演原件保留。

### 首轮回退与发布工具修正

首轮正式切换执行新启动后，`deploy/release.py`把`.last_preupgrade_snapshot`从旧版标记更新为新版身份的正常行为当作“Existing uploaded file changed”，触发自动应用回退623a21d。current未切、新旧库未互相覆盖，旧服务恢复healthy；首轮apply.log、冻结备份及失败暂存目录全部保留。

修正只涉及发布工具：带完整binary_identity的包精确核对新标记为`version|buildTime|gitCommit`；缺失、错误、符号链接标记均拒绝，其余旧文件仍按SHA逐个保护。无身份元数据的历史回退包继续要求原标记保持。新增5项回归，连同原12项包/身份/回执测试，Windows和Linux各17项通过；不改产品二进制或候选制品。服务器实际使用修正工具SHA256 `091f3442fb639b8dde14cc1cb2e5678dfce78af849ad825dceb5a04d00561c4a`。

二轮正式冻结备份为`/var/backups/landlord/pre-run-43784715179-20261116-031311`，含DB、全部文件、受限环境、旧Compose和Caddy。工具包验证1.60s、备份/切换/核对1.11s、工具总2.91s；这不包含下载100.24s、预演、首轮失败处理或文档收尾，不与旧33分钟估计作比例比较。

### 最终核对、配置与回退入口

- 容器healthy、未OOM，资源限额、只读根/前端及loopback18930保持。38张原业务表原字段/ID及结构一致，quick_check=ok，journal_mode由delete切至wal。声明外键仍0，foreign_key_check零异常不能证明未声明的业务关系全部正确。
- 13份原非DB文件SHA、17份旧日志前缀保持；快照标记按新身份核对，必要的新备份/日志允许追加。未创建生产测试账号/微信身份或提交实收，用户会话记录保持。
- bootstrap仅追加`TRUSTED_PROXIES=172.19.0.1/32`，其他原字节前缀及0600保持；原文件受限副本仅存服务器。11:15重新实测Caddy上游127.0.0.1:18930，应用对端::ffff:172.19.0.1；Caddy SHA保持。不同客户端分桶引用既有隔离TCP证据，本轮不在生产灌流量触发限流。
- 3个其他容器ID/启动时间不变；原7个入口200，6个稳定内容指纹保持，邮件页动态内容仅核状态。公网48项检查通过、脚本异常0，Edge桌面与WebKit390×844登录页/截图人工查看正常，精确二进制身份另行通过；新增手机号接口匿名401，已不是旧版404。没有生产账号登录、真实微信code交换、物理手机/系统通知或打印验收。

前版`/opt/landlord/releases/run-43592881908-623a21d30366`及镜像保留。需要回退时先核对后续业务和兼容性，只切该目录的应用/前端，健康及精确入口通过后更新current；不恢复冻结数据库覆盖后来业务，不用down -v，不回退Caddy或其他环境值。新的精确代理项对旧版不生效，但无需为应用回退改动其他配置。

本机证据`.runtime/deploy-review-20261116-01/raw/`；服务器`/opt/landlord/operations/review-20261116-01/`和制品`/opt/landlord/artifacts/github-43784715179/`。首轮备份/日志和`failed-release-attempt01/`不覆盖；只读取脱敏的JSON元数据回执到本机。工具修正/文档以[skip ci]提交，复用上述已验产品，不重新构建镜像；工具补丁与线上产品437b961来源分开记录。SELF-ACCEPT，无独立验收，未安排后台监控。小程序备案/完整类目/域名、隐私联系/产品入口、稳定审核服务及实际手机仍待落实；完整定时/异地备份方案未启用。
