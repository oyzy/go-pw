# GoKeep

一个用 Go 1.27 + Viva 编写的密码管理器，功能和界面参照 KeePass 2，数据库文件
使用本程序自己的格式 `.gkdb`：整个文档用认证加密（AES-256-GCM 或
ChaCha20-Poly1305）封装，密钥来自主密码、可选的密钥文件，经 Argon2id / Argon2d /
AES-KDF 派生。**不读写 KeePass 的 `.kdbx` 文件**，也不需要安装 KeePass。

```powershell
go build -o bin/GoKeep.exe .   # 直接构建
viva build                     # 或让 Viva 构建，同时给 exe 打上图标与版本信息
bin/GoKeep.exe
```

界面由 Go 直接绘制（Viva 把页面画在 GPU 上）：没有浏览器内核，没有前端构建步骤，
没有 Node.js 依赖，整个程序就是一个 exe。

## 图标

图标由代码绘制（`tools/makeicon`，纯 Go，不依赖任何绘图库，形状按有向距离 + 4×4
超采样渲染），同一份 1024×1024 主图同时用于程序图标和窗口图标：

- `build/appicon.png`：`viva.toml` 的 `[app] icon` 指向它，`viva build` 据此写入
  exe 的图标与版本信息
- `internal/desktop/assets/appicon.png`：内嵌进程序，启动时降采样到 512×512 交给
  窗口，决定标题栏与任务栏图标，不依赖外部文件

```powershell
go run ./tools/makeicon      # 修改图标设计后重新生成 png
```

## 多语言

界面语言在「工具 → 选项 → 语言」里切换，支持**简体中文 / 繁體中文 / English /
日本語 / 한국어**，首次启动会跟随操作系统语言。语言包是独立的 JSON 文件，编译时
内嵌进二进制，放在 `internal/desktop/locales/`，加一种语言只要新增一个文件（外加
`internal/desktop/i18n.go` 的 `Languages` 列表里加一行）：

```
internal/desktop/locales/zh-CN.json   # 基准语言，缺 key 时回退到它
internal/desktop/locales/zh-TW.json
internal/desktop/locales/en.json
internal/desktop/locales/ja.json
internal/desktop/locales/ko.json
```

翻译的覆盖范围包括：界面全部文案、对话框与提示、错误信息（程序只返回错误码与参数，
由界面层翻译）、日期/数字格式，以及**菜单栏**——菜单和窗口一样由 Go 页面绘制，
切换语言后立即更新，无需重启。回收站的名字也跟随当前语言（它由“工具”菜单打开，
不占用分组树；标识仍是数据库里的 UUID，改名不会动到任何引用它的条目）。

```powershell
node tools/i18n-check.mjs     # 校验各语言 key 是否齐全、占位符是否一致
go test ./internal/desktop -run Sentence   # 校验每句文案与调用它的 m.tf 参数一致
```

## 自动更新

程序启动时默认检查 GitHub Releases 上的稳定版更新（可在
**工具 → 选项 → 软件更新**关闭），也可以随时通过 **帮助 → 检查更新**手动检查。
发现新版本后会在后台下载；下载完成并校验通过后，仍需点击“安装并重启”才会替换
程序，避免在编辑数据库时自动退出。

更新源为 `https://github.com/oyzy/go-pw/releases/latest/download/latest.json`。
更新包使用 Viva 自带的安全更新流程：

- `latest.json` 由 Ed25519 私钥签名，应用内只信任编译进去的公钥
- 安装包必须通过 HTTPS 下载，并匹配 feed 中的大小与 SHA-256
- macOS 会再次校验 `.app` 的代码签名；Windows 安装器有 Authenticode 签名时会校验
- 安装失败不会替换现有程序；旧版本会继续运行

支持自动更新的安装形态：

| 平台 | 支持自动更新的包 |
| --- | --- |
| Windows | `GoKeep-*-windows-amd64-setup.exe`（NSIS 安装版） |
| macOS | `.zip` 中的 GoKeep `.app`，且不能位于 App Translocation 路径 |
| Linux | 可写目录中的 `.tar.gz` 便携版 |

Windows 便携 ZIP、Linux `.deb` 和 AppImage 不会自更新，请通过对应渠道手动升级。
**从旧版本升级到第一个带更新器的版本时，需要手动安装一次**，之后才能接收自动更新。

## 界面细节

- **窗口完全自绘**：标题栏、菜单栏、工具栏、状态栏都由 Viva 页面绘制，颜色 100%
  跟随应用主题，不受 Windows 系统主题影响；拖动窗口用 Viva 的原生拖动区域实现，
  边缘缩放与贴靠仍由系统窗口管理器处理（保留透气的边距）。
- 关闭窗口（标题栏按钮 / Alt+F4 / 任务栏关闭）会先询问未保存的更改；快捷键在
  `internal/desktop/page.go` 的 `shortcuts` 里声明，「帮助 → 键盘快捷键」里列出全部。
- **不依赖 KeePass**：程序只读写自己的 `.gkdb`，KeePass 装不装、卸不卸都与它无关。

## 功能

数据库格式

只有一种格式：`.gkdb`（`GKDB` 魔数 + 头部 + 随机 nonce + 长度 + AEAD 密文，载荷是
JSON 文档），打开非本格式的文件会直接提示「这不是 GoKeep 数据库文件」。容器布局
（`internal/gkdb`）：

```
"GKDB" | 版本 | 标志 | KDF 参数 | 盐(32) | nonce(12) | 载荷长度(8) | AEAD 密文
        └──────────── 这些字节同时作为 AEAD 的附加认证数据(AAD) ────────────┘
```

头部任何一个字节被改动、截断、换密钥或换 salt 都会导致认证失败（有专门的测试逐字节
验证）。

- 加密算法：AES-256-GCM（默认）、ChaCha20-Poly1305
- 密钥派生：Argon2d、Argon2id、AES-KDF，参数（内存、迭代、并行度、轮数）可调
- 完整性：头部（含所有参数、盐、nonce、长度）整体作为 AEAD 的附加认证数据，任何
  一个字节被改动、截断都会认证失败（有逐字节的测试）
- 复合密钥：主密码、密钥文件（32 字节二进制、64 字符十六进制、XML v1.0/v2.0 都能
  识别），并可随时更换主密钥
- 分组/自定义字段等元数据中的 Unicode、附件与图标全部原样存取

数据管理

- 分组树（可嵌套、可修改图标/标签/备注/过期时间、参与搜索与自动输入的开关）
- 条目：标题、用户名、密码、网址、备注、自定义字段（可单独设为受保护）、标签、
  图标、前景/背景色、替代网址、过期时间
- 附件（数据库内共享二进制池，自动去重与压缩）
- 历史记录：每次修改自动保存旧版本，可查看、恢复、删除，并遵循 KeePass 的历史
  条数与总大小限制
- 回收站：删除先进入回收站，可还原或清空；记录 DeletedObjects 供其他客户端同步
- 搜索：快速搜索、按分组搜索、区分大小写、正则表达式、搜索受保护字段
- 工具：回收站、重复密码检查、过期条目检查、数据库设置、选项、最近文件

使用体验

- 原生菜单栏（文件 / 编辑 / 视图 / 工具 / 帮助）与工具栏
- 密码生成器：字符集选择、排除易混淆字符、每个字符组最少出现次数、避免重复、
  模式生成（`a A d s h H`、`\` 转义、`[自定义字符]`）、实时强度显示
- 密码强度评估（熵 + 常见弱点惩罚），列表与详情中显示强度
- 剪贴板：复制密码/用户名/网址，按 KeePass 的方式在 N 秒后自动清空，退出时清空
- 自动锁定：可配置无操作后自动锁定；锁定后内存中的密钥被清除
- 自动更新：启动时检查签名更新源，后台下载，确认后安装并重启
- 保存时自动生成 `.bak` 备份并轮转
- 导入/导出 CSV（列名与 KeePass 导出格式一致）、数据库整体备份
- 界面对话框中的中文/Unicode 内容、附件与自定义字段全部按 KeePass 语义处理

## 测试

- `TestRoundTrip`：三种密文与三种 KDF 组合的写入/读取一致性，逐项核对保护字段、
  自定义字段、附件、历史、标签、过期时间、自定义图标。
- `TestWrongKeyIsRejected` / 逐字节改动的测试：错误密码与任何一种篡改都必须被拒绝。
- `internal/argon2` 的实现与 `golang.org/x/crypto/argon2` 逐字节对拍。
- `internal/vault` 覆盖回收站政策迁移、历史记录裁剪、搜索、CSV 导入导出。
- `internal/desktop` 用真正的窗口驱动（无头）跑界面逻辑，点按钮、读标签。

```powershell
go test ./...   # 全部测试
```

## 发布签名

发布由 `.github/workflows/release.yml` 在推送 `v*` 标签时执行。macOS 和 Windows
配置签名凭据时会自动签名并校验；凭据缺失时会发布未签名安装包，并在 Actions 日志中
给出警告。Linux 包按 Viva 的约定不做代码签名。Windows 同时发布 NSIS 安装版和
便携 ZIP，自动更新使用 NSIS 安装版。发布作业始终生成签名更新 feed
（`latest.json`、`latest.json.sig`）。所有发布包都会生成 GitHub build provenance，
并连同 `SHA256SUMS` 一起上传。

在仓库的 **Settings → Secrets and variables → Actions** 中配置：

| Secret | 用途 |
| --- | --- |
| `APPLE_CERTIFICATE` | 可选；Developer ID Application 证书和私钥导出的 `.p12`，Base64 编码 |
| `APPLE_CERTIFICATE_PASSWORD` | 上述 `.p12` 的密码 |
| `APPLE_ID` | Apple Developer 账号邮箱 |
| `APPLE_PASSWORD` | 该账号的 App 专用密码 |
| `APPLE_TEAM_ID` | Apple Developer Team ID |
| `APPLE_SIGNING_IDENTITY` | 可选；不填时使用 `.p12` 中的 Developer ID 身份 |
| `WINDOWS_CERTIFICATE` | 可选；代码签名证书 `.pfx` 的 Base64 编码 |
| `WINDOWS_CERTIFICATE_PASSWORD` | 上述 `.pfx` 的密码 |
| `WINDOWS_TIMESTAMP_URL` | 可选；RFC 3161 时间戳服务，默认使用 DigiCert |
| `WINDOWS_SIGN_COMMAND` | 可选；使用云签名或硬件密钥时改为提供签名命令，命令用 `{file}` 表示待签文件 |
| `UPDATE_SIGNING_KEY` | 更新 feed 的 Ed25519 私钥（`viva update-keygen` 生成文件的内容），只应保存在发布 secret 中 |

`WINDOWS_CERTIFICATE`/`WINDOWS_CERTIFICATE_PASSWORD` 与
`WINDOWS_SIGN_COMMAND` 只能二选一；两者都不配置时发布未签名 Windows 包。Apple
签名凭据完整时发布经过签名和公证的 macOS 包，否则发布未签名包。工作流始终检查
更新 feed 的签名密钥，全部通过后才创建 GitHub Release。应用内信任的更新公钥在
`internal/desktop/updates.go` 的 `updatePublicKey`；轮换私钥时必须同步更新公钥，
否则旧版本会拒绝新 feed。私钥文件不得提交到仓库。

发布后可用 GitHub CLI 验证构建来源：

```powershell
gh attestation verify .\GoKeep-0.1.3-windows-amd64-setup.exe --repo oyzy/go-pw
```

## 项目结构

| 路径 | 说明 |
| --- | --- |
| `internal/gkdb` | `.gkdb` 容器：头部、AEAD 封装、压缩、读写的原子替换 |
| `internal/dbdoc` | 内存中的数据库模型：分组树、条目、历史、附件、回收站、元数据 |
| `internal/argon2` | Argon2d / Argon2i / Argon2id（RFC 9106，仅 v1.3） |
| `internal/vault` | 打开的数据库会话：分组/条目编辑、历史、回收站、搜索、CSV、设置 |
| `internal/pwgen` | 密码生成器（字符集与模式） |
| `internal/pwquality` | 密码强度评估 |
| `internal/desktop` | Viva 界面：页面与布局、自绘菜单栏、全部对话框、语言包、会话、命令与签名更新 |
| `internal/config` | 应用配置：主题、语言、最近文件、自动锁定、剪贴板清空时间、更新检查开关 |
| `build/appicon.png` | 图标主图（`viva.toml` 指向它，`viva build` 用它写 exe 资源） |

## 开发

```powershell
viva dev .                 # 热重载运行（改完保存，窗口重载并保留状态）
viva vet                   # 检查那些能编译但会在界面上做错事的地方
viva build                 # 构建到 bin/GoKeep.exe，并写入图标与版本信息
go test ./...              # 测试用无窗口的真实驱动跑界面逻辑
node tools/i18n-check.mjs  # 校验各语言 key 是否齐全、占位符是否一致
```

`TestEverySentenceMatchesTheCallThatShowsIt` 会扫描本包的 Go 源码，逐条核对 m.t/m.tf
的 key 与参数：句子要的 `{name}` 必须有人填、没人用的参数会被报出来，因此提示框里
不会再出现漏填的 `{title}` 之类的字面量。

界面逻辑的测试在 `internal/desktop/app_test.go`：用 `app.NewDriver` 无窗口地跑真实
页面，点按钮、读标签，覆盖新建数据库（没有默认分组）、删除进回收站、错误密码提示、
语言与主题切换等用户能看到的行为。

## 与 KeePass 2 的关系

功能与界面参照 KeePass 2，但文件格式是自有的：`.gkdb` 只有本程序能读写，KeePass 2
打不开它，本程序也打不开 `.kdbx`。以下 KeePass 2 的功能尚未实现，会在后续版本补齐：

- 自动输入引擎（Windows 全局热键 + 模拟按键；数据结构与自动输入序列已经完整读写）
- 插件系统与触发器（Triggers）、KeePass 的 KDBX 明文 XML / 1Password 等其他导入导出
  过滤器（当前支持 CSV 导入导出）
- 二次验证域（KeePass 2 的 “MasterKeyChangeForce” 提醒对话框仅保存字段）
- 规则化密码质量估计使用的 KeePass/zxcvbn 完整字典（当前使用熵 + 常见弱点惩罚）

## 许可

Copyright 2026 oyzy.

本项目采用 [Apache License 2.0](LICENSE) 开源许可。你可以依照许可证条款使用、修改和
分发本项目；软件按“现状”提供，不附带任何明示或暗示的担保。

功能与界面的设计参考了 KeePass 2；文件格式、加密实现与本项目的全部代码为独立实现。
