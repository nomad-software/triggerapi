//! The editable grid of keys and values, with a box to switch each row on
//! and off, a bin to remove it, and an empty row at the end to add another
//! by typing in it. It keeps its rows in alphabetical order of their keys,
//! sorting them once the keyboard has left the table, so that a row never
//! moves from under the user.

use std::collections::HashMap;

use egui::{vec2, Color32, Id, RichText, Sense, TextEdit, Ui};
use triggerapi_core::rest::{self, KeyValue};

use crate::highlight::{self, Colouring};
use crate::icons::{self, Glyph};
use crate::theme::{Kind, Theme};

/// One row of the table.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Row {
    pub key: String,
    pub value: String,
    pub description: String,
    pub enabled: bool,
}

impl Row {
    fn blank() -> Row {
        Row { enabled: true, ..Default::default() }
    }

    fn is_empty(&self) -> bool {
        self.key.is_empty() && self.value.is_empty() && self.description.is_empty()
    }
}

/// How a row of the table of captures went, shown in its status column.
#[derive(Clone, Debug)]
pub struct Status {
    pub good: bool,
    pub tip: String,
}

/// What a column holds.
#[derive(Clone, Copy, PartialEq, Eq, Hash, Debug)]
pub enum Column {
    Key,
    Value,
    Description,
}

/// The table and what is shown of it.
pub struct Table {
    id: Id,
    pub key_label: String,
    pub value_label: String,
    /// The label of the column of descriptions, which is only shown if
    /// there is one.
    pub describe: Option<String>,
    /// The descriptions are written as plainly as the rest, rather than
    /// half transparent.
    pub plain: bool,
    /// Of the rows with one key no more than one is switched on.
    pub exclusive: bool,
    /// The marks of how each row was captured are shown.
    pub statuses: bool,
    /// The placeholders in the keys and values are coloured.
    pub placeholders: bool,
    pub tooltip: Option<String>,
    rows: Vec<Row>,
    /// How each row was captured, by the name of its variable.
    marks: HashMap<String, Status>,
    /// The keyboard was in the table the frame before.
    was_editing: bool,
    /// A cell to put the keyboard in, with its text selected.
    focus: Option<(usize, Column)>,
}

impl Table {
    pub fn new(id: impl std::hash::Hash, key_label: &str, value_label: &str) -> Table {
        Table {
            id: Id::new(id),
            key_label: key_label.into(),
            value_label: value_label.into(),
            describe: None,
            plain: false,
            exclusive: false,
            statuses: false,
            placeholders: true,
            tooltip: None,
            rows: vec![Row::blank()],
            marks: HashMap::new(),
            was_editing: false,
            focus: None,
        }
    }

    /// Returns the pairs that have a key.
    pub fn items(&self) -> Vec<KeyValue> {
        self.rows
            .iter()
            .filter(|r| !r.key.is_empty())
            .map(|r| KeyValue { key: r.key.clone(), value: r.value.clone(), enabled: r.enabled })
            .collect()
    }

    /// Returns the rows that have a key, with their descriptions.
    pub fn described(&self) -> Vec<Row> {
        self.rows.iter().filter(|r| !r.key.is_empty()).cloned().collect()
    }

    /// Shows pairs, in order.
    pub fn set_items(&mut self, items: &[KeyValue]) {
        let rows = items
            .iter()
            .map(|p| Row { key: p.key.clone(), value: p.value.clone(), enabled: p.enabled, description: String::new() })
            .collect();
        self.set_rows(rows);
    }

    /// Shows rows with their descriptions, in order.
    pub fn set_rows(&mut self, mut rows: Vec<Row>) {
        rows.sort_by(compare);
        rows.push(Row::blank());
        self.rows = rows;
        self.marks.clear();
        self.focus = None;
    }

    /// Marks each row with how capturing it went, or takes the marks away
    /// if there are none.
    pub fn show_statuses(&mut self, marks: HashMap<String, Status>) {
        self.marks = marks;
    }

    pub fn has_marks(&self) -> bool {
        !self.marks.is_empty()
    }

    /// Adds a row with a key and value, unless there is one the same, and
    /// puts the keyboard in its key, ready to be changed.
    pub fn add_to_edit(&mut self, key: &str, value: &str) {
        let find = |rows: &[Row]| rows.iter().position(|r| !r.key.is_empty() && r.key == key && r.value == value);
        if find(&self.rows).is_none() {
            let last = self.rows.len() - 1;
            self.rows[last].key = key.into();
            self.rows[last].value = value.into();
            self.rows.push(Row::blank());
            self.sort();
        }
        if let Some(row) = find(&self.rows) {
            self.focus = Some((row, Column::Key));
        }
    }

    /// Puts the rows in order of their keys, ignoring case, and then of
    /// their descriptions. It reports if anything moved.
    fn sort(&mut self) -> bool {
        let last = self.rows.pop().unwrap_or_else(Row::blank);
        let before = self.rows.clone();
        self.rows.sort_by(compare);
        self.rows.push(last);
        before[..] != self.rows[..self.rows.len() - 1]
    }

    /// Of the rows with one key no more than one is switched on: switching
    /// one on switches the others off, and a row given the key of one that
    /// is on is itself switched off.
    fn keep_one(&mut self, row: usize, column: Option<Column>) {
        let Row { key, enabled, .. } = self.rows[row].clone();
        if key.is_empty() || !enabled {
            return;
        }
        for other in 0..self.rows.len() {
            if other == row || !self.rows[other].enabled || self.rows[other].key != key {
                continue;
            }
            match column {
                None => self.rows[other].enabled = false,
                Some(Column::Key) => {
                    self.rows[row].enabled = false;
                    return;
                }
                _ => {}
            }
        }
    }

    /// Draws the table, and reports if the user changed anything.
    pub fn ui(&mut self, ui: &mut Ui, theme: &Theme, found: &dyn Fn(&str) -> bool, font: egui::FontId) -> bool {
        let mut changed = false;
        let mut editing = false;
        let dark = ui.visuals().dark_mode;
        let faint = ui.visuals().text_color().gamma_multiply(0.5);
        let remove_colour = theme.colour(Kind::Remove);

        let width = ui.available_width();
        let check_w = 22.0;
        let bin_w = 24.0;
        let status_w = if self.statuses { 24.0 } else { 0.0 };
        let columns = if self.describe.is_some() { 3.0 } else { 2.0 };
        let spacing = ui.spacing().item_spacing.x;
        // A text field is as wide as it is asked to be and its margins.
        let margins = 2.0 * ui.spacing().button_padding.x.max(4.0);
        let text_w = ((width - check_w - bin_w - status_w - spacing * (columns + 3.0)) / columns - margins).max(40.0);

        let header = |ui: &mut Ui, text: &str, w: f32| {
            ui.add_sized(vec2(w, 18.0), egui::Label::new(RichText::new(text).strong()).truncate());
        };

        let response = ui
            .vertical(|ui| {
                ui.horizontal(|ui| {
                    ui.add_space(check_w);
                    header(ui, &self.key_label.clone(), text_w);
                    header(ui, &self.value_label.clone(), text_w);
                    if let Some(d) = self.describe.clone() {
                        header(ui, &d, text_w);
                    }
                });
                ui.separator();

                egui::ScrollArea::vertical().id_salt(self.id.with("scroll")).auto_shrink([false, true]).show(ui, |ui| {
                    let mut remove = None;
                    let count = self.rows.len();
                    for i in 0..count {
                        let last = i == count - 1;
                        ui.horizontal(|ui| {
                            ui.add_sized(vec2(check_w, 20.0), |ui: &mut Ui| {
                                let r = ui.checkbox(&mut self.rows[i].enabled, "");
                                if r.changed() {
                                    changed = true;
                                    if self.exclusive {
                                        self.keep_one(i, None);
                                    }
                                }
                                r
                            });

                            let describe = self.describe.is_some();
                            let mut key_changed = false;
                            let mut cell = |ui: &mut Ui, column: Column, w: f32| {
                                let id = self.id.with((i, column));
                                let colour = if column == Column::Description && !self.plain { faint } else { ui.visuals().text_color() };
                                let colour_placeholders = self.placeholders && column != Column::Description;
                                let mut layouter = |ui: &Ui, text: &str, _wrap: f32| {
                                    let job = highlight::job(
                                        text,
                                        &Colouring {
                                            theme,
                                            dark,
                                            font: font.clone(),
                                            colour,
                                            json: false,
                                            placeholders: if colour_placeholders { Some(found) } else { None },
                                            matches: &[],
                                            current: None,
                                            wrap: None,
                                        },
                                    );
                                    ui.fonts(|f| f.layout_job(job))
                                };
                                let row = &mut self.rows[i];
                                let text = match column {
                                    Column::Key => &mut row.key,
                                    Column::Value => &mut row.value,
                                    Column::Description => &mut row.description,
                                };
                                let hint = if last && column == Column::Key { "Add…" } else { "" };
                                let output = TextEdit::singleline(text)
                                    .id(id)
                                    .desired_width(w)
                                    .hint_text(hint)
                                    .font(font.clone())
                                    .layouter(&mut layouter)
                                    .show(ui);
                                if self.focus == Some((i, column)) {
                                    output.response.request_focus();
                                    let mut state = output.state.clone();
                                    let all = egui::text::CCursorRange::two(
                                        egui::text::CCursor::new(0),
                                        egui::text::CCursor::new(text.chars().count()),
                                    );
                                    state.cursor.set_char_range(Some(all));
                                    state.store(ui.ctx(), id);
                                    self.focus = None;
                                }
                                if output.response.has_focus() {
                                    editing = true;
                                }
                                if output.response.changed() {
                                    changed = true;
                                    if column == Column::Key {
                                        key_changed = true;
                                    }
                                }
                            };
                            cell(ui, Column::Key, text_w);
                            cell(ui, Column::Value, text_w);
                            if describe {
                                cell(ui, Column::Description, text_w);
                            }
                            if key_changed && self.exclusive {
                                self.keep_one(i, Some(Column::Key));
                            }

                            if self.statuses {
                                let (rect, response) = ui.allocate_exact_size(vec2(status_w, 20.0), Sense::hover());
                                let row = &self.rows[i];
                                if let Some(mark) = self.marks.get(row.key.trim()).filter(|_| row.enabled && !last) {
                                    let colour = theme.colour(if mark.good { Kind::Captured } else { Kind::NotCaptured });
                                    icons::mark(ui.painter(), egui::Rect::from_center_size(rect.center(), vec2(16.0, 16.0)), colour, mark.good);
                                    response.on_hover_text(mark.tip.clone());
                                }
                            }

                            if last {
                                ui.add_space(bin_w);
                            } else if icons::button(ui, Glyph::Bin, remove_colour, "Remove this row", true).clicked() {
                                remove = Some(i);
                            }
                        });
                    }
                    if let Some(i) = remove {
                        self.rows.remove(i);
                        changed = true;
                    }
                });
            })
            .response;
        if let Some(tip) = &self.tooltip {
            response.on_hover_text(tip.clone());
        }

        // Typing in the empty row at the end makes another.
        if self.rows.last().is_some_and(|r| !r.is_empty()) {
            self.rows.push(Row::blank());
        }
        // The rows are put in order once the keyboard has left the table.
        if self.was_editing && !editing && self.focus.is_none() && self.sort() {
            changed = true;
        }
        self.was_editing = editing;
        if changed {
            // The marks were of the rows as they were.
            self.marks.clear();
        }
        let _ = Color32::TRANSPARENT;
        changed
    }
}

/// The order of rows: by key, ignoring case, with those that have none last,
/// and then by description.
fn compare(a: &Row, b: &Row) -> std::cmp::Ordering {
    let pa = KeyValue { key: a.key.clone(), value: String::new(), enabled: false };
    let pb = KeyValue { key: b.key.clone(), value: String::new(), enabled: false };
    rest::compare_pairs(&pa, &pb).then_with(|| a.description.to_lowercase().cmp(&b.description.to_lowercase()))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn keeps_order_and_one_enabled() {
        let mut t = Table::new("t", "Key", "Value");
        t.set_rows(vec![
            Row { key: "b".into(), value: "1".into(), enabled: true, description: String::new() },
            Row { key: "A".into(), value: "2".into(), enabled: true, description: "x".into() },
            Row { key: "a".into(), value: "3".into(), enabled: false, description: "y".into() },
        ]);
        let keys: Vec<String> = t.described().iter().map(|r| r.key.clone()).collect();
        assert_eq!(keys, vec!["A", "a", "b"]);
        t.exclusive = true;
        t.rows[1].key = "A".into();
        t.rows[1].enabled = true;
        t.keep_one(1, None);
        assert!(!t.rows[0].enabled && t.rows[1].enabled);
        t.add_to_edit("c", "path");
        assert_eq!(t.items().last().unwrap().key, "c");
        assert_eq!(t.rows.last().unwrap(), &Row::blank());
    }
}
