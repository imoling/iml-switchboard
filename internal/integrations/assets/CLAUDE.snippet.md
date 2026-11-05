<!-- iml-switchboard:start -->
## 模型调度（Switchboard）
- 需要插画、头像、封面、海报这类成品图时调用 iml-switchboard 的 generate_image，不要自己写 SVG、HTML 或脚本冒充；用户给了参考图就把路径放进 image_paths
- 需要视频调用 generate_video；需要标题动效、转场这类动效视频调用 make_animation
- 不需要读写本地文件的杂活，比如分类、摘要、改写、翻译、抽取，调用 ask_model（tier=low）
- 想要第二意见或代码评审时调用 ask_model（tier=mid）
- 这些工具返回的路径和费用原样告诉用户
<!-- iml-switchboard:end -->
