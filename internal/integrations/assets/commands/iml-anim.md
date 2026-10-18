---
description: 用 Switchboard 做一段动效视频（Remotion 渲染）
---
调用 iml-switchboard 的 make_animation 工具，动画要表现：$ARGUMENTS
如果返回的是任务号，每隔 30 秒用 get_job 查询一次，直到拿到文件路径。
