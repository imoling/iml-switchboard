---
description: 用 Switchboard 生成一段短视频
---
调用 iml-switchboard 的 generate_video 工具，内容是：$ARGUMENTS
如果返回的是任务号，每隔 30 秒用 get_job 查询一次，直到拿到文件路径。
