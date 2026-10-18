# 已确认生成后的制作计划接续

本地候选实现，基于 `e9520b4cf38c309512efa4103107fbd3ee7afd6e`。没有运行真实模型或媒体生成、打开浏览器、修改用户工作区、部署或推送。真实作品验收由集成任务另行进行。

## 产品入口与边界

画布内置助手有未完成制作计划时，点击其图片或视频提议的生成按钮，即沿原生成器提交本次已确认内容，并登记完成后继续原计划的授权。活动画布页面观察原任务；全部任务终态且成功媒体已落地后发起一轮标为「自动继续」的助手消息，只附本次真实结果节点。部分失败进入检查和解释，不自动补单。下一批收费生成仍提出新方案等待人工确认。

侧栏显示等待/继续状态及「停止自动继续」。关闭侧栏不卸载画布观察器；离开画布页面不会新触发模型轮次。重新进入原画布时读取原会话和 Go 回执。已经提交的模型轮次仍按现有 host 执行/恢复，不因关闭页面丢失；这不是关闭页面后持续编排新轮次的后台服务。

接续不读取、发送、拼接或清空未发送草稿、附件、技能选择、临时画布选择。手动检查结果入口保留。检查、剪辑和本地渲染无需再次提交媒体生成，但助手推理和媒体理解仍可能产生模型费用，不能宣传整轮免费。

## 单一事实与执行链

1. `use-canvas-assistant-proposal.ts` 在现有确认快照验证后调用 Go arm。Go 从原业务回合核实所有者、同画布/会话、未撤销、已结算和权限，并固定当前技能 scope/revision。仅计划未完成的确认会登记接续。
2. 既有 image/video executor 决定真实输出节点后、任何付费提交前登记不可缩小的 manifest。原生多输出是一任务多个目标；拆单逐项冻结 operationId/输出集合。登记失败不启动本次生成。
3. Go 只按 owner+canvas+exact operationId 找回任务，复核 task type、原 sourceNodeId、原 outputNodeIds。缺提交回执不等于失败，也不授权重发。List 保存已观察 task IDs，前端复用任务中心的共享订阅，无新任务轮询器。
4. claim 等待所有冻结目标终态。成功结果必须匹配服务端 canonical generation output 的 index、resourceId、target node、媒体类型和 owned READY resource，并已真实保存到画布；取回中、缺节点、输出替换、未知终态保持等待或人工检查。
5. Go 同事务认领授权和创建轮前快照，固定 requestId/turnId，重复 claim 返回同一身份。`/assistant/chat` 对接续从服务端覆盖消息、结果引用和原权限，清空该内部请求的附件/skills/reference；从不采用客户端自报的业务回合。沿原 pi-durable 1.2 submit/observer/history 路径执行。
6. host 既有 per-canvas lock 内恢复/确认 current session，再在官方 submit 前回 Go 验证回执。迟到旧请求不能安装旧 session；请求重放用同一 request/turn pair。已提交输入恢复仍沿官方 durable 队列，没有另建模型 loop。

## 取消与失败

- Stop 先永久取消 Go 授权，再对该回执的固定 session/request 使用现有 targeted Stop；不会取消原收费媒体任务或不相干的新用户回合。已到供应商的在途调用仍遵循现有停止边界。
- 新用户消息 admission、显式新建/切换会话取消尚未运行的授权；已在执行的用户补充沿既有 steer 行为。新 scope 或技能 revision 改变使同会话接续失效。成功 Undo 在恢复画布的同事务取消源回合授权，Undo 拒绝则保留授权。
- 每次 runtime/操作授权再验证原 scope、未撤销和未取消。Go 明确取消/范围改变时 host 将尚未提交的记录结算为停止，避免残留 active 锁住会话；数据库/网络等未知错误不当作取消成功。
- Go claim 后响应丢失可回读同一身份；官方提交后丢响应按既有 history 恢复。前端不会把自动消息暴露为「新用户请求重试」。手动重新读取只重新核对原回执。
- 如果 manifest 已登记但没有可靠任务回执，或只提交了部分原计划，授权保持未知，不自动补齐收费提交。用户可停止并请求新的明确方案。旧批次没有回执时仍走手动检查。

## 验证

Go SQLite fixture：单图/视频、原生多输出/拆单、缺任务、删除原目标、未落地/媒体被换、部分失败、固定身份重开、Stop/新消息/new_scope/clear/Undo、跨用户/会话；迁移 v17→v18保留历史并拒绝缺字段。

官方 pi-durable Node fixture：重复观察与重启不重复模型请求、旧会话恢复/提交拒绝、Go取消与scope失效、暂态校验失败保留原身份、submit前Stop。所有模型响应由本机受控 HTTP fixture 提供。

前端单元/真实执行器边界：空闲/同画布会话检查、claim期间卸载/换账号/停止/用户新消息、固定真实结果引用且不读草稿、登记失败不开始任务或写占位、既有生成/恢复回归。React 页面实际挂载与产品导航、真实模型计划/剪辑能力、真实收费生成和导出仍需集成验收，单元 fixture 不替代这些结论。

### 本地验证回执

以下命令在对应目录执行，均为本地 SQLite/受控 HTTP fixture，不访问模型供应商：

| 目录 | 命令 | 结果 |
| --- | --- | --- |
| `web` | `bun test test/canvas-assistant-continuation.test.ts test/canvas-image-save-barrier.test.ts test/canvas-assistant-proposal-execution.test.ts test/assistant-history-recovery.test.ts` | 56 pass，207 assertions |
| `web` | `bun run typecheck` | exit 0 |
| `agent-host` | `bun test durable-continuation.test.mjs durable-session-owner.test.mjs system-prompt.test.mjs` | 6 pass，30 assertions |
| `agent-host` | `bun test durable-continuation.test.mjs` | 最后新增取消恢复边界后再跑：1 wrapper pass，内部 6 个官方 Durable Node cases |
| `backend` | `go test ./internal/database ./internal/assistantturns ./internal/handler -run 'Test(Continuation\|V13Workspace\|V14\|V16\|Assistant\|RuntimeLifecycle\|SkillContextHTTP\|SkillContext)' -count=1` | 三包 pass |
| `backend` | `go test ./internal/assistantturns ./internal/handler -run 'Test(Continuation\|AssistantContinuation\|RuntimeLifecycle)' -count=1` | 最后修正暂态 admission 与取消回读后，两包 pass |
| `backend` | `go test ./internal/database -count=1` | 完整迁移包 pass |

最后的 HTTP fixture 还覆盖：客户端自报的更高权限、草稿与无关引用被服务端固定回执替换；首次 host 503 保留同一开放回合，重试不创建新回合；取消后的 runtime 回读允许宿主确认停止。原生三输出 fixture 使用真实 canonical task binding：所有 canonical output 的 binding 指向任务主节点，各输出按不可变 `input.metadata.outputNodeIds[index]` 分配。

本地 stdout 保存在 `/tmp/beeftv-auto-continuation-{web,go,final-admission,final-host,final-database,typecheck}.log`，不纳入源码。没有浏览器截图、真实模型质量、真实付费生成或完整作品导出的验证结论；这些由集成后的独立 review 和真实创作验收补齐。依赖复用隔离 worktree 外现有 `node_modules` 符号链接，未安装或修改依赖，符号链接不纳入提交。
