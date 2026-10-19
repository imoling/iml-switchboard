//! Switchboard 桌面壳：托盘、三个窗口、全局快捷键、后台拉起。
//! 所有业务数据都在 React 侧通过 http://127.0.0.1:18081 取，这里不碰。

use tauri::{
    image::Image,
    menu::{MenuBuilder, MenuItemBuilder},
    tray::{MouseButton, MouseButtonState, TrayIconBuilder, TrayIconEvent},
    AppHandle, Emitter, Manager, WebviewUrl, WebviewWindowBuilder, WindowEvent,
};
use tauri_plugin_global_shortcut::{GlobalShortcutExt, Shortcut, ShortcutState};
use tauri_plugin_positioner::{Position, WindowExt};
use tauri_plugin_shell::ShellExt;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Mutex;
use std::time::{Duration, Instant};

const TRAY_ID: &str = "main";

/// 弹窗与快速面板「失焦即隐藏」的状态。
/// armed：显示后真正拿到过焦点才算数；macOS 上应用没被激活时窗口会先收到一次失焦，不能据此藏掉。
/// blurred_at：刚因失焦藏起来的时刻；点托盘图标本身会先让弹窗失焦，紧接着的那次点击应当是「关」而不是「再开」。
struct Flyout {
    armed: AtomicBool,
    blurred_at: Mutex<Option<Instant>>,
}

static POPOVER: Flyout = Flyout { armed: AtomicBool::new(false), blurred_at: Mutex::new(None) };
static QUICK: Flyout = Flyout { armed: AtomicBool::new(false), blurred_at: Mutex::new(None) };

fn flyout(label: &str) -> Option<&'static Flyout> {
    match label {
        "popover" => Some(&POPOVER),
        "quick" => Some(&QUICK),
        _ => None,
    }
}

fn tray_icon(status: &str) -> (Image<'static>, bool) {
    // 模板图只能单色，所以「额度紧张 / 出错 / 未运行」三种带色状态用非模板图
    match status {
        "warn" => (Image::from_bytes(include_bytes!("../icons/tray-warn.png")).expect("tray-warn"), false),
        "error" => (Image::from_bytes(include_bytes!("../icons/tray-err.png")).expect("tray-err"), false),
        "off" => (Image::from_bytes(include_bytes!("../icons/tray-off.png")).expect("tray-off"), false),
        _ => (Image::from_bytes(include_bytes!("../icons/tray.png")).expect("tray"), true),
    }
}

fn toggle_window(app: &AppHandle, label: &str, position: Option<Position>) {
    if let Some(w) = app.get_webview_window(label) {
        let f = flyout(label);
        if let Some(f) = f {
            let just_blurred = f.blurred_at.lock().unwrap().take().is_some_and(|t| t.elapsed() < Duration::from_millis(400));
            if just_blurred {
                return;
            }
        }
        let armed = f.map_or(true, |f| f.armed.load(Ordering::SeqCst));
        if w.is_visible().unwrap_or(false) && (w.is_focused().unwrap_or(false) || !armed) {
            let _ = w.hide();
            return;
        }
        if let Some(f) = f {
            f.armed.store(false, Ordering::SeqCst);
        }
        if let Some(p) = position {
            let _ = w.move_window(p);
        } else {
            let _ = w.center();
        }
        let _ = w.show();
        let _ = w.set_focus();
        let _ = w.emit("imls://shown", ());
    }
}

#[tauri::command]
fn show_main(app: AppHandle) {
    if let Some(w) = app.get_webview_window("main") {
        let _ = w.show();
        let _ = w.unminimize();
        let _ = w.set_focus();
    }
    if let Some(p) = app.get_webview_window("popover") {
        let _ = p.hide();
    }
}

#[tauri::command]
fn toggle_quick(app: AppHandle) {
    toggle_window(&app, "quick", None);
}

#[tauri::command]
fn toggle_popover(app: AppHandle) {
    toggle_window(&app, "popover", Some(Position::TrayCenter));
}

#[tauri::command]
fn hide_window(app: AppHandle, label: String) {
    if let Some(w) = app.get_webview_window(&label) {
        let _ = w.hide();
    }
}

#[tauri::command]
fn set_hotkey(app: AppHandle, hotkey: String) -> Result<(), String> {
    register_hotkey(&app, &hotkey)
}

fn register_hotkey(app: &AppHandle, hotkey: &str) -> Result<(), String> {
    let gs = app.global_shortcut();
    gs.unregister_all().map_err(|e| e.to_string())?;
    let shortcut: Shortcut = hotkey.parse().map_err(|e| format!("{e:?}"))?;
    gs.register(shortcut).map_err(|e| e.to_string())
}

#[tauri::command]
fn set_tray_status(app: AppHandle, status: String, tooltip: Option<String>) {
    if let Some(tray) = app.tray_by_id(TRAY_ID) {
        let (icon, template) = tray_icon(&status);
        let _ = tray.set_icon_as_template(template && cfg!(target_os = "macos"));
        let _ = tray.set_icon(Some(icon));
        if let Some(t) = tooltip {
            let _ = tray.set_tooltip(Some(t));
        }
    }
}

/// 后台没起来时由界面点「启动」：等价于在终端里跑 `imls serve --background`，进程交给系统，不随应用退出。
/// 先用安装包里附带的 imls（Windows 用户不会自己把它放进 PATH），开发时没打包就退回 PATH 里的 imls。
/// 故意不走 shell 插件：插件会在应用退出时杀掉它拉起的所有子进程，后台会跟着一起没了。
#[tauri::command]
async fn start_backend() -> Result<(), String> {
    let mut candidates: Vec<std::path::PathBuf> = Vec::new();
    if let Ok(exe) = std::env::current_exe() {
        if let Some(dir) = exe.parent() {
            candidates.push(dir.join(if cfg!(windows) { "imls.exe" } else { "imls" }));
        }
    }
    candidates.push("imls".into());
    let mut last = String::from("没有找到 imls");
    for bin in candidates {
        match spawn_detached(&bin) {
            Ok(()) => return Ok(()),
            Err(e) => last = format!("{}: {e}", bin.display()),
        }
    }
    Err(last)
}

/// 另起一个进程组、不接管输入输出，应用退出时系统不会连带收掉它
fn spawn_detached(bin: &std::path::Path) -> std::io::Result<()> {
    use std::process::{Command, Stdio};
    let mut cmd = Command::new(bin);
    cmd.args(["serve", "--background"]).stdin(Stdio::null()).stdout(Stdio::null()).stderr(Stdio::null());
    #[cfg(unix)]
    {
        use std::os::unix::process::CommandExt;
        cmd.process_group(0);
    }
    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        const DETACHED_PROCESS: u32 = 0x0000_0008;
        const CREATE_NEW_PROCESS_GROUP: u32 = 0x0000_0200;
        cmd.creation_flags(DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP);
    }
    cmd.spawn().map(|_| ())
}

/// 安装包里附带的 imls 的构建号；界面拿它和正在跑的后台比对。开发时没打包就返回 None，不做比对。
#[tauri::command]
async fn bundled_backend_build(app: AppHandle) -> Option<String> {
    let out = app.shell().sidecar("imls").ok()?.args(["build-id"]).output().await.ok()?;
    let id = String::from_utf8_lossy(&out.stdout).trim().to_string();
    (out.status.success() && !id.is_empty()).then_some(id)
}

/// 快速面板的默认快捷键：Windows 上 Alt+Space 是窗口菜单，换成 Ctrl+Alt+Space（与后台配置的默认值一致）
const DEFAULT_HOTKEY: &str = if cfg!(target_os = "windows") { "Ctrl+Alt+Space" } else { "Alt+Space" };

#[tauri::command]
fn show_about(app: AppHandle) -> Result<(), String> {
    if let Some(w) = app.get_webview_window("about") {
        let _ = w.show();
        let _ = w.set_focus();
        return Ok(());
    }
    WebviewWindowBuilder::new(&app, "about", WebviewUrl::App("index.html?win=about".into()))
        .title("Switchboard")
        .inner_size(360.0, 300.0)
        .resizable(false)
        .minimizable(false)
        .maximizable(false)
        .center()
        .build()
        .map(|_| ())
        .map_err(|e| e.to_string())
}

/// 把媒体产物读进剪贴板；只支持 PNG
#[tauri::command]
fn copy_image(app: AppHandle, path: String) -> Result<bool, String> {
    use tauri_plugin_clipboard_manager::ClipboardExt;
    let img = Image::from_path(&path).map_err(|e| e.to_string())?;
    app.clipboard().write_image(&img).map_err(|e| e.to_string())?;
    Ok(true)
}

#[tauri::command]
fn app_version(app: AppHandle) -> String {
    app.package_info().version.to_string()
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .plugin(tauri_plugin_shell::init())
        .plugin(tauri_plugin_notification::init())
        .plugin(tauri_plugin_positioner::init())
        .plugin(tauri_plugin_clipboard_manager::init())
        .plugin(tauri_plugin_dialog::init())
        .plugin(tauri_plugin_opener::init())
        .plugin(
            tauri_plugin_global_shortcut::Builder::new()
                .with_handler(|app, _shortcut, event| {
                    if event.state() == ShortcutState::Pressed {
                        toggle_window(app, "quick", None);
                    }
                })
                .build(),
        )
        .invoke_handler(tauri::generate_handler![
            show_main,
            toggle_quick,
            toggle_popover,
            hide_window,
            set_hotkey,
            set_tray_status,
            start_backend,
            bundled_backend_build,
            show_about,
            copy_image,
            app_version
        ])
        .setup(|app| {
            let handle = app.handle().clone();

            // 托盘：左键开合弹窗，右键给一个最小菜单
            let (icon, _) = tray_icon("ok");
            let menu = MenuBuilder::new(app)
                .item(&MenuItemBuilder::with_id("open", "打开主窗口").build(app)?)
                .item(&MenuItemBuilder::with_id("about", "关于").build(app)?)
                .separator()
                .item(&MenuItemBuilder::with_id("quit", "退出").build(app)?)
                .build()?;
            TrayIconBuilder::with_id(TRAY_ID)
                .icon(icon)
                .icon_as_template(cfg!(target_os = "macos")) // 单色模板图只有 macOS 菜单栏用得上
                .tooltip("Switchboard")
                .menu(&menu)
                .show_menu_on_left_click(false)
                .on_menu_event(|app, event| match event.id().as_ref() {
                    "open" => show_main(app.clone()),
                    "about" => {
                        let _ = show_about(app.clone());
                    }
                    "quit" => app.exit(0),
                    _ => {}
                })
                .on_tray_icon_event(|tray, event| {
                    tauri_plugin_positioner::on_tray_event(tray.app_handle(), &event);
                    if let TrayIconEvent::Click { button: MouseButton::Left, button_state: MouseButtonState::Up, .. } = event {
                        toggle_window(tray.app_handle(), "popover", Some(Position::TrayCenter));
                    }
                })
                .build(app)?;

            // 先注册默认快捷键；设置页里存的值由前端连上后台后调 set_hotkey 覆盖
            if let Err(e) = register_hotkey(&handle, DEFAULT_HOTKEY) {
                eprintln!("register hotkey: {e}");
            }

            // 弹窗与快速面板：拿到过焦点之后，失焦即隐藏
            for label in ["popover", "quick"] {
                if let (Some(w), Some(f)) = (app.get_webview_window(label), flyout(label)) {
                    let w2 = w.clone();
                    w.on_window_event(move |e| match e {
                        WindowEvent::Focused(true) => f.armed.store(true, Ordering::SeqCst),
                        WindowEvent::Focused(false) if f.armed.swap(false, Ordering::SeqCst) => {
                            *f.blurred_at.lock().unwrap() = Some(Instant::now());
                            let _ = w2.hide();
                        }
                        _ => {}
                    });
                }
            }

            // 主窗口关闭只是隐藏，应用常驻托盘
            if let Some(w) = app.get_webview_window("main") {
                let w2 = w.clone();
                w.on_window_event(move |e| {
                    if let WindowEvent::CloseRequested { api, .. } = e {
                        api.prevent_close();
                        let _ = w2.hide();
                    }
                });
            }
            Ok(())
        })
        .build(tauri::generate_context!())
        .expect("error while running Switchboard")
        .run(|app, event| {
            // 主窗口关掉只是藏起来；点 Dock 图标要能把它叫回来，否则 Dock 菜单里只剩「无可用的窗口」
            #[cfg(target_os = "macos")]
            if let tauri::RunEvent::Reopen { .. } = event {
                show_main(app.clone());
            }
            #[cfg(not(target_os = "macos"))]
            let _ = (app, event);
        });
}
