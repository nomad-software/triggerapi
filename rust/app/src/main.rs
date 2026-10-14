//! TriggerAPI, a desktop client for making REST API and gRPC calls, with an
//! egui user interface. It keeps its data where the Go program does, in the
//! same files, so the two can be used on the same workspaces.

mod app;
mod dialogs;
mod editor;
mod fonts;
mod grpc;
mod highlight;
mod icons;
mod kvtable;
mod lists;
mod request;
mod response;
mod settings_dialog;
mod tabs;
mod theme;
mod tree;

use std::path::PathBuf;

use triggerapi_core::{settings, workspace};

fn main() -> eframe::Result {
    let Some(settings_path) = settings::path() else {
        eprintln!("triggerapi: there is no folder for the settings");
        std::process::exit(1);
    };
    if let Err(e) = settings::prepare(&settings_path) {
        eprintln!("triggerapi: the folder of the settings could not be made: {e}");
    }
    let cwd = std::env::current_dir().unwrap_or_else(|_| PathBuf::from("."));
    let files = app::Files {
        settings: settings_path.clone(),
        workspaces: workspace::default_dir().unwrap_or_else(|| cwd.join("workspaces")),
        old_workspaces: Some(cwd.join("workspaces")),
        environments: cwd.join("environments.json"),
        requests: cwd.join("requests.json"),
    };

    let config = settings::Settings::load(&settings_path);
    let viewport = egui::ViewportBuilder::default()
        .with_title("TriggerAPI")
        .with_app_id("triggerapi")
        .with_inner_size([config.width as f32, config.height as f32])
        .with_maximized(config.maximized)
        .with_min_inner_size([480.0, 320.0])
        .with_icon(icons::app_icon());
    let options = eframe::NativeOptions { viewport, ..Default::default() };
    eframe::run_native("TriggerAPI", options, Box::new(move |cc| Ok(Box::new(app::App::new(&cc.egui_ctx, files)))))
}

/// Draws the window with some data, and saves pictures of it to the folder
/// `SHOT_DIR` names, to look at. It needs a graphics adapter, so is only run
/// when asked for: `SHOT_DIR=/tmp/shots cargo test -p triggerapi shots -- --ignored`.
#[cfg(test)]
mod shots {
    use egui_kittest::Harness;
    use triggerapi_core::collection::{Request, Store};
    use triggerapi_core::crypt::{test_pair, Keys};
    use triggerapi_core::env;
    use triggerapi_core::settings::Settings;
    use triggerapi_core::{rest, rpc, workspace};

    use super::app::{App, Files};

    #[test]
    #[ignore]
    fn shots() {
        let Ok(out) = std::env::var("SHOT_DIR") else { return };
        let tmp = tempfile::tempdir().unwrap();
        let (public, private) = test_pair(tmp.path(), "key", "");
        let keys = Keys::open(&public, &private, &mut || None).unwrap();
        let dir = tmp.path().join("workspaces");
        workspace::create(&dir, "Default").unwrap();
        let mut store = Store::default();
        let hotel = store.add_folder("Hotel");
        let f = store.folder_mut(hotel).unwrap();
        f.favourite = true;
        f.add(Request::new_rest(
            "Book room",
            rest::Request {
                method: "POST".into(),
                url: "{{host}}/rooms".into(),
                params: vec![rest::KeyValue::new("dry", "1", true)],
                body_type: "json".into(),
                body: "{\n  \"room\": {{room}},\n  \"guest\": \"Ann\"\n}".into(),
                ..Default::default()
            },
        ));
        f.add(Request::new_rest("List rooms", rest::Request { method: "GET".into(), url: "{{host}}/rooms".into(), ..Default::default() }));
        let svc = store.add_folder("Service");
        store.folder_mut(svc).unwrap().add(Request::new_grpc("Say", rpc::Request { url: "localhost:50051".into(), method: "test.v1.Echo/Say".into(), ..Default::default() }));
        store.add_folder("Empty");
        store.save_with(&workspace::requests(&dir, "Default"), Some(&keys)).unwrap();
        let envs = env::Store {
            active: 0,
            environments: vec![env::Environment {
                name: "Local".into(),
                variables: vec![env::Variable { key: "host".into(), value: "localhost:8080".into(), enabled: true, description: "The server".into() }],
            }],
        };
        envs.save_with(&workspace::environments(&dir, "Default"), Some(&keys)).unwrap();
        let settings_path = tmp.path().join("settings.json");
        let settings = Settings {
            public_key: public,
            private_key: private,
            workspaces_dir: dir.display().to_string(),
            workspace: "Default".into(),
            width: 1280,
            height: 800,
            ..Default::default()
        };
        settings.save(&settings_path).unwrap();
        let files = Files {
            settings: settings_path,
            workspaces: dir.clone(),
            old_workspaces: None,
            environments: tmp.path().join("environments.json"),
            requests: tmp.path().join("requests.json"),
        };

        let mut harness = Harness::builder().with_size(egui::vec2(1280.0, 800.0)).wgpu().build_eframe(|cc| App::new(&cc.egui_ctx, files));
        harness.run_steps(4);
        std::fs::create_dir_all(&out).unwrap();
        harness.render().unwrap().save(format!("{out}/start.png")).unwrap();

        // A server that answers with JSON.
        let rt = tokio::runtime::Runtime::new().unwrap();
        let listener = rt.block_on(tokio::net::TcpListener::bind("127.0.0.1:0")).unwrap();
        let port = listener.local_addr().unwrap().port();
        rt.spawn(async move {
            use tokio::io::{AsyncReadExt, AsyncWriteExt};
            loop {
                let (mut socket, _) = listener.accept().await.unwrap();
                let mut buf = vec![0u8; 8192];
                let _ = socket.read(&mut buf).await;
                let body = r#"{"id":7,"room":{"number":101,"floor":1,"view":true},"guest":"Ann","tags":["sea","quiet"],"price":120.50,"note":null}"#;
                let reply = format!("HTTP/1.1 201 Created\r\nContent-Type: application/json\r\nX-Request-Id: abc\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{body}", body.len());
                let _ = socket.write_all(reply.as_bytes()).await;
            }
        });

        harness.state_mut().test_side(super::app::Side::Requests);
        harness.state_mut().test_open("Book room");
        harness.run_steps(3);
        harness.render().unwrap().save(format!("{out}/request.png")).unwrap();

        // The environment points at the server.
        let _ = port;
        harness.state_mut().test_set_host(&format!("127.0.0.1:{port}"));
        harness.state_mut().test_send();
        for _ in 0..200 {
            harness.run_steps(1);
            if !harness.state().test_busy() {
                break;
            }
            std::thread::sleep(std::time::Duration::from_millis(10));
        }
        harness.run_steps(3);
        harness.render().unwrap().save(format!("{out}/response.png")).unwrap();

        harness.state_mut().test_find("room");
        harness.run_steps(3);
        harness.render().unwrap().save(format!("{out}/find.png")).unwrap();

        harness.state_mut().test_open("Say");
        harness.run_steps(3);
        harness.render().unwrap().save(format!("{out}/grpc.png")).unwrap();

        harness.state_mut().test_side(super::app::Side::Environments);
        harness.state_mut().test_editor();
        harness.run_steps(3);
        harness.render().unwrap().save(format!("{out}/editor.png")).unwrap();

        harness.state_mut().test_settings();
        harness.run_steps(3);
        harness.render().unwrap().save(format!("{out}/settings.png")).unwrap();
    }
}
