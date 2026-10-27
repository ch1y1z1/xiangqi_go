# Session 接入约定

`New(study domain.Study, backend Backend, persist func(domain.Study) error) *Session` 不执行引擎 I/O，并深复制 study。`*engine.Client` 实现 Backend。Backend 的 `Inspect`、`Search`、`Stop` 签名与 implementation-contract.md 一致；Stop 必须可以并发到达阻塞中的请求，Backend 应支持 context 取消。同一 backend 同时只交给一个活跃 Session；关闭 backend 由 App 负责。

先设置 `s.Dispatch = func(fn func()) { win.Update(fn) }`（按实际窗口 API 适配），再调用 `s.Activate()`。Dispatch 必须真正排入 UI 线程，不能直接在调用者 goroutine 执行 fn。Dispatch 必须在 Leave 之后仍能安全被后台调用；窗口销毁时可以丢弃回调。所有公开方法、公开状态读取和 OnChange 都属于同一 UI 线程；公开状态只读，使用方法更新，不要跨线程访问或直接改 Study/开关。New 没有同步 inspect；未设置 Dispatch/backend 时 Activate 会设置 Error 并保持未激活。

## 实际公开状态

- `Study domain.Study`、`Rules engine.Snapshot`、`Selected *domain.Square`、`Suggestion *engine.Result`。
- `AISide domain.Side`：空串双方手动，Black 我红 AI 黑，Red 我黑 AI 红；`Paused bool` 表示用户明确暂停。局面导航和引擎失败不暗改这两个状态。
- `Busy bool`：后台分支 inspect 或任意 search；`RulesBusy bool`：当前节点规则 inspect。新增 `AnalyzingID string` 区分后台评分目标，主动 AI 时为空。停止按钮可在 Busy 或 RulesBusy 时启用。
- `Active, Saved bool`、`Error string`、`Budget int`（毫秒，默认 1000；标准预设 300/1000/3000）。
- `ShowLights, ShowEvaluation, ShowBranchScores bool`（默认全开）、`BranchParentID string`。
- `Dispatch func(func())`、`OnChange func()`。启动前设置 Dispatch，运行中保持不变；OnChange 可选，只在 UI 线程通知。

`Rules.LegalMoves` 与 `Suggestion.BestMove/PV` 是 UCI 字符串，UI 用 `domain.ParseMove` 解析。Suggestion 保留引擎原始、行棋方视角的 Result；展示红方视角评分用当前节点的 EvalText/EvalDetail，不要直接显示 Suggestion.Score。Rules.Captures 同时包含红黑双方；UI 按 Study.BottomSide 筛选观察方。Flip 不重新评分、不重查规则，仅清选择并保存朝向。

## 实际方法

```go
Activate()
Leave()
Tap(domain.Square)
Drag(domain.Square, domain.Square)
Jump(id string)
CanBack() bool
Back()
Forward() bool
Root()
Flip()
SetAI(domain.Side)
PauseAI()
ResumeAI()
AutomaticTurn() bool
AIBlocked() bool
CanRecommend() bool
Recommend()
Adopt()
Stop()
SetBudget(milliseconds int)
SetShowLights(bool)
SetShowEvaluation(bool)
SetShowBranchScores(bool)
OpenBranches(parentID string)
CloseBranches()
Reanalyze()
EvalText(id string) string
EvalDetail(id string) string
Comparison(id, parentID string) string
ApplyEdit(domain.Study) error
```

Forward 遇未选择的多分支返回 false；UI 展示分支选择后调用 Jump。Jump/Back/Forward/Root 保留控制方与暂停状态，旧请求失效后按新局面调度。Back 在人机模式下回到当前线路最近一手我方落子之前，连同之后的 AI 应手一起撤回；当前我方颜色为 AISide 的对方，暂停或采用建议不改变身份。双方手动时退最近一手。CanBack 为 false 时 Back 不取消任务、不改变局面；UI 与快捷键使用同一目标规则。

PauseAI/ResumeAI 显式暂停或继续；暂停只取消自动落子，不取消仍然有效的规则检查、手动建议或后台评分。更换 AI 控制方保留已有暂停状态，重复选择当前控制方不恢复。双方手动切换为 AI 模式视为启动托管。AutomaticTurn 表示按控制设置轮到 AI，不代表正在搜索；终局、规则检查或故障可能阻止任务。AIBlocked 表示当前自动行棋被故障阻止。CanRecommend 供 UI 判断当前是否允许手动建议；自动回合调用 Recommend 不抢占自动行棋，应先显式暂停。其他回合的 Recommend 只生成建议，不改暂停状态，Adopt 才落子；我方采用建议后，运行中的 AI 会正常应手。

调度优先级为主动建议/托管、当前评分、展开组的即时子局面评分；每次一项。展开或收起分支比较不取消自动行棋或改变暂停状态。当前/分支评分开关互不联动，且不会取消主动建议/托管。Stop 在自动回合等同显式 PauseAI（如 Escape）；在其他回合只停止本次分析、清建议并阻止其立即重启，不改变托管设置。Recommend、ResumeAI、Reanalyze 或切换局面/预算等明确操作可重新启动。

Reanalyze 清当前节点及当前展开组子节点的评分/失败缓存，并重试当前规则错误；仍遵守两个评分开关。自动 AI 或规则检查失败保留托管设置，阻止自动重复请求，以 Reanalyze 显式重试；建议失败用 Recommend 重试。终局不设置暂停，导航到非终局后重新判断。失败的背景评分不会因为开关反复切换而自动重试，详情经 EvalDetail 返回。未获有效分数显示“待评估 / 计算中… / 未获评分”，合法建议可以没有有效评分。

评分缓存包含完整路径节点、起点 FEN/root/study、预算、固定引擎提交和 NNUE 标识；生命周期限制在该 Session 和不可替换的 backend 中。预算变化清缓存；ApplyEdit 即使 root ID 未改，只要初始棋子/先行方改变也清缓存。仅改名/朝向保留有效评分。换引擎/模型请新建 Session。

普通分固定红方视角，黑方取反并交换上下界；M 使用桥接 signed plies 的原版换算。Comparison 仅在同父节点至少两个直接子节点全部是同预算普通精确分时给出“本组较优”或精确分差，终局/M/界限/缺分不参加。

persist 同步在 UI 方法内执行，获得深复制的 Study；普通动作保存失败保留内存状态并设置 Saved=false/Error。ApplyEdit 先 persist，失败不替换 Study。App 应传入可处理编辑前副本语义的保存闭包（如按调用上下文路由 Store.Save / Store.SaveEdited）；Session 自身不创建编辑前副本。开关偏好仍由 App 的 prefs 保存。

## 验证与范围

Fake backend 通过 channel 控制调用、返回、Stop 完成及 UI 回调；无 sleep。覆盖迟到 inspect/search、UI 排队后失效、Stop/旧请求双屏障、抢占、三模式、建议采用分离、偏好路径、分支即时子局面、评分转换/差值、缓存/预算/开关、失败重试和保存失败。另有实际 Backend 接口编译检查。

本目录未接入桌面窗口、prefs 或 store，也未运行真实 Pikafish/NNUE 的端到端 UI 测试；这些由集成层和对应模块负责。规则错误保留引擎原始详情并加中文上下文，未移植 Swift 的逐类友好错误翻译。整个 Session 的 UI 状态不是可供任意 goroutine 并发读写的共享模型；线程安全来自明确 UI 所有权、不可变后台输入和 Dispatch 后再次验证。
