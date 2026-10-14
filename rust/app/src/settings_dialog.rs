//! The Settings dialog: where the workspaces are kept and the keys they are
//! encrypted with, how requests are sent, and the font and colours.

use egui::{Color32, RichText, Ui};
use triggerapi_core::settings::{Settings, MAX_TIMEOUT};

use crate::theme::{self, Kind, Token};

#[derive(Clone, Copy, PartialEq, Eq)]
enum Tab {
    Workspaces,
    Requests,
    Interface,
}

/// What the user did in the dialog.
pub enum Outcome {
    /// The dialog stays open.
    Open,
    Accepted(Settings),
    Cancelled,
    Export,
    Import,
}

pub struct Dialog {
    settings: Settings,
    tab: Tab,
    /// The folder the workspaces are kept in unless another is chosen.
    default_dir: String,
    families: Vec<String>,
}

impl Dialog {
    pub fn new(settings: &Settings, default_dir: &str) -> Dialog {
        let mut settings = settings.clone();
        if settings.workspaces_dir.is_empty() {
            settings.workspaces_dir = default_dir.to_string();
        }
        Dialog { settings, tab: Tab::Workspaces, default_dir: default_dir.into(), families: crate::fonts::families() }
    }

    pub fn ui(&mut self, ui: &mut Ui, dark: bool) -> Outcome {
        let mut outcome = Outcome::Open;
        ui.set_width(620.0);
        ui.horizontal(|ui| {
            ui.selectable_value(&mut self.tab, Tab::Workspaces, "Workspaces");
            ui.selectable_value(&mut self.tab, Tab::Requests, "Requests");
            ui.selectable_value(&mut self.tab, Tab::Interface, "User interface");
        });
        ui.separator();
        egui::ScrollArea::vertical().max_height(480.0).show(ui, |ui| match self.tab {
            Tab::Workspaces => self.workspaces(ui, &mut outcome),
            Tab::Requests => self.requests(ui),
            Tab::Interface => self.interface(ui, dark),
        });
        ui.separator();
        ui.horizontal(|ui| {
            ui.with_layout(egui::Layout::right_to_left(egui::Align::Center), |ui| {
                if ui.button("Cancel").clicked() {
                    outcome = Outcome::Cancelled;
                }
                if ui.button("OK").clicked() {
                    let mut s = self.settings.clone();
                    // The default folder is not named in the settings, so
                    // that it follows the system.
                    if s.workspaces_dir.trim() == self.default_dir || s.workspaces_dir.trim().is_empty() {
                        s.workspaces_dir.clear();
                    }
                    s.public_key = s.public_key.trim().to_string();
                    s.private_key = s.private_key.trim().to_string();
                    outcome = Outcome::Accepted(s);
                }
            });
        });
        outcome
    }

    fn workspaces(&mut self, ui: &mut Ui, outcome: &mut Outcome) {
        let note = |ui: &mut Ui, text: &str| {
            ui.add(egui::Label::new(RichText::new(text).weak()).wrap());
        };
        egui::Grid::new("workspace settings").num_columns(2).spacing([12.0, 8.0]).show(ui, |ui| {
            ui.label("Workspaces folder");
            ui.horizontal(|ui| {
                ui.add(egui::TextEdit::singleline(&mut self.settings.workspaces_dir).desired_width(400.0));
                if ui.button("Browse…").clicked() {
                    if let Some(dir) = rfd::FileDialog::new().set_title("Workspaces Folder").pick_folder() {
                        self.settings.workspaces_dir = dir.display().to_string();
                    }
                }
            });
            ui.end_row();
        });
        note(
            ui,
            "Each workspace has a folder of its own in this one, holding its environments and requests. \
             Choosing a folder that already has workspaces shows those. Choosing one that has none copies the current workspaces there. \
             A relative path is relative to the directory TriggerAPI is run from.",
        );
        ui.add_space(12.0);
        ui.heading("Encryption");
        egui::Grid::new("key settings").num_columns(2).spacing([12.0, 8.0]).show(ui, |ui| {
            for (label, value, hint, title) in [
                ("Public key", &mut self.settings.public_key, "Such as ~/.ssh/id_ed25519.pub", "Public Key"),
                ("Private key", &mut self.settings.private_key, "Such as ~/.ssh/id_ed25519", "Private Key"),
            ] {
                ui.label(label);
                ui.horizontal(|ui| {
                    ui.add(egui::TextEdit::singleline(value).hint_text(hint).desired_width(400.0));
                    if ui.button("Browse…").clicked() {
                        let start = dirs::home_dir().map(|h| h.join(".ssh")).unwrap_or_default();
                        if let Some(file) = rfd::FileDialog::new().set_title(title).set_directory(start).pick_file() {
                            *value = file.display().to_string();
                        }
                    }
                });
                ui.end_row();
            }
        });
        note(
            ui,
            "The environments and requests of every workspace are encrypted with this pair of SSH keys, which can be Ed25519 or RSA. \
             TriggerAPI needs them to work: without them nothing is loaded or saved. Changing the keys encrypts every file again with the new ones. \
             The passphrase of a private key that has one is asked for when TriggerAPI starts.",
        );
        ui.add_space(12.0);
        ui.heading("Import and export");
        ui.horizontal(|ui| {
            if ui.button("Export…").on_hover_text("Write every workspace to a zip file, encrypted for someone else's public key").clicked() {
                *outcome = Outcome::Export;
            }
            if ui.button("Import…").on_hover_text("Add the workspaces of a zip file that was exported for your public key").clicked() {
                *outcome = Outcome::Import;
            }
        });
        note(
            ui,
            "Export asks for the public SSH key of whoever the workspaces are for, and writes every workspace to a zip file encrypted so that only their private key can read it. \
             Your own files are left as they are. Import adds the workspaces of such a file that was exported for your public key. \
             Both use the keys and the folder in effect, not changes made here that have yet to be accepted.",
        );
    }

    fn requests(&mut self, ui: &mut Ui) {
        egui::Grid::new("request settings").num_columns(2).spacing([12.0, 8.0]).show(ui, |ui| {
            ui.label("Timeout");
            ui.add(
                egui::DragValue::new(&mut self.settings.timeout)
                    .range(0..=MAX_TIMEOUT)
                    .suffix(" s")
                    .custom_formatter(|v, _| if v == 0.0 { "No limit".into() } else { format!("{v} s") }),
            )
            .on_hover_text("How long a request can take before it is given up on");
            ui.end_row();
            ui.label("");
            let mut follow = !self.settings.no_redirects;
            if ui
                .checkbox(&mut follow, "Follow redirects")
                .on_hover_text("Send a REST request again to the address a response redirects it to. Without this, the response which redirects is the one shown.")
                .changed()
            {
                self.settings.no_redirects = !follow;
            }
            ui.end_row();
        });
    }

    fn interface(&mut self, ui: &mut Ui, dark: bool) {
        ui.heading("Font");
        ui.horizontal(|ui| {
            ui.label("Code");
            egui::ComboBox::from_id_salt("code font").width(400.0).selected_text(&self.settings.response_font).height(300.0).show_ui(ui, |ui| {
                for family in &self.families {
                    ui.selectable_value(&mut self.settings.response_font, family.clone(), family);
                }
            });
        });
        ui.add_space(14.0);
        ui.heading("Colours");
        egui::Grid::new("colours").num_columns(2).striped(true).spacing([24.0, 6.0]).show(ui, |ui| {
            for (title, kinds) in theme::SECTIONS {
                ui.label(RichText::new(*title).strong());
                ui.end_row();
                for kind in *kinds {
                    ui.label(format!("    {}", theme::label(*kind)));
                    let place = theme::custom(&mut self.settings.colours, *kind);
                    swatch(ui, place, theme::default_hex(*kind));
                    ui.end_row();
                }
            }
            ui.label(RichText::new("JSON").strong());
            ui.end_row();
            for (token, label) in theme::TOKENS {
                ui.label(format!("    {label}"));
                let place = theme::json_custom(&mut self.settings.json_colours, *token);
                swatch(ui, place, theme::json_default(*token, dark));
                ui.end_row();
            }
        });
        ui.add_space(8.0);
        ui.horizontal(|ui| {
            ui.with_layout(egui::Layout::right_to_left(egui::Align::Center), |ui| {
                if ui.button("Use Default Colours").clicked() {
                    self.settings.colours = Default::default();
                    self.settings.json_colours = Default::default();
                }
            });
        });
        let _ = (Kind::Send, Token::Key);
    }
}

/// A button showing a colour, which chooses another. The colour is kept as
/// #rrggbb, or empty for the default.
fn swatch(ui: &mut Ui, place: &mut String, default: &str) {
    let current = theme::parse(place).or_else(|| theme::parse(default)).unwrap_or(Color32::GRAY);
    let mut colour = current;
    ui.horizontal(|ui| {
        if egui::color_picker::color_edit_button_srgba(ui, &mut colour, egui::color_picker::Alpha::Opaque).changed() {
            *place = theme::hex(colour);
        }
        ui.label(RichText::new(theme::hex(current)).monospace().weak());
    })
    .response
    .on_hover_text("Choose the colour");
}
