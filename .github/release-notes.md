## GoKeep v0.1.0

GoKeep 的首个开源版本，使用 Go 1.27 和 Viva 构建。密码数据库使用项目自有的
`.gkdb` 加密格式，不依赖 KeePass，也不读写 `.kdbx` 文件。

### 下载

| 平台 | 安装包 |
| --- | --- |
| macOS（Apple Silicon） | `.dmg` 安装镜像或 `.zip` 应用包 |
| Windows（x64） | `.zip` 便携版，解压后运行 `GoKeep.exe` |
| Linux（x64） | `.deb` 安装包或 `.tar.gz` 便携包 |

每个安装包旁边均提供 `SHA256SUMS`，可用于校验下载文件。

### 说明

- macOS：打开 `.dmg`，将 GoKeep 拖入“应用程序”文件夹。
- Windows：解压后双击 `GoKeep.exe`。
- Linux：使用 `sudo apt install ./GoKeep_0.1.0_amd64.deb`，或解压
  `.tar.gz` 后运行其中的安装脚本。
- 当前版本没有 Apple Developer ID 和 Windows Authenticode 代码签名，因此
  macOS Gatekeeper 与 Windows SmartScreen 可能显示安全提示。
- 数据库使用自有格式，升级前请同时保留 `.gkdb` 文件及其 `.bak` 备份。

### 主要功能

- 分组、条目、自定义字段、附件、历史记录和回收站
- AES-256-GCM / ChaCha20-Poly1305 加密
- Argon2id / Argon2d / AES-KDF 密钥派生
- 密码生成器、强度评估、搜索、CSV 导入导出和自动锁定
- 简体中文、繁體中文、English、日本語、한국어界面

Copyright 2026 oyzy. Licensed under the Apache License 2.0.
