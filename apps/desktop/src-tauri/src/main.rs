// TrafficKit desktop shell.
//
// All it does is start the engine (the `traffickit` sidecar), wait for its
// ready line, and hand the API address and token to the webview. Everything
// else happens in the engine or the UI.
#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

use std::io::{BufRead, BufReader};
use std::path::PathBuf;
use std::process::{Child, Command, Stdio};
use std::sync::{mpsc, Mutex};
use std::thread;
use std::time::Duration;

use serde::{Deserialize, Serialize};
use tauri::{Manager, RunEvent, State};

const READY_TIMEOUT: Duration = Duration::from_secs(15);
const EXIT_GRACE: Duration = Duration::from_secs(3);

#[derive(Deserialize)]
struct ReadyLine {
    event: String,
    api: String,
    token: String,
}

#[derive(Serialize, Clone)]
#[serde(rename_all = "camelCase")]
struct EngineInfo {
    api_url: String,
    token: String,
}

struct Engine {
    info: Result<EngineInfo, String>,
    child: Mutex<Option<Child>>,
}

#[tauri::command]
fn engine_info(engine: State<Engine>) -> Result<EngineInfo, String> {
    engine.info.clone()
}

// Tauri bundles external binaries next to the app executable, without the
// target-triple suffix they have in src-tauri/binaries.
fn engine_path() -> Result<PathBuf, String> {
    let exe = std::env::current_exe().map_err(|e| e.to_string())?;
    let dir = exe.parent().ok_or("app executable has no parent directory")?;
    Ok(dir.join(if cfg!(windows) { "traffickit.exe" } else { "traffickit" }))
}

fn spawn_engine() -> Result<(Child, EngineInfo), String> {
    let path = engine_path()?;
    let mut cmd = Command::new(&path);
    cmd.args(["serve", "--exit-on-stdin-close"])
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .stderr(Stdio::inherit());
    // `tauri dev` serves the UI from Vite, a different origin than the bundle.
    #[cfg(debug_assertions)]
    cmd.args(["--allow-origin", "http://localhost:5173"]);
    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        const CREATE_NO_WINDOW: u32 = 0x0800_0000;
        cmd.creation_flags(CREATE_NO_WINDOW);
    }

    let mut child = cmd
        .spawn()
        .map_err(|e| format!("could not start the engine at {}: {e}", path.display()))?;
    let stdout = child.stdout.take().ok_or("engine stdout not captured")?;

    // Read the ready line on a thread so a hung engine can't hang the app.
    let (tx, rx) = mpsc::channel();
    thread::spawn(move || {
        let mut reader = BufReader::new(stdout);
        let mut line = String::new();
        let res = reader.read_line(&mut line).map(|_| line);
        let _ = tx.send(res);
        // The engine shouldn't write anything else, but keep the pipe drained.
        let _ = std::io::copy(&mut reader, &mut std::io::sink());
    });

    let line = match rx.recv_timeout(READY_TIMEOUT) {
        Ok(Ok(line)) if !line.trim().is_empty() => line,
        Ok(Ok(_)) => {
            let status = child.wait().map(|s| s.to_string()).unwrap_or_default();
            return Err(format!("the engine exited before it was ready ({status})"));
        }
        Ok(Err(e)) => return Err(format!("reading from the engine failed: {e}")),
        Err(_) => {
            let _ = child.kill();
            return Err("the engine did not start within 15 seconds".into());
        }
    };
    let ready: ReadyLine =
        serde_json::from_str(&line).map_err(|e| format!("unexpected engine output: {e}"))?;
    if ready.event != "ready" {
        return Err(format!("unexpected engine event {:?}", ready.event));
    }
    Ok((child, EngineInfo { api_url: ready.api, token: ready.token }))
}

fn stop_engine(mut child: Child) {
    drop(child.stdin.take()); // the engine exits when stdin closes
    let deadline = std::time::Instant::now() + EXIT_GRACE;
    while std::time::Instant::now() < deadline {
        if let Ok(Some(_)) = child.try_wait() {
            return;
        }
        thread::sleep(Duration::from_millis(50));
    }
    let _ = child.kill();
}

fn main() {
    // If the engine can't start, the window still opens and the UI shows why.
    let (child, info) = match spawn_engine() {
        Ok((child, info)) => (Some(child), Ok(info)),
        Err(e) => {
            eprintln!("traffickit: {e}");
            (None, Err(e))
        }
    };

    tauri::Builder::default()
        .manage(Engine { info, child: Mutex::new(child) })
        .invoke_handler(tauri::generate_handler![engine_info])
        .build(tauri::generate_context!())
        .expect("failed to build the TrafficKit window")
        .run(|app, event| {
            if let RunEvent::Exit = event {
                let child = app.state::<Engine>().child.lock().ok().and_then(|mut c| c.take());
                if let Some(child) = child {
                    stop_engine(child);
                }
            }
        });
}
