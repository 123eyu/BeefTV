---
half_life: 30d
archive_at: event:step-7-done
artifact_mode: delivery-doc
scope_type: roadmap
scope_name: BeefTV 重构 - 同一条规则只留一份
coverage: 修复集中区域（生成任务结论与取回、模型能力、错误分类、旧直连生成代码、助手、画布保存、后端分层）的步骤、顺序、每步交付与验收，以及大文件拆分时机
not_complete_for: 付费授权（报价、同意表、后台拒绝、外部程序付费开关）、画布性能、发布与更新器、插件生态、导演台与素材库新功能
verification_level: local
real_smoke_status: not_run
review_status: not_reviewed
reviewer: none
review_command: 未另派评审；第 0、1 步代码由 go test / bun test / bun run build 自查
review_notes: 方案基于 origin/main 285c7dec 的代码与 10 月提交记录，并对照 #84 原稿、#86 修订稿、统筹记录、助手方案 #38
review_owner: Sunny
review_due: event:step-2-start
execution_backend: direct
lead_agent: current
peer_agents: none
builder_agent: none
verifier_agent: none
verification_independence: self_checked
cwf_decision: not_needed
cwf_trigger_boundary: none
goal_handoff: skipped
acceptance_contract_status: owner-confirmed
memory_required: true
memory_space: default
acceptance_memory_id: 292b72ba-e32e-4c18-953b-ef5b27203328
memory_asserted_by: external:claude-code:refactor-plan-2026-10-28
memory_confirmed_by: human:Sunny
memory_intended_for: BeefTV 实施者与评审者
memory_validity: current
memory_valid_from: 2026-10-28
memory_review_due: event:step-4-done
---

# BeefTV 重构方案：同一条规则只留一份

本文替代 `refactor-plan-20261027.md` 的 #84 原稿（`05abab0f`）和 #86 修订稿（`bffc87f5`），两者都没有合进 main。生成任务的详细行为规则仍以[状态方案](generation-task-state.md)为准，助手部分以 [`docs/plans/assistant-refactor.md`](../plans/assistant-refactor.md)（#38）为准；本文只定顺序、每步交付和验收。用户于 2026-10-28 同意本方案。

## 一、为什么要做，以及上一轮为什么效果不理想

10 月 main 上有 246 个 `fix` 提交、52 个 `feat`。被修复提交改得最多的产品文件，正好是前后端各写一份同样规则的地方：

| 文件 | 行数 | 10 月修复次数 |
| --- | --- | --- |
| `web/src/lib/model-capabilities.ts` | 1509 | 16 |
| `backend/internal/generation/provider_error.go` | 1607 | 13 |
| `backend/internal/modelcatalog/capability.go` | 1244 | 13 |
| `agent-host/durable-session-owner.mjs` | 1221 | 13 |
| `web/src/lib/generation-error.ts` | 1180 | 12 |
| `web/src/pages/canvas/project.tsx` | 3556 | 12 |

具体重复：`isSeedance2Family` 有三份（前端、`modelcatalog`、`app`）；`generation-error.ts` 和 `provider_error.go` 有 89 个相同错误码，前端另有约 120 处按错误文字判断；前端 27 个文件自己推断任务能否重试、能否取回，没有一处读后台给的任务结论（`outcome`）。

上一轮在 Todos 上推进不顺，原因是做法：

- 产出以文档和取证为主。#86 把模型能力、错误分类都改成"等找到反例再说"，能开工的只剩生成任务第 3 步；#83 做了 16 项实验；画布性能做到第 6 轮采样，其中几个提交在修测量工具本身。
- 方案改了 4 轮，已合入的 `task/outcome.go` 照的是第 3 轮，和最新规则对不上。

## 二、做法约定

1. 每一步交一个能合并的代码 PR。说明写在 PR 描述里，不另写长文档。
2. 规则写成测试表。方案要改规则，就改测试表。
3. 删重复代码不需要先取证；会改变用户看到的行为时才先复现。
4. 加新路径的同一个 PR 里删掉旧路径；同一批文件同一时间只开一个 PR。
5. 新代码不再放进 `backend/internal/app`；不新增状态机、通用检查框架或内容过滤。

## 三、步骤

工作量是粗估，不含 review 和发版等待。

### 第 0 步：修 `outcome` 的三处问题（已完成）

- 审核失败（`content_policy_violation` / `content_moderation` / `SAFETY.INPUT.*`）由 `blocked` 改为 `confirm`，节点有出口；审核失败不代表没扣钱，所以仍要确认。
- 删除恒为 false 的 `ConfirmedUncharged`。
- "请求尚未提交"改由写入阶段的 `app` 层给出 `RequestNotSent`，`task/outcome.go` 不再比较中文显示文字。
- 长条件拆成 `provablyNotSubmitted` / `isInputRejectedCode`，补表格用例。

验收：`go test ./internal/task/ ./internal/taskdelivery/` 通过。改动前 `outcome` 没有任何前端或后台守门在读，所以这一步没有用户可见变化。

### 第 1 步：删前端旧的直连生成代码（已完成）

- 删除 `requestVideoGeneration` / `createVideoGenerationTask` / `pollVideoGenerationTask`、`requestGeneration` / `requestEdit`，以及只为它们存在的 `video-provider-*.ts`（8 个）、`video-transport.ts`、`video-response.ts`、`video-reference-roles.ts` 和 `image.ts` 内的私有辅助函数。
- `video.ts` 只保留仍在用的 `storeGeneratedVideo` 和类型。`requestImageQuestion`、`requestToolResponse`、`video-validation.ts` 保留。
- 测试：删除 `agnes-video.test.ts`，在其余 8 个文件里只删通过已删代码发请求的用例（共约 43 个）；同文件里测仍在用模块的用例保留。

验收：相关 9 个测试文件 63 个用例通过；前端全量测试和 `bun run build` 结果见本分支 PR 描述。

未做：被删用例里有一部分是在前端验证上游请求格式（例如 H3 关键帧、Seedance 参考素材角色）。产品实际走后端，这些格式要由后端测试保证；本步没有逐条核对后端是否已有对应用例，第 2 步做 Seedance/H3 时一并补。

### 第 2 步：模型能力只留后端一份（3–5 天，先 Seedance/Seedream）

- 删除 `app/modelcatalog_delegate.go` 里那份 `isSeedance2Family`，改调 `modelcatalog`。
- 前端删除按型号写死的规则（Seedance、Seedream 覆盖），改读后端 `capabilityConfig`。
- 后端缺的 `thinking` 补到后端；未保存的自定义配置也交给后端计算。桌面版和 Compose 部署都带后端，不存在后端不在线。音频暂不动。
- 10 月相关修复（时长下限、参考素材合计 12 个、Pro/Flash 识别、线路识别、#273 尺寸校验）各写成一条后端表格用例；补第 1 步留下的请求格式用例。
- 之后每个 PR 只换一个型号系列。

验收：后端表格测试通过；同一已保存型号，前端表单可选项与后端返回一致。

### 第 3 步：错误分类只留后端一份（3–5 天，可与第 2 步并行）

- 逐条列出前端按文字判断的规则，确认后端已给出对应 `reason`；没有就补到后端，同一个 PR 删除前端那条。
- 结束后前端只剩"错误原因 → 显示文字"一张表。

这是状态方案第 7 步"前端不再分类任务错误"，提前做，因为它是修复第二多的地方，且不依赖付费部分。

### 第 4 步：生成任务由后台取回，前端只读 `outcome`（1.5–2 周）

| 子步 | 内容 | 验收 |
| --- | --- | --- |
| 4a 后台补齐取回 | `/recover`、视频与超分自动查上游、同一操作编号只生成一次；复用既有租约、操作回执、交付器，取 #68 中正确部分 | #83 的 16 项夹具接真实入口；重启后不重复生成 |
| 4b 前端读 `outcome` | 按钮、确认框、节点锁定改看 `phase` / `regenerate` / `reason`；删除 `resourceReloadAvailable`、`shouldBlockManualRetry`、`generationSubmissionState` 推断和前端恢复扫描；现有付费确认框保留 | 一次完整操作：提交 → 上游成功 → 下载失败 → 后台取回 → 节点显示成片，含重启；双击重新生成只生成一次；审核失败的节点确认后可重新生成 |
| 4c 扣费原值 | 读 BeefAPI 结算原值，BeefAPI 渠道的 `regenerate` 按结论给出 | 状态方案 3.11 的六种映射 |

4b 不等 4c：4c 之前 BeefAPI 任务一律 `confirm`，只多问一次，不会多扣钱。

不在本方案：报价、同意表、后台拒绝提交、外部程序"允许发起付费生成"开关，另行立项。

### 第 5 步：助手，按 #38 执行

- 现在可开：#38 第 1 步（旧任务回执不覆盖新任务；实时显示与历史读回同一规则；方案明细折叠）和第 4 步（自动接续只由后台发起，删除前端认领）。
- #38 第 2 步（助手读后台任务结论）排在本方案第 4 步之后。
- #38 第 5 步（宿主瘦身、创作方法放 Skill）最后，和拆 `durable-session-owner.mjs` 一起做。
- PR #265 以 #38 为准，不整包合并。

### 第 6 步：画布保存只补一条测试（1–2 天）

现有共用保存入口保留。补一条测试：手动编辑、助手写入、后台写回成片同时发生，不丢改动、版本连续。测试能复现问题才修对应调用。PR #279（#55）先按原流程合入。

### 第 7 步：后端分层，跟着前面各步做

哪一步改到哪个 handler，就在同一个 PR 里把它从 `*app.Service` 换成 `localapp` 的窄接口；44 个文件不一次性迁移。多数迁完后，把 `handler` 加进 `bootstrap/dependency_boundary_test.go`。

### 顺序

第 0、1 步（已完成）和助手 #38 第 1 步 → 第 2、3 步并行 → 第 4 步 → 助手其余步骤与拆大文件。一个人约 5–7 周，两人并行约 3–4 周。

## 四、遇到过的问题能否解决

| 问题 | 例子 | 步骤 | 结果 |
| --- | --- | --- | --- |
| 节点锁死、取不回成片、"继续取回"闪烁 | #15、#16/#263、#193、#246、Runway/Luma/Grok 取不回 | 0、4 | 大部分能修。上游本身没出结果、过了保留期，只能确认后重新生成 |
| 恢复前又点生成、双击重新生成报错 | 状态方案 5.1 第 1、2、11 条 | 4 | 能修。外部程序绕过确认花钱，要等付费部分另行立项 |
| 审核失败被误判 | #255、5.1 第 13 条 | 0、3 | 能修 |
| Seedance/Seedream 参数反复修 | 时长下限、参考素材 12 个、Pro/Flash 识别；Issue #244 | 2 | 以后只改后端一处。#244 是否同一原因未查，不保证修好 |
| 错误提示不准 | #172、#178 VOD 错误码 | 3 | 能修 |
| 修到没人用的旧代码 | `video-provider-*` | 1 | 已修 |
| 助手：旧回执改坏新节点、回复消失或攒到最后、自动接续重复 | 10-17/18 二十多条修复、#36 评审 | 5 | 大部分能修。模型回答质量和提示词不在范围 |
| 画布丢改动、版本冲突 | 10-17 保存对账 | 6 | 测试能复现才修 |
| Mac 画布卡顿、闪烁、丢节点 | 10-26 会议第 5 项 | 不在范围 | 性能单独做；取回按钮来回切造成的闪烁会在第 4 步去掉 |
| 发布与更新器、Topaz 预扣、"拍不了照片"、助手缺 Flash | — | 不在范围 | 不是重构问题 |

## 五、什么时候拆大文件

拆分同时满足三条才做：最近常出问题或常冲突；拆出去的部分只负责一件事；拆完要传的参数变少。只因为行数多不拆。

| 文件 | 行数 / 10 月修复 | 时机 |
| --- | --- | --- |
| `model-capabilities.ts` | 1509 / 16 | 第 2 步自然变小 |
| `generation-error.ts` | 1180 / 12 | 第 3 步自然变小 |
| `provider_error.go` | 1607 / 13 | 第 3 步后它是唯一一份，仍难改再按渠道系列拆 |
| `project.tsx` | 3556 / 12 | 第 4 步后。12 次修复约 7 次与生成、取回有关，先删再拆。已抽出 40 多个 hook，页面仍有 67 个 `useCallback`；这次拆出去的部分要自己管自己的状态 |
| `canvas-node-prompt-panel.tsx` | 1161 / 10 | 第 2、4 步后 |
| `durable-session-owner.mjs` | 1221 / 13 | #38 第 5 步 |
| `use-config-store.ts` | 1280 / 9 | 第 2 步只移走模型能力部分 |
| `repository.go`、导演台两个文件、`project_mutate.go` | 1300–2300 / 0–3 | 不拆 |
| `assets/index.tsx` | 2116 / 7 | 不排期，下次修素材库时顺手拆那一块 |

## 六、与在途工作的交点

- PR #279（#55 绑定修复）仍开着，第 6 步前先合。
- PR #265 评审结论为需修改，助手部分以 #38 为准。
- `8fd8be25 fix(canvas): show blocked generation reasons in the node panel`（Mac 上 Todos 工作区，未合入）改了 `use-canvas-generation-executor.ts` 等第 4 步要动的文件。它管提交前检查，不冲突，可以先合，第 4 步时留意。
- #68 已暂停，第 4a 步取其中正确部分，不整包合入。

## 七、尚未验证

- 第 0、1 步只在本机跑了后端相关包测试和前端测试；`internal/app` 有 3 个视频恢复用例在未改动的 main 上同样失败（本机未装视频插件），与本次改动无关。
- 未运行客户端，未做真实生成。
- 修复次数按提交标题和改动路径统计，标题写法不规范的提交可能计错模块。

## 八、进度（2026-10-29，PR #290）

按 `origin/main` 与 PR 分支实测对比，不按提交说明计。

| 步骤 | 状态 | 实测 |
| --- | --- | --- |
| 第 0 步 outcome 三处问题 | 完成 | 审核失败不再锁死；`ConfirmedUncharged` 与中文阶段比较已删 |
| 第 1 步 删旧直连生成代码 | 完成 | 删 11 个文件约 2350 行 |
| 第 2 步 模型能力只留后端 | 后端完成，前端未完成 | `app` 里那份 Seedance 规则已删，后端有表格用例和能力计算接口；前端 Seedance 规则删除后导致参考素材上限变 0（CI 38 项失败），已恢复。三份变两份 |
| 第 3 步 错误分类只留后端 | 部分完成 | 后端任务失败改用后端类别；前端按文字判断 103 处减到 93 处，浏览器直连渠道和只存文字的批次项仍在前端判断 |
| 第 4a 步 后台取回 | 完成 | 视频、超分自动查询上游，不重新提交；`POST /tasks/:id/recover` |
| 第 4b 步 前端读 outcome | 部分完成 | 32 个前端文件读 `outcome`；旧推断字段从 27 个文件减到 12 个；新增的 `lib/task-outcome.ts` 对没有 outcome 的旧节点仍按错误码推断，属于过渡代码 |
| 第 4c 步 扣费原值 | 完成 | 读 BeefAPI 结算原值；结算列改由 v27 迁移添加，已验证 v2.0.5 库可升级 |
| 第 5 步 助手 #38 | 代码完成，未做桌面实测 | 自动接续只由后台发起；`durable-session-owner.mjs` 1221 → 858 行；两条分镜规则移入「剧本资产视频一条龙创作」技能 |
| 第 6 步 画布三方保存 | 完成 | 并发测试通过，未发现丢改动 |
| 第 7 步 后端分层 | 未开始 | 44 个 handler 文件仍直接用 `app.Service`；`internal/app` 反而增加约 580 行（主要是 `video_auto_recovery.go`） |
| 拆大文件 | 部分 | 提示词面板 1161 → 724 行；`project.tsx` 3556 → 3277 行 |

剩余工作按优先级：前端模型能力改读后端（需先让目录导入与配置加载路径拿到后端结果）；删 `task-outcome.ts` 的旧节点推断（旧节点补齐 outcome 后）；第 3 步剩余文字判断；第 7 步并把新增的取回代码移出 `internal/app`；`project.tsx` 按职责继续拆。
