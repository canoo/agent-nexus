//! Bounded one-request transport to the shared NEXUS control executable.
//! Operation/confirmation policy stays in Go; this module never invokes a shell.
use serde::Deserialize;
use serde_json::Value;
use std::{
    env,
    ffi::OsString,
    fs,
    io::{Read, Write},
    path::PathBuf,
    process::{Child, Command, Stdio},
    sync::mpsc::{self, TryRecvError},
    thread,
    time::{Duration, Instant},
};

pub const ERROR: &str = "The NEXUS control operation could not be completed.";
pub const UNAVAILABLE: &str = "NEXUS controls are unavailable in this installation.";
const MAX_REQUEST: usize = 64 * 1024;
const MAX_OUTPUT: usize = 4 * 1024 * 1024;
const DEADLINE: Duration = Duration::from_secs(10 * 60);

#[derive(Debug)]
struct ControlPaths {
    binary: PathBuf,
    root: PathBuf,
}
impl ControlPaths {
    fn configured(binary: Option<OsString>, root: Option<OsString>) -> Result<Self, &'static str> {
        let binary = PathBuf::from(binary.ok_or(UNAVAILABLE)?);
        let root = PathBuf::from(root.ok_or(UNAVAILABLE)?);
        if !binary.is_absolute() || !root.is_absolute() {
            return Err(UNAVAILABLE);
        }
        let binary_stat = fs::metadata(&binary).map_err(|_| UNAVAILABLE)?;
        if !binary_stat.is_file() || !root.is_dir() {
            return Err(UNAVAILABLE);
        }
        // A native-host payload intentionally contains neither sentinel. Pin
        // config/install operations to the full NEXUS root, never that payload.
        for marker in ["core/NEXUS.md", "tools/mcp/server.mjs"] {
            if !fs::symlink_metadata(root.join(marker))
                .map(|metadata| metadata.file_type().is_file())
                .unwrap_or(false)
            {
                return Err(UNAVAILABLE);
            }
        }
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            if binary_stat.permissions().mode() & 0o111 == 0 {
                return Err(UNAVAILABLE);
            }
        }
        // The desktop currently supports native Unix hosts. Fail closed where
        // process-group cancellation is unavailable rather than killing only
        // the service while an install/update descendant continues running.
        if !cfg!(unix) {
            return Err(UNAVAILABLE);
        }
        Ok(Self {
            binary: fs::canonicalize(binary).map_err(|_| UNAVAILABLE)?,
            root: fs::canonicalize(root).map_err(|_| UNAVAILABLE)?,
        })
    }
}

fn request_bytes(request: &Value) -> Result<Vec<u8>, &'static str> {
    let object = request.as_object().ok_or(ERROR)?;
    if object.get("schemaVersion").and_then(Value::as_u64) != Some(1)
        || object
            .get("operation")
            .and_then(Value::as_str)
            .filter(|value| !value.is_empty())
            .is_none()
        || object.keys().any(|key| {
            !matches!(
                key.as_str(),
                "schemaVersion" | "operation" | "confirm" | "params"
            )
        })
        || object
            .get("confirm")
            .is_some_and(|value| !value.is_boolean())
        || object.get("params").is_some_and(|value| !value.is_object())
    {
        return Err(ERROR);
    }
    let bytes = serde_json::to_vec(request).map_err(|_| ERROR)?;
    if bytes.len() > MAX_REQUEST {
        return Err(ERROR);
    }
    Ok(bytes)
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
struct Reply {
    schema_version: u8,
    ok: bool,
    operation: Option<String>,
    result: Option<Value>,
    error: Option<String>,
}
fn validate_reply(bytes: &[u8], operation: &str, success: bool) -> Result<Value, &'static str> {
    if bytes.is_empty() || bytes.len() > MAX_OUTPUT {
        return Err(ERROR);
    }
    // Deriving a struct rejects duplicate protocol keys as well as unknown
    // fields; Value alone would silently retain the last duplicate key.
    let reply: Reply = serde_json::from_slice(bytes).map_err(|_| ERROR)?;
    let value: Value = serde_json::from_slice(bytes).map_err(|_| ERROR)?;
    let object = value.as_object().ok_or(ERROR)?;
    if reply.schema_version != 1 {
        return Err(ERROR);
    }
    if reply.ok {
        if !success
            || object.len() != 4
            || reply.operation.as_deref() != Some(operation)
            || !object.contains_key("result")
            || object.contains_key("error")
        {
            return Err(ERROR);
        }
        // Null is a valid operation result; Option<Value> alone cannot
        // distinguish a missing result from explicit JSON null.
        let _ = reply.result;
    } else if success
        || object.len() != 3
        || object.contains_key("operation")
        || object.contains_key("result")
        || !matches!(
            reply.error.as_deref(),
            Some(
                "control_command_invalid"
                    | "control_request_invalid"
                    | "control_protocol_unsupported"
                    | "control_operation_unknown"
                    | "control_confirmation_required"
                    | "control_params_invalid"
                    | "control_cloud_acknowledgement_required"
                    | "control_operation_failed"
                    | "control_output_unavailable"
            )
        )
    {
        return Err(ERROR);
    }
    Ok(value)
}

fn terminate(child: &mut Child) {
    #[cfg(unix)]
    {
        extern "C" {
            #[link_name = "kill"]
            fn signal_process_group(pid: i32, signal: i32) -> i32;
        }
        if let Ok(pid) = i32::try_from(child.id()) {
            // SAFETY: kill accepts integer PID/signal values, not pointers.
            // process_group(0) made this child the group leader. POSIX signal
            // 9 is SIGKILL on the native Linux/macOS hosts supported here.
            unsafe { signal_process_group(-pid, 9) };
        }
    }
    let _ = child.kill();
    let _ = child.wait();
}
struct RunningChild {
    child: Child,
    complete: bool,
}
impl Drop for RunningChild {
    fn drop(&mut self) {
        if !self.complete {
            terminate(&mut self.child);
        }
    }
}

fn execute_at(
    paths: &ControlPaths,
    request: Value,
    timeout: Duration,
) -> Result<Value, &'static str> {
    let bytes = request_bytes(&request)?;
    let operation = request["operation"].as_str().ok_or(ERROR)?;
    let mut command = Command::new(&paths.binary);
    command
        .args(["control", "--json"])
        .current_dir(&paths.root)
        // Installed Companion helpers may use a host-only NEXUS_REPO. The
        // shared engine must instead receive its explicitly trusted full root.
        .env("NEXUS_REPO", &paths.root)
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .stderr(Stdio::null());
    #[cfg(unix)]
    {
        use std::os::unix::process::CommandExt;
        command.process_group(0);
    }
    let deadline = Instant::now() + timeout;
    let mut running = RunningChild {
        child: command.spawn().map_err(|_| ERROR)?,
        complete: false,
    };
    let mut stdin = running.child.stdin.take().ok_or(ERROR)?;
    let stdout = running.child.stdout.take().ok_or(ERROR)?;
    let (write_tx, write_rx) = mpsc::channel();
    thread::Builder::new()
        .name("nexus-control-input".to_owned())
        .spawn(move || {
            let result = stdin.write_all(&bytes).map_err(|_| ());
            drop(stdin); // Go reads one bounded request through EOF.
            let _ = write_tx.send(result);
        })
        .map_err(|_| ERROR)?;
    let (read_tx, read_rx) = mpsc::channel();
    thread::Builder::new()
        .name("nexus-control-output".to_owned())
        .spawn(move || {
            let mut bytes = Vec::new();
            let result = stdout
                .take((MAX_OUTPUT + 1) as u64)
                .read_to_end(&mut bytes)
                .map_err(|_| ())
                .and_then(|_| {
                    if bytes.len() <= MAX_OUTPUT {
                        Ok(bytes)
                    } else {
                        Err(())
                    }
                });
            let _ = read_tx.send(result);
        })
        .map_err(|_| ERROR)?;

    let mut written = false;
    let mut output = None;
    let mut status = None;
    loop {
        if Instant::now() >= deadline {
            return Err(ERROR); // Drop kills the whole group and reaps the child.
        }
        if !written {
            match write_rx.try_recv() {
                Ok(Ok(())) => written = true,
                Err(TryRecvError::Empty) => {}
                _ => return Err(ERROR),
            }
        }
        if output.is_none() {
            match read_rx.try_recv() {
                Ok(Ok(bytes)) => output = Some(bytes),
                Err(TryRecvError::Empty) => {}
                _ => return Err(ERROR),
            }
        }
        if status.is_none() {
            status = running.child.try_wait().map_err(|_| ERROR)?;
        }
        if let (true, Some(bytes), Some(status)) = (written, output.as_ref(), status.as_ref()) {
            let reply = validate_reply(bytes, operation, status.success());
            if reply.is_ok() {
                running.complete = true;
            }
            return reply;
        }
        thread::sleep(Duration::from_millis(5));
    }
}

pub fn run(request: Value) -> Result<Value, &'static str> {
    let paths = ControlPaths::configured(
        env::var_os("NEXUS_CONTROL_BINARY"),
        env::var_os("NEXUS_CONTROL_ROOT"),
    )?;
    execute_at(&paths, request, DEADLINE)
}

#[cfg(all(test, unix))]
mod tests {
    use super::*;
    use serde_json::json;
    use std::os::unix::fs::PermissionsExt;
    use std::path::Path;
    use std::time::{SystemTime, UNIX_EPOCH};

    struct Fixture {
        directory: PathBuf,
        paths: ControlPaths,
    }
    impl Drop for Fixture {
        fn drop(&mut self) {
            let _ = fs::remove_dir_all(&self.directory);
        }
    }
    fn quote(value: &str) -> String {
        format!("'{}'", value.replace('\'', "'\\''"))
    }
    fn fixture(script: &str) -> Fixture {
        let directory = env::temp_dir().join(format!(
            "nexus-control-{}-{} ' literal $ and `",
            std::process::id(),
            SystemTime::now()
                .duration_since(UNIX_EPOCH)
                .unwrap()
                .as_nanos()
        ));
        fs::create_dir(&directory).unwrap();
        fs::create_dir_all(directory.join("core")).unwrap();
        fs::create_dir_all(directory.join("tools/mcp")).unwrap();
        fs::write(directory.join("core/NEXUS.md"), "isolated fixture marker").unwrap();
        fs::write(
            directory.join("tools/mcp/server.mjs"),
            "// isolated fixture marker",
        )
        .unwrap();
        let node = Command::new("node")
            .args(["-p", "process.execPath"])
            .output()
            .expect("Node is required for executable control transport tests");
        assert!(node.status.success());
        let node = String::from_utf8(node.stdout).unwrap().trim().to_owned();
        let module = directory.join("fixture.mjs");
        fs::write(&module, script).unwrap();
        let binary = directory.join("pinned control binary");
        fs::write(
            &binary,
            format!(
                "#!/bin/sh\nexec {} {} \"$@\"\n",
                quote(&node),
                quote(module.to_str().unwrap())
            ),
        )
        .unwrap();
        fs::set_permissions(&binary, fs::Permissions::from_mode(0o700)).unwrap();
        let paths =
            ControlPaths::configured(Some(binary.into()), Some(directory.clone().into())).unwrap();
        Fixture { directory, paths }
    }
    fn request() -> Value {
        json!({"schemaVersion":1,"operation":"status.get"})
    }

    #[test]
    fn configured_paths_require_absolute_existing_executable_and_root() {
        let f = fixture("");
        let binary = Some(f.paths.binary.clone().into());
        let root = Some(f.paths.root.clone().into());
        assert!(ControlPaths::configured(None, root.clone()).is_err());
        assert!(ControlPaths::configured(binary.clone(), None).is_err());
        for bad in ["", "node", "/nonexistent-nexus-control"] {
            assert!(ControlPaths::configured(Some(bad.into()), root.clone()).is_err());
            assert!(ControlPaths::configured(binary.clone(), Some(bad.into())).is_err());
        }
        assert!(ControlPaths::configured(root.clone(), root.clone()).is_err());
        assert!(ControlPaths::configured(binary.clone(), binary.clone()).is_err());
        let payload_only = f.directory.join("host-only");
        fs::create_dir(&payload_only).unwrap();
        assert!(ControlPaths::configured(binary.clone(), Some(payload_only.into())).is_err());
        let inert = f.directory.join("nonexecutable");
        fs::write(&inert, "private").unwrap();
        fs::set_permissions(&inert, fs::Permissions::from_mode(0o600)).unwrap();
        assert!(ControlPaths::configured(Some(inert.into()), root).is_err());
    }

    #[test]
    fn malformed_requests_fail_before_starting_the_child() {
        let f = fixture("import {writeFileSync} from 'node:fs';writeFileSync('must-not-execute','unexpected');throw new Error('must not execute');");
        for value in [
            Value::Null,
            json!([]),
            json!({"schemaVersion":1}),
            json!({"schemaVersion":2,"operation":"status.get"}),
            json!({"schemaVersion":1,"operation":"status.get","private":"value"}),
            json!({"schemaVersion":1,"operation":"status.get","params":null}),
            json!({"schemaVersion":1,"operation":"status.get","confirm":"yes"}),
            json!({"schemaVersion":1,"operation":"status.get","params":{"input":"x".repeat(MAX_REQUEST)}}),
        ] {
            assert_eq!(
                execute_at(&f.paths, value, Duration::from_secs(1)),
                Err(ERROR)
            );
        }
        assert!(!f.directory.join("must-not-execute").exists());
        assert!(request_bytes(&json!({"schemaVersion":1,"operation":"memory.command","params":{"input":"literal newline\n$HOME `not shell`"},"confirm":true})).is_ok());
    }

    #[test]
    fn pinned_executable_receives_fixed_arguments_json_and_trusted_full_root() {
        let f = fixture(
            r#"
            import { readFileSync } from 'node:fs';
            const request = JSON.parse(readFileSync(0, 'utf8'));
            const args = process.argv.slice(2);
            if (JSON.stringify(args) !== JSON.stringify(['control','--json']) || process.cwd() !== process.env.NEXUS_REPO) process.exit(3);
            process.stderr.write('private SQL/path must not be forwarded');
            process.stdout.write(JSON.stringify({schemaVersion:1,ok:true,operation:request.operation,result:{args,rootMatches:true,params:request.params}})+'\n');
        "#,
        );
        let value = json!({"schemaVersion":1,"operation":"route.preview","params":{"prompt":"literal $(not-executed)"}});
        let result = execute_at(&f.paths, value.clone(), Duration::from_secs(3)).unwrap();
        assert_eq!(result["result"]["args"], json!(["control", "--json"]));
        assert_eq!(result["result"]["rootMatches"], true);
        assert_eq!(result["result"]["params"], value["params"]);
        assert!(!result.to_string().contains("private SQL"));
    }

    #[test]
    fn only_exact_matching_success_and_fixed_protocol_failures_are_returned() {
        let success = br#"{"schemaVersion":1,"ok":true,"operation":"status.get","result":null}"#;
        assert!(validate_reply(success, "status.get", true).is_ok());
        assert!(validate_reply(success, "status.get", false).is_err());
        assert!(validate_reply(success, "config.get", true).is_err());
        for bytes in [
            b"".as_slice(),
            br#"{"schemaVersion":1,"ok":true,"operation":"status.get"}"#,
            br#"{"schemaVersion":1,"ok":true,"operation":"status.get","result":{},"private":"path"}"#,
            br#"{"schemaVersion":2,"ok":true,"operation":"status.get","result":{}}"#,
            br#"{"schemaVersion":1,"ok":1,"operation":"status.get","result":{}}"#,
            br#"{"schemaVersion":1,"ok":true,"ok":false,"operation":"status.get","result":{}}"#,
            br#"{"schemaVersion":1,"ok":false,"error":"private SQL/path"}"#,
            br#"{"schemaVersion":1,"ok":false,"error":"control_operation_failed","result":null}"#,
            br#"{"schemaVersion":1,"ok":false,"error":"control_operation_failed"} {}"#,
        ] {
            assert_eq!(validate_reply(bytes, "status.get", true), Err(ERROR));
            assert_eq!(validate_reply(bytes, "status.get", false), Err(ERROR));
        }
        let f = fixture("process.stderr.write('private SQL/path');process.stdout.write(JSON.stringify({schemaVersion:1,ok:false,error:'control_confirmation_required'}));process.exitCode=1;");
        let reply = execute_at(&f.paths, request(), Duration::from_secs(3)).unwrap();
        assert_eq!(
            reply,
            json!({"schemaVersion":1,"ok":false,"error":"control_confirmation_required"})
        );
        let f = fixture("process.stderr.write('private SQL/path');process.exitCode=1;");
        assert_eq!(
            execute_at(&f.paths, request(), Duration::from_secs(3)),
            Err(ERROR)
        );
    }

    #[test]
    fn output_limit_terminates_a_stalled_writer_before_the_deadline() {
        let f = fixture(&format!(
            "process.stdout.write('x'.repeat({}));setInterval(()=>{{}},1000);",
            MAX_OUTPUT + 1
        ));
        let start = Instant::now();
        assert_eq!(
            execute_at(&f.paths, request(), Duration::from_secs(5)),
            Err(ERROR)
        );
        assert!(start.elapsed() < Duration::from_secs(4));
    }

    #[test]
    fn deadline_does_not_wait_for_blocked_stdin_or_stdout() {
        let f = fixture("setInterval(()=>{},1000);");
        let large = json!({"schemaVersion":1,"operation":"status.get","params":{"input":"x".repeat(60_000)}});
        let start = Instant::now();
        assert_eq!(
            execute_at(&f.paths, large, Duration::from_millis(150)),
            Err(ERROR)
        );
        assert!(start.elapsed() < Duration::from_secs(2));
    }

    #[test]
    fn deadline_kills_the_group_so_a_descendant_cannot_continue_side_effects() {
        let f = fixture(
            r#"
            import { spawn } from 'node:child_process';
            import { writeFileSync } from 'node:fs';
            writeFileSync('started',String(process.pid));
            spawn(process.execPath,['-e',"setTimeout(()=>require('node:fs').writeFileSync('escaped-side-effect','unexpected'),600)"],{stdio:'ignore'});
            setInterval(()=>{},1000);
        "#,
        );
        assert_eq!(
            execute_at(&f.paths, request(), Duration::from_millis(300)),
            Err(ERROR)
        );
        assert!(
            f.directory.join("started").is_file(),
            "fixture must start before cancellation"
        );
        thread::sleep(Duration::from_millis(800));
        assert!(!f.directory.join("escaped-side-effect").exists());
        let pid = fs::read_to_string(f.directory.join("started")).unwrap();
        assert!(Path::new("/bin/kill").exists());
        let status = Command::new("/bin/kill")
            .args(["-0", pid.trim()])
            .stdout(Stdio::null())
            .stderr(Stdio::null())
            .status()
            .unwrap();
        assert!(!status.success(), "direct child must be killed and reaped");
    }
    #[test]
    fn real_go_capability_contract_when_configured() {
        let binary = env::var_os("NEXUS_CONTROL_TEST_BINARY");
        let root = env::var_os("NEXUS_CONTROL_TEST_ROOT");
        if binary.is_none() && root.is_none() {
            eprintln!("Skipping real Go bridge contract: set NEXUS_CONTROL_TEST_BINARY and NEXUS_CONTROL_TEST_ROOT.");
            return;
        }
        let paths = ControlPaths::configured(binary, root).expect("configured real Go test paths");
        // Capabilities is a pure catalog operation: no profile/store/network
        // reads, install steps or configuration writes are invoked here.
        let response = execute_at(
            &paths,
            json!({"schemaVersion":1,"operation":"capabilities"}),
            Duration::from_secs(10),
        )
        .unwrap();
        assert_eq!(response["schemaVersion"], 1);
        assert_eq!(response["ok"], true);
        assert_eq!(response["operation"], "capabilities");
        let capabilities = response["result"]
            .as_array()
            .expect("Go capability catalog");
        for feature in [
            "Install NEXUS",
            "Configure",
            "Health Check",
            "Task Log",
            "Usage & Cost Dashboard",
            "Companion Tool Activity",
            "Update NEXUS",
            "Uninstall NEXUS",
            "Project Memory",
        ] {
            assert!(
                capabilities.iter().any(|entry| entry["feature"] == feature),
                "missing TUI feature {feature}"
            );
        }
    }
}
