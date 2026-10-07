# 象棋残局

基于 [xiangqi_app](https://github.com/ch1y1z1/xiangqi_app) 的桌面端，使用 Go + [mygo Native UI](https://github.com/egoist/mygo) 和独立 Pikafish 离线引擎进程。窗口、菜单、文件选择和系统凭据接口连接 macOS；棋盘及控件使用 mygo 绘制。

当前已构建并实际运行 **macOS 14+ / Apple Silicon** 应用：`build/darwin-arm64/象棋残局.app`。应用内含引擎与 NNUE，运行时无需安装 Go、Python 或联网下载模型。

## 使用

- 残局库：三个初始样例，新建、改名、复制、删除、草稿和继续研究。
- 摆棋：红黑棋子库存；点击放置或从库存拖入；拖动调整、删除、初始盘、清空、撤销与重做；先行方和朝向分别设置。
- 研究：合法落子、回退/前进、线路跳转、分支保留和下一手分支评分比较；研究自动保存。
- 离线 AI：0.3 / 1 / 3 秒搜索、建议箭头、采用建议、只读参考变化、AI 执红或执黑、暂停和停止。
- 视觉反馈：木色棋盘与楷体棋子、可中断移动、选中放大、吃子淡出、合法落点、上一手和将军提示、安全吃子红绿灯。
- 图片：系统选图或文件拖入、HEIC/JPEG/PNG 等格式、缩放和平移、识别后主动导入和原图校正；识别任务及待导入结果可恢复。

安全吃子绿灯表示当前合法吃子后没有立即合法回吃，不代表多步战术绝对安全。评分统一从红方观察；翻转棋盘不会反转评分含义。

图片识别在设置中选择 DeepSeek 或自定义服务，后者支持 Chat Completions / Responses，需支持图片和 JSON 输出。两类服务的密钥分别存入 macOS Keychain；配置 JSON 不含密钥。只有点击识别才发送图片，不自动重试付费请求。本轮未配置真实密钥、未调用真实付费 API。

| 快捷键 | 操作 |
| --- | --- |
| Cmd+N / Cmd+S | 新建 / 保存摆棋 |
| Cmd+Z / Cmd+Shift+Z | 棋盘获得焦点时撤销 / 重做摆棋；文本框内沿用文本编辑 |
| Delete | 删除编辑器内选中的棋子 |
| Alt+← / Alt+→ | 回退 / 前进 |
| Cmd+J | AI 建议 |
| Cmd+Shift+F | 翻转棋盘 |
| Cmd+, | 设置 |

残局和设置保存在 `~/Library/Application Support/com.chiyizi.xiangqi.desktop/`。图片不写入残局文档；待处理识别任务保存在独立目录。退出会保留已完成结果、标记运行中任务为中断，重启后由用户决定是否重试。

macOS 顶栏融入侧边栏：浅灰残局库背景从系统红黄绿贯通到底部，主内容区以自己的背景承载残局名称和轻量图标操作，分栏线延伸至窗口顶部。收起侧栏后，打开侧栏按钮与残局名称自动移到红黄绿右侧；顶栏空白处可拖动，双击遵循系统窗口行为，全屏自动调整按钮避让。

## 开发与构建

需要 Go 1.27.1、Xcode Command Line Tools、Python 3.11+。首次准备资源会下载固定 Pikafish 源码和 NNUE；下载模型需 `7z` 或 `7zz`，也可用 `python3 scripts/prepare-engine.py --network /path/to/pikafish.nnue` 提供校验匹配的现有模型。

```sh
bash scripts/build-macos.sh
```

脚本按本机架构生成 `.app`，已有模型和引擎可复用；修改辅助程序后自动重建。Intel 构建入口已保留，但当前仅验收 Apple Silicon。

使用独立开发数据运行：

```sh
go run ./cmd/xiangqi --resources-dir "$PWD/resources" --data-dir "$PWD/build/dev-data"
```

macOS 的 HEIC 处理使用系统 `sips`，中文棋子使用系统楷体。Windows/Linux 的运行、字体、HEIC、凭据库和分发仍待实机适配。当前 `.app` 是本机开发构建，正式对外分发的 Developer ID 签名和公证尚未配置。

## 资料

- [完整调研与设计方案](design/desktop-design.md)
- [当前实现与 macOS 验收记录](design/research/implementation-status.md)
- [前期设计探针](design/research/verification.md)
- [研究工作台设计稿](design/previews/desktop-study.png) / [摆棋设计稿](design/previews/desktop-editor.png)

代码采用 GPL-3.0；Pikafish 源码、作者信息和 NNUE 独立许可位于 `resources/`，随应用一起打包。
