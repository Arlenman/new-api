# api-notice 告警通知规则

new-api 的告警通知规则由 Root 管理员在上游渠道页面中配置。浏览器只调用 new-api 后端；规则判断、模板渲染和 api-notice 请求全部在后端执行。浏览器不会直接访问 api-notice，也不会接触 API Key。

## 服务配置

微信、飞书和未来 Provider 共用一份 api-notice 网关配置：

```env
API_NOTICE_BASE_URL=http://192.168.10.157:18080
API_NOTICE_API_KEY=
API_NOTICE_API_KEY_FILE=
```

实际部署时，`API_NOTICE_BASE_URL` 也可以只填写 `192.168.10.157:18080`；new-api 会补全 `http://`，并去除末尾斜杠后拼接接口路径。

接口如下：

- 健康检查：`GET /healthz`
- 就绪检查：`GET /readyz`
- Provider 目录：`GET /v1/providers`
- 通知发送：`POST /v1/notices`

当前 Provider：

- 微信：`weixin`
- 飞书：`feishu`

Provider 的目标、凭证和收件人由 api-notice 服务端固定管理。new-api 不保存微信 Token、飞书 AppSecret、chat ID、用户 ID 或平台 Webhook。

## API Key 与配置存储

new-api 支持两种后端 API Key 来源，优先级如下：

1. `API_NOTICE_API_KEY` 环境变量。
2. `API_NOTICE_API_KEY_FILE` 指定的文件。
3. Root 管理员在“通知配置”弹窗保存的加密配置。

页面保存的 API Key 使用 new-api 现有的服务端加密存储能力，数据库只保存加密值；读取接口只返回“已配置”、掩码和来源，不返回完整值。编辑时 API Key 留空表示保持原值；只有填写新值才更新。清除操作由后端显式处理。

如果要从页面保存 API Key，new-api 后端必须先配置 `CRYPTO_SECRET` 或 `SESSION_SECRET`。API Key 不得写入前端源码、浏览器 `localStorage`、普通日志、镜像层或 Git。

为了兼容早期已经保存的配置，代码仍可只读识别旧的 `API_NOTICE_HMAC_SECRET` / `API_NOTICE_HMAC_SECRET_FILE` 名称；它们只会被当作 Bearer API Key 使用，不会触发 HMAC 签名。新部署和文档只使用 `API_NOTICE_API_KEY` 名称。

## 权限与管理接口

以下接口统一经过 `RootAuth`，仅 Root 管理员可访问：

```text
GET    /api/alert-rules/
POST   /api/alert-rules/
PUT    /api/alert-rules/:id
DELETE /api/alert-rules/:id
GET    /api/alert-rules/providers
GET    /api/alert-rules/config
PUT    /api/alert-rules/config
POST   /api/alert-rules/preview
POST   /api/alert-rules/test-connection
POST   /api/alert-rules/test-send
```

请求体不能覆盖 api-notice 地址，也不能提交平台凭证、收件人 ID 或 Webhook。Provider 的 `ready` 状态和消息能力由 api-notice 的 Provider 目录动态提供；选择多个 Provider 时，消息格式必须被全部 Provider 支持。

## 后端鉴权与发送

new-api 后端向 api-notice 发送通知时使用原始 API Key：

```http
Authorization: Bearer <API_NOTICE_API_KEY>
Content-Type: application/json
```

API Key 不会被哈希、Base64 编码或放入其他请求头。new-api 不生成 Timestamp、Nonce、HMAC 签名，也不会发送 `X-Notice-Timestamp`、`X-Notice-Nonce` 或 `X-Notice-Signature`。

仅发送微信、仅发送飞书或同时发送时，只通过 `providers` 区分：

```json
{"providers":["weixin"]}
{"providers":["feishu"]}
{"providers":["weixin","feishu"]}
```

测试发送使用当前编辑表单的规则草稿和预览事件，由后端先执行与“预览消息”相同的模板渲染，再将同一份渲染结果发送到所选 Provider。尚未保存的阈值、消息模板和 Provider 选择也会用于本次测试，但不会写入规则。

调用者不能指定平台目标、消息接收人或平台凭证。消息支持 `text`、`markdown`、`card`、`table`，模板只允许预定义变量和安全的结构化字段，不执行 Shell、Agent、工具调用或任意模板函数。

## 超时、响应判断与重试

- Provider、健康和就绪探测超时：3 秒。
- `POST /v1/notices` 超时：35 秒。
- Base URL 去除末尾斜杠后再拼接 `/v1/notices`。
- 同一次告警重试保持相同 `idempotency_key`、Provider 列表和原始请求体。
- 发送成功必须同时满足 HTTP 2xx、`success == true`、结果包含请求中的每个平台，并且每个 `receipt.accepted == true`。
- `400`、`401`、`409`、`413`、`422` 不重试。
- `429` 遵循 `Retry-After`；`502`、`503`、`504` 和网络错误按异步投递策略退避重试。
- api-notice 内部已经有 Provider 重试，new-api 不进行快速重复轰炸。

主要错误含义：

- `401`：API Key 缺失或错误。
- `409`：相同幂等键对应了不同内容。
- `422`：消息格式不支持。
- `429`：触发限流。
- `502`：一个或多个 Provider 发送失败。
- `503`：服务未就绪。
- `504`：投递超时。

管理接口只返回 HTTP 状态、Provider、`accepted`、`attempts` 和脱敏错误摘要。普通日志不得记录 API Key、Authorization、消息正文、幂等键、目标 ID、Webhook 或完整 api-notice 响应。

## 规则状态机

当前触发类型为“上游渠道有效余额”，在渠道余额刷新成功并写入数据库后评估：

- `normal`：条件未满足。
- `pending`：条件已满足，但尚未达到连续满足次数。
- `active`：已达到连续满足次数并生成触发事件。
- `recovery`：活跃告警重新恢复正常；只有启用了恢复通知时发送一次恢复事件。

统计窗口限制连续匹配样本的跨度；冷却时间避免同一事件重复告警。待发送请求保存于现有 `SystemTask` 机制，每 15 秒处理一批，最多 100 条。密钥未配置时保留待发送事件，每分钟重新检查，不消耗投递次数。

## 排查与 Docker 验收

1. 在 new-api 后端运行环境确认 `API_NOTICE_BASE_URL`，不能由浏览器或规则请求体覆盖。
2. 确认 API Key 已通过环境变量、文件或页面加密配置提供；不要用 `cat`、`echo` 或日志输出 API Key。
3. 从 new-api 后端所在环境访问 `/healthz`、`/readyz` 和 `/v1/providers`。
4. 在通知配置弹窗点击“测试连接”，检查 health、ready、Provider ready 和“API Key 已配置”。
5. 如果 new-api 在 Docker 中运行，必须从 new-api 容器内部验证 `192.168.10.157:18080` 可达，宿主机可达不能代替容器内验证。
6. 遇到失败时只记录 HTTP 状态、Provider、`accepted`、`attempts` 和脱敏错误摘要。

真实微信、飞书和同时发送验收必须通过 new-api 后端的测试接口完成，不要绕过后端直接向 api-notice 发送请求。不得修改 api-notice、Hermes Agent 或其认证文件。
