# Landlord 租务小程序

原生微信小程序，无客户端npm依赖。租客保留微信登录、首次填写入住手机号、等待房东关联、本人房屋/资料、账单分页与明细、逐笔实收/冲销、合同原件及PDF/图片查看，以及原有四个底部入口。房东第一阶段新增现有账号密码登录、待审核页、轻量工作台、台账切换、房源/账单分页搜索与账单明细、退出切号。房东使用普通页面，共用服务端台账和模块权限；抄表、出账、收款、冲销等写操作继续使用Web，现已增加房东微信绑定/解绑及已绑定微信快捷登录。

启动页为`pages/entry/index`，恢复会话时读取服务端身份；房东审核状态另由`/rental/users/me/approval`核实，因为当前`/auth/me`没有status。平台管理员只访问本人或获授权的业务台账，不提供小程序平台管理控制台。客户端尚未上传或发布；最新本地自验和证据见根目录STATE.md及测试报告§14—15。

## 配置与打开

在项目根目录 `.env` 配置 `WX_APP_ID` 和 `WX_APP_SECRET`。小程序仅取得 AppID；`wx.login` 的一次性 code 交给服务端 `/auth/wechat/login`，房东绑定及快捷登录分别调用 `/auth/wechat/bind` 和 `/auth/wechat/landlord-login`，均由服务端调用 `code2Session`。AppSecret 和 session_key 不进入小程序，OpenID 不作为客户端凭据返回。

本机 `scripts/dev.ps1` 仅在启动后端时读取根 `.env` 的这两个键；前端构建完成后才注入，退出恢复进程原环境。其他启动方式需向后端进程提供同名环境变量。生产部署使用服务器受限环境文件注入，不将凭据写进镜像、源码或小程序包；当前生产是否已配置以 STATE.md 为准。

```powershell
cd F:\zhangwei\AI\Landlord
python scripts/prepare-miniprogram.py
```

用微信开发者工具导入 `F:\zhangwei\AI\Landlord\miniprogram`。本机已定位工具为 `D:\软件\微信web开发者工具\微信开发者工具.exe`，其他机器按实际路径打开。生成器创建被 Git 忽略的 `project.config.json`、`project.private.config.json` 和 `config.local.js`；只将 AppID 和 API 基址写入本地小程序配置，不复制 AppSecret。根目录 `.env` 位于小程序打包目录外。

默认 API 为 `https://www.zoeng.site/landlord/api`。开发配置关闭合法域名校验，备案办理不阻塞开发；真机调试还需按设备环境配置网络/调试选项。进入体验版或发布检查时生成校验开启的配置，并在公众平台完成相应域名条件：

```powershell
python scripts/prepare-miniprogram.py --production
```

生成器保留私有工程配置中的其他字段，仅同步 `setting.urlCheck`，避免私有配置覆盖发布检查。它不替你修改微信公众平台，也不上传或发布代码。

API 基址可换为独立本地实例，例如 `--api-base http://127.0.0.1:18940/api`。模拟器 localhost 指向这台电脑，手机 localhost 指向手机自身；本地地址不作为正式发布配置。

## 账号与文件

- 登录页默认提供租客微信入口，选择“我是房东”后显示现有账号密码表单，不提供小程序注册。租客首次填写入住手机号后创建待关联账号，房东沿用Web核对关联；手填号码不自动认领、复用或合并账号。房东密码只用于本次HTTPS认证，不保存到本地；页面隐藏/切号后清理输入。房东可在工作台或待审核页进入“微信绑定”；仅已审核账号可新绑定，绑定/解绑均验证当前密码。不改原租客身份映射。
- 身份以服务端为准：tenant走本人门户，user/admin走房东业务，待审核房东走独立等待页；跨角色深链重定向至对应页面，不因误入页面销毁有效会话。身份、账单归属和文件权限仍由服务端决定；客户端不传tenant_id获取租客本人资料。
- 房东仅列出active台账，所有业务查询显式携带URL查询参数ledger_id；切台账使旧响应失效，旧请求的401不能清除新台账会话。模块权限不足、无台账和查询失败分别展示状态，管理员没有跨台账特权。
- 账单按服务端 `payable` 展示本期应缴，单列 `total_amount` 与 `carry_in`；逐笔实收、原收据和冲销分别保留，未将各期含结转金额合并成欠费总额。
- 本地仅持久保存应用登录令牌及最小账号标识，业务页面不做离线缓存。隐藏/离开页面、退出/切号时清空页面数据；旧会话的异步结果不能写回新会话。
- 归档 PDF、上传的 PDF/常见图片通过携带 Bearer 的 `wx.downloadFile` 获取，核对 HTTP 状态和文件头后再打开。下载目标显式放在 `wx.env.USER_DATA_PATH` 下的专用文件名；本轮发现模拟器默认 `http://tmp` 路径无法 unlink，不能用默认临时路径实现退出清理。应用创建的文件在下一次文件打开、退出/切号和下次启动时删除，失败保留索引以重试；原生文件查看器的复制、保存和转发由微信与用户控制。HEIC/HEIF/AVIF 等上传格式的真机兼容尚未验证。

## 房东微信绑定与快捷登录

1. 首次选择“我是房东”，用已有账号密码登录；进入工作台的“微信绑定”，再次输入账号密码并确认绑定当前微信。没有台账不影响管理绑定；待审核账号可看状态/解绑，审核通过后才可新绑定。
2. 下次选择“已绑定微信快捷登录”，只查已有user/admin账号；未绑定提示回到账号密码入口，不注册新房东。此入口不受租客门户开关影响。
3. 同一微信或应用账号已有其他绑定时返回冲突，不覆盖租客或其他房东关系，不按手机号合并账号。当前模型是一条微信身份对应一个应用账号。
4. 解绑需输入当前账号密码；身份删除与AuthVersion递增在同一事务中完成。全部既有应用JWT（含Web与密码登录）失效，当前小程序退出，账号与业务资料保留。APIKey及飞牛网关身份不属于JWT，不因本操作撤销。
5. 每次绑定/快捷登录重新调用wx.login，不复用一次性code。操作失败后不自动重发；可刷新状态后重试。网络丢失时若解绑已在服务端完成，下一次认证会返回401并要求重登。

接口为公开`POST /auth/wechat/landlord-login`（code），已认证`GET /auth/wechat/bind`（只返回bound/current_app/available/status）、`POST /auth/wechat/bind`（password/code）、`DELETE /auth/wechat/bind`（password）。不接收客户端指定user_id/OpenID，不向客户端返回微信身份或session_key。绑定状态与操作均不缓存；密码仅用于当前请求，提交/隐藏/切号后清空。

## 验证入口

```powershell
powershell -ExecutionPolicy Bypass -File scripts/verify-miniprogram.ps1
```

可加`-WxmlCompiler 'D:\软件\微信web开发者工具\resources\app.asar.unpacked\node_modules\wcc-exec\wcc.exe'`实际编译当前15页及公共模板，共16份WXML。该编译器可能以退出码0报模板错误，脚本同时核查输出和非空产物；Node入口包括原租客/文件测试与新增房东会话/权限/台账隔离测试。

本机原生模拟器验证使用独立合成业务数据，绝不指向生产库：

1. 用现有 Go 工具链将后端构建到 `.runtime/m5-20261112/landlord.exe`。
2. 执行 `python scripts/miniprogram-tests/serve-fixture.py`，持续运行独立 18940 端口服务；它沿用后端的端口监听方式，测试客户端全部访问 loopback。数据、生成的测试账号和凭据仅存 `.runtime/m5-20261112/fixture-<时间>`。Ctrl+C 关闭本次服务，不删除证据。
3. 执行 `python scripts/prepare-miniprogram.py --api-base http://127.0.0.1:18940/api`，通过工具绝对路径执行 `cli.bat auto --project F:\zhangwei\AI\Landlord\miniprogram --auto-port 9420`。等模拟器完成加载后再连自动化，避免默认页跳转与自动化导航竞争。
4. 将 `miniprogram-automator@0.12.1` 安装到 `.runtime/m5-tools`。`node scripts/miniprogram-tests/verify-runtime.cjs` 在真实模拟器中调用页面处理函数，验证生命周期、实际 wx 网络/文件与 SQLite，并保存截图。它和 `verify-native.cjs` 通过 `fixture-login.cjs` 为预建合成账号设置会话，仅验证业务页面，不能当作微信登录验证；微信链路另用真实 AppID/code2Session 和隔离账号实核。旧 SDK 的完整控件点击在页面跳转时有 Page ID/超时历史，该入口尚未全量通过。原生 PDF 回调仅表示模拟器接受打开，不代表手机显示/打印验收。
5. 验完退出合成账号、停止测试服务，再执行默认的配置生成命令切回公网 API。不上传体验版、不修改生产数据。

当前实现/验证事实与未验证项统一见根目录 [STATE.md](../STATE.md)。

备案期间的隐私数据清单、审核说明草稿、合成演示流程和截图索引见[隐私与提审准备](../doc/小程序隐私与提审准备.md)。它们尚未提交平台；个人主体类目、运营联系资料、正式域名和手机验收需按实际账号落实。

独立后端运行期间还可执行 `node scripts/miniprogram-tests/verify-http.cjs`：它使用实际客户端模块和真实 HTTP/SQLite，但以 Node 适配 wx 网络及文件接口，不作为原生微信页面或文件预览验收。

图标源由 `python scripts/generate-miniprogram-icons.py` 生成（仅开发时需 Pillow），产物 `assets/tabs/*.png` 随客户端打包，运行时不需要生成器或外部图片服务。


本轮房东微信绑定定点自验：Go18（含子用例）、Node49、Python4、WXML16份通过；新隔离库中真实微信身份交换及原生17场景通过、exception0。最终证据在`.runtime/mini-wechat-owner-20261113-04/`，原失败在01—03目录；本机配置保持，临时服务/工程已关闭，口令及临时微信关联已清理。原生部分操作使用页面处理函数，不等于物理手机/全部控件触摸验收；没有上传、发布或全量门禁。本轮绑定修改已按用户指令保存为本地提交，未推送、上传或发布。
