//! The dialogs, shown one at a time over the window, each waiting for the
//! user before what it is for is done.

use egui::{vec2, Color32, Context, Id, Key, RichText, Sense, TextEdit};
use triggerapi_core::settings::Settings;

use crate::app::{App, VERSION};
use crate::icons::{self, Glyph};
use crate::settings_dialog::{self, Outcome};
use crate::theme::Theme;

type Then = Box<dyn FnOnce(&mut App)>;
type Pick = Box<dyn FnOnce(&mut App, usize)>;
type Given = Box<dyn FnOnce(&mut App, Option<String>)>;

pub enum Dialog {
    Message { title: String, text: String },
    Confirm { text: String, yes: Option<Then> },
    /// Offers each choice as an icon with its name beneath.
    Choice { title: String, prompt: String, choices: Vec<(String, Glyph, Color32)>, pick: Option<Pick> },
    Passphrase { path: String, text: String, then: Option<Given>, focus: bool },
    Settings(Box<settings_dialog::Dialog>),
    About,
}

/// What came of drawing a dialog.
pub enum DialogAction {
    /// It stays open.
    Keep,
    Close,
    Run(Then),
    Settings(Box<Settings>),
    Export,
    Import,
}

impl Dialog {
    pub fn message(title: &str, text: &str) -> Dialog {
        Dialog::Message { title: title.into(), text: text.into() }
    }

    pub fn confirm(text: &str, yes: impl FnOnce(&mut App) + 'static) -> Dialog {
        Dialog::Confirm { text: text.into(), yes: Some(Box::new(yes)) }
    }

    pub fn choice(title: &str, prompt: &str, choices: Vec<(String, Glyph, Color32)>, pick: impl FnOnce(&mut App, usize) + 'static) -> Dialog {
        Dialog::Choice { title: title.into(), prompt: prompt.into(), choices, pick: Some(Box::new(pick)) }
    }

    pub fn passphrase(path: &str, then: impl FnOnce(&mut App, Option<String>) + 'static) -> Dialog {
        Dialog::Passphrase { path: path.into(), text: String::new(), then: Some(Box::new(then)), focus: true }
    }

    pub fn ui(&mut self, ctx: &Context, _theme: &Theme, dark: bool) -> DialogAction {
        let mut action = DialogAction::Keep;
        let modal = egui::Modal::new(Id::new("dialog")).show(ctx, |ui| match self {
            Dialog::Message { title, text } => {
                ui.set_max_width(460.0);
                ui.heading(title.as_str());
                ui.add_space(6.0);
                ui.add(egui::Label::new(text.as_str()).wrap());
                ui.add_space(10.0);
                ui.with_layout(egui::Layout::right_to_left(egui::Align::Center), |ui| {
                    if ui.button("OK").clicked() || ui.input(|i| i.key_pressed(Key::Enter)) {
                        action = DialogAction::Close;
                    }
                });
            }
            Dialog::Confirm { text, yes } => {
                ui.set_max_width(460.0);
                ui.heading("TriggerAPI");
                ui.add_space(6.0);
                ui.add(egui::Label::new(text.as_str()).wrap());
                ui.add_space(10.0);
                ui.with_layout(egui::Layout::right_to_left(egui::Align::Center), |ui| {
                    if ui.button("No").clicked() {
                        action = DialogAction::Close;
                    }
                    if ui.button("Yes").clicked() {
                        if let Some(f) = yes.take() {
                            action = DialogAction::Run(f);
                        }
                    }
                });
            }
            Dialog::Choice { title, prompt, choices, pick } => {
                ui.heading(title.as_str());
                ui.label(prompt.as_str());
                ui.add_space(8.0);
                let mut picked = None;
                ui.horizontal(|ui| {
                    for (i, (label, glyph, colour)) in choices.iter().enumerate() {
                        let (rect, response) = ui.allocate_exact_size(vec2(110.0, 110.0), Sense::click());
                        if response.hovered() {
                            ui.painter().rect_filled(rect, 6.0, ui.visuals().widgets.hovered.weak_bg_fill);
                        }
                        let icon = egui::Rect::from_center_size(rect.center() - vec2(0.0, 12.0), vec2(64.0, 64.0));
                        icons::paint(ui.painter(), icon, *colour, *glyph);
                        ui.painter().text(rect.center_bottom() - vec2(0.0, 10.0), egui::Align2::CENTER_BOTTOM, label, egui::FontId::proportional(15.0), ui.visuals().text_color());
                        if response.clicked() {
                            picked = Some(i);
                        }
                    }
                });
                ui.add_space(8.0);
                ui.with_layout(egui::Layout::right_to_left(egui::Align::Center), |ui| {
                    if ui.button("Cancel").clicked() {
                        action = DialogAction::Close;
                    }
                });
                if let Some(i) = picked {
                    if let Some(f) = pick.take() {
                        action = DialogAction::Run(Box::new(move |app| f(app, i)));
                    }
                }
            }
            Dialog::Passphrase { path, text, then, focus } => {
                ui.heading("Private Key");
                ui.label(format!("Passphrase of {path}"));
                let field = ui.add(TextEdit::singleline(text).password(true).desired_width(320.0));
                if *focus {
                    field.request_focus();
                    *focus = false;
                }
                let enter = field.lost_focus() && ui.input(|i| i.key_pressed(Key::Enter));
                ui.with_layout(egui::Layout::right_to_left(egui::Align::Center), |ui| {
                    if ui.button("Cancel").clicked() {
                        if let Some(f) = then.take() {
                            action = DialogAction::Run(Box::new(move |app| f(app, None)));
                        }
                    }
                    if ui.button("OK").clicked() || enter {
                        let given = text.clone();
                        if let Some(f) = then.take() {
                            action = DialogAction::Run(Box::new(move |app| f(app, Some(given))));
                        }
                    }
                });
            }
            Dialog::Settings(dialog) => {
                ui.heading("Settings");
                match dialog.ui(ui, dark) {
                    Outcome::Open => {}
                    Outcome::Cancelled => action = DialogAction::Close,
                    Outcome::Accepted(s) => action = DialogAction::Settings(Box::new(s)),
                    Outcome::Export => action = DialogAction::Export,
                    Outcome::Import => action = DialogAction::Import,
                }
            }
            Dialog::About => {
                ui.set_max_width(460.0);
                ui.horizontal(|ui| {
                    let (rect, _) = ui.allocate_exact_size(vec2(64.0, 64.0), Sense::hover());
                    icons::paint(ui.painter(), rect, crate::theme::parse("#e8590c").unwrap(), Glyph::Revolver);
                    ui.vertical(|ui| {
                        ui.heading(format!("TriggerAPI {VERSION}"));
                        ui.label("A desktop client for making REST API and gRPC calls.");
                        ui.label("Copyright © 2026 Gary Willoughby. All rights reserved.");
                    });
                });
                ui.add_space(10.0);
                egui::Frame::group(ui.style()).show(ui, |ui| {
                    ui.label(RichText::new("Third party licences").strong().small());
                    ui.add(egui::Label::new(RichText::new(
                        "This build is written in Rust. Its user interface is drawn with egui and eframe, used under the MIT and Apache 2.0 licences. \
                         Its other libraries, among them tokio, reqwest, tonic, prost-reflect and age, are used under their own open source licences.",
                    ).small()).wrap());
                });
                ui.add_space(10.0);
                ui.with_layout(egui::Layout::right_to_left(egui::Align::Center), |ui| {
                    if ui.button("OK").clicked() {
                        action = DialogAction::Close;
                    }
                });
            }
        });
        // Escape, or a click outside, closes what can be closed without an
        // answer.
        if modal.should_close() && matches!(action, DialogAction::Keep) {
            action = match self {
                Dialog::Passphrase { then, .. } => match then.take() {
                    Some(f) => DialogAction::Run(Box::new(move |app| f(app, None))),
                    None => DialogAction::Close,
                },
                // The settings are not lost by a stray click.
                Dialog::Settings(_) if !ctx.input(|i| i.key_pressed(Key::Escape)) => DialogAction::Keep,
                _ => DialogAction::Close,
            };
        }
        action
    }
}
