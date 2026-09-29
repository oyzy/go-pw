# GoKeep

一个用 Go 1.27 + Wails 3 编写的 KeePass 2 兼容密码管理器。数据库格式、加密方式与
KeePass 2 完全一致：既能打开 KeePass 生成的 `.kdbx` 文件，生成的文件也能被
KeePass 2 正常打开。

```
go build -o bin/gokeep.exe .
bin/gokeep.exe
```

前端是纯静态的 HTML/CSS/JavaScript，不需要 Node.js、npm 或打包步骤，所以只需一条
`go build` 就能得到可执行文件。

## 图标

图标由代码绘制（`tools/makeicon`，纯 Go，不依赖任何绘图库，形状按有向距离 + 4×4
超采样渲染）：

- `build/appicon.png`：1024×1024 主图，Wails 用它生成 `build/windows/icon.ico`
  和 `build/darwin/icons.icns`
- `rsrc_windows_amd64.syso`：Windows 资源文件，随 exe 一起链接，决定资源管理器里
  显示的图标与文件属性中的版本信息（`build/windows/info.json`）
- 窗口和任务栏图标：Wails 注册窗口类时用的是系统通用图标，所以程序在窗口创建后
  自己发送 `WM_SETICON`（见 `window_icon_windows.go`），图标从内嵌的 .ico 读取，
  不依赖外部文件

```powershell
task icon          # 修改图标设计后重新生成 png / ico / syso
wails3 build       # 构建时会自动执行图标生成步骤
```

## 多语言

界面语言在「工具 → 选项 → 语言」里切换，支持**简体中文 / 繁體中文 / English /
日本語 / 한국어**，首次启动会跟随操作系统语言。语言包是独立的 JSON 文件，放在
`frontend/dist/locales/`，加一种语言只要新增一个文件（外加 `js/i18n.js` 的
`SUPPORTED` 列表里加一行）：

```
frontend/dist/locales/zh-CN.json   # 基准语言，缺 key 时回退到它
frontend/dist/locales/zh-TW.json
frontend/dist/locales/en.json
frontend/dist/locales/ja.json
frontend/dist/locales/ko.json
```

翻译的覆盖范围包括：界面全部文案、对话框与提示、错误信息（后端只返回错误码与参数，
由前端翻译）、日期/数字格式，以及**原生菜单栏**——菜单标签由前端下发给后端重建，
切换语言后菜单立即更新，无需重启。回收站分组也会按当前语言命名（标识仍是数据库里的
UUID，因此不影响与 KeePass 的兼容）。

```powershell
node tools/i18n-check.mjs     # 校验各语言 key 是否齐全、占位符是否一致
```

## 界面细节

- **窗口完全自绘**：窗口是 `Frameless` 的，标题栏（图标、数据库名、最小化/最大化/
  关闭按钮）和菜单栏都由前端绘制，颜色 100% 跟随应用主题，不再受 Windows 系统主题
  影响。拖动、双击最大化、边缘缩放、贴靠（Snap）通过 `--wails-non-client-region`
  交给 Windows 的原生命中测试实现，行为与系统窗口一致。
- 关闭窗口（标题栏按钮 / Alt+F4 / 任务栏关闭）会先询问未保存的更改；快捷键在
  `js/app.js` 里实现（Ctrl+N/O/S/W/L/F/G…），与菜单里显示的提示一致。
- **运行时不依赖 KeePass**：KeePass 只被测试套件与 `testdata/reference/` 下的脚本用来
  做兼容性验证，卸载 KeePass 不影响程序的任何功能（相关测试会自动跳过）。

## 功能

数据库格式

支持两种格式，新建数据库时选择，打开时按文件头自动识别：

| 格式 | 扩展名 | 说明 |
| --- | --- | --- |
| KeePass 兼容 | `.kdbx` | KDBX 4.1 / 4.0 / 3.1，与 KeePass 2、KeePassXC 互相打开 |
| GoKeep 自有 | `.gkdb` | 自有容器格式：`GKDB` 魔数 + 头部 + 随机 nonce，整个文档用 AES-256-GCM 或 ChaCha20-Poly1305 认证加密，载荷为 JSON |

两种格式共用同一套数据模型、同一套密钥文件规则（主密码 + 密钥文件）与同一套 KDF
（Argon2d / Argon2id / AES-KDF）。「另存为」时按扩展名自动切换格式，因此也可以把
KeePass 库转成自有格式，或反过来。

自有格式的容器布局（`internal/gkdb`）：

```
"GKDB" | 版本 | 标志 | KDF 参数 | 盐(32) | nonce(12) | 载荷长度(8) | AEAD 密文
        └──────────── 这些字节同时作为 AEAD 的附加认证数据(AAD) ────────────┘
```

头部任何一个字节被改动、截断、换密钥或换 salt 都会导致认证失败（有专门的测试逐字节
验证）。

- 读取 KDBX 4.1 / 4.0 / 3.1；写入 4.1（KeePass 2.60 的默认格式），也可以另存为
  4.0 或 3.1
- 加密算法：AES-256-CBC、ChaCha20
- 密钥派生：Argon2d、Argon2id、AES-KDF，参数（内存、迭代、并行度、轮数）可调
- 完整性校验：KDBX 4 的头部 SHA-256 + HMAC、HMAC 分块流；KDBX 3.1 的
  HeaderHash 与 HashedBlockStream
- 复合密钥：主密码、密钥文件（32 字节二进制、64 字符十六进制、XML v1.0/v2.0 四种
  格式全部支持），并可随时更换主密钥
- 受保护字段使用 ChaCha20（KDBX 4）或 Salsa20（KDBX 3.1）内层随机流加密

数据管理

- 分组树（可嵌套、可修改图标/标签/备注/过期时间、参与搜索与自动输入的开关）
- 条目：标题、用户名、密码、网址、备注、自定义字段（可单独设为受保护）、标签、
  图标、前景/背景色、替代网址、过期时间
- 附件（数据库内共享二进制池，自动去重与压缩）
- 历史记录：每次修改自动保存旧版本，可查看、恢复、删除，并遵循 KeePass 的历史
  条数与总大小限制
- 回收站：删除先进入回收站，可还原或清空；记录 DeletedObjects 供其他客户端同步
- 搜索：快速搜索、按分组搜索、区分大小写、正则表达式、搜索受保护字段
- 工具：重复密码检查、过期条目检查、数据库设置、选项、最近文件

使用体验

- 原生菜单栏（文件 / 编辑 / 视图 / 工具 / 帮助）与工具栏
- 密码生成器：字符集选择、排除易混淆字符、每个字符组最少出现次数、避免重复、
  模式生成（`a A d s h H`、`\` 转义、`[自定义字符]`）、实时强度显示
- 密码强度评估（熵 + 常见弱点惩罚），列表与详情中显示强度
- 剪贴板：复制密码/用户名/网址，按 KeePass 的方式在 N 秒后自动清空，退出时清空
- 自动锁定：可配置无操作后自动锁定；锁定后内存中的密钥被清除
- 保存时自动生成 `.bak` 备份并轮转
- 导入/导出 CSV（列名与 KeePass 导出格式一致）、数据库整体备份
- 界面对话框中的中文/Unicode 内容、附件与自定义字段全部按 KeePass 语义处理

## 兼容性验证

测试套件会用本机安装的 KeePass 2 的 `KeePassLib` 做双向验证（`testdata/reference/`
下的 PowerShell 脚本）：

- `TestReadKeePassFixtures`：读取由 KeePassLib 生成的 6 个参考文件（4.1/4.0/3.1、
  AES/ChaCha20、Argon2d/Argon2id/AES-KDF、gzip/不压缩、主密码+密钥文件），逐项核对
  保护字段、自定义字段、附件、历史、标签、颜色、自动输入、回收站、Unicode 内容。
- `TestKeePassReadsOurFiles`：用本程序写出 5 种配置的数据库，再让真正的 KeePassLib
  打开并回读内容，验证 KeePass 能正确解析我们生成的文件。
- `TestRoundTripAllFormats`：4 种格式的写入/读取一致性检查。
- `internal/argon2` 的实现与 `golang.org/x/crypto/argon2` 逐字节对拍，Salsa20 与
  `x/crypto/salsa20` 对拍。

```powershell
go test ./...                                   # 全部测试
pwsh -File testdata/reference/make-fixtures.ps1 # 重新生成参考文件（需要本机 KeePass 2）
```

安装 KeePass 的路径默认探测 `D:\software\KeePass\KeePass.exe`，也可以通过环境变量
`KEEPASS_EXE` 指定；找不到时相关测试会自动跳过。

## 项目结构

| 路径 | 说明 |
| --- | --- |
| `internal/kdbx` | KDBX 文件格式：头部、变体字典、KDF、密码算法、HMAC 分块流、HashedBlockStream、XML 编解码 |
| `internal/argon2` | Argon2d / Argon2i / Argon2id（RFC 9106，仅 v1.3） |
| `internal/vault` | 打开的数据库会话：分组/条目编辑、历史、回收站、搜索、CSV、设置 |
| `internal/pwgen` | 密码生成器（字符集与模式） |
| `internal/pwquality` | 密码强度评估 |
| `internal/app` | 暴露给前端的 Wails 服务：VaultService / AppService / GeneratorService |
| `frontend/dist` | 静态前端（index.html、style.css、js/、bindings/） |
| `testdata/reference` | 兼容性测试用的参考文件与 KeePassLib 脚本 |

## 与 KeePass 2 的差异

以下 KeePass 2 的功能尚未实现，会在后续版本补齐：

- 自动输入引擎（Windows 全局热键 + 模拟按键；数据结构与自动输入序列已经完整读写）
- 插件系统与触发器（Triggers）、KeePass 的 KDBX 明文 XML / 1Password 等其他导入导出
  过滤器（当前支持 CSV 导入导出与 KDBX 另存）
- 二次验证域（KeePass 2 的 “MasterKeyChangeForce” 提醒对话框仅保存字段）
- 规则化密码质量估计使用的 KeePass/zxcvbn 完整字典（当前使用熵 + 常见弱点惩罚）

## 许可

KDBX 格式的实现参考了 KeePass 2 的公开文件格式说明，并通过与本机 KeePass 2 的
KeePassLib 交叉验证来保证兼容性。
