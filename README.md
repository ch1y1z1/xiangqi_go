# 象棋残局

基于 [xiangqi_app](https://github.com/ch1y1z1/xiangqi_app) 的桌面端，使用 Go + [mygo Native UI](https://github.com/egoist/mygo) 和独立 Pikafish 离线引擎进程。窗口、菜单、文件选择和凭据库使用系统接口；棋盘及控件使用 mygo 绘制。

从 [GitHub Releases](https://github.com/ch1y1z1/xiangqi_go/releases/latest) 下载三平台应用。下载包内含引擎与 NNUE，运行离线 AI 无需安装 Go、Python 或联网下载模型。

| 平台 | 发布包 |
| --- | --- |
| macOS 14+ Apple Silicon / Intel | 分别提供 `macos-arm64.dmg` / `macos-amd64.dmg` |
| Windows x64 | `windows-amd64-setup.exe` 安装包与 `windows-amd64.zip` 便携包 |
| Linux x64 | `linux-amd64.deb` 与 `linux-amd64.tar.gz` |

包名带应用版本，例如 `xiangqi-0.1.0-macos-arm64.dmg`；每次发布附带 `SHA256SUMS.txt`。Windows 便携包须完整解压，不能只复制主程序。Linux 需要 GTK 3、WebKitGTK 4.1 和中文字体；Ubuntu 24.04 可安装 `libgtk-3-0t64 libwebkit2gtk-4.1-0 fonts-noto-cjk`。图片识别密钥使用 macOS Keychain、Windows Credential Manager 或 Linux Secret Service；Linux 需要已解锁的 GNOME Keyring 或兼容服务。

macOS 下载包使用 ad hoc 签名，尚未配置 Developer ID 和公证；首次打开被拦截时，可在系统设置“隐私与安全性”中允许。Windows 包尚未配置代码签名，可能显示 SmartScreen 提示。macOS Apple Silicon 已实际运行验收；Windows/Linux 的交互界面仍需实机验收。

## 使用

- 残局库：三个初始样例，新建、改名、复制、删除、草稿和继续研究。
- 摆棋：红黑棋子库存；点击放置或从库存拖入；拖动调整、删除、初始盘、清空、撤销与重做；先行方和朝向分别设置。
- 研究：合法落子、回退/前进、线路跳转、分支保留和下一手分支评分比较；研究自动保存。
- 离线 AI：0.3 / 1 / 3 秒搜索、建议箭头、采用建议、只读参考变化、AI 执红或执黑、暂停和停止。
- 视觉反馈：木色棋盘与楷体棋子、可中断移动、选中放大、吃子淡出、合法落点、上一手和将军提示、安全吃子红绿灯。
- 图片：系统选图或文件拖入、HEIC/JPEG/PNG 等格式、缩放和平移、识别后主动导入和原图校正；识别任务及待导入结果可恢复。

安全吃子绿灯表示当前合法吃子后没有立即合法回吃，不代表多步战术绝对安全。评分统一从红方观察；翻转棋盘不会反转评分含义。

图片识别在设置中选择 DeepSeek 或自定义服务，后者支持 Chat Completions / Responses，需支持图片和 JSON 输出。两类服务的密钥分别存入系统凭据库；配置 JSON 不含密钥。只有点击识别才发送图片，不自动重试付费请求。

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

引擎需要 C++17 编译器：macOS 使用 Xcode clang++，Linux 使用 g++，Windows 使用 MinGW-w64 g++。Windows 安装包由 mygo 使用 NSIS 生成；未安装时会下载固定版本。`build-desktop.py` 调用 mygo 构建；Linux 使用 `xiangqi` 包名和命令，避免中文应用名被转换成通用的 `app`。`package-release.py` 校验应用内模型、许可证与引擎协议后，将发布包和校验文件写入 `dist/`。

使用独立开发数据运行：

```sh
go run ./cmd/xiangqi --resources-dir "$PWD/resources" --data-dir "$PWD/build/dev-data"
```

macOS 的 HEIC 处理使用系统 `sips`，中文棋子优先使用系统楷体。Windows/Linux 使用系统中文字体回退，HEIC/HEIF 需先转换为 JPEG 或 PNG。

## GitHub CI 与发布

[CI and Release](https://github.com/ch1y1z1/xiangqi_go/actions/workflows/ci.yml) 在每次推送、PR 和手动触发时构建四个目标：macOS arm64/amd64、Windows amd64、Linux amd64。每个平台检查 Go 格式、运行 `go vet`、race 单元测试和真实离线引擎测试，再打包并校验实际安装目录里的引擎和 NNUE。普通构建的下载包保留在 Actions artifacts 中 14 天。

发布步骤：先修改 `mygo.json` 的 `version` 并提交，再推送匹配的 `v<version>` 标签：

```sh
git tag -a v0.1.0 -m 'Release v0.1.0'
git push origin v0.1.0
```

示例标签只适用于尚未发布的 0.1.0；后续发布应递增版本。CI 拒绝标签和应用版本不匹配的发布。全部目标通过后，发布作业核对所有下载包的 SHA-256，上传到草稿 Release，再将其正式发布。流程使用 GitHub 自带的 `GITHUB_TOKEN`，无需配置发布密钥；仅发布作业获得 `contents: write` 权限。

## 资料

- [完整调研与设计方案](design/desktop-design.md)
- [当前实现与 macOS 验收记录](design/research/implementation-status.md)
- [前期设计探针](design/research/verification.md)
- [研究工作台设计稿](design/previews/desktop-study.png) / [摆棋设计稿](design/previews/desktop-editor.png)

代码采用 GPL-3.0；Pikafish 源码、作者信息和 NNUE 独立许可位于 `resources/`，随应用一起打包。
