mod companion_data;
mod companion_runtime;

use std::{
    env, fs,
    path::{Path, PathBuf},
    process::Command,
    sync::Mutex,
    time::Duration,
};

use rusqlite::{params, Connection, OpenFlags, OptionalExtension, TransactionBehavior};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use tauri::{
    image::Image,
    menu::{Menu, MenuItem},
    tray::TrayIconBuilder,
    Manager,
};

const COMPANION_POLICY_VERSION: i64 = 1;
const NATIVE_HOST_NAME: &str = "com.codelogiic.nexus.companion";
const REGISTRATION_HELPER_ENV: &str = "NEXUS_COMPANION_NATIVE_HOST_REGISTRATION_HELPER";
const COLLECTION_DISABLED_LABEL: &str = "Collection: Disabled";
const COLLECTION_DISABLED_TOOLTIP: &str = "NEXUS Companion — collection disabled";
const TOOL_IDS: [(&str, &str); 5] = [
    ("chatgpt", "ChatGPT"),
    ("claude", "Claude"),
    ("gemini", "Gemini"),
    ("copilot", "Microsoft Copilot"),
    ("perplexity", "Perplexity"),
];
const ADAPTERS: [(&str, &str); 3] = [
    ("browser-chrome", "Chrome browser"),
    ("browser-edge", "Edge browser"),
    ("desktop-foreground-app", "Desktop foreground adapter"),
];

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
enum StoreState {
    Ready,
    Unavailable,
    Error,
}

impl StoreState {
    fn as_str(self) -> &'static str {
        match self {
            Self::Ready => "ready",
            Self::Unavailable => "unavailable",
            Self::Error => "error",
        }
    }
}

#[derive(Clone, Debug, PartialEq, Eq, Serialize)]
#[serde(rename_all = "camelCase")]
struct ConsentStatus {
    adapter_id: &'static str,
    adapter: &'static str,
    tool_id: &'static str,
    tool: &'static str,
    state: &'static str,
}

#[derive(Clone, Debug, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
struct SetConsentRequest {
    adapter_id: String,
    tool_id: String,
    enabled: bool,
    policy_version: i64,
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
struct NativeHostStatus {
    chrome: &'static str,
    edge: &'static str,
    registration_control: &'static str,
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
struct DashboardStatus {
    collection: &'static str,
    store: &'static str,
    consents: Vec<ConsentStatus>,
    native_host: NativeHostStatus,
    data_controls: companion_data::DataControls,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct NativeHostRegistrationRequest {
    browser: String,
    extension_id: String,
    host_path: String,
}

struct TrayState {
    status_item: Mutex<Option<MenuItem<tauri::Wry>>>,
}

fn collection_disabled_icon() -> Image<'static> {
    const SIDE: usize = 32;
    let mut rgba = vec![0; SIDE * SIDE * 4];

    for y in 0..SIDE {
        for x in 0..SIDE {
            let index = (y * SIDE + x) * 4;
            let is_nexus_mark = (x == 8 && (7..25).contains(&y))
                || (x == 23 && (7..25).contains(&y))
                || (x == y && (7..25).contains(&x))
                || (x + y == 31 && (7..25).contains(&x));
            if is_nexus_mark {
                rgba[index..index + 4].copy_from_slice(&[92, 106, 196, 255]);
            }
        }
    }

    Image::new_owned(rgba, SIDE as u32, SIDE as u32)
}

fn show_dashboard(app: &tauri::AppHandle) {
    if let Some(window) = app.get_webview_window("main") {
        let _ = window.unminimize();
        let _ = window.show();
        let _ = window.set_focus();
    }
}

fn companion_database_path() -> Result<PathBuf, ()> {
    Ok(PathBuf::from(env::var_os("HOME").ok_or(())?)
        .join(".config/nexus/logs/observability.sqlite"))
}

fn open_existing_database() -> Result<Connection, StoreState> {
    let database_path = companion_database_path().map_err(|_| StoreState::Unavailable)?;
    if !database_path.is_file() {
        return Err(StoreState::Unavailable);
    }
    let connection = Connection::open_with_flags(
        database_path,
        OpenFlags::SQLITE_OPEN_READ_WRITE | OpenFlags::SQLITE_OPEN_NO_MUTEX,
    )
    .map_err(|_| StoreState::Error)?;
    connection
        .busy_timeout(Duration::from_millis(5000))
        .map_err(|_| StoreState::Error)?;
    Ok(connection)
}

fn fixed_consents(state: &'static str) -> Vec<ConsentStatus> {
    ADAPTERS
        .iter()
        .flat_map(|&(adapter_id, adapter)| {
            TOOL_IDS.iter().map(move |&(tool_id, tool)| ConsentStatus {
                adapter_id,
                adapter,
                tool_id,
                tool,
                state,
            })
        })
        .collect()
}

fn read_consents(database: &Connection) -> Result<Vec<ConsentStatus>, StoreState> {
    let mut statement = database
        .prepare(
            "SELECT enabled FROM companion_tool_consents
             WHERE adapter_id = ?1 AND tool_id = ?2 AND consent_policy_version = ?3",
        )
        .map_err(|_| StoreState::Error)?;
    let mut consents = Vec::with_capacity(ADAPTERS.len() * TOOL_IDS.len());

    for (adapter_id, adapter) in ADAPTERS {
        for (tool_id, tool) in TOOL_IDS {
            let state = match statement.query_row(
                params![adapter_id, tool_id, COMPANION_POLICY_VERSION],
                |row| row.get::<_, i64>(0),
            ) {
                Ok(1) => "enabled",
                Ok(0) | Err(rusqlite::Error::QueryReturnedNoRows) => "disabled",
                Ok(_) | Err(_) => return Err(StoreState::Error),
            };
            consents.push(ConsentStatus {
                adapter_id,
                adapter,
                tool_id,
                tool,
                state,
            });
        }
    }
    Ok(consents)
}

fn validate_consent_request(request: &SetConsentRequest) -> Result<(), StoreState> {
    if request.policy_version != COMPANION_POLICY_VERSION {
        return Err(StoreState::Error);
    }
    if !matches!(
        request.adapter_id.as_str(),
        "browser-chrome" | "browser-edge"
    ) {
        return Err(StoreState::Error);
    }
    if !TOOL_IDS
        .iter()
        .any(|(id, _)| *id == request.tool_id.as_str())
    {
        return Err(StoreState::Error);
    }
    Ok(())
}

fn set_consent_in_database(
    database: &mut Connection,
    request: &SetConsentRequest,
) -> Result<(), StoreState> {
    validate_consent_request(request)?;
    let transaction = database
        .transaction_with_behavior(TransactionBehavior::Immediate)
        .map_err(|_| StoreState::Error)?;
    let setting: i64 = transaction
        .query_row(
            "SELECT collection_enabled FROM companion_settings WHERE id = 1",
            [],
            |row| row.get(0),
        )
        .map_err(|_| StoreState::Error)?;
    if !matches!(setting, 0 | 1) {
        return Err(StoreState::Error);
    }
    let enabled_val: i64 = if request.enabled { 1 } else { 0 };
    transaction
        .execute(
            "INSERT INTO companion_tool_consents (
                adapter_id, tool_id, enabled, consent_policy_version, updated_at
            ) VALUES (?1, ?2, ?3, ?4, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
            ON CONFLICT(adapter_id, tool_id) DO UPDATE SET
                enabled = excluded.enabled,
                consent_policy_version = excluded.consent_policy_version,
                updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')",
            params![
                request.adapter_id,
                request.tool_id,
                enabled_val,
                request.policy_version,
            ],
        )
        .map_err(|_| StoreState::Error)?;
    transaction.commit().map_err(|_| StoreState::Error)?;
    Ok(())
}

fn pause_collection_in_database(database: &mut Connection) -> Result<(), StoreState> {
    let transaction = database
        .transaction_with_behavior(TransactionBehavior::Immediate)
        .map_err(|_| StoreState::Error)?;
    let setting_rows = transaction
        .execute(
            "UPDATE companion_settings
             SET collection_enabled = 0, collection_started_at = NULL, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
             WHERE id = 1",
            [],
        )
        .map_err(|_| StoreState::Error)?;
    if setting_rows != 1 {
        return Err(StoreState::Error);
    }
    transaction.commit().map_err(|_| StoreState::Error)?;
    Ok(())
}

fn resume_collection_in_database(database: &mut Connection) -> Result<(), StoreState> {
    let transaction = database
        .transaction_with_behavior(TransactionBehavior::Immediate)
        .map_err(|_| StoreState::Error)?;
    let setting_exists: bool = transaction
        .query_row("SELECT 1 FROM companion_settings WHERE id = 1", [], |_| {
            Ok(true)
        })
        .optional()
        .map_err(|_| StoreState::Error)?
        .unwrap_or(false);
    if !setting_exists {
        return Err(StoreState::Error);
    }

    let eligible_count: i64 = transaction
        .query_row(
            "SELECT COUNT(*) FROM companion_tool_consents
             WHERE enabled = 1
               AND consent_policy_version = ?1
               AND adapter_id IN ('browser-chrome', 'browser-edge')
               AND tool_id IN ('chatgpt', 'claude', 'gemini', 'copilot', 'perplexity')",
            params![COMPANION_POLICY_VERSION],
            |row| row.get(0),
        )
        .map_err(|_| StoreState::Error)?;
    if eligible_count <= 0 {
        return Err(StoreState::Error);
    }

    let setting_rows = transaction
        .execute(
            "UPDATE companion_settings
             SET collection_enabled = 1, collection_started_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
             WHERE id = 1",
            [],
        )
        .map_err(|_| StoreState::Error)?;
    if setting_rows != 1 {
        return Err(StoreState::Error);
    }
    transaction.commit().map_err(|_| StoreState::Error)?;
    Ok(())
}

fn disable_collection_in_database(database: &mut Connection) -> Result<(), StoreState> {
    let transaction = database
        .transaction_with_behavior(TransactionBehavior::Immediate)
        .map_err(|_| StoreState::Error)?;
    let setting_rows = transaction
        .execute(
            "UPDATE companion_settings
             SET collection_enabled = 0, collection_started_at = NULL, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
             WHERE id = 1",
            [],
        )
        .map_err(|_| StoreState::Error)?;
    if setting_rows != 1 {
        return Err(StoreState::Error);
    }
    transaction
        .execute(
            "UPDATE companion_tool_consents
             SET enabled = 0, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')",
            [],
        )
        .map_err(|_| StoreState::Error)?;
    transaction.commit().map_err(|_| StoreState::Error)
}

fn disable_collection() -> Result<(), StoreState> {
    let mut database = open_existing_database()?;
    disable_collection_in_database(&mut database)
}

fn extension_id_is_valid(value: &str) -> bool {
    value.len() == 32 && value.bytes().all(|byte| (b'a'..=b'p').contains(&byte))
}

fn native_manifest_path(browser: &str) -> Result<PathBuf, ()> {
    let home = PathBuf::from(env::var_os("HOME").ok_or(())?);
    let directory = match (env::consts::OS, browser) {
        ("linux", "chrome") => home.join(".config/google-chrome/NativeMessagingHosts"),
        ("linux", "edge") => home.join(".config/microsoft-edge/NativeMessagingHosts"),
        ("macos", "chrome") => {
            home.join("Library/Application Support/Google/Chrome/NativeMessagingHosts")
        }
        ("macos", "edge") => {
            home.join("Library/Application Support/Microsoft Edge/NativeMessagingHosts")
        }
        _ => return Err(()),
    };
    Ok(directory.join(format!("{NATIVE_HOST_NAME}.json")))
}

fn manifest_is_valid(path: &Path) -> Result<bool, ()> {
    let manifest: Value =
        serde_json::from_str(&fs::read_to_string(path).map_err(|_| ())?).map_err(|_| ())?;
    let origins = manifest
        .get("allowed_origins")
        .and_then(Value::as_array)
        .ok_or(())?;
    let origins_are_valid = !origins.is_empty()
        && origins.iter().all(|origin| {
            let Some(origin) = origin.as_str() else {
                return false;
            };
            origin
                .strip_prefix("chrome-extension://")
                .and_then(|value| value.strip_suffix('/'))
                .is_some_and(extension_id_is_valid)
        });
    let host_path_is_absolute = manifest
        .get("path")
        .and_then(Value::as_str)
        .is_some_and(|value| Path::new(value).is_absolute());

    Ok(
        manifest.get("name").and_then(Value::as_str) == Some(NATIVE_HOST_NAME)
            && manifest.get("type").and_then(Value::as_str) == Some("stdio")
            && origins_are_valid
            && host_path_is_absolute,
    )
}

fn native_host_state(browser: &str) -> &'static str {
    let Ok(path) = native_manifest_path(browser) else {
        return "unavailable";
    };
    if !path.exists() {
        return "unregistered";
    }
    match manifest_is_valid(&path) {
        Ok(true) => "registered",
        Ok(false) | Err(()) => "error",
    }
}

fn registration_helper_is_available() -> bool {
    env::var_os(REGISTRATION_HELPER_ENV)
        .map(PathBuf::from)
        .is_some_and(|path| path.is_absolute() && path.is_file())
        && companion_runtime::node_executable().is_ok()
}

fn native_host_status() -> NativeHostStatus {
    NativeHostStatus {
        chrome: native_host_state("chrome"),
        edge: native_host_state("edge"),
        registration_control: if registration_helper_is_available() {
            "available"
        } else {
            "unavailable"
        },
    }
}

fn unavailable_dashboard(store: StoreState) -> DashboardStatus {
    DashboardStatus {
        collection: "disabled",
        store: store.as_str(),
        consents: fixed_consents("unavailable"),
        native_host: native_host_status(),
        data_controls: companion_data::DataControls::unavailable(),
    }
}

fn dashboard_status() -> DashboardStatus {
    let database = match open_existing_database() {
        Ok(database) => database,
        Err(state) => return unavailable_dashboard(state),
    };
    let collection_enabled = match database.query_row(
        "SELECT collection_enabled FROM companion_settings WHERE id = 1 AND
         (collection_enabled = 0 OR julianday(collection_started_at) IS NOT NULL)",
        [],
        |row| row.get::<_, i64>(0),
    ) {
        Ok(0) => false,
        Ok(1) => true,
        Ok(_) | Err(_) => return unavailable_dashboard(StoreState::Error),
    };
    let consents = match read_consents(&database) {
        Ok(consents) => consents,
        Err(state) => return unavailable_dashboard(state),
    };

    DashboardStatus {
        collection: if collection_enabled {
            "enabled"
        } else {
            "disabled"
        },
        store: StoreState::Ready.as_str(),
        consents,
        native_host: native_host_status(),
        data_controls: companion_data::status(),
    }
}

fn tray_tooltip(status: &DashboardStatus) -> &'static str {
    if status.store != "ready" {
        return "NEXUS Companion — collection status unavailable";
    }
    match status.collection {
        "enabled" => "NEXUS Companion — collection enabled",
        "disabled" => COLLECTION_DISABLED_TOOLTIP,
        _ => "NEXUS Companion — collection status unavailable",
    }
}

fn collection_menu_label(status: &DashboardStatus) -> &'static str {
    if status.store != "ready" {
        return "Collection: Status unavailable";
    }
    match status.collection {
        "enabled" => "Collection: Enabled",
        "disabled" => COLLECTION_DISABLED_LABEL,
        _ => "Collection: Status unavailable",
    }
}

fn refresh_tray_status(app: &tauri::AppHandle) {
    let status = dashboard_status();
    if let Some(tray) = app.tray_by_id("companion-status") {
        let _ = tray.set_tooltip(Some(tray_tooltip(&status)));
    }
    if let Some(state) = app.try_state::<TrayState>() {
        if let Ok(guard) = state.status_item.lock() {
            if let Some(ref item) = *guard {
                let _ = item.set_text(collection_menu_label(&status));
            }
        }
    }
}

fn safe_error(state: StoreState) -> &'static str {
    match state {
        StoreState::Unavailable => {
            "The local NEXUS observability store is unavailable; no setting was changed."
        }
        StoreState::Error => {
            "The local NEXUS observability store could not be updated; no setting was changed."
        }
        StoreState::Ready => "The requested local action could not be completed.",
    }
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct RetentionRequest {
    days: u16,
}
#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct ClearHistoryRequest {
    confirmed: bool,
}
#[tauri::command]
fn set_companion_retention(request: RetentionRequest) -> Result<DashboardStatus, String> {
    companion_data::set_retention(request.days)?;
    Ok(dashboard_status())
}
#[tauri::command]
fn prune_companion_history() -> Result<DashboardStatus, String> {
    companion_data::prune()?;
    Ok(dashboard_status())
}
#[tauri::command]
fn clear_companion_history(request: ClearHistoryRequest) -> Result<DashboardStatus, String> {
    companion_data::clear(request.confirmed)?;
    Ok(dashboard_status())
}

#[tauri::command]
fn get_companion_dashboard() -> DashboardStatus {
    dashboard_status()
}

#[tauri::command]
fn disable_companion_collection(app: tauri::AppHandle) -> Result<DashboardStatus, String> {
    disable_collection().map_err(safe_error)?;
    refresh_tray_status(&app);
    Ok(dashboard_status())
}

#[tauri::command]
fn pause_companion_collection(app: tauri::AppHandle) -> Result<DashboardStatus, String> {
    let mut database = open_existing_database().map_err(safe_error)?;
    pause_collection_in_database(&mut database).map_err(safe_error)?;
    refresh_tray_status(&app);
    Ok(dashboard_status())
}

#[tauri::command]
fn resume_companion_collection(app: tauri::AppHandle) -> Result<DashboardStatus, String> {
    let mut database = open_existing_database().map_err(safe_error)?;
    resume_collection_in_database(&mut database).map_err(safe_error)?;
    refresh_tray_status(&app);
    Ok(dashboard_status())
}

#[tauri::command]
fn set_companion_consent(
    app: tauri::AppHandle,
    request: SetConsentRequest,
) -> Result<DashboardStatus, String> {
    validate_consent_request(&request).map_err(safe_error)?;
    let mut database = open_existing_database().map_err(safe_error)?;
    set_consent_in_database(&mut database, &request).map_err(safe_error)?;
    refresh_tray_status(&app);
    Ok(dashboard_status())
}

#[tauri::command]
fn register_native_host(
    app: tauri::AppHandle,
    request: NativeHostRegistrationRequest,
) -> Result<DashboardStatus, String> {
    if !matches!(request.browser.as_str(), "chrome" | "edge")
        || !extension_id_is_valid(&request.extension_id)
        || !Path::new(&request.host_path).is_absolute()
        || !Path::new(&request.host_path).is_file()
    {
        return Err("Use a supported browser, a published Chrome-format extension ID, and an existing absolute host path.".into());
    }
    let helper = env::var_os(REGISTRATION_HELPER_ENV)
        .map(PathBuf::from)
        .filter(|path| path.is_absolute() && path.is_file())
        .ok_or("Native-host registration is unavailable in this installation.")?;
    let node = companion_runtime::node_executable().map_err(str::to_owned)?;
    let completed = Command::new(node)
        .arg(helper)
        .arg("install")
        .arg("--browser")
        .arg(&request.browser)
        .arg("--extension-id")
        .arg(&request.extension_id)
        .arg("--host-path")
        .arg(&request.host_path)
        .status()
        .map_err(|_| "Native-host registration could not be started.")?;
    if !completed.success() {
        return Err("Native-host registration was not completed.".into());
    }
    refresh_tray_status(&app);
    Ok(dashboard_status())
}

fn main() {
    tauri::Builder::default()
        .invoke_handler(tauri::generate_handler![
            get_companion_dashboard,
            set_companion_retention,
            prune_companion_history,
            clear_companion_history,
            disable_companion_collection,
            pause_companion_collection,
            resume_companion_collection,
            set_companion_consent,
            register_native_host
        ])
        .setup(|app| {
            let status = dashboard_status();
            let open_dashboard =
                MenuItem::with_id(app, "open-dashboard", "Open Dashboard", true, None::<&str>)?;
            let collection_status = MenuItem::with_id(
                app,
                "collection-status",
                collection_menu_label(&status),
                false,
                None::<&str>,
            )?;
            let disable = MenuItem::with_id(
                app,
                "disable-collection",
                "Disable collection and revoke consents",
                true,
                None::<&str>,
            )?;
            let quit = MenuItem::with_id(app, "quit", "Quit", true, None::<&str>)?;
            let menu =
                Menu::with_items(app, &[&open_dashboard, &collection_status, &disable, &quit])?;

            app.manage(TrayState {
                status_item: Mutex::new(Some(collection_status)),
            });

            let tray_result = TrayIconBuilder::with_id("companion-status")
                .icon(collection_disabled_icon())
                .tooltip(tray_tooltip(&status))
                .menu(&menu)
                .show_menu_on_left_click(true)
                .on_menu_event(move |app, event| match event.id.as_ref() {
                    "open-dashboard" => show_dashboard(app),
                    "disable-collection" => {
                        let _ = disable_collection();
                        refresh_tray_status(app);
                    }
                    "quit" => app.exit(0),
                    _ => {}
                })
                .build(app);

            if tray_result.is_err() {
                eprintln!("Tray is unavailable; the dashboard remains usable.");
            }
            Ok(())
        })
        .run(tauri::generate_context!())
        .expect("NEXUS Companion desktop shell failed to start");
}

#[cfg(test)]
mod tests {
    use super::*;

    const COMPANION_MIGRATIONS: &str = concat!(
        include_str!("../../../../tools/mcp/migrations/004_companion-activity.sql"),
        "\n",
        include_str!("../../../../tools/mcp/migrations/005_companion-collection-boundary.sql")
    );

    #[test]
    fn desktop_resume_boundary_is_enforced_by_the_node_store() {
        let root = PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../../..");
        let home = env::temp_dir().join(format!(
            "nexus-desktop-boundary-{}-{}",
            std::process::id(),
            std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .unwrap()
                .as_nanos()
        ));
        fs::create_dir_all(&home).unwrap();
        struct Cleanup(PathBuf);
        impl Drop for Cleanup {
            fn drop(&mut self) {
                let _ = fs::remove_dir_all(&self.0);
            }
        }
        let _cleanup = Cleanup(home.clone());
        let setup = Command::new("node").args(["--input-type=module", "-e", "import {createObservabilityStore} from './tools/mcp/lib/observability-store.mjs';createObservabilityStore().migrate();"])
            .env("HOME", &home).current_dir(&root).output().unwrap();
        assert!(setup.status.success());
        let mut database =
            Connection::open(home.join(".config/nexus/logs/observability.sqlite")).unwrap();
        let grant = SetConsentRequest {
            adapter_id: "browser-chrome".into(),
            tool_id: "chatgpt".into(),
            enabled: true,
            policy_version: 1,
        };
        set_consent_in_database(&mut database, &grant).unwrap();
        resume_collection_in_database(&mut database).unwrap();
        let first: String = database
            .query_row(
                "SELECT collection_started_at FROM companion_settings",
                [],
                |row| row.get(0),
            )
            .unwrap();
        let ingest = |start: &str, expect: &str| {
            let script = "import {createObservabilityStore} from './tools/mcp/lib/observability-store.mjs';const start=process.argv[1],end=new Date(Date.parse(start)+1000).toISOString();const store=createObservabilityStore({now:()=>Date.parse(end)});const r=store.recordToolActivity({tool_id:'chatgpt',surface:'browser',started_at:start,ended_at:end,detector:'selected-browser-tab',confidence:'surface-active',browser_family:'chrome',platform:'linux',schema_version:1,consent_policy_version:1});process.stdout.write(r.sqlite.ok?'accepted':r.sqlite.error);";
            let result = Command::new("node")
                .args(["--input-type=module", "-e", script, start])
                .env("HOME", &home)
                .current_dir(&root)
                .output()
                .unwrap();
            assert!(result.status.success());
            assert_eq!(String::from_utf8(result.stdout).unwrap(), expect);
        };
        ingest(&first, "accepted");
        pause_collection_in_database(&mut database).unwrap();
        let paused: Option<String> = database
            .query_row(
                "SELECT collection_started_at FROM companion_settings",
                [],
                |row| row.get(0),
            )
            .unwrap();
        assert_eq!(paused, None);
        ingest(&first, "companion_collection_disabled");
        std::thread::sleep(Duration::from_millis(5));
        resume_collection_in_database(&mut database).unwrap();
        let second: String = database
            .query_row(
                "SELECT collection_started_at FROM companion_settings",
                [],
                |row| row.get(0),
            )
            .unwrap();
        assert!(second > first);
        ingest(&first, "companion_activity_crosses_boundary");
        ingest(&second, "accepted");
        std::thread::sleep(Duration::from_millis(5));
        set_consent_in_database(&mut database, &grant).unwrap();
        ingest(&second, "companion_activity_crosses_boundary");
        let count: i64 = database
            .query_row("SELECT COUNT(*) FROM tool_activity", [], |row| row.get(0))
            .unwrap();
        assert_eq!(count, 2);
    }

    #[test]
    fn extension_ids_must_be_published_chrome_ids() {
        assert!(extension_id_is_valid("abcdefghijklmnopabcdefghijklmnop"));
        assert!(!extension_id_is_valid("abcdefghijklmnopabcdefghijklmnopq"));
        assert!(!extension_id_is_valid("abcdefghijklmnopabcdefghijklmn0p"));
    }

    #[test]
    fn unavailable_store_fails_closed_for_every_fixed_consent() {
        let status = unavailable_dashboard(StoreState::Unavailable);
        assert_eq!(status.collection, "disabled");
        assert_eq!(status.store, "unavailable");
        assert_eq!(status.consents.len(), ADAPTERS.len() * TOOL_IDS.len());
        assert!(status
            .consents
            .iter()
            .all(|consent| consent.state == "unavailable"));
    }

    #[test]
    fn test_consent_request_validation_and_unknown_serde_fields() {
        let valid_json = r#"{
            "adapterId": "browser-chrome",
            "toolId": "chatgpt",
            "enabled": true,
            "policyVersion": 1
        }"#;
        let req: Result<SetConsentRequest, _> = serde_json::from_str(valid_json);
        assert!(req.is_ok());
        let req = req.unwrap();
        assert_eq!(validate_consent_request(&req), Ok(()));

        let unknown_field_json = r#"{
            "adapterId": "browser-chrome",
            "toolId": "chatgpt",
            "enabled": true,
            "policyVersion": 1,
            "extraField": "malicious"
        }"#;
        let rejected_serde: Result<SetConsentRequest, _> = serde_json::from_str(unknown_field_json);
        assert!(rejected_serde.is_err());

        let desktop_req = SetConsentRequest {
            adapter_id: "desktop-foreground-app".into(),
            tool_id: "chatgpt".into(),
            enabled: true,
            policy_version: 1,
        };
        assert_eq!(
            validate_consent_request(&desktop_req),
            Err(StoreState::Error)
        );

        let unknown_adapter = SetConsentRequest {
            adapter_id: "browser-firefox".into(),
            tool_id: "chatgpt".into(),
            enabled: true,
            policy_version: 1,
        };
        assert_eq!(
            validate_consent_request(&unknown_adapter),
            Err(StoreState::Error)
        );

        let unknown_tool = SetConsentRequest {
            adapter_id: "browser-chrome".into(),
            tool_id: "unknown_ai".into(),
            enabled: true,
            policy_version: 1,
        };
        assert_eq!(
            validate_consent_request(&unknown_tool),
            Err(StoreState::Error)
        );

        let invalid_policy = SetConsentRequest {
            adapter_id: "browser-chrome".into(),
            tool_id: "chatgpt".into(),
            enabled: true,
            policy_version: 2,
        };
        assert_eq!(
            validate_consent_request(&invalid_policy),
            Err(StoreState::Error)
        );
    }

    #[test]
    fn test_grant_without_enabling_using_checked_in_migration() {
        let mut database = Connection::open_in_memory().expect("in-memory database");
        database
            .execute_batch(COMPANION_MIGRATIONS)
            .expect("apply Companion migrations");

        let grant_req = SetConsentRequest {
            adapter_id: "browser-chrome".into(),
            tool_id: "chatgpt".into(),
            enabled: true,
            policy_version: 1,
        };

        set_consent_in_database(&mut database, &grant_req).expect("grant consent");

        let enabled: i64 = database
            .query_row(
                "SELECT enabled FROM companion_tool_consents WHERE adapter_id = 'browser-chrome' AND tool_id = 'chatgpt'",
                [],
                |row| row.get(0),
            )
            .expect("query consent");
        assert_eq!(enabled, 1);

        let collection: i64 = database
            .query_row(
                "SELECT collection_enabled FROM companion_settings WHERE id = 1",
                [],
                |row| row.get(0),
            )
            .expect("query collection");
        assert_eq!(collection, 0);
    }

    #[test]
    fn test_pause_preserves_grant() {
        let mut database = Connection::open_in_memory().expect("in-memory database");
        database
            .execute_batch(COMPANION_MIGRATIONS)
            .expect("apply Companion migrations");

        let grant_req = SetConsentRequest {
            adapter_id: "browser-chrome".into(),
            tool_id: "chatgpt".into(),
            enabled: true,
            policy_version: 1,
        };
        set_consent_in_database(&mut database, &grant_req).expect("grant consent");
        database
            .execute(
                "UPDATE companion_settings SET collection_enabled = 1 WHERE id = 1",
                [],
            )
            .expect("enable collection");

        pause_collection_in_database(&mut database).expect("pause collection");

        let collection: i64 = database
            .query_row(
                "SELECT collection_enabled FROM companion_settings WHERE id = 1",
                [],
                |row| row.get(0),
            )
            .expect("query collection");
        assert_eq!(collection, 0);

        let enabled: i64 = database
            .query_row(
                "SELECT enabled FROM companion_tool_consents WHERE adapter_id = 'browser-chrome' AND tool_id = 'chatgpt'",
                [],
                |row| row.get(0),
            )
            .expect("query consent");
        assert_eq!(enabled, 1);
    }

    #[test]
    fn test_resume_rejects_absent_stale_unknown_and_revoked_consent() {
        let mut database = Connection::open_in_memory().expect("in-memory database");
        database
            .execute_batch(COMPANION_MIGRATIONS)
            .expect("apply Companion migrations");

        // 1. Absent consent
        assert!(resume_collection_in_database(&mut database).is_err());
        let col: i64 = database
            .query_row(
                "SELECT collection_enabled FROM companion_settings WHERE id = 1",
                [],
                |r| r.get(0),
            )
            .unwrap();
        assert_eq!(col, 0);

        // 2. Revoked consent (enabled = 0)
        database
            .execute(
                "INSERT INTO companion_tool_consents (adapter_id, tool_id, enabled, consent_policy_version, updated_at)
                 VALUES ('browser-chrome', 'chatgpt', 0, 1, '2026-10-06T00:00:00Z')",
                [],
            )
            .unwrap();
        assert!(resume_collection_in_database(&mut database).is_err());

        // 3. Stale policy version
        database
            .execute(
                "UPDATE companion_tool_consents SET enabled = 1, consent_policy_version = 2 WHERE adapter_id = 'browser-chrome'",
                [],
            )
            .unwrap();
        assert!(resume_collection_in_database(&mut database).is_err());

        // 4. Desktop-only consent
        database
            .execute("DELETE FROM companion_tool_consents", [])
            .unwrap();
        database
            .execute(
                "INSERT INTO companion_tool_consents (adapter_id, tool_id, enabled, consent_policy_version, updated_at)
                 VALUES ('desktop-foreground-app', 'chatgpt', 1, 1, '2026-10-06T00:00:00Z')",
                [],
            )
            .unwrap();
        assert!(resume_collection_in_database(&mut database).is_err());

        // 5. Unknown tool or adapter
        database
            .execute("DELETE FROM companion_tool_consents", [])
            .unwrap();
        database
            .execute(
                "INSERT INTO companion_tool_consents (adapter_id, tool_id, enabled, consent_policy_version, updated_at)
                 VALUES ('browser-firefox', 'chatgpt', 1, 1, '2026-10-06T00:00:00Z')",
                [],
            )
            .unwrap();
        assert!(resume_collection_in_database(&mut database).is_err());

        // 6. Valid current browser consent enables resume
        database
            .execute("DELETE FROM companion_tool_consents", [])
            .unwrap();
        database
            .execute(
                "INSERT INTO companion_tool_consents (adapter_id, tool_id, enabled, consent_policy_version, updated_at)
                 VALUES ('browser-edge', 'claude', 1, 1, '2026-10-06T00:00:00Z')",
                [],
            )
            .unwrap();
        assert!(resume_collection_in_database(&mut database).is_ok());
        let col: i64 = database
            .query_row(
                "SELECT collection_enabled FROM companion_settings WHERE id = 1",
                [],
                |r| r.get(0),
            )
            .unwrap();
        assert_eq!(col, 1);
    }

    #[test]
    fn test_missing_settings_rollback() {
        let mut database = Connection::open_in_memory().expect("in-memory database");
        database
            .execute_batch(COMPANION_MIGRATIONS)
            .expect("apply Companion migrations");

        database
            .execute(
                "INSERT INTO companion_tool_consents (adapter_id, tool_id, enabled, consent_policy_version, updated_at)
                 VALUES ('browser-chrome', 'chatgpt', 1, 1, '2026-10-06T00:00:00Z')",
                [],
            )
            .unwrap();

        database
            .execute("DELETE FROM companion_settings WHERE id = 1", [])
            .unwrap();

        assert!(resume_collection_in_database(&mut database).is_err());
        assert!(pause_collection_in_database(&mut database).is_err());
        assert!(disable_collection_in_database(&mut database).is_err());
        let grant = SetConsentRequest {
            adapter_id: "browser-edge".into(),
            tool_id: "claude".into(),
            enabled: true,
            policy_version: 1,
        };
        assert!(set_consent_in_database(&mut database, &grant).is_err());
        let count: i64 = database
            .query_row("SELECT COUNT(*) FROM companion_tool_consents", [], |row| {
                row.get(0)
            })
            .unwrap();
        assert_eq!(count, 1);

        let consent_enabled: i64 = database
            .query_row(
                "SELECT enabled FROM companion_tool_consents WHERE tool_id = 'chatgpt'",
                [],
                |r| r.get(0),
            )
            .unwrap();
        assert_eq!(consent_enabled, 1);
    }

    #[test]
    fn test_explicit_disable_revokes_all_consents() {
        let mut database = Connection::open_in_memory().expect("in-memory database");
        database
            .execute_batch(COMPANION_MIGRATIONS)
            .expect("apply Companion migrations");

        database
            .execute(
                "UPDATE companion_settings SET collection_enabled = 1 WHERE id = 1",
                [],
            )
            .unwrap();
        database
            .execute(
                "INSERT INTO companion_tool_consents (adapter_id, tool_id, enabled, consent_policy_version, updated_at)
                 VALUES ('browser-chrome', 'chatgpt', 1, 1, 'before'),
                        ('browser-edge', 'claude', 1, 1, 'before')",
                [],
            )
            .unwrap();

        disable_collection_in_database(&mut database).expect("disable collection");

        let collection: i64 = database
            .query_row(
                "SELECT collection_enabled FROM companion_settings WHERE id = 1",
                [],
                |r| r.get(0),
            )
            .unwrap();
        let enabled_consents: i64 = database
            .query_row(
                "SELECT COUNT(*) FROM companion_tool_consents WHERE enabled = 1",
                [],
                |r| r.get(0),
            )
            .unwrap();
        assert_eq!(collection, 0);
        assert_eq!(enabled_consents, 0);
    }
}
