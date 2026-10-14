# H3 生成、Styles 与 Director 接入记录

本轮范围是 H3、Max、Turbo 的全部生成能力、五种 Styles 和实时 Director；LoRA 训练不在范围内。当前代码尚未发布，不能称为已完整接入。

真实验证总预算为人民币 150 元，包含此前保守预留的 19.241966 元。五种 Styles 新增预留 24.41304 元，累计预留 43.655006 元，剩余预算 106.344994 元。这些金额来自公开价格的保守估算，供应商实际账单仍未确认；本轮五条调用没有扣除 BeefAPI 生产钱包余额。

## 本地实现

- 三款普通模型与五种 Styles 使用精确模型名和独立接口。
- 随机种子 0、关闭安全检查、同步视频数据、提示词增强均可传递；默认增强为 balanced，fast 仅限基础 H3。
- Max/Turbo 的 0.92–15 小数秒贯穿目录读取、画布配置、Go/Web 请求与报价。新增 fractionalMin/continuous 字段，原有 min/step 保持整数，已安装客户端仍可解析目录。
- Max/Turbo 可以调整跨图片、视频、音频的参考顺序。指定关键帧后，该图片从参考列表移除。Max 支持参考素材与关键帧并用，以及 480P/768P 下同时指定首尾帧和片内 24fps 中间帧时间。
- 五种 Styles 支持一张可选首帧、种子、5–15 整数秒，固定输出 768p；VHS 另有磁带磨损设置。普通模型的专属参数不会发送到 Styles。
- 报价分别显示普通生成秒价与参考生成账单规则；基础 H3 标明前五张参考图免费。

BeefAPI 对应实现会先验证账单权限，再开放 Max/Turbo 参考生成，按实际美元账单、冻结汇率和 50% 加价结算。账单 Key 必须属于推理 Key 所在的同一 fal 工作区。当前推理 Key 属于 Lucas，Ender 无法登录该工作区；用户已授权单独创建只读 BILLING Key，但尚未创建或配置。不要在群聊、代码或证据中包含密钥。

## 已取得的真实证据

hand-drawn、16bit-pixel、low-poly、retro-toon-70s、vhs 各一次原始请求均返回有效 MP4：H.264、1344×768、24fps、AAC 音频，种子为 0。请求 5 秒，容器时长为 5.184 秒；容器封装时长不能替代供应商计费时长。

这五份原始结果通过本地后端资源保存、画布绑定、SQLite 关闭重开、文件逐字节校验和幂等恢复。测试以 FAL_H3_REAL_MEDIA_CASES 显式选择五个案例，原有默认案例不变。以上证明供应商生成与本地交付，尚未证明部署后网关钱包结算及正式安装客户端路径。

## Director 尚未完成

已准备钉选官方 @fal-ai/client@1.11.0-alpha.5 WMA SDK、音视频接收、匹配配置确认、递增提示词版本、脚本、配音 replace/queue、replan、终态停止和受限录制。尚无画布入口、服务端代理、钱包与会话管理，不能宣传为可用功能。

公开 /session 返回 session_id，但没有说明它与 billing-events 的 request_id 如何关联。公开 HTTP 只有 /ice、/session、/session/heartbeat；浏览器 SDK close 只关闭本地连接。拒绝心跳后的租约过期时间未公开，也不是立即停止。AsyncAPI 的 max_session_seconds 默认为 null，宣传中的两分钟不能作为强制上限。

启用前必须确认账单关联和完整性，并实现服务端持有的 WebRTC 连接，或取得官方服务端停止/强制限额接口；然后完成钱包、画布录制保存和预算内真实验收。

官方依据：[Max 参考生成 schema](https://fal.ai/api/openapi/queue/openapi.json?endpoint_id=minimax/h3-max/reference-to-video)、[Director AsyncAPI](https://fal.ai/api/apps/fal-ai/minimax-h3-max-director/asyncapi.json)。对应验收矩阵在 BeefAPI 的 docs/runbooks/fal-h3-capability-matrix.json。
