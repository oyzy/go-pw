## GoKeep v0.1.2

GoKeep 0.1.2 聚焦分组树与条目列表的整理体验：分组和条目都能用拖拽移动，回收站
不再占用分组树，列表也补上了列标题。本次构建仍不包含 Apple Developer ID 签名、
公证或 Windows Authenticode 证书签名；安装包附带 `SHA256SUMS` 和 GitHub build
provenance，更新 feed 始终使用 Ed25519 签名。

### 新增与改进

- 分组支持拖拽整理：拖到同级行的上/下边缘可调整顺序，拖到行中间可移入该分组，
  拖到“全部条目”行可移回根级；拖放过程中目标行会高亮
- 条目支持从列表拖到分组树中的任意分组，包括跨层级移动
- 修复分组树点击位置偏移：分组名与分支箭头对齐，点击整行即可选中，不再需要
  点在偏右的位置
- 回收站移出分组树，改由 **工具 → 回收站** 打开；分组右键菜单不再提供清空入口
- 条目列表新增列标题：标题、用户名、网址、强度、修改时间，各列与内容固定对齐，
  窗口变窄时同步收缩
- 在回收站视图下新建条目或分组时，自动回到普通分组/根级，避免写入回收站

### 下载

| 平台 | 安装包 |
| --- | --- |
| macOS（Apple Silicon） | `GoKeep-0.1.2-darwin-arm64.dmg` 或 `.zip` |
| Windows（x64） | `GoKeep-0.1.2-windows-amd64-setup.exe` 安装版，或 `.zip` 便携版 |
| Linux（x64） | `gokeep_0.1.2_amd64.deb` 或 `.tar.gz` 便携包 |

### 安装

- macOS：打开 `.dmg`，将 GoKeep 拖入“应用程序”文件夹。本次应用未签名、未公证；
  如 Gatekeeper 阻止启动，请在 Finder 中右键应用并选择“打开”。
- Windows：运行 `GoKeep-0.1.2-windows-amd64-setup.exe`。安装版支持自动更新；便携
  ZIP 解压后可直接运行，但不会自更新。本次安装包未做 Authenticode 签名；
  SmartScreen 可能提示“未知发布者”，请核对 `SHA256SUMS` 后选择继续运行。
- Linux：运行 `sudo apt install ./gokeep_0.1.2_amd64.deb`，或解压 `.tar.gz` 后
  运行其中的安装脚本。`.tar.gz` 便携版支持自动更新，`.deb` 由包管理器升级。

**升级说明：** 已安装 0.1.1 的用户可在启动时收到本次更新；更早的版本请先手动
安装一次带更新器的版本。Windows 自动更新仅适用于 NSIS 安装版。未签名的 macOS
构建无法通过自身更新器替换应用，需要手动下载新版本；Windows NSIS 和 Linux
`.tar.gz` 的更新包仍由签名 feed 与 SHA-256 保护。

发布目录中的 `latest.json` 与 `latest.json.sig` 是更新 feed 及其 Ed25519 签名，
由发布工作流生成并随 Release 一并上传。

### 校验下载

```sh
shasum -a 256 -c SHA256SUMS --ignore-missing
```

Windows 安装包本次没有 Authenticode 签名，可以确认状态并核对文件哈希：

```powershell
Get-AuthenticodeSignature .\GoKeep-0.1.2-windows-amd64-setup.exe |
  Format-List Status, StatusMessage, SignerCertificate
Get-FileHash .\GoKeep-0.1.2-windows-amd64-setup.exe -Algorithm SHA256
```

安装了 GitHub CLI 时，可以验证任意发布包的构建来源：

```sh
gh attestation verify ./GoKeep-0.1.2-windows-amd64-setup.exe --repo oyzy/go-pw
```

数据库使用自有格式，升级前请同时保留 `.gkdb` 文件及其 `.bak` 备份。

Copyright 2026 oyzy. Licensed under the Apache License 2.0.
