//! The REST request UI: the method, the URL with Send beside it, and tabs
//! for the body, params, authorization and headers. The curl button shows
//! the request as a command in their place.

use egui::{vec2, FontId, Key, RichText, TextEdit, Ui};
use triggerapi_core::capture::Rule;
use triggerapi_core::rest::{self, KeyValue};
use triggerapi_core::{env, format::format_json, query};

use crate::highlight::{self, Colouring};
use crate::icons::{self, Glyph};
use crate::kvtable::{Row, Table};
use crate::theme::{Kind, Theme};

/// The entries of the body type selector, in the order they are shown.
pub const BODY_TYPES: &[(&str, &str)] = &[
    ("None", rest::BODY_NONE),
    ("JSON", rest::BODY_JSON),
    ("Text", rest::BODY_TEXT),
    ("XML", rest::BODY_XML),
    ("Form URL-encoded", rest::BODY_FORM),
];

/// The entries of the authorization type selector.
pub const AUTH_TYPES: &[(&str, &str)] = &[("No Auth", rest::AUTH_NONE), ("Basic Auth", rest::AUTH_BASIC), ("Bearer Token", rest::AUTH_BEARER)];

/// What is shown in place of a placeholder that has no value.
pub const MISSING_VALUE: &str = "\u{25a0}";

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Tab {
    Body,
    Params,
    Authorization,
    Headers,
}

/// What the user did that the window acts on.
#[derive(Default)]
pub struct Output {
    pub changed: bool,
    pub send: bool,
    pub execute: bool,
}

/// Turns a capture table's rows into rules, and back.
pub fn capture_rules(table: &Table) -> Vec<Rule> {
    table
        .described()
        .into_iter()
        .map(|r| Rule { variable: r.key, path: r.value, regex: r.description, enabled: r.enabled })
        .collect()
}

pub fn set_capture_rules(table: &mut Table, rules: &[Rule]) {
    table.set_rows(
        rules
            .iter()
            .map(|r| Row { key: r.variable.clone(), value: r.path.clone(), description: r.regex.clone(), enabled: r.enabled })
            .collect(),
    );
}

/// Returns the table of the Capture tab of a response panel.
pub fn capture_table(id: &str) -> Table {
    let mut table = Table::new(id, "Environment Variable", "Path in the response, e.g. data.items[0].id");
    table.plain = true;
    table.describe = Some("Regex (optional)".into());
    table.statuses = true;
    table.placeholders = false;
    table.tooltip = Some(
        "After a response that is not a failure, each variable is set, in the selected environment, to the value at its path in the JSON of the response.\n\
         Names are separated by dots and an index into an array is in brackets: data.items[0].id\n\
         A regex keeps only the part of the value it matches, or the part its first group matches if it has groups.\n\
         Right-click a value in the response to add it here."
            .into(),
    );
    table
}

/// The editor of the authorization, shared by both request UIs.
pub fn auth_ui(ui: &mut Ui, auth: &mut rest::Auth, layout: &mut dyn FnMut(&Ui, &str, f32) -> std::sync::Arc<egui::Galley>) -> bool {
    let mut changed = false;
    let index = AUTH_TYPES.iter().position(|(_, t)| *t == auth.kind).unwrap_or(0);
    ui.horizontal(|ui| {
        ui.label("Type");
        egui::ComboBox::from_id_salt(ui.id().with("auth type")).selected_text(AUTH_TYPES[index].0).show_ui(ui, |ui| {
            for (label, kind) in AUTH_TYPES {
                if ui.selectable_label(AUTH_TYPES[index].1 == *kind, *label).clicked() && auth.kind != *kind {
                    auth.kind = kind.to_string();
                    changed = true;
                }
            }
        });
    });
    ui.add_space(6.0);
    let kind = if auth.kind.is_empty() { rest::AUTH_NONE } else { auth.kind.as_str() };
    match kind {
        rest::AUTH_BASIC => {
            egui::Grid::new(ui.id().with("basic")).num_columns(2).show(ui, |ui| {
                ui.label("Username");
                changed |= ui.add(TextEdit::singleline(&mut auth.username).desired_width(f32::INFINITY).layouter(layout)).changed();
                ui.end_row();
                // The password is not masked, so that placeholders in it are
                // coloured like the rest.
                ui.label("Password");
                changed |= ui.add(TextEdit::singleline(&mut auth.password).desired_width(f32::INFINITY).layouter(layout)).changed();
                ui.end_row();
            });
        }
        rest::AUTH_BEARER => {
            egui::Grid::new(ui.id().with("bearer")).num_columns(2).show(ui, |ui| {
                ui.label("Token");
                changed |= ui.add(TextEdit::singleline(&mut auth.token).desired_width(f32::INFINITY).layouter(layout)).changed();
                ui.end_row();
            });
        }
        _ => hint(ui, "This request does not use any authorization."),
    }
    changed
}

/// Fills a page with a hint of why it has nothing else on it.
pub fn hint(ui: &mut Ui, text: &str) {
    egui::Frame::group(ui.style()).fill(ui.visuals().extreme_bg_color).show(ui, |ui| {
        ui.set_min_size(ui.available_size());
        ui.centered_and_justified(|ui| ui.label(RichText::new(text).weak()));
    });
}

/// The Format button, which sets out JSON text, and says why it could not
/// beneath it.
pub fn format_button(ui: &mut Ui, text: &mut String, theme: &Theme, error: &mut Option<String>) -> bool {
    let response = icons::button(ui, Glyph::Braces, theme.colour(Kind::Action), "Format the JSON, with each value on a line of its own", true);
    let mut changed = false;
    if response.clicked() {
        match format_json(text) {
            Ok(formatted) => {
                if formatted != *text {
                    *text = formatted;
                    changed = true;
                }
                *error = None;
            }
            Err(e) => *error = Some(format!("This is not JSON: {e}")),
        }
    }
    if let Some(e) = error.clone() {
        ui.colored_label(theme.colour(Kind::Error), e);
    }
    changed
}

pub struct Panel {
    pub method: String,
    /// The URL as shown, with the enabled params as its query string.
    pub url: String,
    pub params: Table,
    pub headers: Table,
    pub auth: rest::Auth,
    pub body_type: String,
    pub body: String,
    pub form: Table,
    pub captures: Table,
    pub tab: Tab,
    /// The URL is shown with the values of its placeholders in place.
    pub preview: bool,
    preview_pieces: Vec<(String, Option<bool>)>,
    /// The request is shown as a curl command.
    pub curl: bool,
    command: String,
    command_ok: bool,
    /// The request the command on show was made from, which Execute runs.
    pub commanded: Option<rest::Request>,
    /// The curl command and preview are to be made again.
    pub stale: bool,
    format_error: Option<String>,
    pub focus_url: bool,
}

impl Panel {
    pub fn new() -> Panel {
        let mut panel = Panel {
            method: "GET".into(),
            url: String::new(),
            params: Table::new("params", "Key", "Value"),
            headers: Table::new("headers", "Header", "Value"),
            auth: rest::Auth::default(),
            body_type: rest::BODY_NONE.into(),
            body: String::new(),
            form: Table::new("form", "Key", "Value"),
            captures: capture_table("rest captures"),
            tab: Tab::Params,
            preview: false,
            preview_pieces: Vec::new(),
            curl: false,
            command: String::new(),
            command_ok: false,
            commanded: None,
            stale: true,
            format_error: None,
            focus_url: true,
        };
        panel.load(&rest::Request { method: "GET".into(), ..Default::default() });
        panel
    }

    /// Reads the request out of the fields. The query string shown in the
    /// URL is left out of it, as the params hold it.
    pub fn request(&self) -> rest::Request {
        let (base, _) = query::split_url(&self.url);
        rest::Request {
            method: self.method.clone(),
            url: base,
            params: self.params.items(),
            headers: self.headers.items(),
            auth: self.auth.clone(),
            body_type: self.body_type.clone(),
            body: self.body.clone(),
            form: self.form.items(),
            captures: capture_rules(&self.captures),
            options: Default::default(),
        }
    }

    /// Shows a request in the fields. Unknown methods and types fall back to
    /// the first of their kind.
    pub fn load(&mut self, r: &rest::Request) {
        self.method = if rest::METHODS.contains(&r.method.as_str()) { r.method.clone() } else { "GET".into() };
        // A query string in the stored URL is moved into the params.
        let (base, mut params) = query::split_url(&r.url);
        params.extend(r.params.iter().cloned());
        rest::sort_pairs(&mut params);
        self.url = query::join_url(&base, &params);
        self.params.set_items(&params);
        self.headers.set_items(&r.headers);
        self.auth = r.auth.clone();
        if !AUTH_TYPES.iter().any(|(_, t)| *t == self.auth.kind) {
            self.auth.kind = rest::AUTH_NONE.into();
        }
        self.body_type = BODY_TYPES.iter().find(|(_, t)| *t == r.body_type).map(|(_, t)| t.to_string()).unwrap_or_else(|| rest::BODY_NONE.into());
        self.body = r.body.clone();
        self.form.set_items(&r.form);
        set_capture_rules(&mut self.captures, &r.captures);
        self.format_error = None;
        self.show_likely_tab();
        self.stale = true;
    }

    /// Selects the tab most likely to be wanted for the method: the body for
    /// a method that sends one, and otherwise the params.
    fn show_likely_tab(&mut self) {
        self.tab = if matches!(self.method.as_str(), "POST" | "PUT" | "PATCH") { Tab::Body } else { Tab::Params };
    }

    /// Closes what is shown in place of the fields: the curl command, or
    /// else the preview of the URL. It reports if there was anything.
    pub fn escape(&mut self) -> bool {
        if self.curl {
            self.curl = false;
        } else if self.preview {
            self.preview = false;
            self.focus_url = true;
        } else {
            return false;
        }
        true
    }

    /// Makes the curl command and the preview again, with the variables of
    /// the active environment in place.
    pub fn refresh(&mut self, environment: Option<&env::Environment>, options: rest::Options) {
        self.stale = false;
        let expander = environment.map(|e| e.expander());
        let expand = |s: &str| match &expander {
            Some(f) => f(s),
            None => s.to_string(),
        };

        let mut request = self.request().expand(&expand);
        request.options = options;
        match request.curl() {
            Ok(c) => {
                self.command = c;
                self.command_ok = true;
            }
            Err(e) => {
                // A comment, so that nothing happens if it is run all the
                // same.
                self.command = format!("# The request cannot be made into a command: {e}");
                self.command_ok = false;
            }
        }
        self.commanded = Some(request);

        // The preview: a placeholder that has no value, or an empty one, is
        // as much a gap in the URL as one that is not defined.
        let mut pieces = Vec::new();
        let mut last = 0;
        for (start, end) in env::find_placeholders(&self.url) {
            pieces.push((self.url[last..start].to_string(), None));
            let m = &self.url[start..end];
            let value = environment.map(|e| e.expand(m)).unwrap_or_else(|| m.to_string());
            if value != m && !value.is_empty() {
                pieces.push((value, Some(true)));
            } else {
                pieces.push((MISSING_VALUE.to_string(), Some(false)));
            }
            last = end;
        }
        pieces.push((self.url[last..].to_string(), None));
        self.preview_pieces = pieces;
    }

    /// Draws the panel and reports what the user did.
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

        if self.curl {
            return self.curl_ui(ui, code, busy);
        }

        // The method, the URL, the preview button and Send.
        ui.horizontal_top(|ui| {
            let before = self.method.clone();
            egui::ComboBox::from_id_salt("method").width(90.0).selected_text(&self.method).show_ui(ui, |ui| {
                for m in rest::METHODS {
                    ui.selectable_value(&mut self.method, m.to_string(), *m);
                }
            });
            if self.method != before {
                self.show_likely_tab();
                out.changed = true;
            }
            let send_width = 70.0;
            let width = (ui.available_width() - send_width - 40.0).max(100.0);
            if self.preview {
                let mut job = egui::text::LayoutJob::default();
                job.wrap.max_width = width;
                for (text, state) in &self.preview_pieces {
                    let colour = match state {
                        Some(true) => theme.colour(Kind::Found),
                        Some(false) => theme.colour(Kind::NotFound),
                        None => text_colour,
                    };
                    job.append(text, 0.0, egui::TextFormat::simple(proportional.clone(), colour));
                }
                egui::Frame::NONE.fill(ui.visuals().extreme_bg_color).inner_margin(4.0).corner_radius(2.0).show(ui, |ui| {
                    ui.set_width(width - 8.0);
                    ui.add(egui::Label::new(job).wrap().selectable(true));
                });
            } else {
                // The URL wraps rather than scrolls, and grows as tall as it
                // needs to be.
                let response = ui.add(
                    TextEdit::multiline(&mut self.url)
                        .id_salt("url")
                        .desired_width(width)
                        .desired_rows(1)
                        .hint_text("Enter URL, e.g. https://api.example.com/users or {{host}}/users")
                        .layouter(&mut line_layout)
                        .return_key(None),
                );
                if self.focus_url {
                    response.request_focus();
                    self.focus_url = false;
                }
                if response.changed() {
                    // The URL is one line.
                    if self.url.contains('\n') {
                        self.url = self.url.replace(['\n', '\r'], "");
                    }
                    self.url_changed();
                    out.changed = true;
                }
                if response.has_focus() && ui.input(|i| i.key_pressed(Key::Enter) && !i.modifiers.ctrl) {
                    out.send = true;
                }
            }
            if icons::toggle(ui, &mut self.preview, Glyph::Eye, theme.colour(Kind::Action), "Show the URL with the values of its placeholders").changed() {
                self.stale = true;
                if !self.preview {
                    self.focus_url = true;
                }
            }
            if primary_button(ui, theme, if busy { "Cancel" } else { "Send" }).on_hover_text("Ctrl+Return").clicked() {
                out.send = true;
            }
        });
        ui.add_space(4.0);

        // The tabs, with the curl button at their right.
        ui.horizontal(|ui| {
            ui.selectable_value(&mut self.tab, Tab::Body, "Body");
            ui.selectable_value(&mut self.tab, Tab::Params, "Params");
            ui.selectable_value(&mut self.tab, Tab::Authorization, "Authorization");
            ui.selectable_value(&mut self.tab, Tab::Headers, "Headers");
            ui.with_layout(egui::Layout::right_to_left(egui::Align::Center), |ui| {
                let mut on = self.curl;
                if icons::toggle(ui, &mut on, Glyph::Prompt, theme.colour(Kind::Action), "Show the request as a curl command").changed() {
                    self.curl = on;
                    self.stale = true;
                }
            });
        });
        ui.separator();

        match self.tab {
            Tab::Body => {
                ui.horizontal(|ui| {
                    ui.label("Type");
                    let index = BODY_TYPES.iter().position(|(_, t)| *t == self.body_type).unwrap_or(0);
                    egui::ComboBox::from_id_salt("body type").selected_text(BODY_TYPES[index].0).show_ui(ui, |ui| {
                        for (label, kind) in BODY_TYPES {
                            if ui.selectable_label(self.body_type == *kind, *label).clicked() && self.body_type != *kind {
                                self.body_type = kind.to_string();
                                out.changed = true;
                            }
                        }
                    });
                    // Only a JSON body can be set out.
                    if self.body_type == rest::BODY_JSON && format_button(ui, &mut self.body, theme, &mut self.format_error) {
                        out.changed = true;
                    }
                });
                match self.body_type.as_str() {
                    rest::BODY_FORM => out.changed |= self.form.ui(ui, theme, found, proportional.clone()),
                    rest::BODY_JSON | rest::BODY_TEXT | rest::BODY_XML => {
                        let json = self.body_type == rest::BODY_JSON;
                        out.changed |= code_editor(ui, "body", &mut self.body, theme, code, json, found, "");
                    }
                    _ => hint(ui, "This request does not have a body."),
                }
            }
            Tab::Params => {
                if self.params.ui(ui, theme, found, proportional.clone()) {
                    self.params_changed();
                    out.changed = true;
                }
            }
            Tab::Authorization => out.changed |= auth_ui(ui, &mut self.auth, &mut line_layout),
            Tab::Headers => out.changed |= self.headers.ui(ui, theme, found, proportional.clone()),
        }
        if out.changed {
            self.stale = true;
        }
        out
    }

    fn curl_ui(&mut self, ui: &mut Ui, code: &FontId, busy: bool) -> Output {
        let mut out = Output::default();
        egui::TopBottomPanel::bottom("curl buttons").frame(egui::Frame::NONE.inner_margin(vec2(0.0, 4.0))).show_inside(ui, |ui| {
            ui.with_layout(egui::Layout::right_to_left(egui::Align::Center), |ui| {
                if ui.button("Close").on_hover_text("Go back to the request").clicked() {
                    self.curl = false;
                }
                if ui.add_enabled(self.command_ok, egui::Button::new("Copy")).on_hover_text("Copy the command, to paste into a terminal").clicked() {
                    ui.ctx().copy_text(self.command.clone());
                }
                let label = if busy { "Cancel" } else { "Execute" };
                if ui.add_enabled(self.command_ok || busy, egui::Button::new(label)).on_hover_text("Run the command with curl and show the response").clicked() {
                    out.execute = true;
                }
            });
        });
        egui::ScrollArea::both().auto_shrink([false, false]).show(ui, |ui| {
            let mut text: &str = self.command.as_str();
            ui.add(TextEdit::multiline(&mut text).font(code.clone()).desired_width(f32::INFINITY).frame(false));
        });
        out
    }

    /// Copies the query string typed into the URL to the params.
    fn url_changed(&mut self) {
        let (_, typed) = query::split_url(&self.url);
        let merged = query::merge_params(&self.params.items(), &typed);
        // The params are put in order, though the URL is left as typed.
        let mut sorted = merged.clone();
        rest::sort_pairs(&mut sorted);
        self.params.set_items(&sorted);
    }

    /// Shows the enabled params as the query string of the URL.
    fn params_changed(&mut self) {
        let (base, _) = query::split_url(&self.url);
        self.url = query::join_url(&base, &self.params.items());
    }
}

/// A button that stands out as the main action of its page, in the colour of
/// the settings.
pub fn primary_button(ui: &mut Ui, theme: &Theme, text: &str) -> egui::Response {
    let fill = theme.colour(Kind::Send);
    ui.add(egui::Button::new(RichText::new(text).color(crate::theme::text_on(fill)).strong()).fill(fill).min_size(vec2(64.0, 24.0)))
}

/// An editor of code, such as a body or a message: JSON is coloured, and
/// the placeholders in it are coloured by whether they have values.
#[allow(clippy::too_many_arguments)]
pub fn code_editor(ui: &mut Ui, id: &str, text: &mut String, theme: &Theme, code: &FontId, json: bool, found: &dyn Fn(&str) -> bool, hint_text: &str) -> bool {
    let dark = ui.visuals().dark_mode;
    let colour = ui.visuals().text_color();
    let mut layout = |ui: &Ui, text: &str, wrap: f32| {
        let job = highlight::job(
            text,
            &Colouring { theme, dark, font: code.clone(), colour, json, placeholders: Some(found), matches: &[], current: None, wrap: Some(wrap) },
        );
        ui.fonts(|f| f.layout_job(job))
    };
    let mut changed = false;
    egui::ScrollArea::vertical().id_salt(id).auto_shrink([false, false]).show(ui, |ui| {
        changed = ui
            .add_sized(
                ui.available_size(),
                TextEdit::multiline(text).id_salt(id).font(code.clone()).code_editor().hint_text(hint_text).layouter(&mut layout),
            )
            .changed();
    });
    changed
}

#[allow(dead_code)]
fn unused(_: KeyValue) {}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn url_and_params_keep_in_step() {
        let mut p = Panel::new();
        p.load(&rest::Request {
            method: "POST".into(),
            url: "{{host}}/users?b=2".into(),
            params: vec![KeyValue::new("a", "1", true), KeyValue::new("off", "x", false)],
            body_type: "json".into(),
            ..Default::default()
        });
        assert_eq!(p.tab, Tab::Body);
        assert_eq!(p.url, "{{host}}/users?a=1&b=2");
        let r = p.request();
        assert_eq!(r.url, "{{host}}/users");
        assert_eq!(r.params.len(), 3);

        p.url = "{{host}}/users?a=9".into();
        p.url_changed();
        let keys: Vec<(String, bool)> = p.request().params.iter().map(|k| (k.key.clone(), k.enabled)).collect();
        assert_eq!(keys, vec![("a".to_string(), true), ("off".to_string(), false)]);
    }
}
