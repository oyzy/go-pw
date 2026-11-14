## GoKeep v0.1.1

GoKeep 0.1.1 聚焦安全加固与可信分发。macOS 安装包现在使用 Developer ID
Application 证书签名并完成 Apple 公证；Windows 安装包使用 Authenticode 签名并带
RFC 3161 时间戳。发布工作流会在签名缺失或校验失败时停止，不再发布未签名安装包。

### 安全与可靠性

- 限制不可信数据库头可触发的 Argon2 和 AES-KDF 资源消耗，避免打开恶意数据库时
  耗尽内存或 CPU
- 加固数据库生命周期、回收站、设置迁移和 `.bak` 备份轮转
- 自动锁定会记录窗口输入活动，并在到期后关闭工作区、清除已打开条目中的敏感内容
- 标题栏新增 GitHub 仓库按钮，可直接打开 `https://github.com/oyzy/go-pw`
- 发布包附带 GitHub 构建来源证明（build provenance）和 `SHA256SUMS`

### 下载

| 平台 | 安装包 |
| --- | --- |
| macOS（Apple Silicon） | `GoKeep-0.1.1-darwin-arm64.dmg` 或 `.zip` |
| Windows（x64） | `GoKeep-0.1.1-windows-amd64.zip` 便携版 |
| Linux（x64） | `gokeep_0.1.1_amd64.deb` 或 `.tar.gz` 便携包 |

### 安装

- macOS：打开 `.dmg`，将 GoKeep 拖入“应用程序”文件夹。应用已签名、公证，公证
  票据已装订，Gatekeeper 可直接验证。
- Windows：解压后双击 `GoKeep.exe`。程序已做 Authenticode 签名和时间戳；新证书
  仍可能需要一段时间积累 SmartScreen 信誉。
- Linux：运行 `sudo apt install ./gokeep_0.1.1_amd64.deb`，或解压 `.tar.gz` 后
  运行其中的安装脚本。Linux 包不做代码签名。

### 校验下载

```sh
shasum -a 256 -c SHA256SUMS --ignore-missing
```

macOS 还可以验证签名和公证票据：

```sh
codesign --verify --deep --strict --verbose=2 /Applications/GoKeep.app
xcrun stapler validate /Applications/GoKeep.app
```

Windows 可以查看 Authenticode 状态：

```powershell
Get-AuthenticodeSignature .\GoKeep.exe | Format-List Status, StatusMessage, SignerCertificate
```

安装了 GitHub CLI 时，可以验证任意发布包的构建来源：

```sh
gh attestation verify ./GoKeep-0.1.1-windows-amd64.zip --repo oyzy/go-pw
```

数据库使用自有格式，升级前请同时保留 `.gkdb` 文件及其 `.bak` 备份。

Copyright 2026 oyzy. Licensed under the Apache License 2.0.
