use std::{
    env,
    path::{Path, PathBuf},
    process::{Command, Stdio},
    thread,
    time::{Duration, Instant},
};

pub const ERROR: &str = "Native-host registration could not be updated.";
const HELPER_ENV: &str = "NEXUS_COMPANION_NATIVE_HOST_REGISTRATION_HELPER";

fn helper_path() -> Result<PathBuf, &'static str> {
    env::var_os(HELPER_ENV)
        .map(PathBuf::from)
        .filter(|path| path.is_absolute() && path.is_file())
        .ok_or(ERROR)
}
pub fn is_available() -> bool {
    cfg!(any(target_os = "linux", target_os = "macos"))
        && !env::var_os("FLATPAK_ID").is_some_and(|value| !value.is_empty())
        && !Path::new("/.flatpak-info").exists()
        && helper_path().is_ok()
        && crate::companion_runtime::node_executable().is_ok()
}
fn execute(command: &mut Command, timeout: Duration) -> Result<(), &'static str> {
    let deadline = Instant::now() + timeout;
    let mut child = command
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .spawn()
        .map_err(|_| ERROR)?;
    loop {
        match child.try_wait() {
            Ok(Some(status)) => return if status.success() { Ok(()) } else { Err(ERROR) },
            Ok(None) if Instant::now() < deadline => thread::sleep(Duration::from_millis(10)),
            _ => {
                let _ = child.kill();
                let _ = child.wait();
                return Err(ERROR);
            }
        }
    }
}
fn run(args: &[&str]) -> Result<(), &'static str> {
    if !is_available() {
        return Err(ERROR);
    }
    let node = crate::companion_runtime::node_executable().map_err(|_| ERROR)?;
    execute(
        Command::new(node).arg(helper_path()?).args(args),
        Duration::from_secs(15),
    )
}
pub fn register(browser: &str, extension_id: &str, host_path: &str) -> Result<(), &'static str> {
    if !matches!(browser, "chrome" | "edge") {
        return Err(ERROR);
    }
    run(&[
        "install",
        "--browser",
        browser,
        "--extension-id",
        extension_id,
        "--host-path",
        host_path,
    ])
}
pub fn unregister(browser: &str) -> Result<(), &'static str> {
    if !matches!(browser, "chrome" | "edge") {
        return Err(ERROR);
    }
    run(&["uninstall", "--browser", browser])
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::{fs, time::SystemTime};
    struct Cleanup(PathBuf);
    impl Drop for Cleanup {
        fn drop(&mut self) {
            let _ = fs::remove_dir_all(&self.0);
        }
    }
    fn fixture() -> Cleanup {
        let path = env::temp_dir().join(format!(
            "nexus-registration-{}-{}",
            std::process::id(),
            SystemTime::now()
                .duration_since(SystemTime::UNIX_EPOCH)
                .unwrap()
                .as_nanos()
        ));
        fs::create_dir(&path).unwrap();
        Cleanup(path)
    }
    #[test]
    fn browser_specific_lifecycle_preserves_other_registration_and_history() {
        let home = fixture();
        let root = PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../../..");
        let helper = root
            .join("apps/companion-native-host/bin/nexus-companion-native-host-registration.mjs");
        let logs = home.0.join(".config/nexus/logs");
        fs::create_dir_all(&logs).unwrap();
        let history = logs.join("observability.sqlite");
        let database = rusqlite::Connection::open(&history).unwrap();
        for name in [
            "001_observability.sql",
            "002_legacy-import-receipts.sql",
            "003_store_meta.sql",
            "004_companion-activity.sql",
            "005_companion-collection-boundary.sql",
        ] {
            database
                .execute_batch(
                    &fs::read_to_string(root.join("tools/mcp/migrations").join(name)).unwrap(),
                )
                .unwrap();
        }
        database.execute_batch("UPDATE companion_settings SET collection_enabled=1,collection_started_at='2026-10-07T12:00:00Z'; INSERT INTO companion_tool_consents VALUES ('browser-chrome','chatgpt',1,1,'2026-10-07T12:00:00Z');").unwrap();
        drop(database);
        let history_before = fs::read(&history).unwrap();
        let call = |args: &[&str]| {
            execute(
                Command::new("node")
                    .arg(&helper)
                    .args(args)
                    .env("HOME", &home.0),
                Duration::from_secs(10),
            )
        };
        for browser in ["chrome", "edge"] {
            let host = root.join(format!(
                "apps/companion-native-host/bin/nexus-companion-native-host-{browser}.mjs"
            ));
            call(&[
                "install",
                "--browser",
                browser,
                "--extension-id",
                "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
                "--host-path",
                host.to_str().unwrap(),
            ])
            .unwrap();
        }
        let base = if cfg!(target_os = "macos") {
            home.0.join("Library/Application Support")
        } else {
            home.0.join(".config")
        };
        let chrome = base
            .join(if cfg!(target_os = "macos") {
                "Google/Chrome/NativeMessagingHosts"
            } else {
                "google-chrome/NativeMessagingHosts"
            })
            .join("com.codelogiic.nexus.companion.json");
        let edge = base
            .join(if cfg!(target_os = "macos") {
                "Microsoft Edge/NativeMessagingHosts"
            } else {
                "microsoft-edge/NativeMessagingHosts"
            })
            .join("com.codelogiic.nexus.companion.json");
        let edge_before = fs::read(&edge).unwrap();
        call(&["uninstall", "--browser", "chrome"]).unwrap();
        call(&["uninstall", "--browser", "chrome"]).unwrap();
        assert!(!chrome.exists());
        assert_eq!(fs::read(&edge).unwrap(), edge_before);
        assert_eq!(fs::read(&history).unwrap(), history_before);
        assert_eq!(unregister("firefox"), Err(ERROR));
    }
    #[test]
    fn noisy_failed_and_stalled_helpers_return_only_fixed_errors() {
        let directory = fixture();
        let helper = directory.0.join("helper.mjs");
        fs::write(&helper, "process.stdout.write('private path');process.stderr.write('private SQL');process.exit(1);").unwrap();
        assert_eq!(
            execute(Command::new("node").arg(&helper), Duration::from_secs(2)),
            Err(ERROR)
        );
        fs::write(
            &helper,
            "process.stdout.write('private path');setInterval(()=>{},1000);",
        )
        .unwrap();
        let start = Instant::now();
        assert_eq!(
            execute(
                Command::new("node").arg(&helper),
                Duration::from_millis(150)
            ),
            Err(ERROR)
        );
        assert!(start.elapsed() < Duration::from_secs(2));
    }
}
