# 生成任务「取回 / 重新生成 / 扣费」由后台判定：重构方案

状态：方案稿，待 Codex 独立评审。只描述设计，不含代码改动。
依据：main `3632485`；PR #263（`tds/conv-01a1766b-…`）、#16 精简版（`tds/conv-01a17704-…`）及其四轮检查记录。

## 一、结论

1. **值得重构，不是收尾 #16 就够。** 「提交成没成、结果在不在、点了扣不扣钱」现在由前端约 30 个文件、后端约 18 个文件分别推断（清单见第二节）。同一个后端接口 `POST /tasks/:id/retry` 内部有三种走法（继续下载 / 按回执免费重建 / 重新提交并扣费），前端事先不知道会走哪条，只能靠自己的推断去猜。今天四轮检查查出的问题全部出在「前端猜错了后台会怎么做」。
2. **做法一句话：后台给每个任务算一个「下一步」结论，前端只照着显示和调用。** 这个结论不是新的状态机，是在后台已有的 `taskdelivery.Project`（它已经算出 `resultState`）旁边多算几个字段，输入全部是后台已经持久化的事实。
3. **免费取回放到后台自动做。** 后台已有每 5 秒运行的取回循环（`startGenerationDeliveryRecovery` → 下载结果 → `recoverPendingCanvasBindings` 把结果写进画布节点）。只要在循环里加上「失败、可能已被上游接纳的图片任务按回执重建，重建不了就写明原因」这一步，客户 A 这类节点会被后台自动修好、写回画布，前端不需要任何自动取回代码。#16 里前端的自动取回扫描就不再需要。
4. **免费和扣费拆成两个接口：** `POST /tasks/:id/recover` 结构上不可能提交上游；`POST /tasks/:id/retry` 和带 `retryOf`、对着一个已有节点的 `POST /tasks` 是唯一会扣费的入口，在「可能重复扣费」时必须带上用户刚确认过、只能用一次的确认 ID，否则后台拒绝；核对和接纳在同一个事务里。不管从画布、提示词面板、任务中心、工作流还是助手发起，守门都在后台。
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
}
```

| phase | 含义 | 用户看到的 | 能做什么 |
| --- | --- | --- | --- |
| `running` | 排队、生成中，或后台在按回执/查上游自动取回 | 现有进度条 | 只能取消 |
| `fetching` | 已生成，后台在下载/入库 | 现有「正在取回结果」+ 大小、速度 | 停止取回 |
| `ready` | 结果已入库 | 成片 | — |
| `retrievable` | 结果大概率还在，可以免费取回 | `auto`：「正在取回」；`manual`：现有「继续取回」按钮 | 继续取回（免费） |
| `failed` | 确定没有可取回的结果 | 失败原因 | 按 `regenerate` 决定 |

`regenerate` 回答的问题是「**这个节点的结果还没到手时**，再新建一个付费任务会不会重复花钱」。结果已经到手（`ready`）后用户主动再生成，不归它管：

| 任务状态 | `regenerate` | 理由 |
| --- | --- | --- |
| `running` / `fetching` / `retrievable` | `blocked` | 结果可能还在，先取回；不能为同一个结果再付一次钱 |
| `ready`（已有成片） | `allowed` | 用户看到成片后主动再生成，是新的一次生成。现有「已有图片原位生成、保留旧版本」（`canvas-image-generation-executor.ts`）和「相同内容再次生成」确认框（`use-canvas-generation-executor.ts`）照旧，不归本方案的守门管 |
| `failed`，上游确定没扣过钱（未发出、明确拒绝、明确失败） | `allowed` | 照常重新生成 |
| `failed`，上游**可能已生成并计费**（提交结论未确认、确认取不回） | `confirm` | 重新生成可能重复扣费，必须确认 |
| `failed`，原输入本身不能再提交（审核未通过等） | `blocked`（只对「原样重跑」） | 改了输入可以重新提交，见下 |

对着已有节点的新提交（包括 `/retry`、带 `retryOf` 的提交、提示词面板的「生成」）一律按节点当前任务的 `regenerate` 判，**改没改输入都一样**：
- `blocked`（`running / fetching / retrievable`）：拒绝，结果还没回来时不再买一份；
- `confirm`：改了输入也必须先确认。上游可能已为原任务扣过费，用户是否愿意再花一次钱，与输入改没改无关；只改一个字就绕过确认，就重新放开了提示词面板绕过扣费确认的路径；
- `allowed`：放行。`ready` 节点主动再生成属于这一类，照旧可用。

只有一种情况要比较输入：原任务因审核未通过而 `blocked`。这时它拦的只是「原样重跑」，改了提示词或参考素材就放行（不扣过费，不需要确认）。比较的是**生成内容**，不是整个请求：后台已有的 `ClientOperationHash` 会把 `metadata.clientOperationId` 一起哈希，只换提交标识也会变，不能直接拿来用。实施时另写一个只覆盖提示词、参考素材、模型和参数的内容指纹，在后台用原任务存下的输入算；前端的 `failedInputFingerprint` 只用于提示，不作判断依据。

输入全部是已持久化的事实：任务 `status/stage/error`、最近一次 `RouteAttempt.DispatchState`、`ImageSubmission.ResponseAccepted`、图片创建回执是否存在且完整、`ProviderRequestID`、`outputs` 的入库状态，加一个新列 `recovery_outcome`（下一节）。计算不读错误文案；文案分类只保留在写入失败时（`task_terminal.go` 已经这么做），不在读时重来。

确认不用读接口返回的 token。读到 token 不等于用户确认过，而且一个只表示「状态版本」的 token 可以被多个不同提交标识的 `POST /tasks` 重复使用，一次确认买到多份。改成**一次确认只换一次付费提交**：

1. 用户在确认框点「仍然重新生成」后，前端调 `POST /tasks/:id/regenerate-confirmations`，请求体带这次要用的 `clientOperationId`。后台检查原任务此刻 `regenerate = confirm`，写一条确认记录：`(原任务 ID, 原任务的 routeRun 与 updatedAt, clientOperationId, 创建时间)`，返回确认 ID。这个接口**只接受桌面界面本身的请求**：
   - 不能用 `requireTrustedDesktopWritePrincipal`：它会放行可写的外部调用方（`externalBusiness`），而 `businessRouteAllowed` 把 `/tasks` 下所有路径都暴露给 business tools，外部程序就能自己申请确认 ID、自己扣费；
   - 校验改用 `trustedDesktopUI`（本机回环 + 桌面外壳信任），并且请求里带 `X-Beeftv-Client` 或任何 business 身份时一律拒绝；Docker/网页部署没有桌面外壳信任，改用已有的本机所有者令牌校验（`requireDesktopUITrust` 里的 `OwnerTokenMatches` 那一套）；
   - 在 `businessRouteAllowed` 里显式排除这个路径，business tools 目录里不出现它；
   - 回归测试：外部调用方（可写令牌）、助手宿主、带 `X-Beeftv-Client` 的请求调用这个接口都返回 403。
2. 扣费提交带上确认 ID。后台在**接纳新任务的同一个事务**里核对并消耗它：确认记录存在、未用过、`clientOperationId` 一致、原任务的 `routeRun/updatedAt` 没变、没超过有效期（例如 10 分钟），然后把记录标成已用、写入新任务，同一事务提交。
3. 同一次确认重放（网络重试、双击）时，`clientOperationId` 相同，走已有的 `admitExisting`，返回同一个任务；换一个 `clientOperationId` 用同一个确认 ID，会被拒绝。
4. 确认框打开期间后台把结果取回了，原任务的 `updatedAt` 变化，确认作废，提交被拒并返回最新 `outcome`。
5. 助手：保留现有人工授权（`SubmitAuthorizedGeneration` 只认用户确认过的制作方案，#261），助手自己不能创建确认记录。方案卡里包含 `confirm` 节点时，用户在桌面界面上确认方案这一步由后台同时生成这些节点的确认记录，助手只能消耗、不能新建。外部调用方（business tools）**不能**对 `confirm` 任务重跑：它们拿不到确认 ID，遇到这类任务后台返回 409，提示到桌面界面里确认。`allowed` 任务照旧按「由外部调用方审批」的约定处理。

这不是第二个状态机：没有新的状态转移，`Outcome` 是对已有事实的只读函数；`resultState` 并入 `phase` 的计算，过渡期两个字段都返回。

### 3.2 免费取回：后台自动做，接口只有一个

在现有取回循环 `startGenerationDeliveryRecovery`（每 5 秒）里加一步「失败但可能可取回的任务」：

- 图片：扫描范围是**所有**「`failed` 且可能已被上游接纳」的图片任务，不要求回执完整，也不要求有路由记录。判定「可能已被接纳」的依据，满足任一条即可：最近一次派发状态是 `accepted/submission_unknown`；`stage = submission_unknown`；存在任何一条图片创建回执（包括没有路由记录的历史任务，`TestRetryAcceptedImageUsesOriginalReceiptWithoutCreate` 就是这种，必须继续能恢复）。对每个任务调 `recoverAcceptedImage`：回执完整就重建，成功后任务变成 `succeeded`，后面完全走已有的下载 → `recoverPendingCanvasBindings` 写回画布；回执缺失或不完整就按下面的分类写结论，不会被扫描漏掉、停在没有出口的状态。
- 视频 / 超分：`failed` + 有 `ProviderRequestID` → 按退避（例如 1、5、15、60 分钟，24 小时内）调 `queryFailedVideoTask`。这是查询请求，不创建上游任务。
- 已有的「成功但未入库」继续走原逻辑。

每次尝试用已有的 `ClaimFailedTaskProviderRecovery` 租约，多处同时触发也只有一个执行。结果分四种写回，其中「拿不回」细分成两种，因为对用户意味着不同的事：

- 取回成功：任务变 `succeeded`；
- **确定上游没有结果**（上游明确返回失败、明确拒绝、确认没接纳）：写 `recovery_outcome = no_result`。上游没生成，也就没扣钱，`phase = failed`、`regenerate = allowed`；
- **系统无法取回**（回执缺失、回执不完整、多条冲突回执、回执里的链接已过期；上游可能已经生成并计费，只是这边拿不到）：写 `recovery_outcome = unrecoverable`。`phase = failed`、`regenerate = confirm`，文案写明「上游可能已生成并扣费，本地拿不到结果」，并保留「查看原任务」；
- 暂时失败（读库出错、锁冲突、存储配额、网络）：什么都不写，下次再试；退避用尽后 `retrieve` 从 `auto` 变 `manual`。

只有前两类拿不回的持久化事实能让 `phase` 变成 `failed`。#16 后端打 `result_unrecoverable` 的几处，按上面拆成这两类写入。

`POST /tasks/:id/recover`：用户点「继续取回」时调用，做同一件事并重置退避。这个处理函数里**不引用** `store.Retry` / `RetryTask` / 任务接纳，在代码结构上就不可能新建任务。验收不按 HTTP 方法判断——现有免费取回本身就会用 POST：超分 `RecoverUpscaleResult` 调原任务的 `/recover`（`generation/upscale.go`），RunningHub 查结果也是 POST。测试断言的是：上游 mock 只收到查询/取回类请求（按协议声明的 request kind 是 `poll`/`query`/`recover`，没有 `create`）；上游任务号始终是原任务的 `ProviderRequestID`；本地任务表没有新增任务，原任务的 `routeRun` 不变。它合并现在分散的三个入口：`/retry` 里的继续下载和回执重建、`/tasks/:id/query-provider`、`/media-upscale/tasks/:id/recover` 里的取回分支。

### 3.3 扣费入口：后台守门

会新建上游任务的只有三条路：`POST /tasks/:id/retry`、带 `metadata.retryOf` 的 `POST /tasks`、对着已有画布节点的 `POST /tasks`。后台统一检查：

1. 先找出**目标节点当前的任务**：任务表里「目标为这个画布、这个节点」的最近一个任务（任务输入里已有目标节点，见 `TargetBindingFromInput`，实施时加成可索引的列）。批次和多输出提交按 `outputNodeIds` 展开，每个目标节点都要查。不依赖前端或画布文档告诉后台节点以前生成过什么。
2. 带了 `retryOf`（或走 `/retry`）时，`retryOf` 必须**等于**目标节点当前的任务，且这个任务的目标节点就是本次的目标节点；不一致就拒绝（409，返回当前任务的 `outcome`）。这样旧快照指着失败任务 A、节点其实已经提交了任务 B 时，调用方没法拿 A 绕过 B 的检查。
3. 按第 3.1 节的表判当前任务的 `regenerate`：
   - `blocked` → 409，返回最新的 `outcome`，前端照着刷新显示；
   - `confirm` → 必须带有效的确认 ID（第 3.1 节），否则 409；
   - `allowed` → 放行。
4. 第 1–3 步的读取和核对，与新任务的接纳**放在同一个事务里**（现有 `retryTask` 已用 `withImmediateTransaction`，接纳新任务也要放进同一种写事务）。两个并发请求不会都看到「当前任务是 A」然后各建一个新任务。

另外三处随之调整：
- `/retry` 不再做回执重建和继续下载（已挪到 `/recover`），只做扣费重跑。旧客户端的「继续取回」也调 `/retry`，所以过渡期的处理见第四节，不能直接删。
- `RecoverMediaUpscale` 里 `not_sent → RetryTask` 的分支删掉，改为 `outcome.regenerate = allowed`，由用户走扣费入口。
- 工作流「重试失败步骤」（`canvas_workflow_run.go`）同样先过这道检查。

提示词面板、悬浮工具条、错误卡、批次子节点、任务中心、助手——不管哪个入口，过的都是同一道检查。

外部调用方（business tools）对 `confirm` 任务重跑同样需要确认 ID；这一点要写进 `openapi.yaml`。

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

桌面版把前端打包在后端里（`backend/cmd/desktop/frontend.go` 的 `go:embed all:frontend/dist`），同一个安装包里前后端版本一致。但仓库还支持前后端分开部署（`docker-compose.yml` 里 `web` 是单独的镜像），浏览器里也可能还开着升级前加载的旧页面，所以**必须假设新后端会收到旧前端的请求**。还要兼容的是升级前保存的画布文档里的旧字段，以及外部调用方。

旧前端的请求在新后端上怎么处理：
- 旧前端的「继续取回」调的是 `POST /tasks/:id/retry`（`use-canvas-resource-handoff.ts`）。新后端的 `/retry` 在任务处于 `fetching` / `retrievable`、或者成功但暂停下载时，**一律按 `/recover` 处理**（免费取回，不新建任务），永远不会把这个请求理解成扣费重跑。这个兼容分支一直保留，到确认不再有旧客户端为止。
- 旧前端发起的扣费重跑（`/retry`、`POST /tasks`）不带确认 ID。遇到 `confirm` 任务时后台拒绝，返回的提示文案让用户刷新页面或升级。被拒绝只会让用户多走一步，不会多扣钱。

| 步 | 改什么 | 用户能感觉到的 | 回退 |
| --- | --- | --- | --- |
| 1 | 后台：`Outcome` 纯函数 + 表格测试，挂到任务读接口上，只增字段 | 无 | 删字段 |
| 2 | 后台：取回循环自动恢复图片（第 3.2 节的完整扫描范围，含没有路由记录的历史回执）；`recovery_outcome` 列（新增可空列）；`/retry` 现有的回执重建分支**原样保留** | **客户 A 的节点自己好了**：后台重建 → 下载 → 写回画布，旧前端照常显示成片；手动点也还能恢复 | 关掉循环里这一步 |
| 3 | 后台：`POST /tasks/:id/recover`；视频/超分自动查上游（带退避）；`/retry` 加上面的兼容分支 | 视频失败后能自己取回 | 关闭自动查询，接口保留 |
| 4 | 后台：确认接口上线，扣费入口守门（第 3.3 节）**先只记日志、不拒绝**。前端只做最小改动：现有的扣费确认框（重新生成、提示词面板、任务中心、批次）在用户点确认后多调一次确认接口，把确认 ID 带进提交；其余前端保护不动。观察一个版本：日志里出现的每一次「本应拦下」都要能对上前端确实带了确认 ID | 无（确认框还是原来那个） | 关掉日志；前端多出的那一次调用可以保留，后台不守门时它不起作用 |
| 5 | 后台：守门改成拒绝；`RecoverMediaUpscale` 去掉扣费分支 | 可能重复扣费时，任何入口都要先确认；第 4 步起前端已经会带确认 ID，确认后能正常重新生成 | 见下 |
| 6 | 前端：所有判断改读 `taskOutcome`；「继续取回」调 `/recover`；删掉第 3.4 节列出的推断函数和 `resourceReloadAvailable` | 错误卡、批次子节点、任务中心按钮一致；不再出现节点锁死 | 回退到第 5 步的前端：它会申请并携带确认 ID，回退后重新生成照样可用 |
| 7 | 清理：`by-operation` 去掉画布过滤；2 分钟判定；前端不再分类任务错误 | 「查不到原任务」不再锁死 | 单独回退 |

顺序的约束：
- 只记日志的观察期（第 4 步）必须在移除任何前端保护（第 6 步）**之前**完成。第 5 步守门切到拒绝之后，就**不能**靠关掉守门回退：第 6 步之后前端已经不再自己拦，关掉守门等于放开未确认的付费提交。第 5 步之后如果守门误拦，只能修判定规则或发修复版本。
- 申请和携带确认 ID 的前端改动在第 4 步就上线，早于第 5 步开始拒绝，所以不会出现「后台要确认 ID、前端还不会给」的版本。第 4、5 步之间前后端各有一道保护，但用户只看到一个确认框：前端原有的确认框点了确认，就顺带申请确认 ID。
- 旧前端（Docker 分开部署、升级前打开的页面）不会申请确认 ID。第 5 步之后它在 `confirm` 任务上重新生成会被拒绝，提示刷新或升级；`allowed` 和 `ready` 的再次生成不受影响。
- 回退第 6 步时连同前端一起回退到第 5 步的版本，而不是只回退某几个文件。第 5 步的前端已经接好确认接口，回退后确认入口仍然可用。

如果 v2.0.4 只带第 1、2 步，`/retry` 的手动回执恢复仍在，历史任务的恢复能力不会少；自动扫描是在这之上加的。

旧画布文档：读取时忽略 `resourceReloadAvailable` 和前端写的 `generationErrorCode`，节点的成败以重新拉到的任务 `outcome` 为准；`canvas-document-rebase.ts` 的字段清单同步删掉。没有 `taskId` 的历史节点保持现状（显示「页面刷新后本地生成已中断」）。

## 五、拿真实问题验收

「为什么不会再发生」写的是结构上的原因；每条都要留一个回归测试，测试素材优先用 #16 已有的。

### 5.1 今天四轮检查查出的问题

| # | 问题（来源） | 新设计下 |
| --- | --- | --- |
| 1 | 客户端写的错误码触发自动扣费重试（#16 第 1 轮 P1） | 前端没有任何自动调用扣费接口的代码；自动取回在后台，只走 `/recover`，那里不引用重试逻辑 |
| 2 | 提示词面板绕过扣费确认（#16 第 1 轮 P1） | 守门在后台：对着已有节点的 `POST /tasks` 先找该节点最近一个任务的 `outcome`，需要确认时不带有效的一次性确认 ID 就拒绝；`retryOf` 指向的不是节点当前任务也拒绝；前端漏不漏弹框都扣不了钱 |
| 3 | 恢复锁冲突（400）、存储配额（403）被当成拿不回（#16 第 1 轮 P2） | 「拿不回」只来自持久化的 `recovery_outcome`（`no_result` / `unrecoverable`）；`/recover` 的任何报错都不改变 `phase`，前端也不根据 HTTP 状态做判断 |
| 4 | 读回执时数据库临时出错被当成拿不回，靠匹配文案判断（#16 第 2 轮） | 同上；读库出错时不写 `recovery_outcome`，下一轮循环再试。#16 后端加的 `result_unrecoverable` 判断点正好就是写 `recovery_outcome` 的位置 |
| 5 | 后到的画布快照重复认领同一节点（#263 第 1 轮） | 取回不在前端认领；后台按任务加租约（`ClaimFailedTaskProviderRecovery`），画布快照来几次都不会多触发 |
| 6 | 恢复响应丢失，被误判为取不回（#263 第 1 轮） | 判定只看后台事实，不看某一次请求的响应；响应丢了，前端订阅到的任务也已经是 `succeeded` |
| 7 | 自动取回和手动重新生成用的是两套判断（#263 第 1 轮） | 只有一个 `outcome`，所有入口读同一个字段 |
| 8 | 节点锁死：任务已终态仍按「提交未确认」锁住（客户 A，#15） | 每个终态都有出口：`retrievable` 后台自动取；`failed` 时 `regenerate` 为 `confirm` 或 `allowed`。只剩审核未通过这类 `blocked`，它只拦原样重跑，改了输入就能直接生成（第 3.1 节） |
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
- **外部调用方**：对 `confirm` 任务，外部程序无法再重跑，只能让用户到桌面界面里确认。这是有意收紧，business tools 的说明里要写清楚。
- **一次确认的有效期内**：用户确认后 10 分钟内，这一个提交标识可以重放，不会多建任务；但用户确认本身可能是误点（像 memoh 误点那次），本方案只能保证「一次确认最多一次付费」，不能保证确认不是误点。

## 六、#263 和 #16 里能直接用的

| 内容 | 处理 |
| --- | --- |
| #16 后端 `ResultUnrecoverable` 的几处判断（`image_result_recovery.go`、`image_submission.go`、`lifecycle.go`）和 `repository.ErrAmbiguousImageCreateReceipts` | **直接复用**，改成在这些位置写 `recovery_outcome`，替代返回 `reason` |
| #16 后端测试 `TestImageReceiptRecoveryReasonSeparatesUnrecoverableFromTemporaryFailure`、`TestRetryUnknownAcceptanceAndImageReceiptStayOnOriginalTask` 新增断言 | 改成断言 `Outcome`，进第 1、2 步 |
| #16 前端测试 `canvas-generation-failure`、`canvas-task-result-recovery`、`canvas-settled-retry-confirmation`、`canvas-late-generation-recovery.browser`、`canvas-uncertain-recovery.browser`、`generation-preflight` 夹具 | 场景全部保留作验收（第五节第 1–7、10 条）；断言从「前端判定函数返回什么」改成「给定后台 `outcome`，界面显示什么、请求发了几次」 |
| #16 错误卡的三种文案（正在自动取回 / 确认取不回可重新生成 / 未定只能查看）和三张截图 | 直接对应 `retrievable+auto`、`failed+confirm`、`running`，文案复用，不加新按钮 |
| #16 / #263 前端的 `canvasTaskProvesSubmissionUnknown`、`canvasNodeNeedsOriginalResultRecovery`、`recoverCanvasTaskResult`、恢复扫描里的自动取回分支、提示词面板付费确认分支 | 不合并，由后台取代 |
| #15 根因说明（`docs/review-evidence/canvas-uncertain-result-recovery/README.md`） | 作为背景材料保留 |

如果 v2.0.4 等不及后面几步，最小止血是只发第 1、2 步：只改后端，`/retry` 现有的手动回执恢复原样保留，客户 A 这类节点会被后台自动修好，前端一行不改。这需要先在本机用一份带完整回执的失败任务验证一次「后台重建 → 下载 → 写回画布 → 旧前端显示成片」这条链路。本方案没有跑过这条链路，这是推断。

## 七、待评审确认的点

第 2 轮评审（Codex）已对 1–6 条给出意见，方案已按意见改：1 同意确认后重新生成；2 同意按目标节点检查，批次检查每个目标；3 同意按渠道限流定退避；4 同意批量查询；5 不向外部调用方开放确认接口（第 3.1 节已改）；6 不允许改输入免确认（第 3.1 节已改）。下面保留原文备查。

1. `failed + confirm`（上游可能已扣费、但确认取不回）时允许用户确认后重新生成，这是沿用 #16 已定的做法；另一种做法是只给「联系支持」，不开放重新生成。
2. 守门按「目标为同一画布、同一节点的最近任务」找原任务。用户把节点复制成新节点再生成，就不算同一个节点，不会弹确认——这和现在「复制节点后点生成可能再次计费」的提示（`use-canvas-node-operations.ts`）一致，但要评审确认是否接受。另外批次子节点、多输出任务的目标节点要按 `outputNodeIds` 展开核对。
3. 视频/超分自动查上游的退避和总时长（方案里写的是 24 小时内 1、5、15、60 分钟）要按各渠道查询接口的限流来定。
4. 列表接口要多查回执是否存在、派发状态，需要批量查询，避免一条任务一次查询。
5. 确认接口（`regenerate-confirmations`）是否对 business tools 外部调用方开放。不开放更安全，但外部调用方就无法重跑 `confirm` 类任务。
6. 「改了输入就不受 `confirm` 约束」：用户只改一个字再生成，等于绕过了确认。另一种做法是只要节点当前任务是 `confirm`，任何新提交都要确认。前者照顾正常编辑，后者更保守。

## 八、上游 Infinite Canvas

`github.com/basketikun/infinite-canvas` 没有任务中心、异步任务和恢复层（#15 根因说明里的对照），这部分没有可照搬的做法，按 BeefTV 已有的任务、回执、取回循环来收拢。
