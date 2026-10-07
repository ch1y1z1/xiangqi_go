# 设计方案自行复核与复现

日期：2026-10-07。当前助手直接核对源码，并重新执行下面的编译、日期和离屏绘制探针；未使用子代理结果。工作区已有业务实现，本轮没有修改或完整验收它；三平台交互验收尚未进行。

## 固定快照

| 仓库 | 提交 |
| --- | --- |
| `ch1y1z1/xiangqi_app` | `391fce78c4d2cd5cacc91ddcfe175d9776a27ce3` |
| `egoist/mygo` | `49b7a7fa599b689720b1fafcc3a5f4fea7fcb94c` |

本机为 macOS arm64。原项目只读克隆，未准备 NNUE、运行 App 或调用识别服务。

## Native 示例编译

在 mygo 固定快照目录执行：

```sh
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -o /tmp/xiangqi-design-audit-darwin-arm64 ./examples/counter-native
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o /tmp/xiangqi-design-audit-windows-amd64.exe ./examples/counter-native
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /tmp/xiangqi-design-audit-linux-amd64 ./examples/counter-native
```

三条命令 exit code 均为 0；文件类型分别为 Mach-O arm64、PE32+ x86-64、ELF x86-64。工具链为 `go1.27.1`，与框架当前 `go.mod` 一致。

这些是普通 `go build` 输出，未经过 mygo 的正式打包步骤；Windows 此次原始产物带 console 子系统标记。产品构建应采用 mygo build 的窗口应用与资源打包流程。本轮未运行 Windows/Linux 产物，也没有证明所有平台动态库、签名、安装、中文 IME、DPI 或 GPU 兼容性。

## Foundation 日期编码

执行：

```sh
swift -e 'import Foundation; let date = Date(timeIntervalSince1970: 0); let data = try! JSONEncoder().encode(date); print(String(data: data, encoding: .utf8)!)'
```

输出 `-978307200`。原项目使用 JSONEncoder 默认日期策略，因此桌面 Go 编解码器需要保留相对 2001 年参考日期的数值秒，不可直接换用默认 RFC3339 字符串。

## Native 绘制设计稿

源文件：[native-preview.go](native-preview.go)。本次将已有探针复制到独立临时 Go module，以 `replace` 指向上述 mygo 快照，用 `CGO_ENABLED=0 go run -mod=mod . /tmp/xiangqi-design-audit-previews` 重新执行。通过 `ui.Render(view, 1440, 960, 1)` 与 `Painter` 绘制生成，并查看两张图片，与工作区已有设计稿一致：

- [desktop-study.png](../previews/desktop-study.png)
- [desktop-editor.png](../previews/desktop-editor.png)

两图均为 1440×960 PNG，本次实际查看了缩略图几何、九宫斜线、炮兵标记、棋子字形、渐变、投影、箭头和红绿图例；并对照原项目的 iPhone 页面和评分面板预览检查视觉继承。

这是**固定布局的设计绘制探针**：按钮和设置是画出的示意控件，没有事件处理；数据、分支和评分为固定示意值。它不实现研究会话、真实引擎、动画、文件保存、图片识别或鼠标交互。它使用 mygo 的离屏软件绘制，不是实际系统窗口截图，不能用来证明 GPU 帧率。

本机使用系统字体与 STKaiti；没有将 Apple 字体文件拷贝入项目。发布版字体建议与需要验证的字形差异见主设计文档。

复现方式：先下载 mygo 并检出上述提交，把 `native-preview.go` 复制到一个独立临时 Go module 的 `main.go`，再执行：

```sh
go mod init xiangqi.design/preview
go mod edit -go=1.27.1
go mod edit -require=github.com/egoist/mygo@v0.0.0
go mod edit -replace=github.com/egoist/mygo=/path/to/mygo-source
go mod tidy
go run . /path/to/output-directory
```

两处 `/path/to/…` 需换成实际路径；输出目录需事先存在。依赖准备可能联网，但绘制过程不请求外部服务。

## 当前助手直接源码复核

| 核对内容 | 直接阅读的来源 | 结论 |
| --- | --- | --- |
| 原范围、库存、编辑、库与保存 | 原项目 `docs/product-spec.md`、`AGENTS.md`、`Domain.swift`、`EditorView.swift`、`LibraryView.swift`、`StudyStore.swift` | 对应主方案 F01–F13；编辑的是初始局面，改初始 FEN 先备份再重建树 |
| 分支、AI、评分与灯 | `StudySession.swift`、`EngineService.swift`、`BoardView.swift`、`PikafishBridge.mm`、`RulesExtension.cpp` | 评分看红方、灯看底部；完整路径用于判定规则；安全吃子不是多步战术证明 |
| 图片与恢复 | `docs/image-import.md`、`RecognitionSettings.swift`、`RecognitionThinking.swift`、`ImageRecognizer.swift`、`DeepSeekRecognition.swift`、`ImageRecognitionJob.swift` | 两类服务、两种自定义接口、显式导入、单任务、取消后迟到结果失效 |
| 框架绘图、输入与状态 | mygo `docs/ui/*`、`ui/paint.go`、`ui/svg.go`、`ui/window.go`、`ui/preferences.go`、`content.go` | 自绘界面；斜线走 Path；自定义动画/主题需自行适配系统偏好；Update 后仍校验业务生命周期 |
| 框架关闭与打包 | `app.go`、`cmd/mygo/deb.go`、`cmd/mygo/installscript.go`、`docs/distribution.md` | 最后窗口默认退出；Deb 默认带 WebKitGTK；平台 helper/资源需匹配并签名 |
| 原版交付要求 | 原项目 `.github/workflows/unsigned-ipa.yml` | 每次成功 push 独立发布；主分支正式，其他预发布；PR/手动不自动发布 |

本次子代理操作已取消，没有采用它的结果。此前文档中“两个独立审查均完成”的描述未作为本次证据，已替换为上述自行核对记录。

根据复核，主报告补充了按屏幕 WorkArea 选窗口尺寸、640 DIP 内容高度布局、文字放大与高对比度、边缘棋子绘制边距、识别状态表与请求参数表、HEIC 解码建议、关闭窗口与退出区别，以及桌面 CI 发布契约。

## 证据边界

编译通过只证明 Go Native 示例可针对相应系统生成二进制。离屏绘制只证明本机软件渲染可生成设计图。尚未在实际窗口验证中文 IME、跨屏 DPI、指针捕获、GPU 帧率、读屏、系统凭据、HEIC helper、安装与签名；本轮也未执行任何真实识别请求。产品完整度需按主方案 F01–F38 和阶段验收条件确认。
