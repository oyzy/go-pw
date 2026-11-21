## GoKeep v0.1.4

GoKeep 0.1.4 修复锁屏状态下窗口无法拖动的问题。本次构建仍不包含 Apple Developer
ID 签名、公证或 Windows Authenticode 证书签名；安装包附带 `SHA256SUMS` 和 GitHub
build provenance，更新 feed 始终使用 Ed25519 签名。

### 修复

- 锁屏与新建数据库页面没有声明窗口拖动区域，窗口在输入主密码之前完全无法移动。
  现在卡片周围的背景可以拖动窗口，卡片上的输入框、按钮和最近文件仍照常响应点击
- 锁屏页面同样支持双击背景最大化或还原窗口
- 解锁后标题栏的拖动行为不变，两处拖动区域都有回归测试覆盖

**升级说明：** 0.1.3 及之后的 Windows 安装版可以自动更新到 0.1.4。仍在 0.1.2
的用户无法自动升级，需要从本 Release 手动安装一次。

### 下载

| 平台 | 安装包 |
| --- | --- |
| macOS（Apple Silicon） | `GoKeep-0.1.4-darwin-arm64.dmg` 或 `.zip` |
| Windows（x64） | `GoKeep-0.1.4-windows-amd64-setup.exe` 安装版，或 `.zip` 便携版 |
| Linux（x64） | `gokeep_0.1.4_amd64.deb` 或 `.tar.gz` 便携包 |

### 安装

- macOS：打开 `.dmg`，将 GoKeep 拖入“应用程序”文件夹。本次应用未签名、未公证；
  如 Gatekeeper 阻止启动，请在 Finder 中右键应用并选择“打开”。
- Windows：运行 `GoKeep-0.1.4-windows-amd64-setup.exe`。安装版支持自动更新；便携
  ZIP 解压后可直接运行，但不会自更新。本次安装包未做 Authenticode 签名；
  SmartScreen 可能提示“未知发布者”，请核对 `SHA256SUMS` 后选择继续运行。
- Linux：运行 `sudo apt install ./gokeep_0.1.4_amd64.deb`，或解压 `.tar.gz` 后
  运行其中的安装脚本。`.tar.gz` 便携版支持自动更新，`.deb` 由包管理器升级。

已安装 0.1.3 的用户保持自动更新即可；0.1.2 及更早的 Windows 更新器无法完成本次
升级。Windows 自动更新仅适用于 NSIS 安装版。未签名的 macOS 构建无法通过自身
更新器替换应用，需要手动下载新版本；Windows NSIS 和 Linux `.tar.gz` 的更新包
仍由签名 feed 与 SHA-256 保护。

发布目录中的 `latest.json` 与 `latest.json.sig` 是更新 feed 及其 Ed25519 签名，
由发布工作流生成并随 Release 一并上传。

### 校验下载

```sh
shasum -a 256 -c SHA256SUMS --ignore-missing
```

Windows 安装包本次没有 Authenticode 签名，可以确认状态并核对文件哈希：

```powershell
Get-AuthenticodeSignature .\GoKeep-0.1.4-windows-amd64-setup.exe |
  Format-List Status, StatusMessage, SignerCertificate
Get-FileHash .\GoKeep-0.1.4-windows-amd64-setup.exe -Algorithm SHA256
```

安装了 GitHub CLI 时，可以验证任意发布包的构建来源：

```sh
gh attestation verify ./GoKeep-0.1.4-windows-amd64-setup.exe --repo oyzy/go-pw
```

数据库使用自有格式，升级前请同时保留 `.gkdb` 文件及其 `.bak` 备份。

Copyright 2026 oyzy. Licensed under the Apache License 2.0.
