## GoKeep v0.1.1

GoKeep 0.1.1 聚焦安全加固与自动更新。本次构建不包含 Apple Developer ID 签名、
公证或 Windows Authenticode 证书签名；安装包仍附带 `SHA256SUMS` 和 GitHub build
provenance，可供完整性校验。更新 feed 始终使用 Ed25519 签名。

### 安全与可靠性

- 限制不可信数据库头可触发的 Argon2 和 AES-KDF 资源消耗，避免打开恶意数据库时
  耗尽内存或 CPU
- 加固数据库生命周期、回收站、设置迁移和 `.bak` 备份轮转
- 自动锁定会记录窗口输入活动，并在到期后关闭工作区、清除已打开条目中的敏感内容
- 标题栏新增 GitHub 仓库按钮，可直接打开 `https://github.com/oyzy/go-pw`
- 启动时自动检查稳定版更新，发现后在后台下载；用户确认后才安装并重启
- 更新 feed 使用 Ed25519 签名，安装包校验 HTTPS、大小与 SHA-256
- **本次 macOS 和 Windows 安装包未做代码签名**，系统可能显示 Gatekeeper 或
  SmartScreen 警告
- 发布包附带 GitHub 构建来源证明（build provenance）和 `SHA256SUMS`

### 下载

| 平台 | 安装包 |
| --- | --- |
| macOS（Apple Silicon） | `GoKeep-0.1.1-darwin-arm64.dmg` 或 `.zip` |
| Windows（x64） | `GoKeep-0.1.1-windows-amd64-setup.exe` 安装版，或 `.zip` 便携版 |
| Linux（x64） | `gokeep_0.1.1_amd64.deb` 或 `.tar.gz` 便携包 |

### 安装

- macOS：打开 `.dmg`，将 GoKeep 拖入“应用程序”文件夹。本次应用未签名、未公证；
  如 Gatekeeper 阻止启动，请在 Finder 中右键应用并选择“打开”。
- Windows：运行 `GoKeep-0.1.1-windows-amd64-setup.exe`。安装版支持自动更新；便携
  ZIP 解压后可直接运行，但不会自更新。本次安装包未做 Authenticode 签名；
  SmartScreen 可能提示“未知发布者”，请核对 `SHA256SUMS` 后选择继续运行。
- Linux：运行 `sudo apt install ./gokeep_0.1.1_amd64.deb`，或解压 `.tar.gz` 后
  运行其中的安装脚本。`.tar.gz` 便携版支持自动更新，`.deb` 由包管理器升级。

**升级说明：** 这是 GoKeep 的第一个带自动更新器的版本。从更早版本升级时请手动
安装一次；之后的版本会在启动时检查更新。Windows 自动更新仅适用于 NSIS 安装版。
未签名的 macOS 构建无法通过自身更新器替换应用，需要手动下载新版本；Windows
NSIS 和 Linux `.tar.gz` 的更新包仍由签名 feed 与 SHA-256 保护。

发布目录中的 `latest.json` 与 `latest.json.sig` 是更新 feed 及其 Ed25519 签名，
由发布工作流生成并随 Release 一并上传。

### 校验下载

```sh
shasum -a 256 -c SHA256SUMS --ignore-missing
```

Windows 安装包本次没有 Authenticode 签名，可以确认状态并核对文件哈希：

```powershell
Get-AuthenticodeSignature .\GoKeep-0.1.1-windows-amd64-setup.exe |
  Format-List Status, StatusMessage, SignerCertificate
Get-FileHash .\GoKeep-0.1.1-windows-amd64-setup.exe -Algorithm SHA256
```

安装了 GitHub CLI 时，可以验证任意发布包的构建来源：

```sh
gh attestation verify ./GoKeep-0.1.1-windows-amd64-setup.exe --repo oyzy/go-pw
```

数据库使用自有格式，升级前请同时保留 `.gkdb` 文件及其 `.bak` 备份。

Copyright 2026 oyzy. Licensed under the Apache License 2.0.
