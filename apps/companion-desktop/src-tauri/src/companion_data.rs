use serde::{Deserialize, Serialize};
use std::{
    env,
    io::Read,
    path::{Path, PathBuf},
    process::{Command, Stdio},
    sync::mpsc,
    thread,
    time::{Duration, Instant},
};

pub const DATA_ERROR: &str =
    "Local Companion data could not be updated; refresh status before trying again.";

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
pub struct DataControls {
    state: &'static str,
    #[serde(skip_serializing_if = "Option::is_none")]
    retention_days: Option<u16>,
    #[serde(skip_serializing_if = "Option::is_none")]
    stored_spans: Option<u64>,
}
impl DataControls {
    pub fn unavailable() -> Self {
        Self {
            state: "unavailable",
            retention_days: None,
            stored_spans: None,
        }
    }
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
struct Reply {
    schema_version: u8,
    ok: bool,
    action: Option<String>,
    retention_days: Option<u16>,
    stored_spans: Option<u64>,
    deleted: Option<u64>,
    error: Option<String>,
}
fn validate_reply(bytes: &[u8], action: &str, success: bool) -> Result<Reply, &'static str> {
    if bytes.is_empty() || bytes.len() > 4096 {
        return Err(DATA_ERROR);
    }
    let reply: Reply = serde_json::from_slice(bytes).map_err(|_| DATA_ERROR)?;
    const MAX_SAFE: u64 = 9_007_199_254_740_991;
    if !success
        || !reply.ok
        || reply.schema_version != 1
        || reply.error.is_some()
        || reply.action.as_deref() != Some(action)
        || !matches!(reply.retention_days, Some(0..=365))
    {
        return Err(DATA_ERROR);
    }
    let valid = if action == "status" {
        matches!(reply.stored_spans, Some(0..=MAX_SAFE)) && reply.deleted.is_none()
    } else {
        matches!(reply.deleted, Some(0..=MAX_SAFE)) && reply.stored_spans.is_none()
    };
    if !valid {
        return Err(DATA_ERROR);
    }
    Ok(reply)
}
fn helper_path() -> Result<PathBuf, &'static str> {
    let root = if let Some(value) = env::var_os("NEXUS_REPO") {
        PathBuf::from(value)
    } else {
        PathBuf::from(env::var_os("HOME").ok_or(DATA_ERROR)?).join(".config/nexus/repo")
    };
    if !root.is_absolute() {
        return Err(DATA_ERROR);
    }
    let path = root.join("tools/mcp/companion-data.mjs");
    if !path.is_file() {
        return Err(DATA_ERROR);
    }
    Ok(path)
}
fn execute(command: &mut Command, action: &str) -> Result<Reply, &'static str> {
    let deadline = Instant::now() + Duration::from_secs(15);
    let mut child = command
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::null())
        .spawn()
        .map_err(|_| DATA_ERROR)?;
    let stdout = child.stdout.take().ok_or(DATA_ERROR)?;
    let (sender, receiver) = mpsc::channel();
    thread::spawn(move || {
        let mut bytes = Vec::new();
        let result = stdout.take(4097).read_to_end(&mut bytes).map(|_| bytes);
        let _ = sender.send(result);
    });
    let status = loop {
        match child.try_wait() {
            Ok(Some(status)) => break status,
            Ok(None) if Instant::now() < deadline => thread::sleep(Duration::from_millis(10)),
            _ => {
                let _ = child.kill();
                let _ = child.wait();
                return Err(DATA_ERROR);
            }
        }
    };
    let bytes = receiver
        .recv_timeout(Duration::from_millis(500))
        .map_err(|_| DATA_ERROR)?
        .map_err(|_| DATA_ERROR)?;
    validate_reply(&bytes, action, status.success())
}
fn run_at(helper: &Path, args: &[&str], home: Option<&Path>) -> Result<Reply, &'static str> {
    let mut command = Command::new("node");
    command.arg(helper).args(args);
    if let Some(home) = home {
        command.env("HOME", home);
    }
    execute(&mut command, args[0])
}
fn run(args: &[&str]) -> Result<Reply, &'static str> {
    run_at(&helper_path()?, args, None)
}

pub fn status() -> DataControls {
    match run(&["status"]) {
        Ok(reply) => DataControls {
            state: "ready",
            retention_days: reply.retention_days,
            stored_spans: reply.stored_spans,
        },
        Err(_) => DataControls::unavailable(),
    }
}
pub fn set_retention(days: u16) -> Result<(), &'static str> {
    if days > 365 {
        return Err(DATA_ERROR);
    }
    let days_text = days.to_string();
    let reply = run(&["retention", "--days", &days_text])?;
    if reply.retention_days != Some(days) {
        return Err(DATA_ERROR);
    }
    Ok(())
}
pub fn prune() -> Result<(), &'static str> {
    run(&["prune"]).map(|_| ())
}
pub fn clear(confirmed: bool) -> Result<(), &'static str> {
    if !confirmed {
        return Err(DATA_ERROR);
    }
    run(&["clear", "--confirm"]).map(|_| ())
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn rejects_unknown_fields_bad_types_and_private_errors() {
        for json in [
            r#"{"schemaVersion":1,"ok":false,"error":"private path"}"#,
            r#"{"schemaVersion":1,"ok":true,"action":"status","retentionDays":366,"storedSpans":0}"#,
            r#"{"schemaVersion":1,"ok":true,"action":"status","retentionDays":14,"storedSpans":-1}"#,
            r#"{"schemaVersion":1,"ok":true,"action":"status","retentionDays":14,"storedSpans":0,"private":"path"}"#,
            r#"{"schemaVersion":1,"ok":true,"action":"clear","retentionDays":14,"deleted":0}"#,
        ] {
            assert_eq!(
                validate_reply(json.as_bytes(), "status", true).unwrap_err(),
                DATA_ERROR
            );
        }
        let valid = br#"{"schemaVersion":1,"ok":true,"action":"status","retentionDays":14,"storedSpans":0}"#;
        assert!(validate_reply(valid, "status", true).is_ok());
        assert!(validate_reply(valid, "status", false).is_err());
        assert!(validate_reply(&vec![b' '; 4097], "status", true).is_err());
        assert_eq!(clear(false), Err(DATA_ERROR));
        assert_eq!(set_retention(366), Err(DATA_ERROR));
    }
    #[test]
    fn shared_helper_uses_isolated_migrated_database() {
        let root = PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../../..");
        let helper = root.join("tools/mcp/companion-data.mjs");
        let home = env::temp_dir().join(format!(
            "nexus-desktop-data-{}-{}",
            std::process::id(),
            std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .unwrap()
                .as_nanos()
        ));
        std::fs::create_dir_all(&home).unwrap();
        struct Cleanup(PathBuf);
        impl Drop for Cleanup {
            fn drop(&mut self) {
                let _ = std::fs::remove_dir_all(&self.0);
            }
        }
        let _cleanup = Cleanup(home.clone());
        assert!(run_at(&helper, &["status"], Some(&home)).is_err());
        assert!(!home.join(".config").exists());
        let setup = Command::new("node").args(["--input-type=module", "-e", "import {createObservabilityStore} from './tools/mcp/lib/observability-store.mjs';createObservabilityStore().migrate();"])
            .current_dir(&root).env("HOME", &home).stderr(Stdio::null()).status().unwrap();
        assert!(setup.success());
        let status = run_at(&helper, &["status"], Some(&home)).unwrap();
        assert_eq!(status.retention_days, Some(14));
        assert_eq!(status.stored_spans, Some(0));
        assert_eq!(
            run_at(&helper, &["retention", "--days", "0"], Some(&home))
                .unwrap()
                .retention_days,
            Some(0)
        );
        assert_eq!(
            run_at(&helper, &["clear", "--confirm"], Some(&home))
                .unwrap()
                .deleted,
            Some(0)
        );
    }
}
