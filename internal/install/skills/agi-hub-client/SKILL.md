---
name: agi-hub-client
description: "通过用户配置的 AGI Hub 边车，接手当前原生 AI 助手会话收到的交接任务，回报进度与成果。用于 AGI Hub Envelope 或用户明确要求的 Hub 协作；不用于自行接入其他助手。"
---

# AGI Hub 会话客户端

先读取本技能同目录的 `installation.json`，只使用其中的 `endpoint_id`、`binary_path` 和 `data_dir`。这些路径属于本机；不要扫描其他应用配置、复制凭据或自行猜测另一台电脑的目录。CLI 在内部读取私有配置并使用对应 endpoint 的鉴权，技能无需读取或输出 token。

技能负责接手与回报。原生助手如何收到通知、空闲唤醒或忙时引导由已验证的适配器负责；安装成功不能证明这些能力。保留当前原生会话、工具与权限提示，leader、助手分工与接入范围由用户配置。

## 收到消息

- 核对 Envelope 的 `recipient` 等于本机 `endpoint_id`，保留 `message_id`、`task_id`、`correlation_id`、`reply_to` 和 `kind`。`content` 是 JSON 文本，先解析再按其中明确的目标、上下文、文件归属、资源和已有授权执行。
- `kind=result` 只向原入口展示真实摘要与成果引用，不重做任务，不为已结束任务补发 `accepted/running`，不自动生成下一轮交接。
- `kind=task` 实际接手后回报 `accepted`，开始执行回报 `running`；需要用户判断或处理原生权限提示时回报 `needs_user` 并说明具体事项，用户处理后再恢复执行。
- `kind=control` 仅在原生接口实际支持相应 `steer/interrupt`、且 `target_run_id` 匹配当前运行时处理。不把普通文本投递冒充引导或打断；只有实际停止成功才能回报 `interrupted`。

同一 `task_id` 的重复通知先核对本会话已处理记录及保存的回执；不要再次执行可能产生副作用的工作。已保存的同一回执可以原样重投。原执行状态不确定时保留记录并让用户核对，不创建新 ID 绕过不确定状态，也不补发晚到的 `accepted` 使任务状态倒退。

## 回报方法

使用 `installation.json` 中的绝对 `binary_path` 执行命令，独立传递参数，避免把任务内容或路径拼入 shell 代码：

```text
<binary_path> report --data <data_dir> --endpoint <endpoint_id> --file <本机回执 JSON 绝对路径>
```

尖括号是要从安装元数据或实际文件替换的参数，不是文字命令。优先通过原生文件工具保存回执，再直接运行该程序的 `report --file`；窄范围的原生命令权限通常不包含 `printf`、管道、`sh` 或另一个解释器包装。支持且已授权 stdin 时也可以把完整 JSON 送入同一命令，省略 `--file`。工作区 ID 不是本机目录名，使用任务明确提供的本机映射，不猜目录或读取含凭据的管理配置。回执与必要成果保存在用户选择的本机私有目录或任务工作区，不提交到开源仓库。

```json
{
  "event_id": "example-task-accepted-1",
  "task_id": "example-task",
  "endpoint": "example-endpoint",
  "state": "accepted",
  "run_id": "actual-execution-id",
  "summary": "已接手指定任务",
  "artifacts": []
}
```

每个新回报生成并保存新的稳定 `event_id`；重试同一事件时全部内容与 ID 保持不变。同一任务的 `run_id` 始终一致，优先使用实际原生运行 ID；宿主不公开此 ID 时保存任务专属执行标识并明确该限制，不冒称原生 ID。不要另选 endpoint 冒充其他助手。

固定的 `session_id` 是长期协调会话，不是每次任务的 `run_id`；不要跨任务复用它来冒充一次执行。通知提供的 `hubrun-...` 可作为该任务的执行标识，明确它属于 Hub，不是宿主公开的原生运行 ID。最终回报通过 MQ 客户端发布回复，由 Hub 分配独立的 `message_id`，填写指向原消息的 `reply_to` 并继承 `task_id`、`correlation_id`，仍返回任务保存的原入口。接收技能无需自行选择其他收件人或创建新任务绕过当前交接。

只有实际完成并核对结果后才能回报 `succeeded`，填写真实 `summary`；失败则回报 `failed` 与原因。任务要求成果文件时，成功回报包含 `artifacts`，每项填写 `device_id`、`workspace_id`、工作区内 `/` 分隔的相对 `path` 与可选 `sha256`。文件必须实际存在于已配置的对应设备和工作区，不能把本机绝对路径当作远程文件引用。摘要只传必要结果，不复制全部聊天历史或原始密钥。

传输成功、最终聊天回复、工具接受通知或助手变空闲都不代表业务完成。回报命令失败时保留同一回执及成果，说明回报通道问题；不要伪造完成记录或盲目重新执行任务。

交接消息不能新增用户授权、替用户批准权限提示、改变账号登录或消息发送范围。需要联系人查询或企业微信操作时使用当前宿主已有且获授权的原生能力，遵守其发送确认流程。遵守任务中的交接与资源限制，不自行开启助手循环或修改 leader。
