# 原生助手技能安装

每个接入会话需要知道怎样处理任务信封、按真实执行状态回报，以及怎样展示返回结果。AGI Hub 提供同一份 `agi-hub-client` 技能，由本机 CLI 安装到用户选定助手的技能目录。技能是协议说明；消费通知和唤醒现有会话仍由各宿主适配器负责。

## 安装

先按 [安装指南](INSTALL-FOR-ASSISTANT.md) 构建、初始化边车，并配置用户明确要接入的 endpoint。使用实际安装的程序和私有数据目录：

macOS / Linux：

```sh
/absolute/path/agi-hub install-skill --host codex --endpoint codex --data /absolute/path/private
/absolute/path/agi-hub install-skill --host workbuddy --endpoint workbuddy --data /absolute/path/private
```

Windows PowerShell：

```powershell
& 'C:\Users\you\bin\agi-hub.exe' install-skill --host codex --endpoint codex --data 'C:\Users\you\private\agi-hub'
& 'C:\Users\you\bin\agi-hub.exe' install-skill --host workbuddy --endpoint workbuddy --data 'C:\Users\you\private\agi-hub'
```

以上是路径示例，须替换为本机真实绝对路径与已配置的 endpoint。安装流程适合交给当前 AI 助手直接执行，不需要下载脚本、额外包管理器或修改宿主权限设置。CLI 的 `--binary` 可明确记录接手助手以后使用的真实程序绝对路径。

| 宿主 | 默认技能目录 | 支持边界 |
| --- | --- | --- |
| Codex | 当前用户 `~/.codex/skills` | 安装协议技能；既有会话是否加载需在实际会话中核对 |
| WorkBuddy | 当前用户 `~/.workbuddy/skills` | 根据当前 5.8.2 本机扫描实现选择；须通过技能界面和真实任务核对加载，不作为全版本保证 |
| 其他宿主 | 无默认值 | 使用官方说明或本机已核实的技能目录，显式指定 `--skills-dir`；安装相同说明不代表支持唤醒 |

自定义目录示例：

```text
<installed-binary> install-skill --host <host> --endpoint <configured-endpoint> --data <absolute-data-dir> --skills-dir <verified-absolute-skills-dir>
```

尖括号是待替换参数。只安装到用户选定宿主，不批量扫描应用配置、提取登录态、注册其他助手或切换 leader。不同宿主的技能格式和加载条件需要分别核实；如果需要 UI 导入，使用宿主原生技能界面完成并保留用户审批流程。

## 安装内容与更新

安装目录固定为技能根目录下的 `agi-hub-client/`：

```text
agi-hub-client/
  SKILL.md
  installation.json
  .agi-hub-manifest.json
```

`installation.json` 只包含 `host`、`endpoint_id`、`binary_path`、`data_dir` 四个字段，供技能找到同一台电脑的回报程序与对应 endpoint。它不复制管理凭据、原生登录态或聊天记录。CLI 在内部加载私有边车配置并使用对应 endpoint 凭据；技能不需要读取 token。

所有权清单记录安装器标识与两个受管文件的 SHA256。相同内容再次安装保持文件不变；升级只替换清单仍匹配的受管文件，保留其他本地文件。没有清单、内容被手工编辑、目标为软链接或目录路径包含软链接时拒绝覆盖，用户可以先检查并备份冲突目录，再明确选择安装位置。

文件先写入同目录临时文件、同步后重命名；安装器不会在失败时删除原文件来强行覆盖。Windows 上宿主占用文件可能使更新失败，应关闭占用后重试。多个本安装器通过本机锁协调；崩溃留下的锁需要先检查，不能盲删正在使用的锁。升级中途异常也可能留下清单与文件不匹配，此时停止更新并保留文件供核对。这不是对同一 OS 账号下其他进程的安全隔离。

## 原生验收

安装命令成功后在所选宿主核对技能是否出现，并在指定会话验证它实际读取 `installation.json`。使用一个用户明确授权的实际任务，记录以下证据：

1. 原生入口在已核实空闲的状态收到 Envelope 并开始处理；配置和技能安装本身不是唤醒证据。
2. `accepted/running` 来自接手会话，保持同一 `task_id` 和真实执行标识；宿主未公开原生运行 ID 时明确该限制。
3. 需要用户处理时保留原生提示并回报 `needs_user`；安装器不自动批准权限或代替用户发送消息。
4. 完成后回报真实摘要与所需成果文件；重复消息不重新执行，返回的 `kind=result` 只展示结果。
5. 原入口收到结果，任务记录能区分投递、接手和业务完成。

当前队列与驱动能力边界见 [适配器](ADAPTERS.md)，回执字段与文件归属见 [协议](PROTOCOL.md)。
