//! Shows the result of the last request: its body, its headers, what is
//! captured from it, and a bar beneath with its status, time and size.

use std::collections::HashMap;
use std::ops::Range;
use std::sync::Arc;
use std::time::Duration;

use egui::{vec2, Color32, FontId, Galley, Key, Rect, RichText, Sense, TextEdit, Ui};
use triggerapi_core::{capture, rest, rpc};

use crate::highlight::{self, Colouring};
use crate::icons::{self, Glyph};
use crate::kvtable::{Status, Table};
use crate::theme::{Kind, Theme};

/// The tabs of a response panel.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Tab {
    Response,
    Headers,
    Capture,
}

/// A labelled value in the bar beneath a response.
#[derive(Clone, Debug)]
pub struct Field {
    pub label: &'static str,
    pub value: String,
    /// The value is an error, shown in the colour of errors.
    pub failure: bool,
}

const STATUS: &str = "Status";
const ERROR: &str = "Error";

fn field(label: &'static str, value: impl Into<String>, failure: bool) -> Field {
    Field { label, value: value.into(), failure }
}

/// What came of one rule of a request: the variable it names, and why it
/// could not be set from the response if it could not.
#[derive(Clone, Debug)]
pub struct Captured {
    pub variable: String,
    pub error: Option<String>,
}

/// What is shown again when the tab of a request is gone back to.
#[derive(Clone)]
pub enum Replay {
    Rest(Arc<rest::Response>, Vec<Captured>),
    Grpc(Arc<rpc::Response>, Vec<Captured>),
    Error(String, Duration),
    Cancelled(Duration),
}

/// The bar text is found in the response with.
#[derive(Default)]
struct Find {
    open: bool,
    query: String,
    match_case: bool,
    matches: Vec<Range<usize>>,
    current: Option<usize>,
    focus: bool,
    /// The selection is to be moved to the match gone to.
    go: bool,
}

pub struct Panel {
    id: egui::Id,
    pub tab: Tab,
    body: String,
    json: bool,
    /// The JSON that values are captured from, which the body ends with: all
    /// of a REST response, and the last message of a gRPC one.
    capture_doc: Option<String>,
    /// The body as it is saved, and the extension of its file.
    content: Vec<u8>,
    extension: String,
    headers: Vec<(String, String)>,
    fields: Vec<Field>,
    pub captured: Vec<Captured>,
    find: Find,
    /// Changes whenever what the body looks like does, so that its layout is
    /// made again.
    version: u64,
    galley: Option<(u64, f32, Arc<Galley>)>,
    /// Where in the body the menu was opened, in bytes.
    menu_at: Option<usize>,
    pub warning: Option<String>,
}

/// What the user did in the panel that the window acts on.
#[derive(Default)]
pub struct Output {
    /// The rules of the Capture tab were changed.
    pub captures_changed: bool,
}

impl Panel {
    pub fn new(id: &str) -> Panel {
        Panel {
            id: egui::Id::new(id),
            tab: Tab::Response,
            body: String::new(),
            json: false,
            capture_doc: None,
            content: Vec::new(),
            extension: String::new(),
            headers: Vec::new(),
            fields: Vec::new(),
            captured: Vec::new(),
            find: Find::default(),
            version: 0,
            galley: None,
            menu_at: None,
            warning: None,
        }
    }

    /// Empties the panel.
    pub fn clear(&mut self) {
        self.set_body(String::new(), false);
        self.content.clear();
        self.headers.clear();
        self.fields.clear();
        self.captured.clear();
    }

    /// Shows text as the response. JSON is coloured.
    fn set_body(&mut self, text: String, json: bool) {
        self.json = json && text.len() <= highlight::MAX_HIGHLIGHT;
        self.body = text;
        self.capture_doc = None;
        self.version += 1;
        // What is being looked for is looked for in the new body.
        self.search();
    }

    /// Makes the layout of the body again, as after the colours change.
    pub fn restyle(&mut self) {
        self.version += 1;
    }

    /// Shows a result, as it is shown again when its tab is gone back to.
    pub fn replay(&mut self, replay: &Replay) {
        match replay {
            Replay::Rest(res, captured) => {
                self.show_rest(res);
                self.captured = captured.clone();
            }
            Replay::Grpc(res, captured) => {
                self.show_rpc(res);
                self.captured = captured.clone();
            }
            Replay::Error(err, elapsed) => {
                self.clear();
                self.fields = vec![field(ERROR, err.clone(), true), field("Time", format_duration(*elapsed), false)];
            }
            Replay::Cancelled(elapsed) => {
                self.clear();
                self.fields = vec![field(ERROR, "Cancelled", true), field("Time", format_duration(*elapsed), false)];
            }
        }
    }

    /// Shows the response to a REST request. Only its body goes in the
    /// response; its status goes in the bar beneath.
    fn show_rest(&mut self, res: &rest::Response) {
        let text = res.text();
        self.set_body(text.clone(), res.is_json());
        // Values are captured from any body that is JSON, whatever it says it
        // is.
        if triggerapi_core::json::is_valid(&String::from_utf8_lossy(&res.body)) {
            self.capture_doc = Some(text.clone());
        }
        // JSON is saved formatted, as it is shown. Anything else is saved as
        // it was received.
        self.content = if res.is_json() { text.into_bytes() } else { res.body.clone() };
        self.extension = extension(res.header("Content-Type").unwrap_or(""));
        let mut headers = res.headers.clone();
        headers.sort_by(|a, b| a.0.cmp(&b.0));
        self.headers = headers;
        self.fields = vec![
            field(STATUS, res.status.clone(), res.status_code >= 400),
            field("Time", format_duration(res.duration), false),
            field("Size", format_size(res.body.len()), false),
            field("Protocol", res.proto.clone(), false),
        ];
    }

    /// Shows the result of a gRPC call: its messages, and its status in the
    /// bar beneath. The trailers are listed after the headers.
    fn show_rpc(&mut self, res: &rpc::Response) {
        self.set_body(res.messages.join("\n\n"), true);
        self.capture_doc = res.messages.last().cloned();
        self.content = self.body.clone().into_bytes();
        self.extension = "json".into();
        let mut headers = res.headers.clone();
        headers.sort_by(|a, b| a.0.cmp(&b.0));
        let mut trailers: Vec<(String, String)> = res.trailers.iter().map(|(k, v)| (format!("{k} (trailer)"), v.clone())).collect();
        trailers.sort_by(|a, b| a.0.cmp(&b.0));
        headers.extend(trailers);
        self.headers = headers;
        self.fields = vec![
            field(STATUS, format!("{} {}", res.code, res.code_name), !res.ok()),
            field(ERROR, res.status.clone(), true),
            field("Time", format_duration(res.duration), false),
            field("Messages", res.messages.len().to_string(), false),
            field("Protocol", res.protocol.clone(), false),
        ];
    }

    /// Opens the bar text is found in the response with, starting from the
    /// text selected in the body, if any.
    pub fn open_find(&mut self, ctx: &egui::Context) {
        self.tab = Tab::Response;
        self.find.open = true;
        self.find.focus = true;
        if let Some(state) = TextEdit::load_state(ctx, self.id.with("body")) {
            if let Some(range) = state.cursor.char_range() {
                let [a, b] = range.sorted();
                if a.index != b.index {
                    let selected: String = self.body.chars().skip(a.index).take(b.index - a.index).collect();
                    if !selected.contains('\n') {
                        self.find.query = selected;
                    }
                }
            }
        }
        self.search();
    }

    /// Closes the find bar, and reports if it was open.
    pub fn close_find(&mut self) -> bool {
        if !self.find.open {
            return false;
        }
        self.find.open = false;
        self.find.matches.clear();
        self.find.current = None;
        self.version += 1;
        true
    }

    fn search(&mut self) {
        if !self.find.open {
            self.find.matches.clear();
            self.find.current = None;
            return;
        }
        self.find.matches = highlight::find_all(&self.body, &self.find.query, self.find.match_case);
        self.find.current = if self.find.matches.is_empty() { None } else { Some(0) };
        self.find.go = true;
        self.version += 1;
    }

    fn step(&mut self, forward: bool) {
        let n = self.find.matches.len();
        if n == 0 {
            return;
        }
        let at = self.find.current.unwrap_or(0);
        self.find.current = Some(if forward { (at + 1) % n } else { (at + n - 1) % n });
        self.find.go = true;
        self.version += 1;
    }

    /// Marks the Capture tab and its rows with how capturing went.
    fn marks(&self) -> HashMap<String, Status> {
        self.captured
            .iter()
            .map(|c| {
                let status = match &c.error {
                    None => Status { good: true, tip: "Captured".into() },
                    Some(e) => Status { good: false, tip: format!("Not captured: {e}") },
                };
                (c.variable.clone(), status)
            })
            .collect()
    }

    /// Draws the panel. `captures` is the table of what the request
    /// captures, which belongs to the request; `request_name` names the
    /// file a response is saved to.
    pub fn ui(&mut self, ui: &mut Ui, theme: &Theme, font: &FontId, captures: &mut Table, found: &dyn Fn(&str) -> bool, request_name: &str) -> Output {
        let mut out = Output::default();
        let action = theme.colour(Kind::Action);
        if !captures.has_marks() && !self.captured.is_empty() {
            captures.show_statuses(self.marks());
        }

        // The bar beneath the response.
        egui::TopBottomPanel::bottom(self.id.with("fields")).frame(egui::Frame::NONE.inner_margin(vec2(0.0, 4.0))).show_inside(ui, |ui| {
            self.fields_ui(ui, theme);
        });

        // The tabs, with the find and save buttons at their right.
        ui.horizontal(|ui| {
            ui.selectable_value(&mut self.tab, Tab::Response, "Response");
            let headers = if self.headers.is_empty() { "Headers".to_string() } else { format!("Headers ({})", self.headers.len()) };
            ui.selectable_value(&mut self.tab, Tab::Headers, headers);
            let capture = ui.selectable_value(&mut self.tab, Tab::Capture, "Capture");
            if !self.captured.is_empty() {
                let failed: Vec<String> = self
                    .captured
                    .iter()
                    .filter_map(|c| c.error.as_ref().map(|e| format!("{}: {e}", c.variable)))
                    .collect();
                let good = failed.is_empty();
                let (rect, response) = ui.allocate_exact_size(vec2(16.0, 16.0), Sense::hover());
                icons::mark(ui.painter(), rect, theme.colour(if good { Kind::Captured } else { Kind::NotCaptured }), good);
                let tip = if good { "Every value was captured".to_string() } else { format!("Not captured:\n{}", failed.join("\n")) };
                response.on_hover_text(tip.clone());
                capture.on_hover_text(tip);
            }
            ui.with_layout(egui::Layout::right_to_left(egui::Align::Center), |ui| {
                if icons::button(ui, Glyph::Disk, action, "Save the response to a file", !self.content.is_empty()).clicked() {
                    self.save(request_name);
                }
                if icons::button(ui, Glyph::Magnifier, action, "Find in the response (Ctrl+F)", true).clicked() {
                    self.open_find(ui.ctx());
                }
            });
        });
        ui.separator();

        match self.tab {
            Tab::Response => self.body_ui(ui, theme, font, captures),
            Tab::Headers => {
                egui::ScrollArea::both().auto_shrink([false, false]).show(ui, |ui| {
                    egui::Grid::new(self.id.with("headers")).striped(true).num_columns(2).show(ui, |ui| {
                        ui.strong("Header");
                        ui.strong("Value");
                        ui.end_row();
                        for (name, value) in &self.headers {
                            ui.add(egui::Label::new(name).selectable(true));
                            ui.add(egui::Label::new(value).selectable(true));
                            ui.end_row();
                        }
                    });
                });
            }
            Tab::Capture => {
                if captures.ui(ui, theme, found, font.clone()) {
                    out.captures_changed = true;
                    self.captured.clear();
                }
            }
        }
        out
    }

    fn fields_ui(&self, ui: &mut Ui, theme: &Theme) {
        let faded = crate::theme::mix(ui.visuals().text_color(), ui.visuals().panel_fill);
        let error = theme.colour(Kind::Error);
        let value = |f: &Field| {
            let text = RichText::new(f.value.clone());
            if f.failure {
                text.color(error)
            } else {
                text
            }
        };
        ui.horizontal_top(|ui| {
            let right: Vec<&Field> = self.fields.iter().filter(|f| f.label != STATUS && f.label != ERROR && !f.value.is_empty()).collect();
            // The status and error are at the left, and the error wraps
            // beneath itself.
            let right_width = right.iter().map(|f| (f.label.len() + f.value.len()) as f32 * 7.5 + 24.0).sum::<f32>();
            let left_width = (ui.available_width() - right_width).max(100.0);
            ui.allocate_ui_with_layout(vec2(left_width, 0.0), egui::Layout::left_to_right(egui::Align::TOP), |ui| {
                for f in self.fields.iter().filter(|f| (f.label == STATUS || f.label == ERROR) && !f.value.is_empty()) {
                    ui.label(RichText::new(format!("{}:", f.label)).strong().color(faded));
                    if f.label == ERROR {
                        ui.add(egui::Label::new(value(f)).wrap().selectable(true));
                    } else {
                        ui.add(egui::Label::new(value(f)).selectable(true));
                        ui.add_space(14.0);
                    }
                }
            });
            ui.with_layout(egui::Layout::right_to_left(egui::Align::TOP), |ui| {
                for f in right.iter().rev() {
                    ui.label(value(f));
                    ui.label(RichText::new(format!("{}:", f.label)).strong().color(faded));
                    ui.add_space(10.0);
                }
            });
        });
    }

    fn body_ui(&mut self, ui: &mut Ui, theme: &Theme, font: &FontId, captures: &mut Table) {
        if self.find.open {
            self.find_ui(ui);
        }
        let dark = ui.visuals().dark_mode;
        let colour = ui.visuals().text_color();
        let body_id = self.id.with("body");

        let mut scroll_to: Option<Rect> = None;
        let mut menu_capture: Option<(String, String)> = None;
        egui::ScrollArea::both().id_salt(self.id.with("scroll")).auto_shrink([false, false]).show(ui, |ui| {
            let version = self.version;
            let cache = &mut self.galley;
            let matches = &self.find.matches;
            let current = self.find.current;
            let json = self.json;
            let mut layouter = |ui: &Ui, text: &str, wrap: f32| {
                if let Some((v, w, g)) = cache.as_ref() {
                    if *v == version && (*w - wrap).abs() < 0.5 {
                        return g.clone();
                    }
                }
                let job = highlight::job(
                    text,
                    &Colouring { theme, dark, font: font.clone(), colour, json, placeholders: None, matches, current, wrap: Some(wrap) },
                );
                let galley = ui.fonts(|f| f.layout_job(job));
                *cache = Some((version, wrap, galley.clone()));
                galley
            };
            let mut text: &str = self.body.as_str();
            let output = TextEdit::multiline(&mut text)
                .id(body_id)
                .font(font.clone())
                .frame(false)
                .desired_width(f32::INFINITY)
                .desired_rows(4)
                .layouter(&mut layouter)
                .show(ui);

            // The match gone to is the selection, and is scrolled to.
            if self.find.go {
                self.find.go = false;
                if let Some(m) = current.and_then(|c| matches.get(c)) {
                    let start = self.body[..m.start].chars().count();
                    let end = start + self.body[m.clone()].chars().count();
                    let mut state = output.state.clone();
                    state.cursor.set_char_range(Some(egui::text::CCursorRange::two(egui::text::CCursor::new(start), egui::text::CCursor::new(end))));
                    state.store(ui.ctx(), body_id);
                    let cursor = output.galley.from_ccursor(egui::text::CCursor::new(start));
                    let rect = output.galley.pos_from_cursor(&cursor);
                    scroll_to = Some(rect.translate(output.galley_pos.to_vec2()).expand(20.0));
                }
            }

            // Right-clicking a value in the JSON offers to capture it.
            if output.response.secondary_clicked() {
                self.menu_at = ui.input(|i| i.pointer.interact_pos()).map(|p| {
                    let cursor = output.galley.cursor_from_pos(p - output.galley_pos);
                    self.body.char_indices().nth(cursor.ccursor.index).map(|(b, _)| b).unwrap_or(self.body.len())
                });
            }
            let capture = self.capture_path();
            output.response.context_menu(|ui| {
                if ui.button("Copy all").clicked() {
                    ui.ctx().copy_text(self.body.clone());
                    ui.close_menu();
                }
                if let Some((path, name)) = &capture {
                    ui.separator();
                    let label = if path.is_empty() { "Capture the whole response".to_string() } else { format!("Capture {path}") };
                    if ui.button(label).clicked() {
                        menu_capture = Some((path.clone(), name.clone()));
                        ui.close_menu();
                    }
                }
            });
        });
        if let Some(rect) = scroll_to {
            ui.scroll_to_rect(rect, Some(egui::Align::Center));
        }
        if let Some((path, name)) = menu_capture {
            self.tab = Tab::Capture;
            captures.add_to_edit(&name, &path);
        }
    }

    /// Returns the path of the value where the menu was opened, and a name
    /// to call it by, if it is in the JSON that values are captured from.
    fn capture_path(&self) -> Option<(String, String)> {
        let doc = self.capture_doc.as_ref()?;
        if !self.body.ends_with(doc.as_str()) {
            return None;
        }
        let at = self.menu_at?;
        let offset = at.checked_sub(self.body.len() - doc.len())?;
        capture::path_at(doc, offset)
    }

    fn find_ui(&mut self, ui: &mut Ui) {
        ui.horizontal(|ui| {
            let field = ui.add(TextEdit::singleline(&mut self.find.query).hint_text("Find in the response").desired_width(240.0).id(self.id.with("find")));
            if self.find.focus {
                field.request_focus();
                self.find.focus = false;
            }
            if field.changed() {
                self.search();
            }
            if field.lost_focus() && ui.input(|i| i.key_pressed(Key::Enter)) {
                let back = ui.input(|i| i.modifiers.shift);
                self.step(!back);
                field.request_focus();
            }
            if field.has_focus() || field.lost_focus() {
                if ui.input(|i| i.key_pressed(Key::Escape)) {
                    self.close_find();
                    return;
                }
            }
            if ui.button("⏶").on_hover_text("Previous match (Shift+Enter)").clicked() {
                self.step(false);
            }
            if ui.button("⏷").on_hover_text("Next match (Enter)").clicked() {
                self.step(true);
            }
            if ui.toggle_value(&mut self.find.match_case, "Aa").on_hover_text("Match case").changed() {
                self.search();
            }
            let count = match (self.find.current, self.find.matches.len()) {
                (_, 0) if self.find.query.is_empty() => String::new(),
                (_, 0) => "No matches".into(),
                (Some(c), n) => format!("{} of {n}", c + 1),
                (None, n) => format!("{n} matches"),
            };
            ui.label(count);
            if ui.button("×").on_hover_text("Close (Escape)").clicked() {
                self.close_find();
            }
        });
    }

    #[cfg(test)]
    pub fn test_query(&mut self, query: &str) {
        self.find.query = query.into();
        self.search();
    }

    /// Asks where to save the body of the response, and saves it.
    fn save(&mut self, request_name: &str) {
        let name = save_file_name(request_name, chrono::Local::now(), &self.extension);
        let Some(path) = rfd::FileDialog::new().set_title("Save Response").set_file_name(&name).save_file() else { return };
        if let Err(e) = std::fs::write(&path, &self.content) {
            self.warning = Some(format!("The response could not be saved: {e}."));
        }
    }
}

/// Returns the name to offer for the file a response is saved to: the name
/// of its request, the time and resp, with underscores between them, and
/// the extension, as in book_room_20261011_173005_resp.json.
pub fn save_file_name(request: &str, at: chrono::DateTime<chrono::Local>, extension: &str) -> String {
    let mut stem = String::new();
    let (mut count, mut gap) = (0, false);
    for c in request.to_lowercase().chars() {
        if !c.is_alphanumeric() {
            gap = true;
            continue;
        }
        if count == 80 {
            break;
        }
        if gap && count > 0 {
            stem.push('_');
        }
        gap = false;
        stem.push(c);
        count += 1;
    }
    if count == 0 {
        stem.push_str("untitled");
    }
    let dot = if extension.is_empty() { String::new() } else { format!(".{extension}") };
    format!("{stem}_{}_resp{dot}", at.format("%Y%m%d_%H%M%S"))
}

/// Returns the extension for the type of content a response says it is.
fn extension(content_type: &str) -> String {
    let lower = content_type.to_lowercase();
    for (part, ext) in [("json", "json"), ("xml", "xml"), ("html", "html"), ("csv", "csv"), ("pdf", "pdf"), ("png", "png"), ("jpeg", "jpg"), ("gif", "gif"), ("zip", "zip"), ("text/", "txt")] {
        if lower.contains(part) {
            return ext.into();
        }
    }
    "bin".into()
}

pub fn format_duration(d: Duration) -> String {
    if d < Duration::from_secs(1) {
        format!("{} ms", d.as_millis())
    } else {
        format!("{:.2} s", d.as_secs_f64())
    }
}

pub fn format_size(bytes: usize) -> String {
    if bytes < 1024 {
        format!("{bytes} B")
    } else if bytes < 1024 * 1024 {
        format!("{:.1} KB", bytes as f64 / 1024.0)
    } else {
        format!("{:.1} MB", bytes as f64 / (1024.0 * 1024.0))
    }
}

#[allow(dead_code)]
fn unused(_: Color32) {}

#[cfg(test)]
mod tests {
    use super::*;
    use chrono::TimeZone;

    #[test]
    fn names_files() {
        let at = chrono::Local.with_ymd_and_hms(2026, 10, 11, 17, 30, 5).unwrap();
        assert_eq!(save_file_name("Book room!", at, "json"), "book_room_20261011_173005_resp.json");
        assert_eq!(save_file_name("", at, "bin"), "untitled_20261011_173005_resp.bin");
        assert_eq!(extension("application/json; charset=utf-8"), "json");
        assert_eq!(format_duration(Duration::from_millis(1500)), "1.50 s");
        assert_eq!(format_size(2048), "2.0 KB");
    }
}
