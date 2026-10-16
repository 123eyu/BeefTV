# 本地优先部署的安全护栏审计

审计基线是 `main` 上的 #171（`63c648d`，默认 `CANVAS_ALLOW_PRIVATE_UPSTREAMS=true`）。结论先说：多租户 SSRF 那一层“默认拒绝回环和 RFC1918”已经拿掉，剩下的限制大多不是重复的私网黑名单。真正还挡局域网模型的，是路径白名单、强制 API Key、HTTPS-only，以及几处只认 `127.0.0.1` 的产品边界。云元数据地址和 URL 凭据应继续拒绝。

本文件是给维护者做取舍的审计，不是面向用户的产品说明。

## 1. #171 之后，出站策略实际还拦什么

实现在 `backend/internal/outbound/outbound.go`。`CANVAS_ALLOW_PRIVATE_UPSTREAMS` 未设置，或不是 `0` / `false` / `no` 时，私网上游为开。`docker-compose.yml`、`docker-compose.local.yml`、`docker-compose.dev.yml` 都把默认值写成 `true`，与代码一致。`AGENTS.md` 第 5 节也已经改成同一口径。

默认放行（解析后的地址）：

| 范围 | 例子 | 说明 |
| --- | --- | --- |
| 回环 | `127.0.0.1`、`localhost`、`::1` | Ollama `11434`、本机 vLLM `8000` |
| RFC1918 | `10/8`、`172.16/12`、`192.168/16` | 局域网推理机 |
| IPv6 ULA | `fc00::/7` | 含 Tailscale IPv6 `fd7a:115c:a1e0::/48` |
| CGNAT | `100.64.0.0/10` | Go 的 `IP.IsPrivate()` 不含这段；默认开启时自定义中转的额外前缀检查被跳过，所以 Tailscale IPv4 能通 |

默认仍然拒绝，而且 **`CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS` 也不能放行**（本分支补上了允许列表绕过）：

| 检查 | 拦住什么 | 为什么还要留 |
| --- | --- | --- |
| 链路本地单播 | `169.254.0.0/16`、`fe80::/10`，包括 `169.254.169.254` | 云元数据、链路本地服务。模型服务用不到 |
| 未指定地址 | `0.0.0.0`、`::` | 不是可用上游 |
| 组播 / 链路本地组播 | `224.0.0.0/4` 等 | 不是 HTTP 模型服务 |
| URL userinfo | `https://user:pass@host` | 凭据进日志、Referer、错误文本 |
| 非 http(s) | `file:`、`gopher:`、`dict:` | 经典 SSRF 方案 |
| 自定义渠道片段 | `#fragment` | 无协议意义，容易藏数据 |
| 自定义渠道重定向 | 任意 3xx | 防止先打公网再跳到元数据；也避免把 Authorization 跟去别的源站 |
| 查询参数里的密钥名 | `key`、`api_key`、`access_token`、`token` | 密钥进 URL。Gemini 密钥由中转改写到 `x-goog-api-key` 头 |
| 自定义 Header 黑名单 | `Authorization`、`Cookie`、`Host`、`X-Forwarded-*`、`X-Canvas-*`、`x-goog-api-key` | 防止浏览器覆盖鉴权、伪造转发链。Gemini/Claude 的密钥由服务端按格式注入 |

`CANVAS_ALLOW_PRIVATE_UPSTREAMS=false` 时额外生效：

- 回环、RFC1918、`localhost` 全部拒绝，除非主机名精确出现在 `CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS`。
- 自定义渠道的 HTTP（明文）只允许该白名单主机；公网 HTTP 渠道被拒。
- 自定义渠道再拒绝一组保留地址：`0.0.0.0/8`、`100.64.0.0/10`、`192.0.0.0/24`、TEST-NET、`198.18.0.0/15`（Clash/Surge fake-ip）、`240.0.0.0/4`、`100::/64`、`2001:db8::/32`。普通出站（非自定义中转）在严格模式下 **不** 套用这组前缀，CGNAT 仍可能被当成公网。这是严格模式的不一致，不是默认路径的问题。

拨号只连接已经过策略检查的 IP，不再把主机名交给 `net.Dialer` 重解析，用来挡 DNS rebinding。这层应保留。

## 2. 清单

严重度是“从默认的本机 + 局域网部署里拿掉之后”的风险，不是 CVSS。云上多租户把同一进程暴露给不可信调用方时，应把 `CANVAS_ALLOW_PRIVATE_UPSTREAMS=false` 当作总开关，而不是把默认再改回拒绝私网。

| 位置 | 拦住什么 | 针对的威胁 | 默认拿掉的严重度 | 建议 |
| --- | --- | --- | --- | --- |
| `outbound.resolveOutboundHostWithPolicy` 链路本地 / 未指定 / 组播 | 元数据、`0.0.0.0`、组播 | 云 SSRF 读 IAM；无意义拨号 | 高 | **保留**。允许列表也不放行 |
| URL userinfo、非 http(s) | 凭据和怪异 scheme | 凭据泄漏、`file`/`gopher` | 高 | **保留** |
| 自定义中转禁止重定向 | 3xx | 跳到元数据或把密钥带到别的源站 | 高 | **保留** |
| 自定义中转路径白名单 `authorizeCustomRelay` | 只允许 OpenAI/Gemini/Claude 及列出的视频查询路径 | 把 `/api/ai/custom` 变成通用代理 | 中高 | **保留**。见下文：这才是 Comfy 原生 API 走不通的原因 |
| 自定义 Header / 查询密钥名 | 覆盖鉴权、密钥进 URL | 请求走私、日志泄密 | 中 | **保留** |
| `CANVAS_ALLOW_PRIVATE_UPSTREAMS=false` 及主机白名单 | 回环和 RFC1918 | 多租户或暴露在公网的实例被用来打内网 | 对默认桌面是低（功能回归）；对公网实例是高 | **严格部署才打开**。默认保持 true |
| 严格模式下自定义渠道拒绝明文 HTTP | 公网 `http://` | 密钥明文出网 | 低（本地服务需要 HTTP） | **保留为严格模式**。默认已允许 HTTP |
| 严格模式额外保留地址（含 `198.18/15`、`100.64/10`） | fake-ip、CGNAT、文档网段 | 缩小严格模式的可达面 | 低，但会误伤 Clash fake-ip 和 Tailscale | **严格模式保留**；不要在默认 true 时启用。fake-ip 误伤记为 P2 |
| `referencestorage.Config.Validate` 强制 HTTPS、无 query | 明文对象存储、带 token 的 URL | 参考图 URL 会交给云端模型拉取；token 进 URL | 中（云拉取）；对纯局域网 MinIO 是功能限制 | **保留 HTTPS 与无凭据**。私网 HTTPS 在默认策略下已经允许。明文局域网 MinIO 见 P1，不要默默放开公网 HTTP |
| `trustedFullVideoAssetSource` | 素材 URL 必须与 API 同主机，或用户显式填写的 HTTPS 源站（端口只能 443） | 全参视频回执把素材指到攻击者服务器；密钥不发往该源站，但后端会去拉 | 中 | **保留同源和显式源站**。局域网非 443 源站见 P1 |
| `generation/http.go` 离开源站即剥掉 Authorization 和渠道头 | 重定向后的签名 URL | 渠道密钥跟到 CDN | 高 | **保留** |
| 结果下载仍走 `ValidateOutboundURL` | 与出站同一策略 | 模型回执里的 URL 是第二跳 SSRF | 高（若改回“只许公网”则功能坏；若连元数据也不查则危险） | **保留当前策略**，不要退回“只许公网” |
| 桌面 `ProfileDesktop` 只监听 `127.0.0.1`，且要启动令牌 | 局域网直接打桌面 API | 本机 WebView 以外的调用方 | 低（桌面本来就该只听本机） | **保留** |
| 服务端 profile 监听 `:8080` 且没有启动令牌 | 什么都不拦 | 这是缺口，不是过度限制 | 把 API 暴露到 LAN/公网时高 | **不要再削出站来补偿**。P0 是给对外服务端补监听地址或共享令牌，而不是放宽 SSRF |
| `cmd/beeftv` CLI 只接受回环 `BEEFTV_BASE_URL` | 连局域网另一台 BeefTV | 首版把 owner token 只发给本机 | 低到中 | **先保留**。服务端还没有等价于桌面令牌的认证时，放开 CLI 等于把工作区凭据发到任意主机 |
| Eagle `validateBaseURL` | 只许 `http://127.0.0.1\|localhost\|::1:41595`，禁止重定向 | Eagle 本地 API 无鉴权，不能变成通用代理 | 低 | **保留**。Eagle 是本机应用，不是局域网服务 |
| 浏览器自定义渠道 `requireHttpUrl` | 只查 http(s) 和主机名非空 | 不重复私网策略 | 无 | **保留为格式校验**。私网判断只放后端 |
| `serviceConnectionError` 与多数插件 `apiKey.required: true` | 没有密钥不能保存渠道 | 防空密钥打到计费 API | 对 Ollama 是功能限制，不是安全收益 | **软化**，见 P1。运行时 `ApplyProtocolAuth` 对 `auth.type=none` 已经不发送密钥 |
| `/api/ai/custom` 空 API Key 直接 401 | 无密钥的 OpenAI 兼容本地服务 | 防止匿名中转 | 低到中 | **P1 可选**：仅当目标已判定为回环/私网且用户显式勾选“无密钥”时放行。公网仍要密钥 |
| 服务端 CORS | 默认同源反射；`CANVAS_CORS_ORIGINS` 为空时额外允许 localhost；Compose 默认 `http://localhost:3000` | 带凭证的跨站调用 | 低。经 Nginx 用局域网 IP 打开同一站点时，Host 与 Origin 相同，已经放行 | **保留白名单**。不要改成反射任意私网 Origin（那是局域网 CSRF） |
| 桌面 CORS 反射任意 Origin | 桌面 WebView 的 `wails:` / `wails.localhost` | 由启动令牌兜底，不是浏览器站点 | 低 | **保留**。没有启动令牌的服务端不要套用这段 |
| 频控 `DefaultRuntimePolicy` 30 次/分钟等 | 托管配额 | 多租户刷接口 | 桌面/当前服务端都走 `NewLocal` → `selfUseRuntimePolicy`（上限 999999） | **保留代码路径**。不要在 local mode 启用托管配额 |
| `CANVAS_REGISTRATION_ENABLED` | 无。Go 不读取。桌面与服务端都只注册 `RegisterDesktopCanvasAPI`，没有 `/api/register` | 历史托管注册开关 | 无 | **文档与 Compose 里标成无效**，或删掉。不是安全开关 |
| `CANVAS_LOCAL_MODE`（Compose） | 无。Go 不读取。`bootstrap` 对 server 和 desktop 都 `app.NewLocal` | 误以为能切回托管模式 | 无 | **标成无效**。前端对应的是构建期 `VITE_CANVAS_LOCAL_MODE` |
| 链接里的 `apiKey` 查询参数被丢弃并警告 | 从 URL 导入密钥 | 密钥进历史、Referer、日志 | 中 | **保留**。只警告，不阻止导入 `baseUrl` |
| 技能/用户内容的 CSP `sandbox`、`default-src 'none'` | 渲染不可信 HTML | XSS | 中 | **保留**。站点本身没有全局 CSP，这里不是过度限制 |
| Nginx `Referrer-Policy: no-referrer` | 不发送 Referer | 减少 URL 泄漏 | 低 | **保留**。不挡局域网 |
| 前端 `resourceIdFromTrustedLocalFileUrl` 只认回环绝对 URL | 不把局域网绝对 URL 当成自己的资源 ID | 防止把外站 `/resources/:id/file` 误认成工作区资源 | 低 | **保留**。资源规范地址是相对路径 `/api/resources/:id/file` |
| BeefAPI `CanonicalOrigin` 只许官方源或 loopback 测试源 | 企业登录回跳 | 授权码被送到假源站 | 高 | **保留**。这是 OAuth 源站绑定，不是模型上游 |

## 3. 还值得留的，以及可以当噪音的

值得留：

1. 链路本地 / 未指定 / 组播，包括允许列表和默认私网开关都绕不过 `169.254.169.254`。
2. URL 里的 userinfo，以及自定义渠道查询串里的 `key` / `token`。
3. 自定义中转不跟随重定向；视频下载离开源站就丢掉 Authorization。
4. 自定义中转的路径白名单和 Header 黑名单。服务端 profile 没有桌面启动令牌，`:8080` 对到达该端口的人是开放的，这条中转不能再变成任意 URL 代理。
5. 严格模式开关本身。公网或多人实例应设 `CANVAS_ALLOW_PRIVATE_UPSTREAMS=false`，再用主机名白名单点名放行。

已经不是问题（#171 之后）：

- 本机 Ollama、vLLM、LocalAI、LiteLLM、局域网 RFC1918、Tailscale `100.64/10` 的 HTTP 模型地址。协议插件走 `ValidateOutboundURL`，不再被“拒绝私网”挡住。
- 前端没有第二套私网黑名单。`channelRequest` 只检查 scheme。

仍然像噪音、或只在严格/特定产品路径上碍事：

- `SECURITY.md` / `AGENTS.md` 仍写“公开注册默认关闭”。注册路由已经不存在，`CANVAS_REGISTRATION_ENABLED` 没有任何 Go 读取点。开关是空的。
- Compose 的 `CANVAS_LOCAL_MODE` 同样没有 Go 读取点。服务端进程已经是 local mode。
- 参考素材文档原先写“拒绝私网地址”。代码只是强制 HTTPS，私网是否拒绝取决于上面的出站开关。本分支已改文档。
- `video-model-contracts.mdx` 和 `backend-database.mdx` 仍写“公网地址检查”。本分支已改成与 #171 一致。
- Ollama 插件 `auth.type` 是 `none`，配置字段却 `required: true`，设置页 `serviceConnectionError` 对所有渠道都要 API Key。运行时不会发送这个密钥，但用户不填就无法保存。这是表单限制，不是 SSRF。
- 本机 ComfyUI 的 `/prompt`、`/history` 不在自定义中转白名单里，也没有对应协议插件。`runninghub-workflow` 的默认 Base URL 是 `http://127.0.0.1:8188`，路径却是 RunningHub 的 `/task/openapi/*`，鉴权是 bearer。#171 解决了端口可达，没有因此接上原生 Comfy API。
- 全参视频的跨源素材地址只接受 443 端口的 HTTPS。同源 HTTP（API 自己也是 HTTP）已经允许。
- Eagle 锁死本机 41595。对这款本机软件是匹配的，不是误伤局域网模型。

## 4. 建议的改动顺序

### P0

1. **已在本分支做：** 允许列表不再放行链路本地、未指定和组播。此前 `CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS=169.254.169.254` 可以打到云元数据。回环和 RFC1918 的白名单行为不变。
2. **不要在这次做，但应单独立项：** `cmd/server` 默认 `:8080` 且 `launchToken` 为空，API 没有桌面那层令牌。出站私网已经默认开放，于是任何能访问到该端口的人都可以让进程去打局域网模型，并读取工作区。桌面形态没有这个问题。建议默认只绑 `127.0.0.1`，或增加与桌面令牌同类的 `CANVAS_SERVER_TOKEN`，由反向代理在本机注入。这是收紧暴露面，不是放宽 SSRF。

### P1

1. **Ollama / 无密钥本地服务。** 精确改动：`plugin-packages/ollama-chat` 的 `configuration.fields[].required` 改为 false，并与文档块同步；`web/src/lib/model-service-presets.ts` 的 `serviceConnectionError` 在协议鉴权为 `none` 时跳过“请填写 API Key”。不要对所有渠道取消密钥检查。`/api/ai/custom` 仍要求密钥，因为那条是浏览器通用中转；Ollama 原生 `/api/chat` 走协议插件，不走这条白名单。
2. **参考素材明文局域网端点。** 仅当 `allowPrivateUpstreams()` 为真，且 `ValidateOutboundURL` 解析结果全部是回环或 `IsPrivate()` 或 `100.64.0.0/10` 时，允许 `http`。公网主机、链路本地、带 userinfo 或 query 的地址维持现状。严格模式保持只许 HTTPS。云端模型仍然拉不到局域网 URL，文案要写明。
3. **全参视频素材源。** `trustedFullVideoAssetSource` 在私网开关打开、且源站主机通过同一出站策略、用户已填写 `referenceAssetOrigin` 时，允许 `http` 和非 443 端口。未填写源站时保持“必须与 API 同主机”。不要接受回执里任意局域网 URL。

### P2

1. 从 Compose 删除或注释 `CANVAS_REGISTRATION_ENABLED`、`CANVAS_LOCAL_MODE`，并改 `SECURITY.md` 里“公开注册默认关闭”这句话，避免把空开关当成控制项。
2. 严格模式下，把 `198.18.0.0/15` 从“无条件拒绝”改成“允许列表可放行”，避免 Clash fake-ip 在公网隔离部署里被误伤。默认 true 时这段本来就不会拦。
3. 严格模式的普通出站与自定义中转对齐 CGNAT：要么都拒绝 `100.64/10`，要么都允许白名单放行。现在只有自定义中转在严格模式下拒绝它。
4. 出站错误文案“请联系管理员把主机加入允许列表”只适用于 `CANVAS_ALLOW_PRIVATE_UPSTREAMS=false`。链路本地那句已经改成明确说允许列表无效。

明确不做：

- 不把默认改回拒绝私网。
- 不去掉路径白名单，也不让 `/api/ai/custom` 转发任意路径。要接原生 Comfy，应新增协议插件，而不是打开通用代理。
- 不让 CORS 反射任意局域网 Origin。
- 不放宽 BeefAPI 企业源站、Eagle 端口、桌面只听回环、URL 凭据拒绝。

## 5. 与 #171 之后的 main 不一致的文档和 Compose

| 位置 | #171 之后的事实 | 本分支 |
| --- | --- | --- |
| `AGENTS.md` | 已写成默认开放回环和私网 | 未改，已经对齐 |
| `docker-compose*.yml` 的 `CANVAS_ALLOW_PRIVATE_UPSTREAMS` | 默认 `true` | 未改，已经对齐 |
| `docs/content/docs/backend/video-model-contracts.mdx` | 仍写“公网地址校验 / 公网地址检查” | 已改成统一出站策略 |
| `docs/content/docs/backend/backend-database.mdx` | 仍写“公网/SSRF 校验” | 已改 |
| `docs/storage-settings.md` | 仍写“拒绝私网地址” | 已改：默认允许私网 HTTPS，端点仍必须是 HTTPS |
| `SECURITY.md`、Compose 的 `CANVAS_REGISTRATION_ENABLED` | 注册开关没有实现 | 只记录，未删，避免把空环境变量当成行为变更 |
| Compose 的 `CANVAS_LOCAL_MODE` | Go 不读取 | 只记录 |

## 6. 本分支实际改了的代码

`resolveOutboundHostWithPolicy` 对链路本地、未指定和组播一律拒绝，不再因为主机名出现在 `CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS` 而跳过。`127.0.0.1` 白名单测试保持通过。新增用例覆盖 `169.254.169.254` 在允许列表和默认私网开关下都被拒绝。错误分类对这句提示不再建议“加入允许列表”。
