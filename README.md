# 象棋残局

基于 [xiangqi_app](https://github.com/ch1y1z1/xiangqi_app) 的桌面端，使用 Go + [mygo Native UI](https://github.com/egoist/mygo) 和独立 Pikafish 离线引擎进程。窗口、菜单、文件选择和凭据库使用系统接口；棋盘及控件使用 mygo 绘制。

从 [GitHub Releases](https://github.com/ch1y1z1/xiangqi_go/releases/latest) 下载三平台应用。下载包内含引擎与 NNUE，运行离线 AI 无需安装 Go、Python 或联网下载模型。

| 平台 | 发布包 |
| --- | --- |
| macOS 14+ Apple Silicon / Intel | 分别提供 `macos-arm64.dmg` / `macos-amd64.dmg` |
| Windows x64 | `windows-amd64-setup.exe` 安装包与 `windows-amd64.zip` 便携包 |
| Linux x64 | `linux-amd64.deb` 与 `linux-amd64.tar.gz` |

包名带应用版本，例如 `xiangqi-0.1.2-macos-arm64.dmg`；每次发布附带 `SHA256SUMS.txt`。Windows 便携包须完整解压，不能只复制主程序。Linux 需要 GTK 3、WebKitGTK 4.1 和中文字体；Ubuntu 24.04 可安装 `libgtk-3-0t64 libwebkit2gtk-4.1-0 fonts-noto-cjk`。图片识别密钥使用 macOS Keychain、Windows Credential Manager 或 Linux Secret Service；Linux 需要已解锁的 GNOME Keyring 或兼容服务。

macOS 下载包使用 ad hoc 签名，尚未配置 Developer ID 和公证；首次打开被拦截时，可在系统设置“隐私与安全性”中允许。Windows 包尚未配置代码签名，可能显示 SmartScreen 提示。macOS Apple Silicon 已实际运行验收；Windows/Linux 的交互界面仍需实机验收。

## 使用

- 残局库：三个初始样例，新建、改名、复制、删除、草稿和继续研究。
- 摆棋：红黑棋子库存；点击放置或从库存拖入；拖动调整、删除、初始盘、清空、撤销与重做；先行方和朝向分别设置。
- 研究：合法落子、回退/前进、线路跳转、分支保留和下一手分支评分比较；研究自动保存。
- 离线 AI：0.3 / 1 / 3 秒搜索、建议箭头、采用建议、只读参考变化、AI 执红或执黑、暂停和停止。
- 视觉反馈：木色棋盘与楷体棋子、可中断移动、选中放大、吃子淡出、合法落点、上一手和将军提示、安全吃子红绿灯。
- 图片：系统选图或文件拖入、HEIC/JPEG/PNG 等格式、缩放和平移、识别后主动导入和原图校正；识别任务及待导入结果可恢复。

安全吃子绿灯表示当前合法吃子后没有立即合法回吃，不代表多步战术绝对安全。评分统一从红方观察；翻转棋盘不会反转评分含义。

人机模式中的“回退”会回到当前线路上我方上一手落子之前：AI 尚未应手时撤回我方一手，AI 已应手时连同应手一起撤回。以当前设定的我方颜色判断，采用建议也算我方落子；双方手动时撤回最近一手。AI 先行且我方尚未落子时，回退不可用。原有着法保留，改走会形成分支。

起点、回退、前进和切换分支不会改变 AI 控制方或暂停状态。运行中轮到 AI 就从当前局面接着走，原本暂停则继续暂停；可随时点击“暂停托管”或“继续托管”。我方回合或暂停时可请求建议，采用后才落子；停止一次建议或后台评分不改变托管设置。AI 行棋期间先暂停托管，再逐步研究。每次打开研究仍默认双方手动。

图片识别在设置中选择 DeepSeek 或自定义服务，后者支持 Chat Completions / Responses，需支持图片和 JSON 输出。两类服务的密钥分别存入系统凭据库；配置 JSON 不含密钥。只有点击识别才发送图片，不自动重试付费请求。

应用默认每天自动检查 GitHub Releases 中的新版本，并通过中文原生窗口显示更新说明。可在设置中关闭自动检查，或开启“自动下载并安装”（默认关闭，安装后下次启动生效）；更新选项立即保存。文件菜单和设置都提供“检查更新”。下载的更新包必须通过内置公钥的签名校验。首次启用此功能的版本需要手动下载安装，之后才能接收应用内更新。

自动更新需要应用所在目录可写：macOS 请将应用拖入 Applications 后运行，Windows 使用当前用户安装或可写的便携目录，Linux 使用解压后的用户目录。通过 `.deb` 等包管理器安装的应用由包管理器升级；开发模式不执行后台更新。

错误和崩溃日志默认只保存在本机。在设置的“诊断日志”中可以打开日志文件夹，或预览并导出 ZIP 诊断包。macOS 日志目录为 `~/Library/Logs/象棋残局/`；Windows 为 `%APPDATA%/象棋残局/logs/`；Linux 为 `$XDG_CONFIG_HOME/象棋残局/logs/`（未设置时为 `~/.config/象棋残局/logs/`）。使用 `--data-dir` 时，日志放在指定目录下的 `logs/`。

每次启动在 `sessions/` 下创建独立目录，保存 `app.log`（运行事件和引擎诊断）、`crash.log`（Go 崩溃堆栈）、`stderr.log`（主进程错误输出）与 `session.json`（版本和结束状态）。运行日志按 2 MiB 轮转，保留 3 份；启动时保留最近 10 个已结束会话，活动会话不清理。崩溃文件保留完整内容，因此该规则不是整个目录的硬容量上限。

重启发现未正常结束的会话时提示一次；强制结束或断电不能保证留下堆栈，也不能仅凭该状态认定代码崩溃。原生库的完整调用栈仍可能需要系统崩溃报告辅助。诊断包只包含白名单日志及版本元数据，不打包棋谱、图片、设置或更新签名密钥；原始崩溃信息可能含本地路径和异常文本，请先预览再导出。日志文件创建或写入失败会给出提示，仍可继续使用应用。

| 快捷键 | 操作 |
| --- | --- |
| Cmd+N / Cmd+S | 新建 / 保存摆棋 |
| Cmd+Z / Cmd+Shift+Z | 棋盘获得焦点时撤销 / 重做摆棋；文本框内沿用文本编辑 |
| Delete | 删除编辑器内选中的棋子 |
| Alt+← / Alt+→ | 回退 / 前进 |
| Cmd+J | AI 建议 |
| Cmd+Shift+F | 翻转棋盘 |
| Cmd+, | 设置 |

残局和设置保存在系统应用数据目录下的 `com.chiyizi.xiangqi.desktop/`，macOS 为 `~/Library/Application Support/com.chiyizi.xiangqi.desktop/`。图片不写入残局文档；待处理识别任务保存在独立目录。退出会保留已完成结果、标记运行中任务为中断，重启后由用户决定是否重试。

macOS 顶栏融入侧边栏：浅灰残局库背景从系统红黄绿贯通到底部，主内容区以自己的背景承载残局名称和轻量图标操作，分栏线延伸至窗口顶部。收起侧栏后，打开侧栏按钮与残局名称自动移到红黄绿右侧；顶栏空白处可拖动，双击遵循系统窗口行为，全屏自动调整按钮避让。

## 开发与构建

需要 Go 1.27.1、Xcode Command Line Tools、Python 3.11+。首次准备资源会下载固定 Pikafish 源码和 NNUE；下载模型需 `7z` 或 `7zz`，也可用 `python3 scripts/prepare-engine.py --network /path/to/pikafish.nnue` 提供校验匹配的现有模型。

mygo 库和 Go 工具统一固定为 `v0.3.3`，包含 0.3.0 的 UI 生命周期迁移及后续输入时序修复。修改界面后运行 `go tool mygo vet .` 和 `go test -race ./...`；前者同时检查 Go 代码和 UI 控件/上下文生命周期，CI 使用相同检查。

```sh
bash scripts/build-macos.sh
```

脚本按本机架构生成 `.app`，已有模型和引擎可复用；修改辅助程序后自动重建。

三平台原生构建与打包（将目标替换为本机平台，如 `darwin/arm64`、`darwin/amd64`、`windows/amd64`、`linux/amd64`）：

```sh
python3 scripts/prepare-engine.py
python3 scripts/build-engine.py --os linux --arch amd64
python3 scripts/build-desktop.py --platform linux/amd64
python3 scripts/package-release.py --os linux --arch amd64
```

引擎需要 C++17 编译器：macOS 使用 Xcode clang++，Linux 使用 g++，Windows 使用 MinGW-w64 g++。Windows 安装包由 mygo 使用 NSIS 生成；未安装时会下载固定版本。`build-desktop.py` 调用 mygo 构建；Windows 主程序使用 `Xiangqi.exe`，避免安装器的中文路径编码问题；Linux 使用 `xiangqi` 包名和命令，避免中文应用名被转换成通用的 `app`。应用窗口继续使用中文名称。`package-release.py` 校验应用内模型、许可证与引擎协议后，将发布包和校验文件写入 `dist/`。

使用独立开发数据运行：

```sh
go run ./cmd/xiangqi --resources-dir "$PWD/resources" --data-dir "$PWD/build/dev-data"
```

macOS 的 HEIC 处理使用系统 `sips`。中文棋子优先加载本机已安装的楷体文件，缺失时使用系统中文字体回退，不触发可选字体下载；Apple 字体不随应用分发。Windows/Linux 的 HEIC/HEIF 需先转换为 JPEG 或 PNG。

## GitHub CI 与发布

[CI and Release](https://github.com/ch1y1z1/xiangqi_go/actions/workflows/ci.yml) 在每次推送、PR 和手动触发时构建四个目标：macOS arm64/amd64、Windows amd64、Linux amd64。每个平台检查 Go 格式、运行 `go vet`、race 单元测试和真实离线引擎测试，再打包并校验实际安装目录里的引擎和 NNUE。普通构建的下载包保留在 Actions artifacts 中 14 天。

更新包使用独立的 Ed25519 密钥签名，与 macOS/Windows 的应用代码签名分别管理。仓库只保存 `mygo.json` 中的公钥；对应私钥通过仓库 Actions secret `MYGO_UPDATER_PRIVATE_KEY` 提供。签名私钥需要单独备份并持续使用，不能随意换成新密钥，否则已安装的应用将无法验证后续更新。首次为自己的仓库配置时运行 `go tool mygo keygen -o /secure/path/update-keys`，将公钥填入配置，并用 `gh secret set MYGO_UPDATER_PRIVATE_KEY --repo owner/repo < /secure/path/update-keys/mygo-update.key` 上传私钥；不要提交私钥。

发布步骤：修改 `mygo.json` 的 `version`，并在 `CHANGELOG.md` 添加对应的 `## <version>` 更新说明，提交后推送匹配的 `v<version>` 标签：

```sh
git tag -a v0.1.2 -m 'Release v0.1.2'
git push origin v0.1.2
```

每次发布都应递增版本，并使用与应用版本匹配的新标签。CI 拒绝标签、应用版本和更新清单版本不匹配的发布，缺少更新签名私钥或任一平台的签名包也会失败。全部目标通过后，发布作业核对所有下载文件的 SHA-256，上传到草稿 Release，再将其正式发布为 latest。除用户下载的安装包外，Release 还包含原名保留的更新归档、增量包（如有）及 `update-<os>-<arch>.json`；不要删除、重命名这些更新文件。下载包上传使用 GitHub 自带的 `GITHUB_TOKEN`；仅发布作业获得 `contents: write` 权限。

普通分支和 PR 构建无需签名私钥，仍可生成下载包。需要在本机生成签名更新包时，将私钥通过 `MYGO_UPDATER_PRIVATE_KEY` 环境变量提供给构建命令；完成后运行 `python3 scripts/package-release.py --os darwin --arch arm64 --require-updates`（按本机平台调整）。更新窗口和 GitHub Release 的更新说明均读取当前版本的 `CHANGELOG.md` 段落。

## 资料

- [完整调研与设计方案](design/desktop-design.md)
- [当前实现与 macOS 验收记录](design/research/implementation-status.md)
- [前期设计探针](design/research/verification.md)
- [研究工作台设计稿](design/previews/desktop-study.png) / [摆棋设计稿](design/previews/desktop-editor.png)

代码采用 GPL-3.0；Pikafish 源码、作者信息和 NNUE 独立许可位于 `resources/`，随应用一起打包。
