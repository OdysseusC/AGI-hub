# Grok lab 离线恢复

[简体中文](RECOVERY.md) | [English](RECOVERY.en.md)

`recovery.py` 是一次性维护入口，不领取 Hub 消息，也不调用普通邮箱 adapter。它只适用于用户已授权的同一 Routine、同一电脑上的既有 lab。正常协作仍走 [Hub 邮箱](README.md)。

**默认拒绝，原生维护仍待验收。** 仅支持业务邮箱的 Routine 不能使用此入口。有些版本的官方“编辑”按钮生成聊天草稿，没有独立保存表单；打开编辑、复制 webhook 或保存本地配置都不证明维护分支已加载。应先核对实际官方配置方式与用户允许的恢复通路。

## 独立配置与计划

以下均为示例值。文件放在仓库外的私有目录，核对实际 target、既有 RootEntry 和本次用户授权；不把 URL、key 或私人路径提交到仓库。只有本机管理员通过真实可用的官方配置入口增加并核验独立分支后，才可登记 `maintenance_contract`，不得用虚构的 `evidence_ref` 解锁。

`recovery-config.json`：

```json
{
  "maintenance_contract": {
    "version": 1,
    "routine_id": "existing-authorized-routine",
    "evidence_ref": "<actual-operator-verification-record>"
  },
  "native": {
    "routine_id": "existing-authorized-routine",
    "url": "https://existing-routine.example.invalid/webhook",
    "headers": {"Authorization": "Bearer <existing-webhook-key>"},
    "timeout_seconds": 30
  }
}
```

契约恰有这三个字段：整数 `version:1`、与 `native.routine_id` 完全相同的 `routine_id`、非空且不含控制字符的 `evidence_ref`（最多 256 字符）。引用只是操作员记录，不会读取其路径。URL 必须为既有 Routine 的 HTTPS 地址；不新建凭据或权限。也可将 `native` 内字段移至顶层，保留同级契约。完整 Client 配置可作为容器读取，但不要求或使用 `endpoint_token`。这是独立恢复配置，不能写回普通消费者配置；既有 `native.body` 被忽略，程序固定发送唯一字段 `agi_hub_lab_recovery`，没有自由 prompt 或 body 模板接口。

`plan.json`：

```json
{
  "type": "agi_hub_lab_recovery",
  "source": "out_of_band_maintenance",
  "hub_claimed": false,
  "attempt_id": "lab-recovery-20261027-01",
  "expires_at": "2026-10-27T12:15:00Z",
  "target": {
    "hub_id": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
    "device_id": "windows-lab",
    "data_dir": "C:\\Users\\owner\\existing-lab-hub"
  },
  "root_entry": {
    "hub_id": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
    "endpoint": "codex"
  },
  "original_work_ref": "human-recovery-authorization-20261027"
}
```

所有键均必填，额外键拒绝；前三项为固定值。Hub ID 为 64 位小写十六进制，`data_dir` 须是已知 lab 的绝对非根目录且不含 `..`。使用符合 Hub 格式的 device、endpoint 和 attempt 标识；`original_work_ref` 是本次用户授权的简短引用，不放指令或凭据。**替换示例截止时间**：必须是未来 UTC 时间，最多 24 小时；过期即拒绝，不能借维护执行先前的重启请求。

## 一次调用与结果

仅在上述真实核验完成后，从仓库根目录执行一次，三个文件参数均为绝对路径：

```sh
python3 clients/grok-routine/recovery.py --config /absolute/private/recovery-config.json --plan /absolute/private/plan.json --attempt-record /absolute/private/lab-recovery-20261027-01.json
```

Windows 使用 `py -3` 及加引号的本机绝对路径。程序先用 `O_EXCL` 创建私有 attempt 文件，写入 `sending` 和实际 payload 的 SHA-256 并 `fsync`，然后只调用一次 `post_json_once`。记录和终端输出不保存 URL、key 或请求正文。

防重复**仅针对指定 attempt 文件**，不是官方服务端幂等证明；改文件名、删除记录或重建同 ID 会破坏这道本地屏障。已有记录一律拒绝。超时、非 2xx 或结果落盘不确定都不自动重发；先核对原生证据，保留原记录。`transport_accepted`（包括 HTTP 202）只表示 HTTP 传输被接受，执行仍未验证；`uncertain`、`not_sent` 和 `rejected` 均不代表恢复成功。程序不会伪造 Hub receipt、业务回复或 Report。

## 供管理员审阅的 Routine 独立分支模板

下面是待核对的指令模板；须在目标环境安装并验证。管理员须确认目标已有技能、固定 helper、工具权限及实际指令加载方式；缺少正式配置入口时停止，不向聊天草稿发送来冒充配置。保留既有业务分支，维护请求必须独立处理：

```text
仅当 webhook JSON 恰有 agi_hub_lab_recovery 字段时进入此维护分支；
不得转入 grok_pr9_mailbox 或把它当作已领取的 Hub request。
核对其中计划的 type=agi_hub_lab_recovery、source=out_of_band_maintenance、
hub_claimed=false、未过期 expires_at，以及本机既有 lab 的 hub_id/device_id/data_dir。
任一项不匹配就停止。副作用前持久记录 attempt_id；重复 ID 不再执行。

只使用该 lab 已有技能、固定工具和登记命令，检查并恢复原 lab Hub、
已启用且已登记的 Client 与诊断；健康进程只附着，保留生产环境。
不重启/关机，不领取、重放或执行旧业务消息，不改 ACL、凭据、verification、
宿主权限或登录项，不复制 IPC，不启动 Claude，不创建其他助手会话。
缺少既有工具、真实宿主环境或必要代理验证时记 needs_host 并停止相应恢复。

Hub 恢复后，只用本 endpoint 的既有固定 helper，向 root_entry 开新会话回报
attempt_id、original_work_ref、实际 boot time、Hub/Client 状态及剩余阻断。
不含凭据或业务正文，不伪造 receipt/Report，不用窗口聊天或 HTTP 成功代替回报。
Hub 尚不可用时保留本地诊断，不扩大权限或另开通信通路。
```

离线检查：`python3 clients/grok-routine/test_recovery.py -v`。测试通过不证明所选 Routine 能访问目标电脑或完成原生恢复。
