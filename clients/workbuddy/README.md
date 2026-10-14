# WorkBuddy Client

版本 `0.2.0`，Hub 协议 `1`，依赖同级 `shared` runtime `0.1.0` 和 Python 3.11+。使用 Python 标准库；单独复制本目录时必须一起复制匹配的 shared，保持相邻目录结构。

Client 明确选择官方 CLI 会话恢复或官方持续本地助理 OpenAPI，只支持 `queue`。本机已正式登录的独立 CLI 会话已通过真实 MQ 自动领取、原生执行与报告、成果文件校验；它不保证继承 WorkBuddy 桌面身份、企业微信连接器或原生工具，原桌面配置根的默认恢复仍返回需要登录。manifest 的通用验证声明保持 `unverified`，实际结论限定为本机测试入口。[本机验证记录](../../docs/NATIVE-VALIDATION.md)

首个验收任务确认原生明确拒绝且未执行后，完成技能 `Read`、真实工作区 `Read/Write`、专属报告 helper `Bash` 的窄范围配置及绝对工作目录修正，再恢复原任务；原生主动报告 `accepted/running/succeeded` 并生成 SHA-256 核验一致的文件。第二个任务由 SSE 自动领取，默认 `resume` 同一既有 CLI，会话首次执行即发出三种原生回执并写入新版本成果，SHA-256 一致；未传工具或权限覆盖参数。

两个任务的执行标识不同，结果消息与原交接的关联正确；同一 Submission 和已保存原生 Report 的重复提交去重，没有新消息或重复执行，终态资源释放已核对。两个结果由正在运行的 Codex 入口通过官方桌面工具实际引导回原聊天。独立 Python proxy 尚无可用外部控制 socket，因此未实现独立边车无人值守自动返回，也未验收原桌面企业微信工具继承。

## 配置与运行

先注册用户选定的 endpoint，将对应 binding 设置为 `"driver": {"type": "external"}`，其中 `endpoint.driver` 为 `workbuddy`。CLI 的 `endpoint.session_id` 必须与 `native.session_id` 一致；OpenAPI 则必须为 `localassistant`，表示官方持续专用助理。Client 每次连接先核对绑定，再订阅和领取。导出私有配置：

```sh
agi-hub client-config --endpoint workbuddy --file /absolute/path/to/private/workbuddy/client.json
```

命令只导出 endpoint 凭据和空 `native`，不覆盖已有文件。按 [client.example.json](client.example.json) 填写所选传输；默认 Hub 为本机 `http://127.0.0.1:7331`，以导出配置为准。在仓库根目录运行：

```sh
python3 clients/workbuddy/main.py --config /absolute/path/to/private/workbuddy/client.json
```

Windows 使用 `py -3` 和该电脑的绝对路径。个人配置放在仓库外；不要并行启动同一 endpoint 的两个 Client。未完成目标会话的唤醒与回报验收前，不把 `idle_wake_verified` 设为 `true`。

固定的 `session_id` 标识长期会话，不是每个任务的 `run_id`。报告使用真实原生执行 ID；没有公开 ID 时使用通知中的任务专属 `hubrun-...` 并说明其 Hub 归属。同一任务保留同一执行标识，不能跨任务复用会话 ID。

### CLI 会话恢复

示例使用 `native.transport="cli"`。先在所选配置目录通过官方流程完成本人登录并创建会话；Client 只恢复已有 ID，不代替用户登录或创建会话。必填字段：

| 字段 | 要求 |
| --- | --- |
| `command` | 实际可执行文件的绝对路径；是一个字符串，不是整段命令或 argv 数组 |
| `cwd` | 本机已存在工作目录的绝对路径 |
| `config_dir` | 本机已存在 CLI 配置目录的绝对路径；Client 设置为 `CODEBUDDY_CONFIG_DIR` |
| `session_id` | 该 CLI 环境已有会话 ID，与 Hub 绑定完全一致 |

可选 `timeout_seconds` 默认 `120`，范围 `1` 至 `600` 秒。可选 `entry_script` 是本机现存普通 JavaScript 文件的绝对路径，扩展名为 `.js`、`.mjs`、`.cjs`，或文件名为 `codebuddy`。Windows 要求实际 `.exe`，拒绝 `.cmd` / `.bat`；可配置实际 `node.exe` 与官方 `entry_script`，例如 `command="C:/Program Files/nodejs/node.exe"` 和 `entry_script="C:/absolute/path/to/official/codebuddy.js"`。这种方式需要已安装 Node.js 与官方 CLI。

可选 `environment` 仅接受字符串值：`CODEBUDDY_FORCE_LITE_WB_BUNDLE`、`DISABLE_AUTOUPDATER` 各为 `"0"` 或 `"1"`；`CODEBUDDY_INTERNET_ENVIRONMENT` 为 `"internal"`、`"external"`、`"ioa"`、`"cloudhosted"` 或 `"selfhosted"`。示例的 `internal` 应匹配本人实际登录站点，bundle 标志匹配实际安装入口；它们不代表新增桌面工具。不能传入凭据、IPC 参数或任意环境变量。配置目录隔离不等于凭据存储隔离，官方 CLI 可能使用产品自己的共享认证存储。

调用参数固定为：

```text
<command> [<entry_script>] --resume <session_id> --print --output-format json --input-format text
```

完整 Envelope 与报告指引通过 stdin 临时普通文件传入，stdout 也通过临时普通文件完整读取；不把任务正文拼入命令行或 shell。不传工具、权限、MCP、模型或 profile 覆盖参数，也不关闭会话持久化；使用目标 CLI 会话的默认工作环境，续聊默认持久化。官方依据见 [无头模式](https://www.workbuddy.cn/docs/cli/headless)。

默认持久化的失败尝试仍可能追加历史。本次原桌面 ID 的默认恢复返回需要登录，却追加了原历史，原有前缀经核对完整保留；不能把失败视为没有任何会话变化。保留不确定状态并核对原生记录，不盲目再次执行。

Client 核对完整 JSON 的末尾 `type=result`、匹配 `session_id` 和非错误结果后，仅记录 `delivered`。登录错误、超时、残缺或不匹配输出保留 `uncertain`，不重发。退出码 `0`、结果 `success`、模型拒绝或 `UNKNOWN` 都不能代替原生接手与完成 Report；成果仍须核对。临时输出可能含旧历史，Client 不把历史正文公开为诊断。

安装技能不自动授予非交互执行权限。须在官方 CLI 自身配置中核实技能元数据读取、指定报告程序执行和本次成果工作区的文件权限；Client 不追加权限覆盖参数或替用户批准。本次已通过窄范围配置与真实文件生成验收，该配置不代表其他工作区或命令已获授权。报告通道可用时以 `needs_user` 说明原生权限问题；通道也被拦时保留原生提示与已有投递证据，不能伪造回执。

原生终态 Report 核对通过后，Hub 分配独立的结果 `message_id`，令 `reply_to` 指向原交接消息，并保留 `task_id`、`correlation_id` 返回保存的原入口。`kind=result` 只展示结果，不再次执行任务。可选的 `result_queue_while_busy_verified` 仅声明原生忙时接收结果的已验证能力，默认关闭；它不意味着 WorkBuddy CLI 已能引导或打断忙碌运行，当前没有将其标记为验证通过。

### 官方本地助理 OpenAPI

显式设置 `native.transport="openapi"`；旧配置省略 `transport` 时仍按 OpenAPI 解释，不会自动尝试 CLI。将整个 `native` 替换为：

```json
{
  "transport": "openapi",
  "access_token": "<short-lived access_token from the user's official OAuth grant>",
  "oauth_scopes": ["user.localassistant.invokable"],
  "timeout_seconds": 30
}
```

该方式使用官方 `/openapi/v2/localassistant/message`，目标为持续本地助理，不能填写普通聊天 ID。接口没有普通桌面会话的定位参数，需要用户本人通过正式第三方 OAuth 流程授予短期 access token；CLI 登录不授予这项权限。缺少 token 或 invokable 范围时，在领取消息前停止。

只有 HTTP 2xx 且业务响应 `code` 为整数 `0`、`data.message_id` 为合法非空字符串，才记录已投递；其他响应保留不确定状态，不重发。原生助手仍须主动报告接手和成果。不读取桌面 Cookie、缓存 token 或 IPC ticket，不支持自动 token 续期；官方授权及真实闭环仍待验收。[官方 OpenAPI](https://open.workbuddy.cn/docs/openapi)

## 维护与测试

WorkBuddy 的实现、配置、manifest 和专项测试都放在本目录。单独验证适配器：

```sh
python3 clients/workbuddy/test_adapter.py -v
```

公共协议及跨 Client 兼容测试在 `clients/tests`，公共夹具在 `clients/tests/support.py`。仓库根目录运行 `python3 -m unittest discover -s clients/tests -v` 会汇总所有助手专项测试；只使用本地模拟，不调用官方真实接口。协议和恢复说明见 [Client 文档](../../docs/CLIENTS.md)。
