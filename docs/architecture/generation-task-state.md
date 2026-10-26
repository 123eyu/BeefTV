# 生成任务「取回 / 重新生成 / 扣费」由后台判定：重构方案

状态：方案稿，待 Codex 独立评审。只描述设计，不含代码改动。
依据：main `3632485`；PR #263（`tds/conv-01a1766b-…`）、#16 精简版（`tds/conv-01a17704-…`）及其四轮检查记录。

## 一、结论

1. **值得重构，不是收尾 #16 就够。** 「提交成没成、结果在不在、点了扣不扣钱」现在由前端约 30 个文件、后端约 18 个文件分别推断（清单见第二节）。同一个后端接口 `POST /tasks/:id/retry` 内部有三种走法（继续下载 / 按回执免费重建 / 重新提交并扣费），前端事先不知道会走哪条，只能靠自己的推断去猜。今天四轮检查查出的问题全部出在「前端猜错了后台会怎么做」。
2. **做法一句话：后台给每个任务算一个「下一步」结论，前端只照着显示和调用。** 这个结论不是新的状态机，是在后台已有的 `taskdelivery.Project`（它已经算出 `resultState`）旁边多算几个字段，输入全部是后台已经持久化的事实。
3. **免费取回放到后台自动做。** 后台已有每 5 秒运行的取回循环（`startGenerationDeliveryRecovery` → 下载结果 → `recoverPendingCanvasBindings` 把结果写进画布节点）。只要在循环里加上「失败的图片任务有完整成功回执就按回执重建」这一步，客户 A 这类节点会被后台自动修好、写回画布，前端不需要任何自动取回代码。#16 里前端的自动取回扫描就不再需要。
4. **免费和扣费拆成两个接口：** `POST /tasks/:id/recover` 结构上不可能提交上游；`POST /tasks/:id/retry` 和带 `retryOf`、对着一个已有节点的 `POST /tasks` 是唯一会扣费的入口，在「可能重复扣费」时必须带上用户确认过的凭据，否则后台拒绝。不管从画布、提示词面板、任务中心、工作流还是助手发起，守门都在后台。
5. **节点上 7 个字段收成 1 个**（`taskOutcome`，从任务抄过来，前端不写），另加只在「任务号还没拿到」时才存在的 `taskClientOperationId` / `generationSubmissionState` 两个字段。
6. **#16 的前端改动不合并；后端的 `result_unrecoverable` 改法和全部回归测试拿来复用**（见第六节）。

## 二、盘点：现在谁在推断

数法：`rg` 搜 `submission_unknown|submission_uncertain|generationSubmissionState|resourceReloadAvailable|shouldBlockManualRetry|blockManualRetry|isKeptGenerationResultCode|taskRetryBlocked|imageSubmissionUnknown|canvasGenerationRetryBlocked`，前端（不含测试）命中 37 个文件、139 处；后端用对应词搜命中 22 个文件、66 处。下面只列真正在做判断的地方。

### 2.1 后端：一个接口多种含义

| 位置 | 判断什么 | 问题 |
| --- | --- | --- |
| `app/service.go` `RetryTask` | 成功但暂停下载 → 继续下载；失败且有回执 → 免费重建；否则 → 进入扣费重试 | 免费和扣费共用 `/retry`，调用方事先不知道走哪条 |
| `app/image_result_recovery.go` `recoverAcceptedImage` | 回执能不能用 | 只在用户手动 `/retry` 时才跑，后台从不自动跑；读库失败和回执确实不能用返回同一句话（#16 才加了 `reason`） |
| `app/image_submission.go` `validateImageTaskRetry`、`shouldDeferImageRecovery` | 派发状态 `accepted/submission_unknown` 时禁止重试；自动重发窗口 23 小时、最多 12 次 | 窗口过后任务永久卡在「失败 + 禁止重试」 |
| `app/generation_failure.go` `persistedFailureErrorCode`、`persistedFailureBlocksRetry` | 按 stage 和**错误文案**重新分类 | 靠文案分类 |
| `app/task_domain_adapter.go` `taskFailuresAdapter`、`task/lifecycle.go` `retry` | 能不能重试、拒绝时给哪句话 | 拒绝只给文案，前端只能匹配文案 |
| `app/task_terminal.go` | 何时把任务标成 `submission_unknown` | — |
| `app/task_output.go` `safePersistedFailureMessage`、`correctPersistedVODParameterFailure` | 读的时候再纠正一次分类 | 读时纠正，前后两套口径 |
| `app/task_route_executor.go`、`modelcatalog/route_attempt.go` | 派发状态 `not_sent/accepted/submission_unknown/rejected_no_job` | 这是事实来源，保留 |
| `app/media_upscale.go` `RecoverMediaUpscale` | 超分「取回原结果」：成功就交付；`not_sent` 就**调 `RetryTask` 扣费重试**；否则查上游 | 名叫取回，其中一条分支会扣费 |
| `app/provider_task_recovery.go` `QueryFailedVideoTask` | 视频/超分查上游 | 只能手动触发，图片不支持 |
| `app/task_delivery_control.go`、`handler/routes.go` `/cancel` | 对成功任务调 `/cancel` = 暂停下载 | `/cancel` 也一口两用 |
| `app/canvas_workflow_run.go` | 工作流重试失败步骤直接调 `RetryTask` / `RecoverMediaUpscale` | 又一个扣费入口 |
| `task/delivery.go` `ResultState`、`taskdelivery/project.go` | 结果是否已入库 | **已经是后台算好给前端的投影**，本方案在它上面扩展 |
| `handler/business_catalog.go` | 把 `/tasks`、`/tasks/:id/retry` 标为外部调用会计费 | 保留 |

### 2.2 前端：各自重新推断

| 位置 | 推断什么 |
| --- | --- |
| `lib/generation-error.ts`（1180 行）`explainGenerationError`、`shouldBlockManualRetry` | 把任务的错误文案**再分类一遍**，得出 `uncertain` / `blockAutomaticRetry` / `blockManualRetry`；和后端 `generation/provider_error.go`（1607 行）做的是同一件事 |
| `lib/vod-image-error.ts` | VOD 错误码 → 「提交未确认」，和 `fixtures/vod-image-errors.json` 重复 |
| `pages/canvas/canvas-generation-failure.ts` | `isKeptGenerationResultCode`、`canvasGenerationFailureMetadata`（写 `resourceReloadAvailable`）、`canvasGenerationRetryBlocked`、`canvasImageGenerationHasPendingResult`、`imageSubmissionUnknown`（同时看 3 个字段）、`canvasGenerationTaskNodes`（算暂停/保留取回/是否完成） |
| `lib/generation-task-display.ts` | 按 stage 判断「提交待确认」「待取回」 |
| `lib/canvas/canvas-generation-task-sync.ts` `generationTaskCanReloadResource` | 能不能继续取回 |
| `lib/canvas/canvas-ai-upscale-task.ts` | 超分有没有挂起的付费任务、失败后写 `resourceReloadAvailable` |
| `lib/canvas/canvas-depth-capture.ts` `canvasNodeFailureExplanation` | 深度任务覆盖 `blockManualRetry` |
| `lib/canvas/canvas-project-generation.ts` | 写 `generationSubmissionState`；`canvasNodeNeedsGenerationRecovery` |
| `lib/canvas/canvas-node-task-state.ts` `isCanvasNodeGenerating` | 节点算不算生成中 |
| `lib/canvas/canvas-image-batch-retry.ts` | 批次重试时搬运 `resourceReloadAvailable` |
| `pages/canvas/use-canvas-generation.ts` | 加载时恢复扫描；查不到任务时**客户端自己写** `generationSubmissionState: "unknown"`；`applyGenerationTaskResult` 写 `resourceReloadAvailable` |
| `pages/canvas/use-canvas-generation-retry.ts` | 视频直接调 `/retry`；其他按 `canvasGenerationRetryBlocked` / `shouldBlockManualRetry` 决定是否带 `retryOf` |
| `pages/canvas/use-canvas-generation-executor.ts` | 提示词面板「生成」前查挂起提交；重复内容确认框 |
| `pages/canvas/canvas-image-generation-executor.ts` | 用户停止等待时写 `resourceReloadAvailable: true` + `generationSubmissionState` |
| `pages/canvas/use-canvas-resource-handoff.ts` `reloadCanvasNodeResource` | 「继续取回」：先查任务，暂停下载就调 `/retry` |
| `pages/canvas/use-canvas-media-tools.ts` | 深度任务失败时**客户端写** `taskStage: "submission_unknown"`；超分取回调 `/media-upscale/.../recover` |
| `pages/canvas/use-canvas-upscale-batch.ts` | 写 `generationSubmissionState` |
| `pages/canvas/canvas-generation-orchestration.ts`、`use-canvas-generation-batches.ts` | 批次里哪些子节点可重试 |
| `components/canvas/canvas-node-content.tsx` `ErrorContent` | 错误卡上「重新生成」按 `uncertain` 隐藏，「继续取回」按 `resourceReloadAvailable` 显示 |
| `components/canvas/canvas-node.tsx` | 批次子节点按钮按 `shouldBlockManualRetry` 判断——和错误卡**来源不同**（#16 说明里已指出） |
| `components/canvas/canvas-node-prompt-panel.tsx`、`components/generation/generation-failure-notice.tsx` | 缺提交标识、是否显示重试 |
| `pages/tasks/task-shared.tsx` 及 `index.tsx` 等 4 个文件 | 任务中心 `taskRetryBlocked`、单条与整组扣费重试 |
| `pages/canvas/canvas-project-status-dialogs.tsx`、`services/api/task-center.ts` `canRetrieveVideoResult` | 视频能否查上游 |
| `pages/create/creation-workspace.tsx` | 创作页是否显示重试 |
| `services/api/generation-submission.ts` `submissionAcceptanceUnknown` | 按 HTTP 状态猜提交成没成 |
| `services/canvas-generation-consumer.ts` | 哪些入库错误算永久失败 |

节点元数据里同时描述成败的字段：`status`、`generationErrorCode`、`resourceReloadAvailable`、`generationSubmissionState`、`taskStatus`、`taskStage`、`taskErrorCode`，其中 `generationErrorCode`、`resourceReloadAvailable`、`generationSubmissionState`、部分 `taskStage` 由前端自己写。

`git log --since=2026-08-01` 里标题含「取回 / 恢复 / 未确认 / 重复 / 扣费 / 重试 / 回执」等词的提交有 86 个（粗数，含发布回执、助手等无关项）；逐个打开核对过的同类修复见第五节。

## 三、设计

### 3.1 后台给出「下一步」：`task.outcome`

在任务的读接口（`GET /tasks/:id`、列表、订阅推送、`by-operation`）上增加一个字段，和现有的 `resultState`、`outputs` 在同一个投影函数里算：

```go
// backend/internal/task/outcome.go（新文件，纯函数，表格测试）
type Outcome struct {
    Phase        string `json:"phase"`        // running | fetching | ready | retrievable | failed
    Retrieve     string `json:"retrieve"`     // auto | manual | ""   只在 retrievable 时有值
    Regenerate   string `json:"regenerate"`   // allowed | confirm | blocked
    Reason       string `json:"reason"`       // 机器原因，前端只用来选文案
    ConfirmToken string `json:"confirmToken,omitempty"` // 只在 regenerate=confirm 时给
}
```

| phase | 含义 | 用户看到的 | 能做什么 |
| --- | --- | --- | --- |
| `running` | 排队、生成中，或后台在按回执/查上游自动取回 | 现有进度条 | 只能取消 |
| `fetching` | 已生成，后台在下载/入库 | 现有「正在取回结果」+ 大小、速度 | 停止取回 |
| `ready` | 结果已入库 | 成片 | — |
| `retrievable` | 结果大概率还在，可以免费取回 | `auto`：「正在取回」；`manual`：现有「继续取回」按钮 | 继续取回（免费） |
| `failed` | 确定没有可取回的结果 | 失败原因 | 按 `regenerate` 决定 |

`regenerate`：`allowed` = 上游确定没扣过钱（未发出、明确拒绝、明确失败），照常重新生成；`confirm` = 上游**可能已经生成并计费**（提交结论未确认、回执确认不能用），重新生成可能重复扣费，必须确认；`blocked` = 原任务本身不能重跑（审核未通过等），要改输入另建任务。

只有 `failed` 才可能出现 `regenerate != blocked`；`running / fetching / retrievable` 一律 `blocked`——结果还可能在，就不能新建付费任务。

输入全部是已持久化的事实：任务 `status/stage/error`、最近一次 `RouteAttempt.DispatchState`、`ImageSubmission.ResponseAccepted`、图片创建回执是否存在且完整、`ProviderRequestID`、`outputs` 的入库状态，加一个新列 `recovery_outcome`（下一节）。计算不读错误文案；文案分类只保留在写入失败时（`task_terminal.go` 已经这么做），不在读时重来。

`confirmToken` = `hash(taskID, routeRun, updatedAt, phase)`。它不是安全凭据（扣费入口本来就要求可信桌面身份），作用是证明「用户确认的就是此刻这个状态」：确认框打开期间后台把任务取回成功了，旧 token 立即作废，后台拒绝扣费请求。

这不是第二个状态机：没有新的状态转移，`Outcome` 是对已有事实的只读函数；`resultState` 并入 `phase` 的计算，过渡期两个字段都返回。

### 3.2 免费取回：后台自动做，接口只有一个

在现有取回循环 `startGenerationDeliveryRecovery`（每 5 秒）里加一步「失败但可能可取回的任务」：

- 图片：`failed` + 派发状态 `accepted/submission_unknown` + 有完整成功回执 → 调 `recoverAcceptedImage`。成功后任务变成 `succeeded`，后面完全走已有的下载 → `recoverPendingCanvasBindings` 写回画布。
- 视频 / 超分：`failed` + 有 `ProviderRequestID` → 按退避（例如 1、5、15、60 分钟，24 小时内）调 `queryFailedVideoTask`。这是查询请求，不创建上游任务。
- 已有的「成功但未入库」继续走原逻辑。

每次尝试用已有的 `ClaimFailedTaskProviderRecovery` 租约，多处同时触发也只有一个执行。结果分三种写回：

- 取回成功：任务变 `succeeded`；
- **确认取不回**（回执缺失或不完整、多条冲突回执、上游明确返回失败）：写 `recovery_outcome = unrecoverable`。只有这个持久化的事实能让 `phase` 变成 `failed`；
- 暂时失败（读库出错、锁冲突、存储配额、网络）：什么都不写，下次再试；退避用尽后 `retrieve` 从 `auto` 变 `manual`。

`POST /tasks/:id/recover`：用户点「继续取回」时调用，做同一件事并重置退避。这个处理函数里**不引用** `store.Retry` / `RetryTask`，在代码结构上就不可能提交上游；用一个测试锁住：mock 上游，断言请求方法里没有 POST。它合并现在分散的三个入口：`/retry` 里的继续下载和回执重建、`/tasks/:id/query-provider`、`/media-upscale/tasks/:id/recover` 里的取回分支。

### 3.3 扣费入口：后台守门

会新建上游任务的只有三条路：`POST /tasks/:id/retry`、带 `metadata.retryOf` 的 `POST /tasks`、对着已有画布节点的 `POST /tasks`。后台统一检查：

1. 原任务的 `outcome.regenerate`。原任务是 `retryOf` 指向的任务；没有 `retryOf` 时，是任务表里「目标为这个画布、这个节点」的最近一个任务（任务输入里已有目标节点，见 `TargetBindingFromInput`，实施时加成可索引的列）。这样不依赖前端或画布文档告诉后台节点以前生成过什么：
   - `blocked` → 409，返回最新的 `outcome`，前端照着刷新显示；
   - `confirm` → 请求体必须带 `confirmToken` 且与当前一致，否则 409；
   - `allowed` → 放行。
2. `/retry` 不再做回执重建和继续下载（已挪到 `/recover`），只做扣费重跑。
3. `RecoverMediaUpscale` 里 `not_sent → RetryTask` 的分支删掉，改为 `outcome.regenerate = allowed`，由用户走扣费入口。
4. 工作流「重试失败步骤」（`canvas_workflow_run.go`）同样先过这道检查。

提示词面板、悬浮工具条、错误卡、批次子节点、任务中心、助手——不管哪个入口，过的都是同一道检查。

外部调用方（business tools）也要先读任务拿到 `confirmToken` 才能重跑可能重复扣费的任务；这一点要写进 `openapi.yaml`。

### 3.4 前端只照着做

- `bindGenerationTask` 把 `task.outcome` 原样存到节点 `metadata.taskOutcome`。所有「能不能重新生成 / 显示继续取回 / 是否锁住」的判断只读这一个字段，集中在一个 `lib/task-outcome.ts` 里给出按钮和文案。错误卡、批次子节点、悬浮工具条、提示词面板、任务中心共用它，消除来源不同的问题。
- 删除：`resourceReloadAvailable`、`isKeptGenerationResultCode`、`canvasGenerationRetryBlocked`、`imageSubmissionUnknown`、`canvasImageGenerationHasPendingResult`、`taskRetryBlocked`、`canRetrieveVideoResult`、`generationTaskCanReloadResource`，以及 #263/#16 新增的 `canvasTaskProvesSubmissionUnknown` 一类判定。
- `taskStatus / taskStage / taskProgress` 保留为显示用的副本，只由 `bindGenerationTask` 写；加一个测试：判定函数不允许读这几个字段。
- `lib/generation-error.ts` 不再分类**任务**的错误，只用于前端本地错误（还没发出请求时的网络、参数、保存失败），任务失败的文案由 `outcome.reason` 选。这部分可以在第 5 步之后慢慢缩，不阻塞上线。
- 前端不再有任何自动调用 `/retry` 或 `/recover` 的代码。加载画布时的恢复扫描（`use-canvas-generation.ts`）只负责订阅任务、把 `ready` 的结果写回节点，这是现在已有的流程。

### 3.5 唯一留在前端的状态：任务号还没拿到

点下生成到拿到任务号之间，只有前端知道「我发过一个请求」，这一段仍用 `taskClientOperationId` + `generationSubmissionState: submitting | unknown`。改两点：

1. `GET /tasks/by-operation/:operationId` 去掉画布过滤，按用户 + 提交标识查（#16 说明「未覆盖」第 3 条指出画布过滤会把已有任务误判为不存在）；画布不一致单独报错。
2. 桌面版后端是本机进程，接纳任务在一个事务里完成。所以「发出超过 2 分钟、按标识确实查不到」可以判定为后台没接纳、也没扣钱，节点直接变成 `failed + regenerate: allowed`。这解决客户 B 的候选路径 2/3（节点停在「尚未查到上次提交的任务……暂勿重新生成」）。重新生成时复用同一个 `clientOperationId`，后台 `admitExisting` 去重，即使判断错了也不会建出第二个任务。2 分钟要大于后端接纳请求的超时，实施时以代码里的超时为准。

### 3.6 四种任务共用一套

`Outcome` 的字段对所有任务类型相同，不同的只是「去哪里找可取回的结果」：

| 类型 | 免费取回的来源 | 重新生成 |
| --- | --- | --- |
| 图片 `canvas_image` | 创建回执 / `ImageSubmission` | 走扣费入口 |
| 视频 `canvas_video` | `ProviderRequestID` 查上游 | 走扣费入口 |
| 超分（按 `UpscaleMediaType(model)` 识别） | `ProviderRequestID` 查上游 | 走扣费入口 |
| 深度捕捉 `depth_capture` | 本地执行器的产物 | 本地计算不扣费，`regenerate` 只会是 `allowed` / `blocked` |
| 文本 `canvas_text` | 无（流式结果已在本地） | 走扣费入口 |

这个分派写在后台一个函数里（按类型选来源），前端不知道这些区别。深度任务里前端自己写 `taskStage: "submission_unknown"` 的两处（`use-canvas-media-tools.ts`）改为由后台根据执行器状态给出。

## 四、迁移：每步单独上线、单独回退

桌面版把前端打包在后端里（`backend/cmd/desktop/frontend.go` 的 `go:embed all:frontend/dist`），同一个安装包里前后端版本一致，不存在「新后端 + 旧前端」长期并存。需要兼容的只有：升级前保存的画布文档里的旧字段，以及外部调用方。

| 步 | 改什么 | 用户能感觉到的 | 回退 |
| --- | --- | --- | --- |
| 1 | 后台：`Outcome` 纯函数 + 表格测试，挂到任务读接口上，只增字段 | 无 | 删字段 |
| 2 | 后台：取回循环自动按回执重建图片；`recovery_outcome` 列（新增可空列） | **客户 A 的节点自己好了**：后台重建 → 下载 → 写回画布，旧前端照常显示成片 | 关掉循环里这一步 |
| 3 | 后台：`POST /tasks/:id/recover`；视频/超分自动查上游（带退避） | 视频失败后能自己取回 | 关闭自动查询，接口保留 |
| 4 | 前端：所有判断改读 `taskOutcome`；「继续取回」调 `/recover`；删掉第 3.4 节列出的推断函数和 `resourceReloadAvailable` | 错误卡、批次子节点、任务中心按钮一致；不再出现节点锁死 | 回退前端提交（后端字段是增量，旧前端照常工作） |
| 5 | 后台：扣费入口守门（第 3.3 节）；`/retry` 去掉免费分支；`RecoverMediaUpscale` 去掉扣费分支 | 可能重复扣费时，任何入口都会先弹确认 | 守门做成开关，可以先只记日志不拒绝，观察一个版本再改成拒绝 |
| 6 | 清理：`by-operation` 去掉画布过滤；2 分钟判定；前端不再分类任务错误 | 「查不到原任务」不再锁死 | 单独回退 |

第 4 步和第 5 步要放进同一个安装包：前端不再自己拦扣费、后端还没开始守门的版本不能单独发出去。第 1–3 步可以先发一个版本。

旧画布文档：读取时忽略 `resourceReloadAvailable` 和前端写的 `generationErrorCode`，节点的成败以重新拉到的任务 `outcome` 为准；`canvas-document-rebase.ts` 的字段清单同步删掉。没有 `taskId` 的历史节点保持现状（显示「页面刷新后本地生成已中断」）。

## 五、拿真实问题验收

「为什么不会再发生」写的是结构上的原因；每条都要留一个回归测试，测试素材优先用 #16 已有的。

### 5.1 今天四轮检查查出的问题

| # | 问题（来源） | 新设计下 |
| --- | --- | --- |
| 1 | 客户端写的错误码触发自动扣费重试（#16 第 1 轮 P1） | 前端没有任何自动调用扣费接口的代码；自动取回在后台，只走 `/recover`，那里不引用重试逻辑 |
| 2 | 提示词面板绕过扣费确认（#16 第 1 轮 P1） | 守门在后台：对着已有节点的 `POST /tasks` 先找该节点最近一个任务的 `outcome`，需要确认时不带 token 就拒绝；前端漏不漏弹框都扣不了钱 |
| 3 | 恢复锁冲突（400）、存储配额（403）被当成拿不回（#16 第 1 轮 P2） | 「拿不回」只来自持久化的 `recovery_outcome = unrecoverable`；`/recover` 的任何报错都不改变 `phase`，前端也不根据 HTTP 状态做判断 |
| 4 | 读回执时数据库临时出错被当成拿不回，靠匹配文案判断（#16 第 2 轮） | 同上；读库出错时不写 `recovery_outcome`，下一轮循环再试。#16 后端加的 `result_unrecoverable` 判断点正好就是写 `recovery_outcome` 的位置 |
| 5 | 后到的画布快照重复认领同一节点（#263 第 1 轮） | 取回不在前端认领；后台按任务加租约（`ClaimFailedTaskProviderRecovery`），画布快照来几次都不会多触发 |
| 6 | 恢复响应丢失，被误判为取不回（#263 第 1 轮） | 判定只看后台事实，不看某一次请求的响应；响应丢了，前端订阅到的任务也已经是 `succeeded` |
| 7 | 自动取回和手动重新生成用的是两套判断（#263 第 1 轮） | 只有一个 `outcome`，所有入口读同一个字段 |
| 8 | 节点锁死：任务已终态仍按「提交未确认」锁住（客户 A，#15） | 每个终态都有出口：`retrievable` 后台自动取；`failed` 时 `regenerate` 为 `confirm` 或 `allowed`。只剩审核未通过这类 `blocked`，那是改输入另建任务 |
| 9 | 查不到原任务就永远「暂勿重新生成」（客户 B 候选路径 2/3，#16 未覆盖） | 第 3.5 节：去掉画布过滤 + 超过 2 分钟查不到即判定未接纳；重新生成复用同一提交标识，后台去重 |
| 10 | 错误卡和批次子节点对「能否重试」的判断来源不同（#16 未覆盖第 2 条） | 两处都改读 `taskOutcome` |

### 5.2 过去的同类修复（逐个打开核对过）

| 提交 | 当时的问题 | 新设计下 |
| --- | --- | --- |
| `d989cce`（#90）阻止恢复前重复收费 | 结果还没恢复，用户又点生成 | 后台守门：`retrievable` 时 `regenerate = blocked` |
| `463b558` 继续取回闪烁 | `resourceReloadAvailable` 被多处写来写去 | 字段删掉 |
| `ce93191` 晚到快照对账 | 前端恢复扫描和后到快照抢节点 | 取回挪到后台，前端只订阅 |
| `b95cb1a`（#193）视频恢复补全原任务关联 | 节点丢了任务号，取回提示不对 | 部分覆盖：取回提示统一由 `outcome` 给；任务号关联本身仍靠 `generationTaskNodeId`，不在本方案里 |
| `ceb3173` 区分人工与自动重试 | 准备失败时自动重试会重发 | 自动路径不再有重试，只有取回；人工重试的条件由 `regenerate` 给 |
| `2a58c3b` 区分「未发出」和「提交未确认」 | 两者混在一起 | 后台事实层已经区分（`not_sent` / `request_not_sent`）；新设计把它集中到 `Outcome` 的一张测试表里，前端不再另做一遍 |
| `cfbe1ff`（#178）VOD 错误提示与恢复后原任务保护 | VOD 错误码前后端各一份 | 前端只用 `reason` 选文案，不再维护判定表 |
| `21af10c`（#142）深度任务恢复与重试 | 深度任务特殊处理散在前端 | 第 3.6 节，深度只是一种取回来源 |
| `5cee841`、`2a3b343` 视频原任务取回校验身份 | 取回时核对任务身份 | 已在后台，保留 |
| `0ecf309`、`907d36d` 拒绝不完整回执、恢复原图下载 | 回执有效性校验 | 已在后台，保留，并从「只有手动才跑」改为循环自动跑 |
| `1049faa` 路由或取消前保留未确认提交 | 派发状态记录时机 | **新设计管不到**：这是事实层的记录错误 |
| `da5cfae` 多张图片输出贯穿恢复与重试 | 多输出只恢复了一张 | 部分覆盖：`outputs` 已按序号投影；`Outcome` 是任务级的，部分输出失败时用 `fetching` / `retrievable`，需要在表格测试里加多输出用例 |

### 5.3 新设计也防不住的

- **事实记错了**：派发状态、回执落库、上游错误分类本身出错（如 `1049faa`），`Outcome` 会照着错误的事实给出错误结论。好处只是这类错误只需要在后端一处修、一张测试表里补用例。
- **上游自己的问题**：上游返回成功却没有图、查询接口报错、资源链接过期，只能落到 `failed + confirm`，由用户决定是否重新生成、可能重复扣费。
- **第 3.5 节的 2 分钟判定**：极端情况下（本机后端卡住超过 2 分钟后才接纳），会把已接纳的任务判为未接纳。复用同一提交标识重新生成时后台会认出是同一个任务，所以不会重复扣钱，但用户会看到一次多余的「重新生成」入口。
- **外部调用方**：持有 `confirmToken` 的外部程序仍然可以自己决定重跑，这是 business tools 现有的「由外部调用方审批」约定，本方案不改变。

## 六、#263 和 #16 里能直接用的

| 内容 | 处理 |
| --- | --- |
| #16 后端 `ResultUnrecoverable` 的几处判断（`image_result_recovery.go`、`image_submission.go`、`lifecycle.go`）和 `repository.ErrAmbiguousImageCreateReceipts` | **直接复用**，改成在这些位置写 `recovery_outcome`，替代返回 `reason` |
| #16 后端测试 `TestImageReceiptRecoveryReasonSeparatesUnrecoverableFromTemporaryFailure`、`TestRetryUnknownAcceptanceAndImageReceiptStayOnOriginalTask` 新增断言 | 改成断言 `Outcome`，进第 1、2 步 |
| #16 前端测试 `canvas-generation-failure`、`canvas-task-result-recovery`、`canvas-settled-retry-confirmation`、`canvas-late-generation-recovery.browser`、`canvas-uncertain-recovery.browser`、`generation-preflight` 夹具 | 场景全部保留作验收（第五节第 1–7、10 条）；断言从「前端判定函数返回什么」改成「给定后台 `outcome`，界面显示什么、请求发了几次」 |
| #16 错误卡的三种文案（正在自动取回 / 确认取不回可重新生成 / 未定只能查看）和三张截图 | 直接对应 `retrievable+auto`、`failed+confirm`、`running`，文案复用，不加新按钮 |
| #16 / #263 前端的 `canvasTaskProvesSubmissionUnknown`、`canvasNodeNeedsOriginalResultRecovery`、`recoverCanvasTaskResult`、恢复扫描里的自动取回分支、提示词面板付费确认分支 | 不合并，由后台取代 |
| #15 根因说明（`docs/review-evidence/canvas-uncertain-result-recovery/README.md`） | 作为背景材料保留 |

如果 v2.0.4 等不及第 4、5 步，最小止血是只发第 1、2 步：只改后端，客户 A 这类节点会被后台自动修好，前端一行不改。这需要先在本机用一份带完整回执的失败任务验证一次「后台重建 → 下载 → 写回画布 → 旧前端显示成片」这条链路。本方案没有跑过这条链路，这是推断。

## 七、待评审确认的点

1. `failed + confirm`（上游可能已扣费、但确认取不回）时允许用户确认后重新生成，这是沿用 #16 已定的做法；另一种做法是只给「联系支持」，不开放重新生成。
2. 守门按「目标为同一画布、同一节点的最近任务」找原任务。用户把节点复制成新节点再生成，就不算同一个节点，不会弹确认——这和现在「复制节点后点生成可能再次计费」的提示（`use-canvas-node-operations.ts`）一致，但要评审确认是否接受。另外批次子节点、多输出任务的目标节点要按 `outputNodeIds` 展开核对。
3. 视频/超分自动查上游的退避和总时长（方案里写的是 24 小时内 1、5、15、60 分钟）要按各渠道查询接口的限流来定。
4. 列表接口要多查回执是否存在、派发状态，需要批量查询，避免一条任务一次查询。

## 八、上游 Infinite Canvas

`github.com/basketikun/infinite-canvas` 没有任务中心、异步任务和恢复层（#15 根因说明里的对照），这部分没有可照搬的做法，按 BeefTV 已有的任务、回执、取回循环来收拢。
