//! The gRPC request UI: the server, the method chosen from those its service
//! definition lists, Invoke, and tabs for the message, authorization,
//! metadata and the service definitions.

use std::collections::{BTreeSet, HashSet};

use egui::{FontId, RichText, TextEdit, Ui};
use triggerapi_core::collection::{Folder, Request, TYPE_GRPC};
use triggerapi_core::rpc;

use crate::highlight::{self, Colouring};
use crate::kvtable::Table;
use crate::request::{auth_ui, capture_rules, capture_table, code_editor, format_button, hint, primary_button, set_capture_rules};
use crate::theme::{Kind, Theme};

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Tab {
    Message,
    Authorization,
    Metadata,
    Definitions,
}

/// What the user did that the window acts on.
#[derive(Default)]
pub struct Output {
    pub changed: bool,
    pub invoke: bool,
    /// The server address or TLS changed, so the definition is loaded.
    pub target: bool,
    pub reload: bool,
    pub example: bool,
    /// Create Requests or Update Requests was pressed, with the methods.
    pub create: Option<Vec<String>>,
}

/// The table of the endpoints of the server, to make requests for.
#[derive(Default)]
pub struct Endpoints {
    methods: Vec<String>,
    chosen: HashSet<String>,
    /// The methods the collection of the request on show has requests for,
    /// which are ticked and cannot be unticked.
    existing: HashSet<String>,
    /// The request on show is in such a collection.
    pub editing: bool,
    search: String,
}

impl Endpoints {
    pub fn set_methods(&mut self, methods: Vec<String>) {
        if methods != self.methods {
            self.chosen.clear();
            self.existing.clear();
            self.editing = false;
        }
        self.methods = methods;
    }

    pub fn has_service(&self, service: &str) -> bool {
        let prefix = format!("{service}/");
        self.methods.iter().any(|m| m.starts_with(&prefix))
    }

    pub fn set_existing(&mut self, existing: HashSet<String>, editing: bool) {
        self.existing = existing;
        self.editing = editing;
    }

    fn shown(&self, name: &str) -> bool {
        let lower = name.to_lowercase();
        self.search.to_lowercase().split_whitespace().all(|w| lower.contains(w))
    }

    /// The methods that can be ticked and are on show.
    fn free(&self) -> Vec<&String> {
        self.methods.iter().filter(|m| !self.existing.contains(*m) && self.shown(m)).collect()
    }

    fn added(&self) -> Vec<String> {
        self.methods.iter().filter(|m| self.chosen.contains(*m) && !self.existing.contains(*m)).cloned().collect()
    }

    fn ui(&mut self, ui: &mut Ui) -> Option<Vec<String>> {
        let mut create = None;
        egui::TopBottomPanel::bottom("endpoint buttons").frame(egui::Frame::NONE.inner_margin(egui::vec2(0.0, 4.0))).show_inside(ui, |ui| {
            ui.horizontal(|ui| {
                let free = self.free();
                let all = !free.is_empty() && free.iter().all(|m| self.chosen.contains(*m));
                let label = if all { "Select None" } else { "Select All" };
                if ui.add_enabled(!free.is_empty(), egui::Button::new(label)).clicked() {
                    let names: Vec<String> = free.into_iter().cloned().collect();
                    for m in names {
                        if all {
                            self.chosen.remove(&m);
                        } else {
                            self.chosen.insert(m);
                        }
                    }
                }
                let (text, tip) = if self.editing {
                    ("Update Requests", "Add a request for each endpoint that has been ticked to the folder this request is in")
                } else {
                    ("Create Requests", "Add a folder of requests to the sidebar, with a request for each endpoint that is ticked")
                };
                let added = self.added();
                if ui.add_enabled(!added.is_empty(), egui::Button::new(text)).on_hover_text(tip).clicked() {
                    create = Some(added);
                }
            });
        });
        ui.add_enabled(!self.methods.is_empty(), TextEdit::singleline(&mut self.search).hint_text("Search endpoints").desired_width(f32::INFINITY));
        if self.methods.is_empty() {
            hint(ui, "The endpoints of the server are listed here once they have been loaded.");
            return create;
        }
        egui::ScrollArea::vertical().auto_shrink([false, false]).show(ui, |ui| {
            let mut service = "";
            let shown: Vec<String> = self.methods.iter().filter(|m| self.shown(m)).cloned().collect();
            for name in &shown {
                let (parent, method) = name.split_once('/').unwrap_or((name, ""));
                if parent != service {
                    service = parent;
                    ui.label(RichText::new(parent).strong());
                }
                ui.horizontal(|ui| {
                    ui.add_space(8.0);
                    let existing = self.existing.contains(name);
                    let mut ticked = existing || self.chosen.contains(name);
                    if ui.add_enabled(!existing, egui::Checkbox::new(&mut ticked, method)).changed() {
                        if ticked {
                            self.chosen.insert(name.clone());
                        } else {
                            self.chosen.remove(name);
                        }
                    }
                });
            }
        });
        create
    }
}

/// Returns a folder named after a server, with a folder for each service
/// holding a request named after each method.
pub fn endpoint_folder(name: &str, url: &str, methods: &[String]) -> Folder {
    let mut folder = Folder::new(name);
    add_endpoints(&mut folder, url, methods);
    folder
}

/// Returns the methods a collection made from endpoints has requests for:
/// gRPC requests in a folder named after the service of their method.
pub fn endpoints_of(folder: &Folder) -> HashSet<String> {
    let mut found = HashSet::new();
    for service in &folder.folders {
        for r in &service.requests {
            if r.kind == TYPE_GRPC {
                if let Some(g) = &r.grpc {
                    if g.method.starts_with(&format!("{}/", service.name)) {
                        found.insert(g.method.clone());
                    }
                }
            }
        }
    }
    found
}

/// Adds a request for each method a collection does not yet have one for,
/// in the folder of its service, which is made if need be.
pub fn add_endpoints(folder: &mut Folder, url: &str, methods: &[String]) {
    let mut existing = endpoints_of(folder);
    for full in methods {
        if !existing.insert(full.clone()) {
            continue;
        }
        let (parent, method) = full.split_once('/').unwrap_or((full, ""));
        let index = match folder.folders.iter().position(|f| f.name == parent) {
            Some(i) => i,
            None => {
                folder.add_folder(parent);
                folder.folders.len() - 1
            }
        };
        folder.folders[index].add(Request::new_grpc(method, rpc::Request { url: url.into(), method: full.clone(), ..Default::default() }));
    }
}

pub struct Panel {
    pub url: String,
    pub tls: bool,
    /// The full name of the method chosen.
    pub method: String,
    /// The methods of the server, once its definition has been loaded.
    pub methods: Vec<String>,
    pub message: String,
    pub metadata: Table,
    pub auth: triggerapi_core::rest::Auth,
    pub captures: Table,
    pub endpoints: Endpoints,
    pub tab: Tab,
    /// Says why the definition could not be loaded, or what else is wrong.
    pub status: String,
    /// The server and TLS the definition was last asked for.
    pub target: String,
    format_error: Option<String>,
}

impl Panel {
    pub fn new() -> Panel {
        Panel {
            url: String::new(),
            tls: false,
            method: String::new(),
            methods: Vec::new(),
            message: String::new(),
            metadata: Table::new("metadata", "Key", "Value"),
            auth: Default::default(),
            captures: capture_table("grpc captures"),
            endpoints: Endpoints::default(),
            tab: Tab::Message,
            status: String::new(),
            target: String::new(),
            format_error: None,
        }
    }

    pub fn request(&self) -> rpc::Request {
        rpc::Request {
            url: self.url.clone(),
            tls: self.tls,
            method: self.method.clone(),
            message: self.message.clone(),
            metadata: self.metadata.items(),
            auth: self.auth.clone(),
            captures: capture_rules(&self.captures),
        }
    }

    pub fn load(&mut self, r: &rpc::Request) {
        self.url = r.url.clone();
        self.tls = r.tls;
        self.target = self.target_key();
        self.method = r.method.clone();
        self.methods.clear();
        self.message = r.message.clone();
        self.metadata.set_items(&r.metadata);
        set_capture_rules(&mut self.captures, &r.captures);
        self.auth = r.auth.clone();
        self.endpoints.set_methods(Vec::new());
        self.status.clear();
        self.format_error = None;
    }

    pub fn target_key(&self) -> String {
        format!("{} {}", self.url, self.tls)
    }

    /// Lists the methods of the server, keeping the one chosen, or choosing
    /// the first if none was. It reports if the method changed.
    pub fn set_schema(&mut self, schema: &rpc::Schema) -> bool {
        self.methods = schema.methods.iter().map(|m| m.name.clone()).collect();
        self.endpoints.set_methods(self.methods.clone());
        self.status.clear();
        if self.method.is_empty() {
            if let Some(first) = self.methods.first() {
                self.method = first.clone();
                return true;
            }
        }
        false
    }

    pub fn set_schema_error(&mut self, err: &str) {
        self.endpoints.set_methods(Vec::new());
        self.status = format!("The service definition could not be loaded: {err}");
    }

    pub fn ui(&mut self, ui: &mut Ui, theme: &Theme, code: &FontId, found: &dyn Fn(&str) -> bool, busy: bool) -> Output {
        let mut out = Output::default();
        let dark = ui.visuals().dark_mode;
        let text_colour = ui.visuals().text_color();
        let proportional = egui::TextStyle::Body.resolve(ui.style());
        let mut line_layout = |ui: &Ui, text: &str, wrap: f32| {
            let job = highlight::job(
                text,
                &Colouring { theme, dark, font: proportional.clone(), colour: text_colour, json: false, placeholders: Some(found), matches: &[], current: None, wrap: Some(wrap) },
            );
            ui.fonts(|f| f.layout_job(job))
        };

        ui.horizontal(|ui| {
            let width = ui.available_width();
            let url = ui.add(
                TextEdit::singleline(&mut self.url)
                    .hint_text("Server address, e.g. localhost:50051")
                    .desired_width((width * 0.3).max(160.0))
                    .layouter(&mut line_layout),
            );
            if url.changed() {
                out.changed = true;
            }
            if url.lost_focus() {
                if ui.input(|i| i.key_pressed(egui::Key::Enter)) {
                    out.invoke = true;
                }
                if self.target_key() != self.target {
                    out.target = true;
                }
            }

            // The methods, under headings for their services.
            let shown = match self.method.split_once('/') {
                Some(_) => self.method.clone(),
                None if self.method.is_empty() => "No method".into(),
                None => self.method.clone(),
            };
            let combo_width = (ui.available_width() - 230.0).max(160.0);
            egui::ComboBox::from_id_salt("grpc method").width(combo_width).selected_text(shown).height(400.0).show_ui(ui, |ui| {
                if !self.method.is_empty() && !self.methods.contains(&self.method) {
                    let m = self.method.clone();
                    ui.selectable_value(&mut self.method, m.clone(), m);
                }
                let mut service = "";
                for name in &self.methods {
                    let (parent, method) = name.split_once('/').unwrap_or((name, ""));
                    if parent != service {
                        service = parent;
                        ui.label(RichText::new(parent).strong());
                    }
                    if ui.selectable_label(self.method == *name, format!("    {method}")).clicked() && self.method != *name {
                        self.method = name.clone();
                        out.changed = true;
                    }
                }
            });
            if ui.button("Reload").on_hover_text("Load the service definition from the server again").clicked() {
                out.reload = true;
            }
            if ui.checkbox(&mut self.tls, "TLS").on_hover_text("Connect to the server using TLS").changed() {
                out.changed = true;
                out.target = true;
            }
            if primary_button(ui, theme, if busy { "Cancel" } else { "Invoke" }).on_hover_text("Ctrl+Return").clicked() {
                out.invoke = true;
            }
        });
        if !self.status.is_empty() {
            ui.add(egui::Label::new(RichText::new(&self.status).color(theme.colour(Kind::Error))).wrap().selectable(true));
        }
        ui.add_space(4.0);

        ui.horizontal(|ui| {
            ui.selectable_value(&mut self.tab, Tab::Message, "Message");
            ui.selectable_value(&mut self.tab, Tab::Authorization, "Authorization");
            ui.selectable_value(&mut self.tab, Tab::Metadata, "Metadata");
            ui.selectable_value(&mut self.tab, Tab::Definitions, "Service Definitions");
        });
        ui.separator();

        match self.tab {
            Tab::Message => {
                egui::TopBottomPanel::bottom("message buttons").frame(egui::Frame::NONE.inner_margin(egui::vec2(0.0, 4.0))).show_inside(ui, |ui| {
                    ui.horizontal(|ui| {
                        if ui.button("Use Example Message").on_hover_text("Replace the message with one that has every field of the method's request").clicked() {
                            out.example = true;
                        }
                        if format_button(ui, &mut self.message, theme, &mut self.format_error) {
                            out.changed = true;
                        }
                    });
                });
                out.changed |= code_editor(ui, "message", &mut self.message, theme, code, true, found, "The request message as JSON");
            }
            Tab::Authorization => out.changed |= auth_ui(ui, &mut self.auth, &mut line_layout),
            Tab::Metadata => out.changed |= self.metadata.ui(ui, theme, found, proportional.clone()),
            Tab::Definitions => out.create = self.endpoints.ui(ui),
        }
        out
    }
}

#[allow(dead_code)]
fn unused(_: BTreeSet<u8>) {}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn makes_endpoint_collections() {
        let methods = vec!["a.S/One".to_string(), "a.S/Two".to_string(), "b.T/Three".to_string()];
        let mut folder = endpoint_folder("host:1", "host:1", &methods[..1]);
        assert_eq!(endpoints_of(&folder), HashSet::from(["a.S/One".to_string()]));
        add_endpoints(&mut folder, "host:1", &methods);
        assert_eq!(folder.folders.len(), 2);
        assert_eq!(folder.folders[0].requests.len(), 2);
        assert_eq!(folder.folders[1].requests[0].name, "Three");
        assert_eq!(endpoints_of(&folder).len(), 3);
    }
}
