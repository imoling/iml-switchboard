// Package autostart 管理开机自启：登录后自动跑 imls serve。
// macOS 用 launchd，Windows 用当前用户的 Run 注册表项，Linux 用 systemd 用户服务。
// 三个平台提供同样的 Install、Uninstall、Installed。
package autostart
