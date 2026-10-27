Go + mygo 原生界面的象棋残局桌面应用，支持残局库、摆棋、分支研究、离线 Pikafish AI 和图片识别。

设置中的“诊断日志”可打开本地日志目录，或预览并导出诊断包。发生闪退后重新启动即可查看异常退出提示；诊断数据由用户主动导出。

下载包包含离线引擎、经过 SHA-256 校验的 NNUE 模型及许可证；使用离线 AI 无需安装 Go、Python 或下载模型。图片识别需要自行配置支持视觉输入的服务和 API 密钥。

| 平台 | 下载与安装 |
| --- | --- |
| macOS 14+ Apple Silicon | `macos-arm64.dmg`，拖入 Applications |
| macOS 14+ Intel | `macos-amd64.dmg`，拖入 Applications |
| Windows x64 | `windows-amd64-setup.exe` 安装包，或解压 `windows-amd64.zip` 后运行 `Xiangqi.exe`；请保留整个目录 |
| Linux x64 | Debian/Ubuntu 使用 `linux-amd64.deb`，或解压 `linux-amd64.tar.gz` 后运行其中的应用 |

Linux 需要 GTK 3、WebKitGTK 4.1 和中文字体。Ubuntu 24.04 可安装 `libgtk-3-0t64 libwebkit2gtk-4.1-0 fonts-noto-cjk`；保存图片识别密钥还需要已解锁的 Secret Service（如 GNOME Keyring）。Windows/Linux 的 HEIC/HEIF 图片需先转换为 JPEG 或 PNG。

macOS 使用 ad hoc 签名，尚未配置 Developer ID 和公证；首次打开可能被 Gatekeeper 阻止，可在系统设置的“隐私与安全性”中允许打开。Windows 安装包尚未配置代码签名，可能显示 SmartScreen 提示。

CI 已在每个目标平台执行单元测试、race 检查、真实引擎规则/搜索测试及打包校验。Windows/Linux 的交互界面仍需实机验收。

`SHA256SUMS.txt` 提供全部下载包的 SHA-256 校验值。应用采用 GPL-3.0，Pikafish 和 NNUE 的许可随包附带；引擎固定提交及本地补丁见对应标签下的 `scripts/prepare-engine.py`、`engine-worker/`。

此版本支持签名自动更新，可在设置中调整自动检查、自动安装或手动检查更新。尚未支持应用内更新的旧版本需要先手动下载安装本版本。自动更新需要可写的安装目录；Linux 的 `.deb` 安装仍由包管理器升级。`update-*.json`、更新 `.tar.gz` 和 `.delta` 文件供应用内更新使用，请按上表选择首次安装包。
