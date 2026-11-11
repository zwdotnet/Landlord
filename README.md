# Landlord

以 [TechFunWay/rental](https://github.com/TechFunWay/rental) v0.7.1 为起点改造的房东管理平台，面向自己和少数熟悉的房东。Web 使用 Vue 3 + TypeScript，后端使用 Go + Gin + GORM + SQLite。

当前已实现账单隔离、日期续租与折算、合同 PDF 归档、逐笔实收与冲销，以及周期抄表、自动出账、站内提醒、PWA 安装更新与 Web Push。已部署至新加坡 VPS，Linux 中文 PDF 与公网 Edge/WebKit 验证通过；iPhone 真机系统通知和微信小程序仍待后续完成。

正式入口：<https://www.zoeng.site/landlord/>。首次管理员需使用部署者提供的初始化邀请链接，自行设置账号密码；日常使用正式入口登录。部署与回退见 [部署记录](doc/部署候选与Safari验收.md)。

当前线上运行GitHub云端构建制品（源码e1d14c9，2026-11-11重新部署）。部署、备份和清理已完成；资源实测见[新加坡VPS资源检查](doc/新加坡VPS资源检查-20261111.md)，单次PDF峰值接近512MiB容器上限，后续保持串行PDF并优先优化该场景。

- 当前需求、技术决定、验收及阶段安排：[项目启动与实施计划](doc/项目启动与实施计划.md)
- 当前事实与验证结果：[STATE.md](STATE.md)
- 改造前实机评估：[rental 实机验证与复用建议](doc/rental实机验证与复用建议-20261111.md)
- 原始上游说明：[upstream-rental-README.md](docs/upstream-rental-README.md)

## Windows 本地运行

需要 Node.js、npm、Go 1.26.5、支持 CGO 的 C 编译器，以及用于生成 PDF 的本机 Edge/Chrome/Chromium。当前工作区已放置便携 Go 和 LLVM-MinGW，`scripts/use-local-toolchain.ps1` 会在当前进程中使用它们；这些本机工具位于忽略目录 `.runtime/toolchains`，不随 Git 分发。

```powershell
cd F:\zhangwei\AI\Landlord
npm --prefix web ci --no-audit --no-fund
powershell -ExecutionPolicy Bypass -File .\scripts\dev.ps1
```

打开 <http://127.0.0.1:18920>。全新数据库由首个注册账号成为管理员；数据保存到 `.runtime/dev-m2/data`。Ctrl+C 停止。已有构建时可用 `-NoBuild`；可用 `-Port` 指定端口，`-Instance` 指定独立的数据目录。M2 预览与旧版 18910 实例使用不同数据库，不能让两个版本同时读写同一数据库。开发启动关闭了上游匿名使用统计。

## 合同与收据

在「租户管理 → 合同 → 合同 PDF」生成和查看归档原件；续租协议需选择对应续租记录。管理员在「系统配置 → 平台合同模板」维护正文，每次保存创建新版本。原件不随档案修改或模板升级改变，生成的是待签署打印稿，签署件通过原有附件入口上传。

在账单登记线下实收后自动生成本次收据，在账单详情或收款记录查看；填原因可整笔冲销，原流水与原 PDF 保留，新增负数流水和冲销凭证。当前账单的打印入口统一显示应缴通知单，逐笔收据读取归档文件。旧流水不会自动伪造历史 PDF。

PDF 和数据快照分别存入 SQLite，现有数据库备份可一起保存；下载会校验文件指纹。Windows 自动查找 Edge/Chrome，也可设置 `LANDLORD_PDF_BROWSER` 为浏览器可执行文件路径。Linux 镜像的 Chromium 与 Noto CJK 已实测，HOME/XDG 位于单次转换临时目录，兼容只读容器；转换器不可用时收款/冲销整体不落库，恢复后用原表单重试。同一实例串行转换 PDF，含排队最长等待 45 秒。

## 周期任务与提醒

房东在「周期任务」按房源和明确的承租关系建立计划。默认沿用缴费日，抄表和出账提前 3 天（最早当月 1 日），再提前 3 天提醒；日期可调整，范围为 1–28 日。所有日期以香港时间 08:00 为起点，每分钟检查，启动后补偿停机期间的到期任务。

当期缺抄表时等待，补录后自动继续；多个欠缺账期按先后处理。重复检查复用原账单，租约或费用条款改变时需要重新保存确认；现有租客需房东主动启用计划。抄表待办发给有权限的房东成员，新账单只通知绑定的对应租客。

在「消息与推送」查看站内消息、标记已读、开启/关闭本设备通知和发送测试通知。Safari 请先通过分享菜单「添加到主屏幕」，从桌面图标打开后点「开启此设备通知」。系统推送需要 HTTPS 和用户授权；服务返回接收成功不代表手机已显示。锁屏通知只提示有新消息，详情需登录查看。退出账号会解除本设备推送；离线仅显示重连页，不缓存业务 API 或 PDF。

后续构建使用 [GitHub Actions](https://github.com/zwdotnet/Landlord/actions)，步骤与制品说明见 [GitHub 云端构建](doc/GitHub云端构建.md)。GitHub 托管 runner 构建前端与 Linux 镜像，保存可部署制品，发布仍单独执行。`deploy/build-image.sh` 保留为独立 Linux 构建机的备用入口，要求至少有 3 GiB 可用内存。生产 VPS 已设置 4 GiB swap，仅接收制品并运行服务。

## 验证

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\verify.ps1 -Focused
powershell -ExecutionPolicy Bypass -File .\scripts\verify.ps1
```

默认运行完整后端测试、前端类型检查和生产构建，以及前端租期日期和 Service Worker 测试。`-Focused` 仅缩小后端测试范围。结果写入 `.runtime/verification/<时间>/`，任何步骤失败都会返回失败。

上游 `scripts/test-all.sh` 和打包脚本仍保留历史路径、上游发布渠道与镜像配置，尚未作为 Landlord 的发布入口。当前使用 GitHub Actions 制品保存镜像与匹配的只读前端，没有向公共容器仓库推送镜像。

## 上游来源与许可

上游标签 `v0.7.1`，提交 `98e65d2c65c04a8e6bba321b6774ded72c5e812d`。原始 MIT [LICENSE](LICENSE) 保留；本地导入基线提交为 `a647d66`。版本 `0.10.0` 已用于小范围部署，尚未作为 GitHub 发行版发布。
