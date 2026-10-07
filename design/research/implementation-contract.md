# 首版模块约定

模块 `github.com/ch1y1z1/xiangqi_go`，Go 1.27.1。各实现者只修改自己负责目录，不修改 go.mod/go.sum、README 或其他模块。新增依赖写到交付说明，由集成者合并。

## domain / store

domain.Side 为 string，常量 Red="red"、Black="black"；方法 Opponent() Side、Title() string。
domain.Kind 为 string，常量 King/Advisor/Elephant/Horse/Rook/Cannon/Pawn；Kinds []Kind；方法 Limit() int、Glyph(Side) string。
domain.Square{File,Rank int}，方法 UCI() string、Valid() bool；ParseSquare(string)(Square,error)。
domain.Piece{ID string, Side Side, Kind Kind, Square Square}；domain.Move{From,To Square}，UCI() string；ParseMove(string)(Move,error)；Notation(Move,[]Piece) string。
domain.Node{ID string, ParentID string, Move *Move, Children []string}。
domain.Study{ID,Name string, CreatedAt,ModifiedAt Date, InitialPieces []Piece, InitialSide Side, Nodes []Node, RootID,CurrentID string, BottomSide Side, IsDraft bool, SchemaVersion int}。
Date 保留 Swift Date numeric JSON，提供 NewDate(time.Time) Date、Time() time.Time。
NewID() string；NewStudy(name string,pieces []Piece,side Side) Study；Examples() []Study；InitialPieces() []Piece；PiecesFromFEN(string)([]Piece,error)；FEN([]Piece,Side) string。
Study 方法 Node(id string)*Node、Line(id string) []Node、CurrentPieces() []Piece、SideToMove() Side、InitialFEN() string、History() []string、Play(Move) string（返回选中子节点ID）、Label(Node) string、BranchCount() int、Duplicate() Study、EditSetup(name string,pieces []Piece,side,bottom Side) Study。树需稳定UUID、不合并不同历史、原字段和日期兼容。
Editor{Pieces []Piece, Selected *Square, PlacementSide Side, PlacementKind Kind}；NewEditor([]Piece) *Editor；Choose(Kind,Side)、Tap(Square) error、Move(Square,Square) error、DeleteSelected()、Replace([]Piece)、Undo()、Redo()、CanUndo() bool、CanRedo() bool；库存限制与原版一致，历史只恢复棋子数组。
store.New(directory string)(*Store,error)，自动初始化原三样例一次；Store.List() []domain.Study、Save(domain.Study) error、SaveEdited(domain.Study) error、Delete(id string) error；按 modifiedAt 排序，原子写入、编辑前副本。directory 由主App传入。

## engine

不依赖domain。Capture{Move,Side string}；Snapshot{Error string,LegalMoves []string,Check,Finished bool,Winner,Outcome string,Captures []Capture}。
Result{BestMove string,PV []string,Depth,Score int,Mate bool,Bound string,HasScore,Cancelled bool}；Mate 的 Score 是原桥接的 plies。
New(workerPath,networkPath string)(*Client,error)；Client.Inspect(ctx context.Context,fen string,moves []string,hints bool)(Snapshot,error)；Search(ctx,fen,moves,milliseconds int)(Result,error)（前3项同Inspect类型）; Stop() error；Close() error。
实现 C++ worker JSONL 协议与Go客户端，搜索不能堵协议reader。复用原规则/安全吃子扩展；固定引擎和NNUE；脚本按平台构建并准备资源。资源目录资源/bin/xiangqi-engine(.exe)、pikafish.nnue，开发模式可传明确路径。

## recognition / credentials

Settings{Provider,Address,Model,API,DeepSeekThinking,CustomThinking string}；Provider deepSeek/custom；API chatCompletions/responses；思考off/low/high/max，自定义空串服务默认。
DefaultSettings() Settings；(Settings).Endpoint()(string,error)；Validate(key string) error；Request(ctx context.Context,jpeg []byte,key string,settings Settings)(*http.Request,error)；Recognize(ctx,jpeg,key,settings)(Setup,error)。
PrepareImage([]byte)([]byte,error)，方向校正+2048 JPEG；失败说明不支持格式，交付时说明覆盖；不得网络实际付费调用。
RecognizedPiece{Side,Kind string,Column,Row int}；Setup{Name,BottomSide,SideToMove,Notes string,Pieces []RecognizedPiece}；Validate() error；ChessPieces() []domain.Piece（允许依赖domain）。
JobManager 应用单任务，NewJobs(directory string)(*Jobs,error)；Start(jpeg []byte,key string,settings Settings) error；Stop() error；Clear() error；Record() *JobRecord（返回线程安全拷贝）；Image() []byte；OnChange func() 通知字段。JobRecord{ID,Status,Error string,StartedAt time.Time,Result *Setup}，Status running/ready/failed；运行不随页面离开取消；保存状态和图片无密钥；重启中断记失败不自动重试；完成删请求响应。Start 不允许隐式覆盖未完成任务。加入有效取消检查与http测试。
credentials.Get(service string)(string,error)、Set(service,key string) error、Delete(service string) error；按系统凭据库存储，两类service隔离，禁止明文兜底。没有密钥以空串nil返回。

## integration

session.New(study domain.Study,backend Backend,persist func(domain.Study)error)*Session；Backend接口等同engine.Client.Inspect/Search/Stop（Stop()error）。Session可导出字段 Study、Rules、Selected、Suggestion、AISide domain.Side(空=手动)、Paused、Busy、RulesBusy bool、Active bool、Saved bool、Error string、Budget int、ShowLights/ShowEvaluation/ShowBranchScores bool、BranchParentID string；Dispatch func(func()) 将回调送主线程；OnChange func() 可选。
方法 Activate()、Leave()、Tap(Square)、Drag(Square,Square)、Jump(id)、Back()、Forward() bool（多分支未选择返回false）、Root()、Flip()、SetAI(Side)、ResumeAI()、Recommend()、Adopt()、Stop()、SetBudget(int)、SetShowLights(bool)、SetShowEvaluation(bool)、SetShowBranchScores(bool)、OpenBranches(parentID string)、CloseBranches()、Reanalyze()、EvalText(id string)string、EvalDetail(id string)string、Comparison(id,parentID string)string、ApplyEdit(domain.Study)error。
评估类型在session内部定义，UI用EvalText/Detail/Comparison读取。不要直接从goroutine改导出UI状态；每次回调Dispatch后再检查有效代次。Leave关取消时延迟回调不得访问失效状态。评分开关持久化由App调用prefs实现，session默认两个开，AI始终默认手动。规则inspect也不得阻塞UI主线程，使用异步并显示RulesBusy，最终规则快照对应有效node/setup。

Go UI 与会话由集成者实现。外部服务调用都必须来自用户实际点击识别；启动、测试、布局与服务设置验证不得自动请求付费模型。不得读本机已有项目的个人密钥。契约不清可在交付说明明确改动并提供实际API。
