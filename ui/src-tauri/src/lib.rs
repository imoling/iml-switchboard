//! Switchboard 桌面壳：托盘、三个窗口、全局快捷键、后台拉起。
//! 所有业务数据都在 React 侧通过 http://127.0.0.1:18081 取，这里不碰。

use tauri::{
    image::Image,
    menu::{MenuBuilder, MenuItemBuilder},
    tray::{MouseButton, MouseButtonState, TrayIconBuilder, TrayIconEvent},
    AppHandle, Emitter, Manager, WebviewUrl, WebviewWindowBuilder, WindowEvent,
};
use tauri_plugin_global_shortcut::{GlobalShortcutExt, Shortcut, ShortcutState};
use tauri_plugin_shell::ShellExt;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Mutex;
use std::time::{Duration, Instant};

const TRAY_ID: &str = "main";

/// 弹窗与快速面板「失焦即隐藏」的状态。
/// armed：显示后真正拿到过焦点才算数；macOS 上应用没被激活时窗口会先收到一次失焦，不能据此藏掉。
/// blurred_at：刚因失焦藏起来的时刻；点托盘图标本身会先让弹窗失焦，紧接着的那次点击应当是「关」而不是「再开」。
/// anchor：最近一次摆放的左上角；前端按内容改高度后用它把顶边摆回去（macOS 改大小时固定的是左下角，顶边会漂）。
struct Flyout {
    armed: AtomicBool,
    blurred_at: Mutex<Option<Instant>>,
    anchor: Mutex<Option<tauri::Position>>,
}

static POPOVER: Flyout = Flyout { armed: AtomicBool::new(false), blurred_at: Mutex::new(None), anchor: Mutex::new(None) };
static QUICK: Flyout = Flyout { armed: AtomicBool::new(false), blurred_at: Mutex::new(None), anchor: Mutex::new(None) };

/// 刘海动画（实验功能）：贴在刘海上的透明窗口。普通窗口上不了菜单栏，要把层级抬到状态栏之上；
/// 鼠标穿透，两耳下面的菜单照常能点；不抢焦点，跟着切桌面和全屏。
#[cfg(target_os = "macos")]
mod notch {
    use objc2::MainThreadMarker;
    use objc2_app_kit::{NSScreen, NSWindow, NSWindowCollectionBehavior};
    use objc2_foundation::{NSPoint, NSRect, NSSize};

    const EAR: f64 = 44.0; // 一只耳朵的宽度，右耳不挡到紧挨刘海的状态栏图标
    const MARGIN: f64 = 36.0; // 两侧留给内圆角和外发光的余量
    const DROP: f64 = 54.0; // 刘海下方留给能量带和外发光的高度；都是透明的，而且鼠标穿透
    const LEVEL: isize = 26; // 菜单栏 24、状态栏 25；系统弹出菜单 101 仍在它上面

    pub struct Geometry {
        frame: NSRect,
        pub width: f64,
        pub height: f64,
    }

    /// 带刘海的屏幕；外接屏没有刘海，合盖时就返回 None
    pub fn geometry() -> Option<Geometry> {
        let mtm = MainThreadMarker::new()?;
        for s in NSScreen::screens(mtm).iter() {
            let height = s.safeAreaInsets().top;
            if height <= 0.0 {
                continue;
            }
            let (f, l, r) = (s.frame(), s.auxiliaryTopLeftArea(), s.auxiliaryTopRightArea());
            // 刘海就是菜单栏两块可用区域中间的缺口，永远居中
            let width = f.size.width - l.size.width - r.size.width;
            let (ww, wh) = (width + 2.0 * (EAR + MARGIN), height + DROP);
            let origin = NSPoint::new(f.origin.x + (f.size.width - ww) / 2.0, f.origin.y + f.size.height - wh);
            return Some(Geometry { frame: NSRect::new(origin, NSSize::new(ww, wh)), width, height });
        }
        None
    }

    pub fn place(window: &tauri::WebviewWindow, g: &Geometry) {
        let Ok(ns) = window.ns_window() else { return };
        // SAFETY: Tauri 给的就是这个窗口的 NSWindow 指针，命令在主线程上跑
        let win: &NSWindow = unsafe { &*ns.cast() };
        win.setLevel(LEVEL);
        win.setIgnoresMouseEvents(true);
        win.setCollectionBehavior(
            NSWindowCollectionBehavior::CanJoinAllSpaces
                | NSWindowCollectionBehavior::Stationary
                | NSWindowCollectionBehavior::FullScreenAuxiliary
                | NSWindowCollectionBehavior::IgnoresCycle,
        );
        win.setFrame_display(g.frame, true);
        win.orderFrontRegardless(); // 不用 show()：那会让它变成键盘焦点窗口
    }
}

/// 打开或关掉刘海动画；这块屏没有刘海时什么也不做
#[tauri::command]
fn set_notch(app: AppHandle, on: bool) -> Result<(), String> {
    #[cfg(target_os = "macos")]
    {
        let current = app.get_webview_window("notch");
        if !on {
            if let Some(w) = current {
                let _ = w.close();
            }
            return Ok(());
        }
        let Some(g) = notch::geometry() else { return Ok(()) };
        let w = match current {
            Some(w) => w,
            None => WebviewWindowBuilder::new(&app, "notch", WebviewUrl::App(format!("index.html?win=notch&w={}&h={}", g.width, g.height).into()))
                .title("")
                .decorations(false)
                .transparent(true)
                .shadow(false)
                .resizable(false)
                .skip_taskbar(true)
                .focused(false)
                .visible(false)
                .build()
                .map_err(|e| e.to_string())?,
        };
        notch::place(&w, &g);
    }
    #[cfg(not(target_os = "macos"))]
    let _ = (app, on);
    Ok(())
}

/// 两耳伸出来之前重新对位：换了屏幕或合盖后刘海可能不在原处，也可能没有了
#[tauri::command]
fn place_notch(app: AppHandle) {
    #[cfg(target_os = "macos")]
    if let Some(w) = app.get_webview_window("notch") {
        match notch::geometry() {
            Some(g) => {
                notch::place(&w, &g);
                let _ = w.emit("imls://notch-size", (g.width, g.height)); // 改了显示缩放，刘海的点数会变
            }
            None => {
                let _ = w.hide();
            }
        }
    }
    #[cfg(not(target_os = "macos"))]
    let _ = app;
}

/// 托盘图标在屏幕上的矩形（物理像素），每次托盘事件都更新；弹窗按它摆位
#[derive(Clone, Copy, Debug)]
struct TrayRect {
    x: i32,
    y: i32,
    w: i32,
    h: i32,
}

static TRAY_RECT: Mutex<Option<TrayRect>> = Mutex::new(None);

/// 弹出窗口摆在哪
enum Place {
    /// 贴着托盘图标；没点过托盘（快捷键、界面里唤起）时放到鼠标所在屏幕的顶部
    Tray,
    /// 屏幕正中
    Center,
}

fn flyout(label: &str) -> Option<&'static Flyout> {
    match label {
        "popover" => Some(&POPOVER),
        "quick" => Some(&QUICK),
        _ => None,
    }
}

/// 菜单栏图标的样式和状态，由界面按设置和后台状态告诉我们；样式见 icons/tray/ 下的目录
static TRAY: Mutex<(String, String)> = Mutex::new((String::new(), String::new()));

/// 一种样式四个状态的 2x 位图：正常、告警、出错、暂停
macro_rules! tray_set {
    ($dir:literal) => {
        [
            include_bytes!(concat!("../icons/tray/", $dir, "/tray@2x.png")) as &[u8],
            include_bytes!(concat!("../icons/tray/", $dir, "/tray-warn@2x.png")),
            include_bytes!(concat!("../icons/tray/", $dir, "/tray-err@2x.png")),
            include_bytes!(concat!("../icons/tray/", $dir, "/tray-off@2x.png")),
        ]
    };
}

fn tray_bytes(style: &str, status: &str) -> &'static [u8] {
    let set: [&'static [u8]; 4] = match style {
        "frame" => tray_set!("frame"),
        "fork" => tray_set!("fork"),
        "patchbay" => tray_set!("patchbay"),
        "lever" => tray_set!("lever"),
        "breakout" => tray_set!("breakout"),
        "pixel" => tray_set!("pixel"),
        "ladder" => tray_set!("ladder"),
        _ => tray_set!("tile"),
    };
    set[match status {
        "warn" => 1,
        "error" => 2,
        "off" => 3,
        _ => 0,
    }]
}

fn tray_icon(style: &str, status: &str) -> (Image<'static>, bool) {
    // 模板图只能单色，所以「额度紧张 / 出错 / 未运行」三种带色状态用非模板图。
    // 一律给 2x 位图：tray-icon 在 macOS 上总把图设成 18 pt 高，给 18 px 的图在 Retina 上会被拉糊
    (Image::from_bytes(tray_bytes(style, status)).expect("tray icon"), !matches!(status, "warn" | "error" | "off"))
}

fn apply_tray(app: &AppHandle) {
    if let Some(tray) = app.tray_by_id(TRAY_ID) {
        let (style, status) = TRAY.lock().map(|t| t.clone()).unwrap_or_default();
        let (icon, template) = tray_icon(&style, &status);
        // tray-icon 的 set_icon 会把新图设成非模板，先设模板标记再换图等于没设：模板图会被画成黑底。
        // macOS 用原子接口一次设好；其它平台没有模板图这回事
        #[cfg(target_os = "macos")]
        let _ = tray.set_icon_with_as_template(Some(icon), template);
        #[cfg(not(target_os = "macos"))]
        let _ = tray.set_icon(Some(icon));
    }
}

fn toggle_window(app: &AppHandle, label: &str, place: Place) {
    if let Some(w) = app.get_webview_window(label) {
        let f = flyout(label);
        if let Some(f) = f {
            let just_blurred = f.blurred_at.lock().unwrap().take().is_some_and(|t| t.elapsed() < Duration::from_millis(400));
            if just_blurred {
                trace(app, &format!("toggle {label}: just blurred, treat as close"));
                return;
            }
        }
        let armed = f.map_or(true, |f| f.armed.load(Ordering::SeqCst));
        if w.is_visible().unwrap_or(false) && (w.is_focused().unwrap_or(false) || !armed) {
            trace(app, &format!("toggle {label}: hide (focused={:?} armed={armed})", w.is_focused().ok()));
            let _ = w.hide();
            return;
        }
        if let Some(f) = f {
            f.armed.store(false, Ordering::SeqCst);
        }
        let anchor = match place {
            Place::Tray => tray_anchor(app, &w),
            Place::Center => None,
        };
        match anchor {
            Some(p) => {
                let _ = w.set_position(p);
            }
            None => {
                let _ = w.center();
            }
        }
        if let Some(f) = f {
            *f.anchor.lock().unwrap() = anchor;
        }
        let _ = w.show();
        let _ = w.set_focus();
        let _ = w.emit("imls://shown", ());
        // 位置不在这里读：macOS 上移窗、显示都是排进主线程队列异步做的，此刻读到的还是旧坐标，以上面 anchor 那行为准
        trace(app, &format!("show {label} visible={:?} focused={:?}", w.is_visible().ok(), w.is_focused().ok()));
    }
}

/// 弹窗的左上角：横向对准托盘图标中心，纵向贴在菜单栏下沿（托盘在屏幕下半部时放到图标上面），
/// 整个窗口留在那块显示器的工作区里。没点过托盘就按鼠标所在的屏幕放到顶部。
/// 不问窗口自己在哪块显示器：从没显示过的窗口答不出来，定位插件就是栽在这，启动后第一次弹出总落在屏幕正中。
fn tray_anchor(app: &AppHandle, w: &tauri::WebviewWindow) -> Option<tauri::Position> {
    let monitors = w.available_monitors().ok()?;
    let contains = |m: &tauri::Monitor, x: i32, y: i32| {
        let (p, s) = (m.position(), m.size());
        x >= p.x && y >= p.y && x < p.x + s.width as i32 && y < p.y + s.height as i32
    };
    // 图标被挤进菜单栏的溢出区时矩形可能是空的，那就按鼠标位置摆
    let tray = (*TRAY_RECT.lock().unwrap()).filter(|t| t.w > 0 && t.h > 0);
    let cursor = app.cursor_position().ok().map(|c| (c.x as i32, c.y as i32));
    let (ax, ay) = match (tray, cursor) {
        (Some(t), _) => (t.x + t.w / 2, t.y + t.h / 2),
        (None, Some(c)) => c,
        (None, None) => return None,
    };
    let m = monitors
        .iter()
        .find(|m| contains(m, ax, ay))
        .or_else(|| cursor.and_then(|(cx, cy)| monitors.iter().find(|m| contains(m, cx, cy))))
        .or_else(|| monitors.first())?;
    let scale = m.scale_factor();
    // 窗口尺寸按目标显示器的缩放换算：两块屏缩放不同时，窗口自己报的物理尺寸是按它当前那块屏算的
    let size = w.outer_size().ok()?;
    let wsf = w.scale_factor().unwrap_or(scale);
    let (ww, wh) = ((size.width as f64 / wsf * scale).round() as i32, (size.height as f64 / wsf * scale).round() as i32);
    let (mp, ms) = (m.position(), m.size());
    let area = m.work_area();
    let (area_x, area_y, area_w, area_h) = if area.size.width > 0 && area.size.height > 0 {
        (area.position.x, area.position.y, area.size.width as i32, area.size.height as i32)
    } else {
        (mp.x, mp.y, ms.width as i32, ms.height as i32)
    };
    let x = (ax - ww / 2).clamp(area_x, (area_x + area_w - ww).max(area_x));
    let y = match tray {
        Some(t) if ay >= mp.y + ms.height as i32 / 2 => (t.y.min(area_y + area_h) - wh).max(area_y),
        Some(t) => (t.y + t.h).max(area_y),
        None => area_y,
    };
    trace(app, &format!("anchor ({x},{y}) tray={tray:?} window={ww}x{wh} monitor={:?} area=({area_x},{area_y} {area_w}x{area_h}) scale={scale}", m.name()));
    // macOS 的窗口坐标按点算，物理像素除以这块屏的缩放才准；其它平台直接用物理像素
    Some(if cfg!(target_os = "macos") {
        tauri::Position::Logical(tauri::LogicalPosition::new(x as f64 / scale, y as f64 / scale))
    } else {
        tauri::Position::Physical(tauri::PhysicalPosition::new(x, y))
    })
}

/// 弹窗按内容改高度。macOS 改窗口大小固定的是左下角，顶边会跟着漂；改完把左上角摆回原处
#[tauri::command]
fn resize_self(window: tauri::WebviewWindow, width: f64, height: f64) {
    let _ = window.set_size(tauri::LogicalSize::new(width, height));
    if let Some(p) = flyout(window.label()).and_then(|f| *f.anchor.lock().unwrap()) {
        let _ = window.set_position(p);
    }
}

/// 托盘与弹窗的动作记一行到数据目录的 logs/app.log：面板「不见了」时看这里
fn trace(app: &AppHandle, msg: &str) {
    use std::io::Write;
    let dir = if cfg!(target_os = "macos") {
        app.path().home_dir().ok().map(|h| h.join("Library/Application Support/Switchboard/logs"))
    } else {
        app.path().app_log_dir().ok()
    };
    let Some(dir) = dir else { return };
    let _ = std::fs::create_dir_all(&dir);
    if let Ok(mut f) = std::fs::OpenOptions::new().create(true).append(true).open(dir.join("app.log")) {
        let ts = std::time::SystemTime::now().duration_since(std::time::UNIX_EPOCH).map(|d| d.as_secs()).unwrap_or(0);
        let _ = writeln!(f, "{ts} {msg}");
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
    toggle_window(&app, "quick", Place::Center);
}

#[tauri::command]
fn toggle_popover(app: AppHandle) {
    toggle_window(&app, "popover", Place::Tray);
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
    if let Ok(mut t) = TRAY.lock() {
        t.1 = status;
    }
    apply_tray(&app);
    if let (Some(tray), Some(t)) = (app.tray_by_id(TRAY_ID), tooltip) {
        let _ = tray.set_tooltip(Some(t));
    }
}

/// 换菜单栏图标的样式（设置里的实验功能）；状态照旧
#[tauri::command]
fn set_tray_style(app: AppHandle, style: String) {
    if let Ok(mut t) = TRAY.lock() {
        t.0 = style;
    }
    apply_tray(&app);
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
        .plugin(tauri_plugin_clipboard_manager::init())
        .plugin(tauri_plugin_dialog::init())
        .plugin(tauri_plugin_opener::init())
        .plugin(
            tauri_plugin_global_shortcut::Builder::new()
                .with_handler(|app, _shortcut, event| {
                    if event.state() == ShortcutState::Pressed {
                        toggle_window(app, "quick", Place::Center);
                    }
                })
                .build(),
        )
        .invoke_handler(tauri::generate_handler![
            show_main,
            toggle_quick,
            toggle_popover,
            hide_window,
            resize_self,
            set_notch,
            place_notch,
            set_hotkey,
            set_tray_status,
            set_tray_style,
            start_backend,
            bundled_backend_build,
            show_about,
            copy_image,
            app_version
        ])
        .setup(|app| {
            let handle = app.handle().clone();

            // 托盘：左键开合弹窗，右键给一个最小菜单
            let (icon, _) = tray_icon("", "ok");
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
                    // 图标的矩形每次事件都带着，记下来给弹窗摆位用（tray-icon 给的就是物理像素）
                    if let TrayIconEvent::Click { rect, .. } | TrayIconEvent::Enter { rect, .. } | TrayIconEvent::Move { rect, .. } | TrayIconEvent::Leave { rect, .. } = &event {
                        let (p, s) = (rect.position.to_physical::<i32>(1.0), rect.size.to_physical::<u32>(1.0));
                        *TRAY_RECT.lock().unwrap() = Some(TrayRect { x: p.x, y: p.y, w: s.width as i32, h: s.height as i32 });
                    }
                    if let TrayIconEvent::Click { button: MouseButton::Left, button_state: MouseButtonState::Up, position, rect, .. } = event {
                        trace(tray.app_handle(), &format!("tray click at {:?} rect={:?}", position, rect.position));
                        toggle_window(tray.app_handle(), "popover", Place::Tray);
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
                            trace(&w2.app_handle().clone(), &format!("{label}: blurred, hide"));
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
