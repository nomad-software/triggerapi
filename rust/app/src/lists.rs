//! The Workspaces and Environments tabs of the sidebar: a list of each, with
//! the buttons that act on them along the bottom.

use egui::{pos2, vec2, Align2, CornerRadius, FontId, Id, Key, Rect, Sense, TextEdit, Ui};

use crate::icons::{self, Glyph};
use crate::theme::{Kind, Theme};

/// What the user asked for in a list.
#[derive(Debug, PartialEq, Eq)]
pub enum ListEvent {
    Select(usize),
    Add,
    Duplicate,
    /// A workspace was renamed in the list.
    Renamed(usize, String),
    Remove,
    Edit,
    Import,
}

/// A list of names, each with an icon, one of which is selected.
pub struct List {
    id: Id,
    /// The row being renamed, with the name as it is being typed.
    renaming: Option<(usize, String)>,
    rename_focus: bool,
    pub focus: bool,
}

impl List {
    pub fn new(id: &str) -> List {
        List { id: Id::new(id), renaming: None, rename_focus: false, focus: false }
    }

    /// Starts renaming a row.
    pub fn rename(&mut self, row: usize, name: &str) {
        self.renaming = Some((row, name.to_string()));
        self.rename_focus = true;
    }

    /// Draws the names, with the selected one, and reports what the user
    /// did. `on_f2` is what F2 does.
    fn rows(&mut self, ui: &mut Ui, names: &[String], selected: Option<usize>, colour: egui::Color32, glyph: Glyph, f2: ListEvent, events: &mut Vec<ListEvent>) {
        let list_id = self.id.with("list");
        if self.focus {
            ui.memory_mut(|m| m.request_focus(list_id));
            self.focus = false;
        }
        let focused = ui.memory(|m| m.has_focus(list_id));
        if focused && self.renaming.is_none() {
            ui.memory_mut(|m| m.set_focus_lock_filter(list_id, egui::EventFilter { vertical_arrows: true, ..Default::default() }));
            let pressed = |k: Key| ui.input_mut(|i| i.consume_key(egui::Modifiers::NONE, k));
            if pressed(Key::ArrowDown) && !names.is_empty() {
                events.push(ListEvent::Select(selected.map(|s| (s + 1).min(names.len() - 1)).unwrap_or(0)));
            }
            if pressed(Key::ArrowUp) && !names.is_empty() {
                events.push(ListEvent::Select(selected.map(|s| s.saturating_sub(1)).unwrap_or(0)));
            }
            if pressed(Key::F2) {
                events.push(f2);
            }
        }

        egui::ScrollArea::vertical().auto_shrink([false, false]).show(ui, |ui| {
            for (i, name) in names.iter().enumerate() {
                let (rect, response) = ui.allocate_exact_size(vec2(ui.available_width(), 24.0), Sense::click());
                let visuals = ui.visuals().clone();
                if selected == Some(i) {
                    ui.painter().rect_filled(rect, CornerRadius::same(3), visuals.selection.bg_fill.gamma_multiply(0.7));
                } else if response.hovered() {
                    ui.painter().rect_filled(rect, CornerRadius::same(3), visuals.widgets.hovered.weak_bg_fill);
                }
                let icon = Rect::from_min_size(pos2(rect.left() + 4.0, rect.center().y - 9.0), vec2(18.0, 18.0));
                icons::paint(ui.painter(), icon, colour, glyph);
                let name_rect = Rect::from_min_max(pos2(rect.left() + 28.0, rect.top()), rect.max);

                let mut drawn = false;
                if let Some((row, text)) = &mut self.renaming {
                    if *row == i {
                        drawn = true;
                        let id = self.id.with(("rename", i));
                        let edit = ui.put(name_rect, TextEdit::singleline(text).id(id));
                        if self.rename_focus {
                            edit.request_focus();
                            let mut state = TextEdit::load_state(ui.ctx(), id).unwrap_or_default();
                            let all = egui::text::CCursorRange::two(egui::text::CCursor::new(0), egui::text::CCursor::new(text.chars().count()));
                            state.cursor.set_char_range(Some(all));
                            state.store(ui.ctx(), id);
                            self.rename_focus = false;
                        } else if ui.input(|inp| inp.key_pressed(Key::Escape)) && (edit.lost_focus() || edit.has_focus()) {
                            self.renaming = None;
                        } else if edit.lost_focus() || !edit.has_focus() {
                            events.push(ListEvent::Renamed(i, text.clone()));
                            self.renaming = None;
                        }
                    }
                }
                if !drawn {
                    ui.painter().text(name_rect.left_center(), Align2::LEFT_CENTER, name, FontId::proportional(14.0), visuals.text_color());
                }
                if response.clicked() {
                    ui.memory_mut(|m| m.request_focus(list_id));
                    if selected != Some(i) {
                        events.push(ListEvent::Select(i));
                    }
                }
                if response.double_clicked() {
                    events.push(ListEvent::Edit);
                }
            }
        });
    }

    /// Draws the Workspaces tab.
    pub fn workspaces(&mut self, ui: &mut Ui, names: &[String], selected: Option<usize>, theme: &Theme) -> Vec<ListEvent> {
        let mut events = Vec::new();
        let colour = theme.colour(Kind::Workspaces);
        egui::TopBottomPanel::bottom("workspace buttons").frame(egui::Frame::NONE).show_inside(ui, |ui| {
            ui.add_space(4.0);
            ui.label(egui::RichText::new("Each workspace has its own environments and requests. The selected workspace is the one in use.").weak());
            ui.horizontal(|ui| {
                if icons::button(ui, Glyph::Plus, colour, "Add a workspace", true).clicked() {
                    events.push(ListEvent::Add);
                }
                let some = selected.is_some();
                if icons::button(ui, Glyph::Copy, colour, "Duplicate the selected workspace", some).clicked() {
                    events.push(ListEvent::Duplicate);
                }
                if icons::button(ui, Glyph::Pencil, colour, "Rename the selected workspace", some).clicked() {
                    if let Some(s) = selected {
                        self.rename(s, &names[s]);
                    }
                }
                if icons::button(ui, Glyph::Bin, theme.colour(Kind::Remove), "Remove the selected workspace", some).clicked() {
                    events.push(ListEvent::Remove);
                }
            });
        });
        let mut f2 = Vec::new();
        self.rows(ui, names, selected, colour, Glyph::Monitor, ListEvent::Edit, &mut f2);
        for e in f2 {
            // F2 renames a workspace, as its rename button does.
            match e {
                ListEvent::Edit => {
                    if let Some(s) = selected {
                        self.rename(s, &names[s]);
                    }
                }
                other => events.push(other),
            }
        }
        events
    }

    /// Draws the Environments tab.
    pub fn environments(&mut self, ui: &mut Ui, names: &[String], selected: Option<usize>, theme: &Theme) -> Vec<ListEvent> {
        let mut events = Vec::new();
        let colour = theme.colour(Kind::Environments);
        egui::TopBottomPanel::bottom("environment buttons").frame(egui::Frame::NONE).show_inside(ui, |ui| {
            ui.add_space(4.0);
            ui.label(egui::RichText::new("Use {{variable}} in any request field to insert a value from the selected environment.").weak());
            ui.horizontal(|ui| {
                if icons::button(ui, Glyph::Plus, colour, "Add an environment", true).clicked() {
                    events.push(ListEvent::Add);
                }
                let some = selected.is_some();
                if icons::button(ui, Glyph::Copy, colour, "Duplicate the selected environment", some).clicked() {
                    events.push(ListEvent::Duplicate);
                }
                if icons::button(ui, Glyph::Pencil, colour, "Edit the selected environment", some).clicked() {
                    events.push(ListEvent::Edit);
                }
                if icons::button(ui, Glyph::Bin, theme.colour(Kind::Remove), "Remove the selected environment", some).clicked() {
                    events.push(ListEvent::Remove);
                }
                ui.with_layout(egui::Layout::right_to_left(egui::Align::Center), |ui| {
                    if icons::button(ui, Glyph::Import, colour, "Import an environment exported from Postman", true).clicked() {
                        events.push(ListEvent::Import);
                    }
                });
            });
        });
        self.rows(ui, names, selected, colour, Glyph::Globe, ListEvent::Edit, &mut events);
        events
    }
}
