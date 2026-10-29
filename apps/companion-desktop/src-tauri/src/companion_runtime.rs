use std::{env, ffi::OsString, path::PathBuf};

pub const RUNTIME_ERROR: &str = "Companion runtime is unavailable in this installation.";

fn resolve_node(configured: Option<OsString>) -> Result<PathBuf, &'static str> {
    match configured {
        None => Ok(PathBuf::from("node")),
        Some(value) => {
            let path = PathBuf::from(value);
            if path.is_absolute() && path.is_file() {
                Ok(path)
            } else {
                Err(RUNTIME_ERROR)
            }
        }
    }
}

pub fn node_executable() -> Result<PathBuf, &'static str> {
    resolve_node(env::var_os("NEXUS_COMPANION_NODE"))
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn explicit_runtime_must_exist_and_never_falls_back_to_path() {
        assert_eq!(resolve_node(None).unwrap(), PathBuf::from("node"));
        for path in ["", "relative/node", "/nonexistent-nexus-runtime/node"] {
            assert_eq!(resolve_node(Some(path.into())), Err(RUNTIME_ERROR));
        }
        let executable = env::current_exe().unwrap();
        assert_eq!(
            resolve_node(Some(executable.clone().into())),
            Ok(executable)
        );
        assert_eq!(
            resolve_node(Some(env::temp_dir().into())),
            Err(RUNTIME_ERROR)
        );
    }
}
