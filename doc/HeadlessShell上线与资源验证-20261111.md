# Headless Shell 上线与资源验证

2026-11-11（Asia/Hong_Kong）22:10 已将 PDF 转换器替换为 Chromium Headless Shell，线上入口为 <https://www.zoeng.site/landlord/>。当前应用源码 `5323d747465d3a0aa4d55b25565108644485a71f`，来自 [GitHub Actions 43033817431](https://github.com/zwdotnet/Landlord/actions/runs/43033817431)。构建在 GitHub 托管 runner 完成；VPS 只接收、校验和运行制品。

## 改动

Linux 镜像安装 `chromium-headless-shell`，移除完整 `chromium`，继续使用 Noto CJK 和原 HTML 模板。每份 PDF 仍使用独立临时 profile，转换后清理；只读根目录、串行转换和原有 512 MiB 内存限额保留。

隔离验证发现原 PDF 超时为 45 秒，而 HTTP 写超时为 30 秒，转换卡住时客户端先断开。现将排队与转换合计限制为 20 秒，为错误响应和事务回滚留出时间。实测约 20.02 秒返回 HTTP 500，收款数量和余额不变，模拟子进程退出。已补充排队请求取消的回归测试。

## VPS 实测

资源采样时间 2026-11-11T22:10:59.764775+08:00 至 2026-11-11T22:11:30.180478+08:00；清理后的最终快照 2026-11-11T22:13:08.023727+08:00。生产测试仅在 `/tmp` 转换一份当前业务 HTML 的合成住宅合同，没有创建生产账号或合同记录。

| 项目 | 实测 | 口径 |
|---|---:|---|
| PDF 前空闲内存 | 28.23–28.33 MiB | 24 秒；memory.current 减 inactive_file |
| 空闲 CPU 均值 | 0.13% | 100% 代表占满 1 核 |
| 本次 PDF 内存峰值 | 288.13 MiB | cgroup memory.peak；包含 Go 服务、文件缓存及内核记账 |
| PDF 耗时 | 1.41 秒 | 约 100 ms 采样，含启动和临时文件操作 |
| PDF 大小 | 367041 字节 | 临时文件已清理 |
| 最终 Docker 内存 | 16.57 MiB / 512 MiB | memory.current 减 inactive_file |
| 最终原始 cgroup 内存 | 187.79 MiB | 已包含 171.23 MiB inactive_file，不能与上一行相加 |
| 本项目 swap | 0.00 MiB | cgroup memory.swap.current |
| 内存上限事件 / OOM / OOM kill | 0 / 0 / 0 | memory.events 的 max / oom / oom_kill |
| 任务数 | 9 / 160，峰值 75 | 包含线程 |
| 主机可用内存 | 979.82 MiB | MemAvailable，含可回收缓存 |
| 全主机 swap 使用 | 304.08 MiB / 4096 MiB | 含其他服务；swappiness=10 |
| 主机磁盘剩余 | 16.76 GiB | 清理后的根文件系统 |
| 业务数据 / 部署目录 / 备份 | 1.43 / 1119.80 / 6.72 MiB | du 实际分配；部署目录不含 Docker 镜像存储 |

PDF 刚结束时 Docker 读数曾为 193.1 MiB，其中文件缓存约 185.59 MiB；最终部分缓存转入 inactive_file 后，CLI 读数降至 16.6 MiB。因此不能把刚结束时的缓存读数当成浏览器残留或 Go 堆增长。最终 docker top 只有 landlord 主进程，无残留浏览器。

旧 VPS 诊断样本曾触及约 512 MiB，但其 HTML 与启动方式和本次不同，不能据此计算严格的节省百分比。先前研究中的 104 MiB 是 NAS 上独立渲染器样本，也不等于当前 VPS 完整服务峰值。短时样本不能证明多人并发或所有模板的最大占用，生产限额继续保持 512 MiB。

## 同机业务比较

在 NAS 隔离容器中，通过真实 HTTP 接口生成同类普通合同；旧完整 Chromium 一次，最终 Shell 三次，均含 Go 与 SQLite，串行、新 profile、1 CPU、512 MiB RAM。10 ms 采样，主机文件缓存没有全局清空。

| 指标 | 完整 Chromium | 最终 Headless Shell |
|---|---:|---:|
| 普通合同总内存采样峰值 | 320.09 MiB | 221.49 / 234.90 / 231.41 MiB |
| 匿名内存采样峰值 | 241.34 MiB | 94.21 / 101.48 / 97.02 MiB |
| 请求耗时 | 5.08 秒 | 1.97 / 1.16 / 1.18 秒 |

Shell 三次中位数相对这一基线，总峰值下降约 27.7%，匿名峰值下降约 59.8%。最终镜像的文件缓存记账高于先前中间候选，故使用最终镜像结果，不沿用更低的中间值。最大模板 30 条 × 2000 字，生成 31 页；最终镜像该次总采样峰值 239.21 MiB，耗时 1.74 秒。

## 验证与收尾

- 隔离业务累计 30 项通过，其中最终镜像重新执行 16 项：合同及幂等重试、续签、收据、冲销、长模板、历史 PDF 指纹保持、转换失败与超时回滚、重启持久化、无残留转换进程。
- 15 份归档 PDF 的 SHA、预期文字和非空页检查通过，包含最终镜像新生成的 7 份；最终 7 页抽样图已目视核对中文、表格、金额、长正文和签字区，未见裁切或重叠。此处合成合同不含周期计划，因此普通合同为 1 页，不取代既有带周期约定的 2 页样本。
- 本轮后端定点 3 项通过；云端前端类型/生产构建、日期/PWA 5 项及实际启动参数下的 Shell 中文 PDF 检查通过。没有重跑全量后端；此前 501 项是历史基线。MAIN 自验，无独立验收。
- 公网 15 项通过，包含入口与资源指纹、Edge/WebKit 手机页面、真实 SW scope、离线页、401/no-store 与无脚本错误。iPhone 主屏幕安装、系统推送显示仍为 UNOBSERVED。
- 原有 7 个站点入口均 200，6 个稳定内容指纹不变；Keepup healthy，其他容器 ID 与启动时间未变。Caddy 内容不变，无 reload。
- 生产 SQLite quick_check=ok，用户、房源、租客、账单、收款、计划、任务仍为 0；初始化邀请和 VAPID 持久数据保留，未创建生产测试账号。
- 精确清理中间 `598badb` 镜像、其 release 与压缩包以及最终重复 ZIP；VPS 观察释放约 2.19 GiB。NAS 合成数据卷与临时账号令牌已删除，其他镜像和服务未清理。当前和前一生产版镜像、归档、release 与一致性备份均保留。

当前 release：`/opt/landlord/releases/20261111-0.10.0-shell-5323d74`；回退 release：`/opt/landlord/releases/20261111-0.10.0-gh-e1d14c9`；切换前备份：`/var/backups/landlord/pre-shell-20261111-220921`。最终制品及操作记录存于 `/opt/landlord/artifacts/github-43033817431`，不依赖 GitHub 7 天保留期。回退步骤见 [部署与 Safari 验收](部署候选与Safari验收.md)。详细数据见 [上线验证记录](HeadlessShell上线验证记录-20261111.json)。
