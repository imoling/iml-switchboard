---
description: 用 Switchboard 生成一张图片并保存到项目的 assets 里
---
调用 iml-switchboard 的 generate_image 工具生成图片，提示词是：$ARGUMENTS
如果我在对话里贴了图、或提到了某张图片，把它们的本地路径放进 image_paths 当参考图。
生成后把文件路径和费用告诉我，并在需要的地方引用这张图。
