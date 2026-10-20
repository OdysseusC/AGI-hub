# Client 协议与兼容测试

[简体中文](README.md) | [English](README.en.md)

`test_clients.py` 保留公共传输、事件消费和跨助手兼容检查。共享的本地假 HTTP、消息和 binding 夹具位于 `support.py`，不依赖任何具体助手实现。

各助手的专项测试与实现一起维护：`clients/codex/test_adapter.py`、`clients/workbuddy/test_adapter.py`、`clients/grok-routine/test_adapter.py`。这些文件也能直接单独运行，命令见对应助手 README。

在仓库根目录统一运行：

```sh
python3 -m unittest discover -s clients/tests -v
```

公共测试的 `load_tests` 按唯一模块名加载各助手测试，避免不同目录下的 `test_adapter.py` 名称冲突。此命令汇总公共及各助手专项检查，不重复复制测试；所有传输和子进程均为本地模拟，不调用真实助手。
