use std::{
    env, fs,
    path::{Path, PathBuf},
    process::Command,
};

use rusqlite::{params, Connection, OpenFlags};
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
    ("copilot", "GitHub Copilot"),
    ("perplexity", "Perplexity"),
];
const ADAPTERS: [(&str, &str); 3] = [
    ("browser-chrome", "Chrome browser"),
    ("browser-edge", "Edge browser"),
    ("desktop-foreground-app", "Desktop foreground adapter"),
];

#[derive(Clone, Copy, Debug)]
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

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
struct ConsentStatus {
    adapter: &'static str,
    tool: &'static str,
    state: &'static str,
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
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct NativeHostRegistrationRequest {
    browser: String,
    extension_id: String,
    host_path: String,
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
    Connection::open_with_flags(
        database_path,
        OpenFlags::SQLITE_OPEN_READ_WRITE | OpenFlags::SQLITE_OPEN_NO_MUTEX,
    )
    .map_err(|_| StoreState::Error)
}

fn fixed_consents(state: &'static str) -> Vec<ConsentStatus> {
    ADAPTERS
        .iter()
        .flat_map(|(_, adapter)| {
            TOOL_IDS.iter().map(move |(_, tool)| ConsentStatus {
                adapter,
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
                adapter,
                tool,
                state,
            });
        }
    }
    Ok(consents)
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
    }
}

fn dashboard_status() -> DashboardStatus {
    let database = match open_existing_database() {
        Ok(database) => database,
        Err(state) => return unavailable_dashboard(state),
    };
    let collection_enabled = match database.query_row(
        "SELECT collection_enabled FROM companion_settings WHERE id = 1",
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
    }
}

fn disable_collection() -> Result<(), StoreState> {
    let mut database = open_existing_database()?;
    disable_collection_in_database(&mut database)
}

fn disable_collection_in_database(database: &mut Connection) -> Result<(), StoreState> {
    let transaction = database.transaction().map_err(|_| StoreState::Error)?;
    let setting_rows = transaction
        .execute(
            "UPDATE companion_settings
             SET collection_enabled = 0, updated_at = CURRENT_TIMESTAMP
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
             SET enabled = 0, updated_at = CURRENT_TIMESTAMP",
            [],
        )
        .map_err(|_| StoreState::Error)?;
    transaction.commit().map_err(|_| StoreState::Error)
}

fn tray_tooltip(status: &DashboardStatus) -> &'static str {
    match status.collection {
        "enabled" => "NEXUS Companion — collection enabled",
        "disabled" => COLLECTION_DISABLED_TOOLTIP,
        _ => "NEXUS Companion — collection status unavailable",
    }
}

fn collection_menu_label(status: &DashboardStatus) -> &'static str {
    match status.collection {
        "enabled" => "Collection: Enabled",
        "disabled" => COLLECTION_DISABLED_LABEL,
        _ => "Collection: Status unavailable",
    }
}

fn refresh_tray_status(app: &tauri::AppHandle) {
    if let Some(tray) = app.tray_by_id("companion-status") {
        let status = dashboard_status();
        let _ = tray.set_tooltip(Some(tray_tooltip(&status)));
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
    let completed = Command::new("node")
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
            disable_companion_collection,
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
            let collection_status_for_events = collection_status.clone();

            TrayIconBuilder::with_id("companion-status")
                .icon(collection_disabled_icon())
                .tooltip(tray_tooltip(&status))
                .menu(&menu)
                .show_menu_on_left_click(true)
                .on_menu_event(move |app, event| match event.id.as_ref() {
                    "open-dashboard" => show_dashboard(app),
                    "disable-collection" => {
                        let _ = disable_collection();
                        refresh_tray_status(app);
                        let _ = collection_status_for_events
                            .set_text(collection_menu_label(&dashboard_status()));
                    }
                    "quit" => app.exit(0),
                    _ => {}
                })
                .build(app)?;
            Ok(())
        })
        .run(tauri::generate_context!())
        .expect("NEXUS Companion desktop shell failed to start");
}

#[cfg(test)]
mod tests {
    use super::*;

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
    fn explicit_disable_turns_off_the_shared_setting_and_fixed_consents() {
        let mut database = Connection::open_in_memory().expect("in-memory database");
        database
            .execute_batch(
                "CREATE TABLE companion_settings (
                    id INTEGER PRIMARY KEY,
                    collection_enabled INTEGER NOT NULL,
                    updated_at TEXT NOT NULL
                );
                CREATE TABLE companion_tool_consents (
                    adapter_id TEXT NOT NULL,
                    tool_id TEXT NOT NULL,
                    enabled INTEGER NOT NULL,
                    consent_policy_version INTEGER NOT NULL,
                    updated_at TEXT NOT NULL
                );
                INSERT INTO companion_settings VALUES (1, 1, 'before');
                INSERT INTO companion_tool_consents VALUES
                    ('browser-chrome', 'chatgpt', 1, 1, 'before'),
                    ('browser-edge', 'claude', 1, 1, 'before');",
            )
            .expect("test schema");

        disable_collection_in_database(&mut database).expect("disable collection");

        let collection: i64 = database
            .query_row(
                "SELECT collection_enabled FROM companion_settings WHERE id = 1",
                [],
                |row| row.get(0),
            )
            .expect("collection setting");
        let enabled_consents: i64 = database
            .query_row(
                "SELECT COUNT(*) FROM companion_tool_consents WHERE enabled = 1",
                [],
                |row| row.get(0),
            )
            .expect("consent count");
        assert_eq!(collection, 0);
        assert_eq!(enabled_consents, 0);
    }
}
