//! The main window, which holds the state of the program and wires the parts
//! of the UI together.

use std::collections::{HashMap, HashSet};
use std::path::{Path, PathBuf};
use std::sync::mpsc::{channel, Receiver, Sender};
use std::sync::Arc;
use std::time::{Duration, Instant};

use egui::{vec2, Context, Key, KeyboardShortcut, Modifiers, RichText};
use triggerapi_core::capture::Rule;
use triggerapi_core::collection::{self, Store};
use triggerapi_core::crypt::Keys;
use triggerapi_core::env;
use triggerapi_core::settings::{Pin, Settings};
use triggerapi_core::{jsonfile, openapi, postman, rest, rpc, workspace};

use crate::dialogs::{Dialog, DialogAction};
use crate::editor::Editor;
use crate::grpc;
use crate::icons::{self, Glyph};
use crate::lists::{List, ListEvent};
use crate::request;
use crate::response::{self, Captured, Replay};
use crate::settings_dialog;
use crate::tabs::Tabs;
use crate::theme::{Kind, Theme};
use crate::tree::{self, Tree};

/// The version of the program, shown in the about box.
pub const VERSION: &str = "v0.0.1";

/// How long a server is given to describe its services.
const REFLECT_TIMEOUT: Duration = Duration::from_secs(15);

/// What is wrong when the settings have no keys.
pub const NEED_KEYS: &str = "TriggerAPI needs a pair of SSH keys to encrypt its files with. \
     Until they are added under Encryption in File > Settings, nothing is loaded or saved.";

/// The places the program's data is kept in.
#[derive(Clone, Debug)]
pub struct Files {
    pub settings: PathBuf,
    /// The folder the workspaces are kept in, unless the settings name
    /// another.
    pub workspaces: PathBuf,
    /// The folder the workspaces were kept in before, whose workspaces are
    /// brought in if there are none.
    pub old_workspaces: Option<PathBuf>,
    /// The files of the environments and requests from before there were
    /// workspaces.
    pub environments: PathBuf,
    pub requests: PathBuf,
}

/// The tabs of the sidebar.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Side {
    Workspaces,
    Environments,
    Requests,
}

/// What a request in flight was started by.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
enum Work {
    Send,
    Execute,
    Invoke,
}

struct InFlight {
    token: u64,
    abort: tokio::task::AbortHandle,
    work: Work,
    /// The value of `shown` when it started, so that its result is dropped
    /// if another request has been shown since.
    shown: u64,
    tab: u64,
    start: Instant,
    captures: Vec<Rule>,
}

enum Outcome {
    Rest(Result<rest::Response, String>),
    Grpc(Result<rpc::Response, String>),
    TimedOut,
}

enum Msg {
    Done { token: u64, outcome: Outcome },
    Reflected { target: String, reflecting: u64, result: Result<Arc<rpc::Schema>, String> },
}

pub struct App {
    rt: tokio::runtime::Runtime,
    tx: Sender<Msg>,
    rx: Receiver<Msg>,
    ctx: Context,

    files: Files,
    pub config: Settings,
    /// The keys of the settings, which are none if the settings name none.
    keys: Option<Keys>,
    pub theme: Theme,
    code_font: String,

    workspaces: Vec<String>,
    envs: env::Store,
    envs_path: Option<PathBuf>,
    store: Store,
    store_path: Option<PathBuf>,

    side: Side,
    workspace_list: List,
    env_list: List,
    tree: Tree,
    tabs: Tabs,

    /// The saved request on show, which edits are written back to; none
    /// for a REST request that is not saved.
    open: Option<u64>,
    editor_open: bool,
    rest: request::Panel,
    grpc: grpc::Panel,
    editor: Editor,
    rest_response: response::Panel,
    grpc_response: response::Panel,

    shown: u64,
    in_flight: Option<InFlight>,
    next_token: u64,
    schemas: HashMap<String, Arc<rpc::Schema>>,
    reflecting: u64,

    dialogs: Vec<Dialog>,
    status: String,
    started: bool,
    sidebar_width: f32,
    response_height: f32,
    closing: bool,
}

/// How opening the keys of the settings went.
enum Opened {
    None,
    Keys(Keys),
    /// They could not be opened, for the reason given.
    Failed(String),
    /// The private key has a passphrase, which is to be asked for.
    NeedPassphrase,
}

/// Opens the keys the settings name.
fn open_keys(public: &str, private: &str, passphrase: Option<String>) -> Opened {
    if public.is_empty() && private.is_empty() {
        return Opened::None;
    }
    let mut asked = false;
    let given = passphrase.is_some();
    match Keys::open(public, private, &mut || {
        asked = true;
        passphrase.clone()
    }) {
        Ok(k) => Opened::Keys(k),
        Err(_) if asked && !given => Opened::NeedPassphrase,
        Err(e) => Opened::Failed(format!("the keys of the settings cannot be used: {e}")),
    }
}

impl App {
    pub fn new(ctx: &Context, files: Files) -> App {
        let (tx, rx) = channel();
        let config = Settings::load(&files.settings);
        let theme = Theme { colours: config.colours.clone(), json: config.json_colours.clone() };
        crate::fonts::install(ctx, &config.response_font);
        let mut app = App {
            rt: tokio::runtime::Builder::new_multi_thread().enable_all().build().expect("a runtime"),
            tx,
            rx,
            ctx: ctx.clone(),
            files,
            code_font: config.response_font.clone(),
            config,
            keys: None,
            theme,
            workspaces: Vec::new(),
            envs: env::Store::default(),
            envs_path: None,
            store: Store::default(),
            store_path: None,
            side: Side::Requests,
            workspace_list: List::new("workspaces"),
            env_list: List::new("environments"),
            tree: Tree::new(),
            tabs: Tabs::new(),
            open: None,
            editor_open: false,
            rest: request::Panel::new(),
            grpc: grpc::Panel::new(),
            editor: Editor::new(),
            rest_response: response::Panel::new("rest response"),
            grpc_response: response::Panel::new("grpc response"),
            shown: 0,
            in_flight: None,
            next_token: 0,
            schemas: HashMap::new(),
            reflecting: 0,
            dialogs: Vec::new(),
            status: String::new(),
            started: false,
            sidebar_width: 0.0,
            response_height: 0.0,
            closing: false,
        };
        app.sidebar_width = if app.config.sidebar_width > 0 { app.config.sidebar_width as f32 } else { 350.0 };
        app.response_height = app.config.response_height as f32;

        // The workspace last in use is opened, which is the first there is
        // if this is the first run with workspaces.
        app.migrate();
        app.list_workspaces();
        if !app.workspaces.contains(&app.config.workspace) {
            app.config.workspace = app.workspaces.first().cloned().unwrap_or_default();
        }
        match open_keys(&app.config.public_key, &app.config.private_key, None) {
            Opened::NeedPassphrase => {
                let path = app.config.private_key.clone();
                app.dialogs.push(Dialog::passphrase(&path, |app, given| {
                    let none = "the keys of the settings cannot be used: no passphrase was given";
                    app.keys = match given {
                        None => Some(Keys::unusable(none)),
                        Some(p) => match open_keys(&app.config.public_key, &app.config.private_key, Some(p)) {
                            Opened::Keys(k) => Some(k),
                            Opened::Failed(e) => Some(Keys::unusable(e)),
                            _ => Some(Keys::unusable(none)),
                        },
                    };
                    app.finish_start();
                }));
            }
            opened => {
                app.keys = match opened {
                    Opened::Keys(k) => Some(k),
                    Opened::Failed(e) => Some(Keys::unusable(e)),
                    _ => None,
                };
                app.finish_start();
            }
        }
        app
    }

    /// Loads the workspace in use once the keys are known.
    fn finish_start(&mut self) {
        let name = self.config.workspace.clone();
        self.load_workspace(&name);
        self.tree.start_view(&self.store);
        self.restore_tabs();
        if self.keys.is_none() {
            self.dialogs.push(Dialog::message("Keys Needed", NEED_KEYS));
        }
        self.started = true;
    }

    fn workspaces_dir(&self) -> PathBuf {
        if self.config.workspaces_dir.is_empty() {
            self.files.workspaces.clone()
        } else {
            PathBuf::from(&self.config.workspaces_dir)
        }
    }

    fn default_workspaces_dir(&self) -> String {
        self.files.workspaces.display().to_string()
    }

    /// Brings in what was saved by earlier versions if there are no
    /// workspaces: those of the folder they used to be kept in, or else the
    /// files from before there were workspaces, in a workspace of their own.
    fn migrate(&mut self) {
        let dir = self.workspaces_dir();
        match workspace::list(&dir) {
            Ok(names) if names.is_empty() => {}
            _ => return,
        }
        if self.config.workspaces_dir.is_empty() {
            if let Some(old) = self.files.old_workspaces.clone() {
                if let Err(e) = workspace::copy_all(&old, &dir) {
                    self.set_status(&format!("The workspaces could not be moved to {}: {e}", dir.display()));
                    return;
                }
                if workspace::list(&dir).map(|n| !n.is_empty()).unwrap_or(true) {
                    return;
                }
            }
        }
        let result = workspace::create(&dir, workspace::DEFAULT)
            .and_then(|_| workspace::import(&dir, workspace::DEFAULT, &self.files.environments, &self.files.requests));
        if let Err(e) = result {
            self.set_status(&format!("The first workspace could not be made: {e}"));
        }
    }

    fn list_workspaces(&mut self) {
        match workspace::list(&self.workspaces_dir()) {
            Ok(names) => self.workspaces = names,
            Err(e) => {
                self.workspaces.clear();
                self.set_status(&format!("The workspaces could not be listed: {e}"));
            }
        }
    }

    /// Shows a message in the status bar until another replaces it. The bar
    /// is kept for problems with the saved data.
    fn set_status(&mut self, message: &str) {
        self.status = message.split_whitespace().collect::<Vec<_>>().join(" ");
    }

    /// Reads the environments and requests of a workspace. A file that could
    /// not be read is never saved to, so it is not overwritten.
    fn load_workspace(&mut self, name: &str) {
        self.envs = env::Store::default();
        self.store = Store::default();
        self.envs_path = None;
        self.store_path = None;
        if name.is_empty() {
            self.set_status("There is no workspace, so changes will not be saved.");
            return;
        }
        // Nothing is read or saved without keys to encrypt it with.
        let Some(keys) = self.keys.clone() else {
            self.set_status(NEED_KEYS);
            return;
        };
        let dir = self.workspaces_dir();
        let env_path = workspace::environments(&dir, name);
        let requests_path = workspace::requests(&dir, name);
        let mut failed = Vec::new();

        // A file from before there were keys is encrypted once it has been
        // read.
        match env::Store::load_with(&env_path, Some(&keys)).and_then(|s| jsonfile::convert(&env_path, Some(&keys), Some(&keys)).map(|_| s)) {
            Ok(s) => {
                self.envs = s;
                self.envs_path = Some(env_path);
            }
            Err(e) => failed.push(e),
        }
        match Store::load_with(&requests_path, Some(&keys)).and_then(|s| jsonfile::convert(&requests_path, Some(&keys), Some(&keys)).map(|_| s)) {
            Ok(s) => {
                self.store = s;
                self.store_path = Some(requests_path);
            }
            Err(e) => failed.push(e),
        }
        if failed.is_empty() {
            self.set_status("");
        } else {
            self.set_status(&format!("Saved data could not be loaded, changes will not be saved: {}", failed.join("; ")));
        }
    }

    fn save_envs(&mut self) {
        if let Some(path) = &self.envs_path {
            if let Err(e) = self.envs.save_with(path, self.keys.as_ref()) {
                self.set_status(&format!("Changes could not be saved: {e}"));
            }
        }
        self.environment_changed();
    }

    fn save_store(&mut self) {
        if let Some(path) = &self.store_path {
            if let Err(e) = self.store.save_with(path, self.keys.as_ref()) {
                self.set_status(&format!("Changes could not be saved: {e}"));
            }
        }
        self.sync_endpoints();
    }

    /// Refreshes what depends on the active environment.
    fn environment_changed(&mut self) {
        self.rest.stale = true;
    }

    // ---- workspaces ----

    /// Makes a workspace the one in use, showing its environments and
    /// requests in place of those of the one before.
    fn open_workspace(&mut self, name: &str) {
        self.keep_pins();
        self.load_workspace(name);
        self.config.workspace = name.to_string();
        self.close_editor();
        self.tree.search.clear();
        self.tree.selected = None;
        self.restore_tabs();
        self.environment_changed();
    }

    /// Records the pinned requests of the workspace in use, to be opened
    /// again the next time it is used. Those of workspaces that have gone
    /// are forgotten.
    fn keep_pins(&mut self) {
        let name = self.config.workspace.clone();
        let mut pins: Vec<Pin> = self.config.pinned.iter().filter(|p| p.workspace != name).cloned().collect();
        for r in self.tabs.pinned() {
            if let Some(path) = self.store.path(r) {
                pins.push(Pin { workspace: name.clone(), path });
            }
        }
        let names = self.workspaces.clone();
        pins.retain(|p| names.contains(&p.workspace));
        self.config.pinned = pins;
    }

    /// Opens a tab for each request pinned in the workspace in use, and
    /// shows the first.
    fn restore_tabs(&mut self) {
        let mut pinned = Vec::new();
        for pin in &self.config.pinned {
            if pin.workspace != self.config.workspace {
                continue;
            }
            if let Some(r) = self.store.find(&pin.path) {
                if !pinned.contains(&r) {
                    pinned.push(r);
                }
            }
        }
        self.tabs.reset(&pinned);
        self.show_tab(0, true);
    }

    fn workspace_event(&mut self, event: ListEvent) {
        let dir = self.workspaces_dir();
        match event {
            ListEvent::Select(i) => {
                if let Some(name) = self.workspaces.get(i).cloned() {
                    if name != self.config.workspace {
                        self.open_workspace(&name);
                    }
                }
            }
            ListEvent::Add => {
                let name = workspace::unique(&dir, "New Workspace");
                match workspace::create(&dir, &name) {
                    Ok(()) => {
                        self.list_workspaces();
                        self.open_workspace(&name);
                        if let Some(i) = self.workspaces.iter().position(|n| *n == name) {
                            self.workspace_list.rename(i, &name);
                        }
                    }
                    Err(e) => self.warn(&format!("The workspace could not be added: {e}.")),
                }
            }
            ListEvent::Duplicate => {
                let current = self.config.workspace.clone();
                match workspace::duplicate(&dir, &current) {
                    Ok(name) => {
                        self.list_workspaces();
                        self.open_workspace(&name);
                        if let Some(i) = self.workspaces.iter().position(|n| *n == name) {
                            self.workspace_list.rename(i, &name);
                        }
                    }
                    Err(e) => self.warn(&format!("The workspace could not be duplicated: {e}.")),
                }
            }
            ListEvent::Renamed(i, text) => {
                let Some(previous) = self.workspaces.get(i).cloned() else { return };
                let name = text.trim().to_string();
                match workspace::rename(&dir, &previous, &name) {
                    Ok(()) if name != previous => {
                        // The workspace in use follows its new name.
                        if previous == self.config.workspace {
                            for p in &mut self.config.pinned {
                                if p.workspace == previous {
                                    p.workspace = name.clone();
                                }
                            }
                            self.config.workspace = name.clone();
                            if self.envs_path.is_some() {
                                self.envs_path = Some(workspace::environments(&dir, &name));
                            }
                            if self.store_path.is_some() {
                                self.store_path = Some(workspace::requests(&dir, &name));
                            }
                        }
                        self.list_workspaces();
                    }
                    Ok(()) => {}
                    Err(e) => {
                        self.list_workspaces();
                        self.warn(&format!("The workspace was not renamed: {e}."));
                    }
                }
            }
            ListEvent::Remove => {
                let Some(row) = self.workspaces.iter().position(|n| *n == self.config.workspace) else { return };
                if self.workspaces.len() == 1 {
                    self.warn("The last workspace cannot be removed.");
                    return;
                }
                let name = self.workspaces[row].clone();
                let next = if row + 1 < self.workspaces.len() { self.workspaces[row + 1].clone() } else { self.workspaces[row.saturating_sub(1)].clone() };
                self.dialogs.push(Dialog::confirm(
                    &format!("Remove the workspace {name:?} with all of its environments and requests? This cannot be undone."),
                    move |app| {
                        let dir = app.workspaces_dir();
                        match workspace::remove(&dir, &name) {
                            Ok(()) => {
                                app.list_workspaces();
                                app.open_workspace(&next);
                            }
                            Err(e) => app.warn(&format!("The workspace was not removed: {e}.")),
                        }
                    },
                ));
            }
            _ => {}
        }
    }

    // ---- environments ----

    fn env_event(&mut self, event: ListEvent) {
        match event {
            ListEvent::Select(i) => {
                self.envs.active = i as i64;
                self.save_envs();
                // The editor follows the selection while it is open.
                if self.editor_open {
                    self.open_editor(false);
                }
            }
            ListEvent::Add => self.add_environment(env::Environment { name: "New Environment".into(), variables: Vec::new() }, true),
            ListEvent::Duplicate => {
                if let Some(e) = self.envs.current() {
                    let copy = env::Environment { name: format!("{} copy", e.name), variables: e.variables.clone() };
                    self.add_environment(copy, true);
                }
            }
            ListEvent::Edit => self.open_editor(true),
            ListEvent::Remove => {
                let Some(e) = self.envs.current() else { return };
                let question = format!("Remove the environment {:?}?", e.name);
                let row = self.envs.active as usize;
                self.dialogs.push(Dialog::confirm(&question, move |app| {
                    if row < app.envs.environments.len() {
                        app.envs.environments.remove(row);
                        let len = app.envs.environments.len();
                        app.envs.active = if len == 0 { -1 } else { row.min(len - 1) as i64 };
                        app.save_envs();
                        if app.editor_open {
                            app.open_editor(false);
                        }
                    }
                }));
            }
            ListEvent::Import => {
                let choices = vec![("Postman".to_string(), Glyph::Plane, crate::theme::parse("#ef5b25").unwrap())];
                self.dialogs.push(Dialog::choice("Import", "Choose what to import an environment from.", choices, |app, _| {
                    let Some(path) = rfd::FileDialog::new().set_title("Import Postman").add_filter("Postman environments", &["json"]).add_filter("All files", &["*"]).pick_file() else {
                        return;
                    };
                    let result = std::fs::read(&path).map_err(|e| e.to_string()).and_then(|d| postman::import_environment(&d));
                    match result {
                        Ok(e) => app.add_environment(e, false),
                        Err(e) => app.warn(&format!("{} could not be imported: {e}.", file_name(&path))),
                    }
                }));
            }
            ListEvent::Renamed(..) => {}
        }
    }

    fn add_environment(&mut self, environment: env::Environment, edit: bool) {
        self.envs.environments.push(environment);
        self.envs.active = self.envs.environments.len() as i64 - 1;
        self.envs.sort();
        self.save_envs();
        if edit {
            self.open_editor(true);
        }
    }

    /// Shows the selected environment in place of the request, or closes
    /// the editor if none is selected.
    fn open_editor(&mut self, focus: bool) {
        let Some(e) = self.envs.current().cloned() else {
            self.close_editor();
            return;
        };
        self.editor.load(&e);
        self.editor.focus_name = focus;
        self.editor_open = true;
    }

    fn close_editor(&mut self) {
        self.editor_open = false;
    }

    // ---- requests ----

    fn is_grpc_open(&self) -> bool {
        self.open.and_then(|id| self.store.request(id)).is_some_and(|r| r.is_grpc())
    }

    /// Shows a saved request selected in the sidebar: in the tab it is
    /// pinned in, or else in the tab that is not pinned.
    fn open_request(&mut self, id: u64) {
        if self.open != Some(id) {
            self.stash();
            let at = self.tabs.open(Some(id));
            self.show_tab(at, false);
        }
        self.close_editor();
    }

    /// Keeps the request being composed in the current tab, if it is one
    /// that is not saved.
    fn stash(&mut self) {
        if self.tabs.current().request.is_none() {
            let draft = self.rest.request();
            self.tabs.current_mut().draft = Some(draft);
        }
    }

    /// Shows the request of a tab, with the response it was last given. The
    /// request is selected in the sidebar too if `sync` is set.
    fn show_tab(&mut self, at: usize, sync: bool) {
        self.tabs.current = at.min(self.tabs.tabs.len() - 1);
        let tab = self.tabs.current().clone();
        self.show(tab.request);
        if tab.request.is_none() {
            if let Some(d) = &tab.draft {
                self.rest.load(d);
            }
        }
        if let Some(replay) = &tab.replay {
            self.replay(replay);
        }
        if sync {
            self.tree.select_request(&self.store, tab.request);
        }
    }

    /// Replaces what the request UI shows with a saved request, or with an
    /// empty REST request if there is none. The environment editor is left
    /// open if it is.
    fn show(&mut self, request: Option<u64>) {
        if let Some(f) = self.in_flight.take() {
            f.abort.abort();
        }
        self.shown += 1;
        self.open = request;
        let saved = request.and_then(|id| self.store.request(id)).cloned();
        match saved {
            None => {
                self.open = None;
                self.rest.load(&rest::Request { method: "GET".into(), ..Default::default() });
                self.rest_response.clear();
                self.rest.captures.show_statuses(HashMap::new());
            }
            Some(r) if r.is_grpc() => {
                self.grpc.load(r.grpc.as_ref().unwrap());
                self.grpc_response.clear();
                self.grpc.captures.show_statuses(HashMap::new());
                self.reflect(false);
            }
            Some(r) => {
                self.rest.load(r.rest.as_ref().unwrap());
                self.rest_response.clear();
                self.rest.captures.show_statuses(HashMap::new());
            }
        }
    }

    /// Shows a result in the response panel of its kind.
    fn replay(&mut self, replay: &Replay) {
        let grpc = self.is_grpc_open();
        let (panel, captures) = if grpc { (&mut self.grpc_response, &mut self.grpc.captures) } else { (&mut self.rest_response, &mut self.rest.captures) };
        panel.replay(replay);
        let marks = panel
            .captured
            .iter()
            .map(|c| {
                let status = match &c.error {
                    None => crate::kvtable::Status { good: true, tip: "Captured".into() },
                    Some(e) => crate::kvtable::Status { good: false, tip: format!("Not captured: {e}") },
                };
                (c.variable.clone(), status)
            })
            .collect();
        captures.show_statuses(marks);
    }

    fn tree_output(&mut self, out: tree::Output) {
        if out.save {
            self.save_store();
        }
        for r in &out.removed {
            self.removed_request(*r);
        }
        if let Some(id) = out.open {
            self.open_request(id);
        }
        if out.add_request {
            self.ask_request_type();
        }
        if out.import {
            self.import_requests();
        }
        if let Some((sel, question)) = out.confirm_remove {
            self.dialogs.push(Dialog::confirm(&question, move |app| {
                let (removed, open) = app.tree.remove(&mut app.store, sel);
                app.save_store();
                for r in removed {
                    app.removed_request(r);
                }
                if let Some(id) = open {
                    app.open_request(id);
                }
            }));
        }
    }

    /// Closes the tabs of a request that has gone, showing another if it
    /// was the one on show.
    fn removed_request(&mut self, id: u64) {
        self.tabs.remove(id);
        if self.open == Some(id) {
            self.show_tab(self.tabs.current, false);
        }
    }

    /// Asks the type of a request to add, and adds it.
    fn ask_request_type(&mut self) {
        let choices = vec![
            ("REST".to_string(), Glyph::Braces, self.theme.colour(Kind::Rest)),
            ("gRPC".to_string(), Glyph::Arrows, self.theme.colour(Kind::Grpc)),
        ];
        self.dialogs.push(Dialog::choice("New Request", "Choose the type of request to add.", choices, |app, picked| {
            let request = if picked == 1 {
                collection::Request::new_grpc("New Request", rpc::Request::default())
            } else if app.open.is_none() {
                // An unsaved request being composed becomes the new request.
                collection::Request::new_rest("New Request", app.rest.request())
            } else {
                collection::Request::new_rest("New Request", rest::Request { method: "GET".into(), ..Default::default() })
            };
            let id = app.tree.add_request(&mut app.store, request);
            app.save_store();
            app.side = Side::Requests;
            app.open_request(id);
        }));
    }

    /// Asks what kind of file to import, and for the file, and imports it.
    fn import_requests(&mut self) {
        let choices = vec![
            ("OpenAPI".to_string(), Glyph::Document, crate::theme::parse("#6ba539").unwrap()),
            ("Postman".to_string(), Glyph::Plane, crate::theme::parse("#ef5b25").unwrap()),
        ];
        self.dialogs.push(Dialog::choice("Import", "Choose what to import requests from.", choices, |app, picked| {
            let (label, filter, extensions): (&str, &str, &[&str]) = if picked == 0 {
                ("OpenAPI", "OpenAPI documents", &["json", "yaml", "yml"])
            } else {
                ("Postman", "Postman collections", &["json"])
            };
            let Some(path) = rfd::FileDialog::new().set_title(format!("Import {label}")).add_filter(filter, extensions).add_filter("All files", &["*"]).pick_file() else {
                return;
            };
            let result = std::fs::read(&path).map_err(|e| e.to_string()).and_then(|d| if picked == 0 { openapi::import(&d) } else { postman::import(&d) });
            match result {
                Ok(mut folder) => {
                    // The folders inside it start off closed, as there can be
                    // a great many.
                    folder.collapse();
                    app.tree.adopt(&mut app.store, folder);
                    app.save_store();
                }
                Err(e) => app.warn(&format!("{} could not be imported: {e}.", file_name(&path))),
            }
        }));
    }

    fn warn(&mut self, message: &str) {
        self.dialogs.push(Dialog::message("TriggerAPI", message));
    }

    // ---- sending ----

    fn options(&self) -> rest::Options {
        rest::Options {
            timeout: (self.config.timeout > 0).then(|| Duration::from_secs(self.config.timeout as u64)),
            no_redirects: self.config.no_redirects,
        }
    }

    fn expander(&self) -> env::Expander {
        self.envs.expander()
    }

    /// Cancels the request in flight, showing that it was cancelled.
    fn cancel(&mut self) {
        let Some(f) = self.in_flight.take() else { return };
        f.abort.abort();
        let replay = Replay::Cancelled(f.start.elapsed());
        if f.shown == self.shown {
            self.replay(&replay);
        }
        if let Some(tab) = self.tabs.by_id(f.tab) {
            tab.replay = Some(replay);
        }
    }

    /// Does the work of Send, Execute or Invoke in the background, or
    /// cancels the request in flight.
    fn run(&mut self, work: Work) {
        if self.in_flight.is_some() {
            self.cancel();
            return;
        }
        let expand = self.expander();
        let options = self.options();
        let limit = options.timeout;
        let token = self.next_token;
        self.next_token += 1;
        let tx = self.tx.clone();
        let ctx = self.ctx.clone();

        let (captures, task): (Vec<Rule>, std::pin::Pin<Box<dyn std::future::Future<Output = Outcome> + Send>>) = match work {
            Work::Send | Work::Execute => {
                let request = self.rest.request();
                let captures = request.captures.clone();
                let mut req = if work == Work::Execute {
                    // What runs is the command as it is shown: a value made
                    // by a function would not be the same if it were made
                    // again.
                    self.rest.commanded.clone().unwrap_or_else(|| request.expand(&*expand))
                } else {
                    request.expand(&*expand)
                };
                req.options = options;
                self.rest_response.clear();
                self.rest.captures.show_statuses(HashMap::new());
                let execute = work == Work::Execute;
                (captures, Box::pin(async move {
                    let result = if execute { triggerapi_core::curl::run_curl(&req).await } else { rest::send(&req).await };
                    match result {
                        Err(e) if e == rest::TIMED_OUT => Outcome::TimedOut,
                        other => Outcome::Rest(other),
                    }
                }))
            }
            Work::Invoke => {
                let request = self.grpc.request();
                let captures = request.captures.clone();
                let req = request.expand(&*expand);
                let schema = req.target().ok().and_then(|t| self.schemas.get(&t).cloned());
                self.grpc_response.clear();
                self.grpc.captures.show_statuses(HashMap::new());
                (captures, Box::pin(async move { Outcome::Grpc(rpc::invoke(&req, schema.as_deref()).await) }))
            }
        };

        let handle = self.rt.spawn(async move {
            let outcome = match limit {
                Some(limit) => tokio::time::timeout(limit, task).await.unwrap_or(Outcome::TimedOut),
                None => task.await,
            };
            let _ = tx.send(Msg::Done { token, outcome });
            ctx.request_repaint();
        });
        self.in_flight = Some(InFlight {
            token,
            abort: handle.abort_handle(),
            work,
            shown: self.shown,
            tab: self.tabs.current().id,
            start: Instant::now(),
            captures,
        });
    }

    fn done(&mut self, token: u64, outcome: Outcome) {
        let Some(f) = self.in_flight.as_ref() else { return };
        if f.token != token {
            return;
        }
        let f = self.in_flight.take().unwrap();
        let elapsed = f.start.elapsed();

        // The request was made for what it captures as much as for what it
        // shows, so that is kept whatever is on show by now.
        let replay = match outcome {
            Outcome::TimedOut => Replay::Error(
                format!("timed out after {}, which is the limit in the settings", tree::plural(self.config.timeout as usize, "second")),
                elapsed,
            ),
            Outcome::Rest(Err(e)) | Outcome::Grpc(Err(e)) => Replay::Error(e, elapsed),
            Outcome::Rest(Ok(res)) => {
                let captured = if (200..300).contains(&res.status_code) {
                    self.capture(&f.captures, &String::from_utf8_lossy(&res.body))
                } else {
                    Vec::new()
                };
                Replay::Rest(Arc::new(res), captured)
            }
            Outcome::Grpc(Ok(res)) => {
                // Values are captured from the last message of a call that
                // ended with OK.
                let captured = if !res.ok() {
                    Vec::new()
                } else if let Some(last) = res.messages.last() {
                    self.capture(&f.captures, last)
                } else {
                    failed_captures(&f.captures, "there was no response message")
                };
                Replay::Grpc(Arc::new(res), captured)
            }
        };
        if f.shown == self.shown {
            self.replay(&replay);
        }
        if let Some(tab) = self.tabs.by_id(f.tab) {
            tab.replay = Some(replay);
        }
    }

    /// Sets variables of the active environment from the JSON of a response,
    /// as the rules say, and returns what came of each that is in use.
    fn capture(&mut self, rules: &[Rule], document: &str) -> Vec<Captured> {
        let rules = enabled_captures(rules);
        if rules.is_empty() {
            return Vec::new();
        }
        let Some(environment) = self.envs.current_mut() else {
            return failed_captures(&rules, "no environment is selected to keep it in");
        };
        let mut results = Vec::new();
        let mut changed = false;
        for rule in &rules {
            match rule.extract(document) {
                Ok(value) => {
                    environment.set(&rule.variable, &value);
                    changed = true;
                    results.push(Captured { variable: rule.variable.clone(), error: None });
                }
                Err(e) => results.push(Captured { variable: rule.variable.clone(), error: Some(e) }),
            }
        }
        if changed {
            self.save_envs();
            if self.editor_open {
                if let Some(e) = self.envs.current().cloned() {
                    self.editor.load(&e);
                }
            }
        }
        results
    }

    // ---- gRPC ----

    /// Loads the service definition of the server of the gRPC request, to
    /// offer its methods. One loaded before is used again unless `force`.
    fn reflect(&mut self, force: bool) {
        self.reflecting += 1;
        let reflecting = self.reflecting;
        let req = self.grpc.request().expand(&|s| self.envs.expand(s));
        let Ok(target) = req.target() else {
            // There is no server to ask yet.
            self.grpc.status.clear();
            return;
        };
        self.grpc.target = self.grpc.target_key();
        if let Some(schema) = self.schemas.get(&target).cloned() {
            if !force {
                self.set_schema(&schema);
                return;
            }
        }
        self.grpc.status.clear();
        let tx = self.tx.clone();
        let ctx = self.ctx.clone();
        self.rt.spawn(async move {
            let result = match tokio::time::timeout(REFLECT_TIMEOUT, rpc::reflect(&req)).await {
                Ok(r) => r.map(Arc::new),
                Err(_) => Err("the server took too long to describe its services".to_string()),
            };
            let _ = tx.send(Msg::Reflected { target, reflecting, result });
            ctx.request_repaint();
        });
    }

    fn set_schema(&mut self, schema: &rpc::Schema) {
        if self.grpc.set_schema(schema) {
            self.grpc_changed();
        }
        self.sync_endpoints();
    }

    fn grpc_changed(&mut self) {
        if let Some(id) = self.open {
            let request = self.grpc.request();
            if let Some(r) = self.store.request_mut(id) {
                if r.is_grpc() {
                    r.grpc = Some(request);
                    self.save_store();
                }
            }
        }
    }

    /// Returns the id of the folder of requests made from endpoints that the
    /// gRPC request on show is in: the folder holding its folder, when that
    /// is named after a service of the server.
    fn endpoint_collection(&self) -> Option<u64> {
        let id = self.open?;
        if !self.store.request(id)?.is_grpc() {
            return None;
        }
        let service = self.store.folder_of(id)?;
        if !self.grpc.endpoints.has_service(&service.name) {
            return None;
        }
        self.store.parent_of(service.id)?
    }

    fn sync_endpoints(&mut self) {
        match self.endpoint_collection().and_then(|outer| self.store.folder(outer)) {
            Some(outer) => {
                let existing = grpc::endpoints_of(outer);
                self.grpc.endpoints.set_existing(existing, true);
            }
            None => self.grpc.endpoints.set_existing(HashSet::new(), false),
        }
    }

    /// Makes a gRPC request for each method, in the collection the request
    /// on show is in, or else in a new folder named after the server.
    fn create_requests(&mut self, methods: Vec<String>) {
        let url = self.grpc.url.clone();
        if let Some(outer) = self.endpoint_collection() {
            if let Some(folder) = self.store.folder_mut(outer) {
                grpc::add_endpoints(folder, &url, &methods);
            }
            self.tree.extended(&mut self.store, self.open);
        } else {
            let name = if url.trim().is_empty() { "New Folder".to_string() } else { url.trim().to_string() };
            self.tree.adopt(&mut self.store, grpc::endpoint_folder(&name, &url, &methods));
        }
        self.save_store();
        self.side = Side::Requests;
    }

    /// Replaces the message with an example for the method.
    fn use_example(&mut self) {
        let req = self.grpc.request().expand(&|s| self.envs.expand(s));
        let target = req.target().unwrap_or_default();
        let Some(schema) = self.schemas.get(&target).cloned() else {
            self.grpc.status = "An example needs the service definition, which has not been loaded. Enter the server address and press Reload.".into();
            return;
        };
        match schema.example(&req.method) {
            Ok(example) => {
                self.grpc.message = example;
                self.grpc_changed();
            }
            Err(e) => self.grpc.status = format!("An example could not be made: {e}"),
        }
    }

    // ---- settings ----

    fn open_settings(&mut self) {
        let dialog = settings_dialog::Dialog::new(&self.config, &self.default_workspaces_dir());
        self.dialogs.push(Dialog::Settings(Box::new(dialog)));
    }

    /// Puts settings into effect and saves them. A setting that cannot be
    /// applied is left as it was, and reported.
    fn apply_settings(&mut self, s: Settings, passphrase: Option<String>) {
        let mut failed: Vec<String> = Vec::new();

        if s.response_font != self.code_font {
            crate::fonts::install(&self.ctx, &s.response_font);
            self.code_font = s.response_font.clone();
        }
        self.config.response_font = s.response_font.clone();
        self.config.timeout = s.timeout;
        self.config.no_redirects = s.no_redirects;
        self.rest.stale = true;
        self.config.json_colours = s.json_colours.clone();
        self.config.colours = s.colours.clone();
        self.theme = Theme { colours: s.colours.clone(), json: s.json_colours.clone() };
        self.rest_response.restyle();
        self.grpc_response.restyle();

        // Keys that could not be opened are tried again, as the passphrase
        // may not have been given.
        let keys_failed = self.keys.as_ref().is_some_and(|k| k.err().is_some());
        if s.public_key != self.config.public_key || s.private_key != self.config.private_key || keys_failed {
            match open_keys(&s.public_key, &s.private_key, passphrase.clone()) {
                Opened::NeedPassphrase => {
                    let path = s.private_key.clone();
                    self.dialogs.push(Dialog::passphrase(&path, move |app, given| {
                        app.apply_settings(s, Some(given.unwrap_or_default()));
                    }));
                    return;
                }
                Opened::Failed(e) => failed.push(e),
                Opened::None => {
                    self.config.public_key.clear();
                    self.config.private_key.clear();
                    let _ = self.change_keys(None);
                }
                Opened::Keys(k) => {
                    self.config.public_key = s.public_key.clone();
                    self.config.private_key = s.private_key.clone();
                    if let Err(e) = self.change_keys(Some(k)) {
                        failed.push(format!("not every file could be given the keys: {e}"));
                    }
                }
            }
        }

        let (setting, previous) = (self.config.workspaces_dir.clone(), self.workspaces_dir());
        self.config.workspaces_dir = s.workspaces_dir.clone();
        let dir = self.workspaces_dir();
        if dir != previous {
            if let Err(e) = self.move_workspaces(&previous) {
                self.config.workspaces_dir = setting;
                failed.push(format!("the workspaces were not moved to {}: {e}", dir.display()));
            }
        }

        if let Err(e) = self.settings().save(&self.files.settings) {
            failed.push(format!("the settings were not saved: {e}"));
        }
        if !failed.is_empty() {
            self.dialogs.push(Dialog::message("Settings", &failed.join("\n")));
        }
    }

    /// Has the files of every workspace encrypted with other keys from now
    /// on. Taking the keys away leaves the files as they are.
    fn change_keys(&mut self, keys: Option<Keys>) -> Result<(), String> {
        let Some(keys) = keys else {
            self.keys = None;
            let name = self.config.workspace.clone();
            self.open_workspace(&name);
            self.dialogs.push(Dialog::message("Keys Needed", NEED_KEYS));
            return Ok(());
        };
        let dir = self.workspaces_dir();
        let mut failed = Vec::new();
        for name in workspace::list(&dir).unwrap_or_default() {
            for path in [workspace::environments(&dir, &name), workspace::requests(&dir, &name)] {
                if let Err(e) = jsonfile::convert(&path, self.keys.as_ref(), Some(&keys)) {
                    failed.push(format!("{}: {e}", path.display()));
                }
            }
        }
        self.keys = Some(keys);
        if self.envs_path.is_none() || self.store_path.is_none() {
            let name = self.config.workspace.clone();
            self.open_workspace(&name);
        }
        if failed.is_empty() {
            Ok(())
        } else {
            Err(failed.join("; "))
        }
    }

    /// Keeps the workspaces in another folder from now on, copying the ones
    /// there are into it if it has none.
    fn move_workspaces(&mut self, from: &Path) -> Result<(), String> {
        let to = self.workspaces_dir();
        if workspace::list(&to)?.is_empty() {
            workspace::copy_all(from, &to)?;
        }
        self.list_workspaces();
        if !self.workspaces.contains(&self.config.workspace) {
            self.config.workspace = self.workspaces.first().cloned().unwrap_or_default();
        }
        let name = self.config.workspace.clone();
        self.open_workspace(&name);
        Ok(())
    }

    /// Asks whose public key the workspaces are to be exported for and
    /// where to, and exports them.
    fn export_workspaces(&mut self) {
        let Some(keys) = self.keys.clone() else {
            self.dialogs.push(Dialog::message("Keys Needed", NEED_KEYS));
            return;
        };
        let ssh = dirs::home_dir().map(|h| h.join(".ssh")).unwrap_or_default();
        let Some(public) = rfd::FileDialog::new().set_title("Public Key To Export For").set_directory(ssh).pick_file() else { return };
        let home = dirs::home_dir().unwrap_or_default();
        let Some(archive) = rfd::FileDialog::new()
            .set_title("Export Workspaces")
            .set_directory(home)
            .set_file_name("triggerapi-workspaces.zip")
            .add_filter("Zip files", &["zip"])
            .save_file()
        else {
            return;
        };
        let result = Keys::recipient(&public.display().to_string()).and_then(|r| workspace::export(&self.workspaces_dir(), &archive, Some(&keys), Some(&r)));
        match result {
            Ok(()) => self.dialogs.push(Dialog::message(
                "Export Workspaces",
                &format!("The workspaces were exported to {}, which can be read only with the private key that goes with {}.", archive.display(), public.display()),
            )),
            Err(e) => self.dialogs.push(Dialog::message("Export Workspaces", &format!("The workspaces were not exported: {e}."))),
        }
    }

    /// Asks for a file of exported workspaces and adds them.
    fn import_workspaces(&mut self) {
        let Some(keys) = self.keys.clone() else {
            self.dialogs.push(Dialog::message("Keys Needed", NEED_KEYS));
            return;
        };
        let Some(archive) = rfd::FileDialog::new().set_title("Import Workspaces").add_filter("Zip files", &["zip"]).add_filter("All files", &["*"]).pick_file() else {
            return;
        };
        match workspace::unpack(&self.workspaces_dir(), &archive, Some(&keys)) {
            Ok(added) => {
                self.list_workspaces();
                self.dialogs.push(Dialog::message("Import Workspaces", &format!("Imported {}.", added.join(", "))));
            }
            Err(e) => self.dialogs.push(Dialog::message("Import Workspaces", &format!("The workspaces were not all imported: {e}."))),
        }
    }

    /// Returns the state to remember for the next run.
    fn settings(&mut self) -> Settings {
        self.keep_pins();
        let mut s = self.config.clone();
        s.sidebar_width = self.sidebar_width.round() as i64;
        s.response_height = self.response_height.round() as i64;
        let (size, maximized) = self.ctx.input(|i| (i.viewport().inner_rect.map(|r| r.size()), i.viewport().maximized.unwrap_or(false)));
        s.maximized = maximized;
        if let (Some(size), false) = (size, maximized) {
            s.width = size.x.round() as i64;
            s.height = size.y.round() as i64;
        }
        s
    }

    // ---- keys and focus ----

    /// Closes what is open in place of the request: the environment editor,
    /// or else the find bar, or else the curl command or the URL preview.
    fn escape(&mut self) {
        if self.editor_open {
            self.close_editor();
        } else if self.is_grpc_open() {
            self.grpc_response.close_find();
        } else if !self.rest_response.close_find() {
            self.rest.escape();
        }
    }

    fn shortcuts(&mut self, ctx: &Context) {
        let ctrl = |key| KeyboardShortcut::new(Modifiers::COMMAND, key);
        let pressed = |s: KeyboardShortcut| ctx.input_mut(|i| i.consume_shortcut(&s));

        // Escape reaches the window when what has the keyboard has no use
        // for it, so it still cancels the editing of a text.
        let typing = ctx.memory(|m| m.focused()).is_some_and(|id| egui::TextEdit::load_state(ctx, id).is_some());
        if !typing && ctx.input(|i| i.key_pressed(Key::Escape)) && self.dialogs.is_empty() {
            self.escape();
        }
        if !self.dialogs.is_empty() {
            return;
        }
        if pressed(ctrl(Key::Enter)) && !self.editor_open {
            if self.is_grpc_open() {
                self.run(Work::Invoke);
            } else {
                self.run(Work::Send);
            }
        }
        if pressed(ctrl(Key::F)) && !self.editor_open {
            if self.is_grpc_open() {
                self.grpc_response.open_find(ctx);
            } else {
                self.rest_response.open_find(ctx);
            }
        }
        if pressed(ctrl(Key::P)) {
            self.side = Side::Requests;
            self.tree.focus_search = true;
        }
        for (key, side) in [(Key::Num1, Side::Workspaces), (Key::Num2, Side::Environments), (Key::Num3, Side::Requests)] {
            if pressed(ctrl(key)) {
                self.side = side;
                match side {
                    Side::Workspaces => self.workspace_list.focus = true,
                    Side::Environments => self.env_list.focus = true,
                    Side::Requests => self.tree.focus_tree = true,
                }
            }
        }
        if pressed(ctrl(Key::Comma)) {
            self.open_settings();
        }
        if pressed(ctrl(Key::Q)) {
            ctx.send_viewport_cmd(egui::ViewportCommand::Close);
        }
        if pressed(ctrl(Key::I)) {
            self.dialogs.push(Dialog::About);
        }
    }

    // ---- drawing ----

    fn menu_bar(&mut self, ui: &mut egui::Ui) {
        egui::menu::bar(ui, |ui| {
            ui.menu_button("File", |ui| {
                if ui.add(egui::Button::new("Settings…").shortcut_text("Ctrl+,")).clicked() {
                    ui.close_menu();
                    self.open_settings();
                }
                ui.separator();
                if ui.add(egui::Button::new("Quit").shortcut_text("Ctrl+Q")).clicked() {
                    ui.close_menu();
                    ui.ctx().send_viewport_cmd(egui::ViewportCommand::Close);
                }
            });
            ui.menu_button("Help", |ui| {
                if ui.add(egui::Button::new("About").shortcut_text("Ctrl+I")).clicked() {
                    ui.close_menu();
                    self.dialogs.push(Dialog::About);
                }
            });
        });
    }

    fn sidebar(&mut self, ui: &mut egui::Ui) {
        ui.horizontal_top(|ui| {
            // The tabs are icons down the left edge, named in their
            // tooltips.
            ui.vertical(|ui| {
                ui.set_width(40.0);
                for (side, glyph, kind, name) in [
                    (Side::Workspaces, Glyph::Monitor, Kind::Workspaces, "Workspaces"),
                    (Side::Environments, Glyph::Globe, Kind::Environments, "Environments"),
                    (Side::Requests, Glyph::Folder, Kind::Requests, "Requests"),
                ] {
                    let (rect, response) = ui.allocate_exact_size(vec2(40.0, 44.0), egui::Sense::click());
                    if self.side == side {
                        ui.painter().rect_filled(rect, egui::CornerRadius::same(4), ui.visuals().selection.bg_fill.gamma_multiply(0.5));
                    } else if response.hovered() {
                        ui.painter().rect_filled(rect, egui::CornerRadius::same(4), ui.visuals().widgets.hovered.weak_bg_fill);
                    }
                    icons::paint(ui.painter(), egui::Rect::from_center_size(rect.center(), vec2(30.0, 30.0)), self.theme.colour(kind), glyph);
                    if response.on_hover_text(name).clicked() {
                        self.side = side;
                    }
                }
            });
            ui.separator();
            ui.vertical(|ui| match self.side {
                Side::Workspaces => {
                    let selected = self.workspaces.iter().position(|n| *n == self.config.workspace);
                    let names = self.workspaces.clone();
                    let events = self.workspace_list.workspaces(ui, &names, selected, &self.theme);
                    for e in events {
                        self.workspace_event(e);
                    }
                }
                Side::Environments => {
                    let names: Vec<String> = self.envs.environments.iter().map(|e| e.name.clone()).collect();
                    let selected = usize::try_from(self.envs.active).ok().filter(|i| *i < names.len());
                    let events = self.env_list.environments(ui, &names, selected, &self.theme);
                    for e in events {
                        self.env_event(e);
                    }
                }
                Side::Requests => {
                    let theme = self.theme.clone();
                    let out = self.tree.ui(ui, &mut self.store, &theme);
                    self.tree_output(out);
                }
            });
        });
    }

    /// The line above the tabs, with the workspace and the environment in
    /// use, which select them in the sidebar.
    fn selector(&mut self, ui: &mut egui::Ui) {
        ui.horizontal(|ui| {
            ui.with_layout(egui::Layout::right_to_left(egui::Align::Center), |ui| {
                let some = self.envs.current().is_some();
                if icons::button(ui, Glyph::Globe, self.theme.colour(Kind::Environments), "Edit the selected environment", some).clicked() {
                    self.open_editor(true);
                }
                let names: Vec<String> = self.envs.environments.iter().map(|e| e.name.clone()).collect();
                let current = self.envs.current().map(|e| e.name.clone()).unwrap_or_else(|| "None".into());
                let mut chosen: Option<i64> = None;
                egui::ComboBox::from_id_salt("environment selector").selected_text(current).show_ui(ui, |ui| {
                    if ui.selectable_label(self.envs.active < 0, "None").clicked() {
                        chosen = Some(-1);
                    }
                    for (i, name) in names.iter().enumerate() {
                        if ui.selectable_label(self.envs.active == i as i64, name).clicked() {
                            chosen = Some(i as i64);
                        }
                    }
                });
                ui.label(RichText::new("Environment:").strong());
                ui.add_space(12.0);
                let mut workspace: Option<String> = None;
                egui::ComboBox::from_id_salt("workspace selector").selected_text(&self.config.workspace).show_ui(ui, |ui| {
                    for name in &self.workspaces {
                        if ui.selectable_label(*name == self.config.workspace, name).clicked() {
                            workspace = Some(name.clone());
                        }
                    }
                });
                ui.label(RichText::new("Workspace:").strong());
                if let Some(c) = chosen {
                    if c != self.envs.active {
                        self.envs.active = c;
                        self.save_envs();
                        if self.editor_open {
                            self.open_editor(false);
                        }
                    }
                }
                if let Some(name) = workspace {
                    if name != self.config.workspace {
                        self.open_workspace(&name);
                    }
                }
            });
        });
    }

    fn page(&mut self, ui: &mut egui::Ui) {
        let theme = self.theme.clone();
        let code = crate::fonts::code();
        if self.editor_open {
            let found = |name: &str| self.envs.current().and_then(|e| e.lookup(name)).is_some_and(|v| !v.is_empty());
            let body = egui::TextStyle::Body.resolve(ui.style());
            let out = self.editor.ui(ui, &theme, &found, body);
            if let Some((name, variables)) = out.changed {
                if let Some(e) = self.envs.current_mut() {
                    let renamed = e.name != name;
                    e.name = name;
                    e.variables = variables;
                    if renamed {
                        self.envs.sort();
                    }
                }
                self.save_envs();
            }
            if out.close {
                self.close_editor();
            }
            return;
        }

        let grpc = self.is_grpc_open();
        let busy = self.in_flight.as_ref().map(|f| f.work);
        let request_name = self.open.and_then(|id| self.store.request(id)).map(|r| r.name.clone()).unwrap_or_default();
        let default_height = if self.response_height > 0.0 { self.response_height } else { ui.available_height() / 2.0 };
        if !grpc && self.rest.stale {
            let environment = self.envs.current().cloned();
            let options = self.options();
            self.rest.refresh(environment.as_ref(), options);
        }

        // The response is beneath the request, at the height it was last
        // dragged to, which both kinds of request share.
        let envs = &self.envs;
        let found = |name: &str| envs.current().and_then(|e| e.lookup(name)).is_some_and(|v| !v.is_empty());
        let (grpc_panel, rest_panel) = (&mut self.grpc, &mut self.rest);
        let (grpc_response, rest_response) = (&mut self.grpc_response, &mut self.rest_response);
        let panel = egui::TopBottomPanel::bottom("response")
            .resizable(true)
            .default_height(default_height)
            .min_height(80.0)
            .max_height((ui.available_height() - 120.0).max(100.0))
            .show_inside(ui, |ui| {
                if grpc {
                    grpc_response.ui(ui, &theme, &code, &mut grpc_panel.captures, &found, &request_name)
                } else {
                    rest_response.ui(ui, &theme, &code, &mut rest_panel.captures, &found, &request_name)
                }
            });
        let response_height = panel.response.rect.height();
        let captures_changed = panel.inner.captures_changed;

        let mut grpc_out = None;
        let mut rest_out = None;
        egui::CentralPanel::default().frame(egui::Frame::NONE).show_inside(ui, |ui| {
            if grpc {
                grpc_out = Some(grpc_panel.ui(ui, &theme, &code, &found, busy == Some(Work::Invoke)));
            } else {
                rest_out = Some(rest_panel.ui(ui, &theme, &code, &found, busy.is_some_and(|w| w != Work::Invoke)));
            }
        });
        self.response_height = response_height;

        if let Some(out) = grpc_out {
            if out.changed || captures_changed {
                self.grpc_changed();
            }
            if out.target {
                self.reflect(false);
            }
            if out.reload {
                self.reflect(true);
            }
            if out.example {
                self.use_example();
            }
            if let Some(methods) = out.create {
                self.create_requests(methods);
            }
            if out.invoke {
                self.run(Work::Invoke);
            }
        }
        if let Some(out) = rest_out {
            if out.changed || captures_changed {
                if let Some(id) = self.open {
                    let request = self.rest.request();
                    if let Some(r) = self.store.request_mut(id) {
                        if !r.is_grpc() {
                            r.rest = Some(request);
                            self.save_store();
                        }
                    }
                }
            }
            if out.send {
                self.run(Work::Send);
            }
            if out.execute {
                self.run(Work::Execute);
            }
        }
    }

    fn dialogs_ui(&mut self, ctx: &Context) {
        let Some(mut dialog) = self.dialogs.pop() else { return };
        let dark = ctx.style().visuals.dark_mode;
        match dialog.ui(ctx, &self.theme, dark) {
            DialogAction::Keep => self.dialogs.push(dialog),
            DialogAction::Close => {}
            DialogAction::Run(f) => f(self),
            DialogAction::Settings(s) => self.apply_settings(*s, None),
            DialogAction::Export => {
                self.dialogs.push(dialog);
                self.export_workspaces();
            }
            DialogAction::Import => {
                self.dialogs.push(dialog);
                self.import_workspaces();
            }
        }
    }

    fn messages(&mut self) {
        while let Ok(msg) = self.rx.try_recv() {
            match msg {
                Msg::Done { token, outcome } => self.done(token, outcome),
                Msg::Reflected { target, reflecting, result } => {
                    if let Ok(schema) = &result {
                        self.schemas.insert(target, schema.clone());
                    }
                    // A later request for a definition has replaced this
                    // one.
                    if reflecting != self.reflecting {
                        continue;
                    }
                    match result {
                        Ok(schema) => self.set_schema(&schema),
                        Err(e) => self.grpc.set_schema_error(&e),
                    }
                }
            }
        }
    }
}

#[cfg(test)]
impl App {
    /// Opens the saved request of a name, as clicking it in the tree does.
    pub fn test_open(&mut self, name: &str) {
        let id = self.store.all().iter().find(|r| r.name == name).map(|r| r.id).expect("a request of that name");
        self.tree.select_request(&self.store, Some(id));
        self.open_request(id);
    }

    pub fn test_send(&mut self) {
        if self.is_grpc_open() {
            self.run(Work::Invoke);
        } else {
            self.run(Work::Send);
        }
    }

    pub fn test_set_host(&mut self, host: &str) {
        if let Some(e) = self.envs.current_mut() {
            e.set("host", host);
            e.set("room", "101");
        }
        self.save_envs();
    }

    pub fn test_busy(&self) -> bool {
        self.in_flight.is_some()
    }

    pub fn test_side(&mut self, side: Side) {
        self.side = side;
    }

    pub fn test_editor(&mut self) {
        self.open_editor(false);
    }

    pub fn test_settings(&mut self) {
        self.open_settings();
    }

    pub fn test_find(&mut self, query: &str) {
        let ctx = self.ctx.clone();
        self.rest_response.open_find(&ctx);
        self.rest_response.test_query(query);
    }
}

/// Returns the rules that are in use: those that are enabled and name a
/// variable.
fn enabled_captures(rules: &[Rule]) -> Vec<Rule> {
    rules
        .iter()
        .filter_map(|r| {
            let mut r = r.clone();
            r.variable = r.variable.trim().to_string();
            (r.enabled && !r.variable.is_empty()).then_some(r)
        })
        .collect()
}

/// Returns the results of rules none of which could be followed, all for the
/// same reason.
fn failed_captures(rules: &[Rule], err: &str) -> Vec<Captured> {
    enabled_captures(rules).into_iter().map(|r| Captured { variable: r.variable, error: Some(err.into()) }).collect()
}

fn file_name(path: &Path) -> String {
    path.file_name().map(|n| n.to_string_lossy().into_owned()).unwrap_or_default()
}

impl eframe::App for App {
    fn update(&mut self, ctx: &Context, _frame: &mut eframe::Frame) {
        self.messages();
        if self.in_flight.is_some() {
            ctx.request_repaint_after(Duration::from_millis(250));
        }
        self.shortcuts(ctx);

        egui::TopBottomPanel::top("menu").show(ctx, |ui| self.menu_bar(ui));
        egui::TopBottomPanel::bottom("status").show(ctx, |ui| {
            ui.label(RichText::new(if self.status.is_empty() { " " } else { &self.status }).small());
        });
        let sidebar = egui::SidePanel::left("sidebar")
            .resizable(true)
            .default_width(self.sidebar_width)
            .width_range(200.0..=800.0)
            .show(ctx, |ui| self.sidebar(ui));
        self.sidebar_width = sidebar.response.rect.width();

        egui::CentralPanel::default().show(ctx, |ui| {
            self.selector(ui);
            if !self.editor_open {
                let theme = self.theme.clone();
                if let Some((from, to)) = self.tabs.ui(ui, &self.store, &theme) {
                    // The tab left keeps what was typed in a request that is
                    // not saved.
                    self.tabs.current = from;
                    self.stash();
                    self.show_tab(to, true);
                }
                ui.separator();
            }
            self.page(ui);
        });

        if let Some(warning) = self.rest_response.warning.take().or_else(|| self.grpc_response.warning.take()) {
            self.warn(&warning);
        }
        self.dialogs_ui(ctx);

        if ctx.input(|i| i.viewport().close_requested()) && !self.closing {
            self.closing = true;
            let settings = self.settings();
            if let Err(e) = settings.save(&self.files.settings) {
                eprintln!("triggerapi: settings could not be saved: {e}");
            }
        }
    }
}
