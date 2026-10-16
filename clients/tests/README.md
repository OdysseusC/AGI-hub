# Client 协议与兼容测试

`test_clients.py` 保留公共传输、事件消费和跨助手兼容检查。共享的本地假 HTTP、消息和 binding 夹具位于 `support.py`，不依赖任何具体助手实现。

各助手的专项测试与实现一起维护：`clients/codex/test_adapter.py`、`clients/workbuddy/test_adapter.py`、`clients/grok-routine/test_adapter.py`。这些文件也能直接单独运行，命令见对应助手 README。

在仓库根目录统一运行：

```sh
python3 -m unittest discover -s clients/tests -v
```

公共测试的 `load_tests` 按唯一模块名加载各助手测试，避免不同目录下的 `test_adapter.py` 名称冲突。此命令汇总公共及各助手专项检查，不重复复制测试；所有传输和子进程均为本地模拟，不调用真实助手。

构建本机 Hub 后，可运行两个独立的端到端协议检查：

```sh
python3 tools/smoke_client.py --hub /absolute/path/to/agi-hub
python3 tools/smoke_grok_client.py --hub /absolute/path/to/agi-hub
```

Grok 检查使用真实 Hub、文件 Client 和固定 endpoint 的报告程序；本地模拟 Routine 核对先有完整文件、再收到仅含 ID 的 webhook，然后回报并核验成果和一次关联结果。它还检查 Client 重建后不会重复发送。没有真实助手或外部 webhook，也不修改个人能力验收标记。Windows 使用实际 `.exe` 路径；如需验证 exFAT 等文件系统，可加 `--temporary-parent <该文件系统上已存在且位于 Git 仓库外的目录>`，临时数据在结束后清理。
