# 可选登录自启动

[简体中文](AUTOSTART.md) | [English](AUTOSTART.en.md)

从 **0.5.0** 起，可选择在当前用户下次登录 Windows 或 macOS 时恢复 Hub 和已登记的独立 Client。首次安装默认关闭，升级不会擅自启用；不会因为安装、登记 Client 或打开管理页就启动模型。Hub 仍只处理消息，登录 runner 只管理明确选择的本机进程。

## 平台与生效时间

| 平台 | 本版方式 | 生效范围 |
| --- | --- | --- |
| macOS | 当前用户 `~/Library/LaunchAgents/` 下的 LaunchAgent | 下次用户登录时启动 runner；不立即加载登录项，不设置无限 `KeepAlive` |
| Windows | 当前用户的 Task Scheduler 登录触发任务 | 下次该用户登录时启动 runner；登记时不立即运行任务，不配置无限重启 |
| Linux | 不支持登录自启动 | 仅用于源码 CI 和离线检查，不发布 Linux 安装包 |

macOS 的用户级 agent 随用户登录运行，Windows 登录触发器按指定用户登录触发，分别见 [Apple Launch Daemons and Agents](https://developer.apple.com/library/archive/documentation/MacOSX/Conceptual/BPSystemStartup/Chapters/CreatingLaunchdJobs.html) 和 [Microsoft LogonTrigger](https://learn.microsoft.com/en-us/windows/win32/taskschd/logontrigger)。本版使用的是用户登录触发，不是登录前的系统服务。

`enable`、`disable` 和管理页保存只改变以后的登录行为，不启动、停止或重载当前 Hub、Client、原生助手或任务。关闭自启动不等于停止正在运行的 runner；当前实例按其原有生命周期继续运行。变更 Client 选择也不热更新当前实例。

## 先登记，再选择

使用实际安装到稳定位置的 Hub 可执行程序和原有私有数据目录。不要把下载临时目录、公开仓库或别人的路径直接套入示例。Windows 使用实际 `.exe`；PowerShell 调用带空格的程序路径时使用 `&`。

先查看当前计划和平台注册状态：

```sh
agi-hub autostart status --data /absolute/private/agi-hub
```

只恢复 Hub，不选择任何 Client：

```sh
agi-hub autostart enable --data /absolute/private/agi-hub --binary /absolute/stable/agi-hub --clients ''
```

要恢复 WorkBuddy，先确认对应 endpoint 已绑定并启用，且其原有 Client 私有配置、官方登录、会话和权限已可用。另存一份启动描述，例如 `/absolute/private/workbuddy/launch.json`：

```json
{
  "endpoint": "workbuddy",
  "enabled": false,
  "command": "/absolute/path/to/python3",
  "args": [
    "/absolute/stable/agi-hub/clients/workbuddy/main.py",
    "--config",
    "/absolute/private/workbuddy/client.json"
  ],
  "cwd": "/absolute/stable/agi-hub",
  "requires_host": false
}
```

`command` 是绝对可执行文件路径，`args` 是逐项参数，`cwd` 是绝对工作目录；不经过 shell。这里启动的是消费邮箱的 Python `main.py`，不是直接运行 `codebuddy --resume`。原生 CLI 的工作目录、配置目录和会话仍由既有 `client.json` 决定。稳定安装须保留相邻的 `clients/shared`。Windows 将 Python 和所有路径替换为本机实际绝对路径。

登记并显式选择：

```sh
agi-hub autostart register-client --data /absolute/private/agi-hub --endpoint workbuddy --file /absolute/private/workbuddy/launch.json
agi-hub autostart enable --data /absolute/private/agi-hub --binary /absolute/stable/agi-hub --clients workbuddy
```

首次登记的 Client 总是未选择状态，描述中的 `enabled` 不会自动启用它；重新登记既有 endpoint 保留原选择。`enable` 不带 `--clients` 会保留既有选择；`--clients workbuddy,grok` 会替换为列出的已登记 endpoint；`--clients ''` 明确清空选择，仅恢复 Hub。endpoint 名称以本机实际配置为准，不接受未登记的 Client。

管理页提供“下次登录自启动”总开关和已登记 Client 的勾选项。网页只能选择既有登记，不能上传可执行路径、参数或私有配置文件；首次登记和改启动路径须在目标电脑本机使用 CLI。获单独授权的[远端管理页](LAN.md#打开另一台电脑的管理页)也可更改这些已有选择，仍只在目标用户下次登录生效。

停用未来登录项或移除一项登记：

```sh
agi-hub autostart disable --data /absolute/private/agi-hub
agi-hub autostart remove-client --data /absolute/private/agi-hub --endpoint workbuddy
```

这些命令不停止当前进程。`autostart run` 是登录项调用的实际执行入口；手动运行它会按已启用计划启动进程，不能当成只读检查。

## 各 Client 的恢复边界

| Client | 0.5.0 行为 |
| --- | --- |
| WorkBuddy | 可恢复已登记的独立 Python 消费者；收到消息后仍由原 Client 恢复固定官方 CLI 会话。登录、原生权限及业务授权不因自启动扩大 |
| Grok Routine | 可恢复独立 Python 消费者，入口为 `clients/grok-routine/main.py --config <私有配置>`。Client `0.2.0` 已实现 queue 邮箱协议 1，旧 `0.1` 须实际升级；仍需本人配置的官方 Routine webhook、固定身份 helper 与工作区协作规则。真实原生闭环待验收，自启动不代替这些前置条件 |
| Codex | 登记时强制标记 `requires_host=true`；只显示 `needs_host_environment`，runner 不直接启动它 |
| Claude Code | 同样只登记原生宿主需求；交互式 Code 进程及其 mod 必须按原流程启动，runner 不执行 `claude` 或 `-p --resume` |

Codex/Claude 的最小登记描述可以是 `{"endpoint":"codex","requires_host":true}` 或 `{"endpoint":"claude","requires_host":true}`，用同一 `register-client` 命令登记。不要为这些条目填写启动命令或环境。Codex 的官方桥接环境、签名父链和真实 caller 身份必须由自然宿主提供；首次绑定不能将这些条件永久保存为任意后台进程都可恢复。Claude 仍须满足所选交互式宿主、插件、固定会话和用户网络要求。

启动描述与带凭据的 `client.json` 是两份不同配置。启动计划只保存后者的路径，不复制 endpoint token、登录凭据、宿主 IPC、pipe 或模型密钥。可选 `environment` 仅接受代码规定的 OS、语言及时区、显式代理变量白名单；代理 URL 不接受凭据。runner 不捕获当前助手的代理、宿主或密钥环境。各 Client 自身仍可进一步筛选环境，配置代理变量也不证明指定代理链与出口已经核实；需要这类门禁的宿主必须按原要求验证，不得回退直连。详见[各 Client 说明](CLIENTS.md)。

## 就绪、重试与诊断

runner 先使用当前私有配置认证本机 Hub。已有 Hub 就绪时只附着，不接管其进程；之后该外部 Hub 消失，也不会擅自替它重启。只有确认本机监听不可用时才尝试启动自己的 Hub，认证失败或无法确认身份时停在 `needs_user`。Hub 就绪后才尝试启动选中的独立 Client；已停用或未绑定的 endpoint 不启动。

同一 runner 生命周期内，自有 Hub 和每个启动失败或异常退出的独立 Client 各最多 **首次启动加 5 次重试**，延迟依次为 **1、2、4、8、16 秒**。用尽后保留失败状态，不由 OS 无限重拉。Client 正常退出不自动重启。该有限重试只恢复进程，不重投 `dispatching/uncertain` 消息、不更改原生会话状态，也不代替 Client 自身的认证和投递核验。

`autostart status` 同时报告未来登录项、已登记选择和最近一次运行快照；PID 与快照可能已过时。`running` 仅表示进程已启动，`hub_ready` 仅表示本机 Hub 已认证就绪，都不证明原生助手已连接或业务已完成。`needs_host_environment` 表示须从原生宿主启动，`needs_user` 表示前置条件或认证需处理。

私有数据目录新增 `startup.json`、`startup-state.json` 和 `logs/autostart/`。runner 日志只记录固定状态与次数，不记录命令参数、环境值或子进程输出。停止 runner 时只清理本次创建的子进程，已附着的 Hub 保持由原启动者管理。

## 验证状态

本版已通过离线平台定义、计划校验、进程替身、有限重试、所有权、管理 API 与网页测试。它们未调用真实助手。**真实电脑的重启、用户登录触发、原生会话在新生命周期中的恢复，以及各目标环境的全链路结果仍未验收。** 已有手动启动下的原生验收不能代替这些检查；不要把已登记、平台定义存在或 `running` 写成无人值守恢复通过。

```text
go test ./internal/autostart ./internal/app ./cmd/agi-hub
node --test internal/app/web/dashboard.test.cjs
```
