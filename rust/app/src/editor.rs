//! The environment editor, shown in place of the request: the name of the
//! environment and a table of its variables, each with a description.

use egui::{FontId, TextEdit, Ui};
use triggerapi_core::env::{Environment, Variable};

use crate::kvtable::{Row, Table};
use crate::theme::Theme;

pub struct Editor {
    pub name: String,
    /// The name the environment has, which is kept while the field is
    /// empty, as it has to be on the way to another being typed.
    named: String,
    pub variables: Table,
    pub focus_name: bool,
}

/// What the user did in the editor.
#[derive(Default)]
pub struct Output {
    /// The name or the variables were changed.
    pub changed: Option<(String, Vec<Variable>)>,
    pub close: bool,
}

impl Editor {
    pub fn new() -> Editor {
        let mut variables = Table::new("variables", "Variable", "Value");
        variables.describe = Some("Description".into());
        variables.exclusive = true;
        Editor { name: String::new(), named: String::new(), variables, focus_name: false }
    }

    pub fn load(&mut self, environment: &Environment) {
        self.named = environment.name.clone();
        self.name = environment.name.clone();
        self.variables.set_rows(
            environment
                .variables
                .iter()
                .map(|v| Row { key: v.key.clone(), value: v.value.clone(), description: v.description.clone(), enabled: v.enabled })
                .collect(),
        );
    }

    pub fn ui(&mut self, ui: &mut Ui, theme: &Theme, found: &dyn Fn(&str) -> bool, font: FontId) -> Output {
        let mut out = Output::default();
        let mut changed = false;
        ui.horizontal(|ui| {
            ui.label("Environment");
            let width = ui.available_width() - 70.0;
            let field = ui.add(TextEdit::singleline(&mut self.name).hint_text("Environment name").desired_width(width));
            if self.focus_name {
                field.request_focus();
                let mut state = TextEdit::load_state(ui.ctx(), field.id).unwrap_or_default();
                let all = egui::text::CCursorRange::two(egui::text::CCursor::new(0), egui::text::CCursor::new(self.name.chars().count()));
                state.cursor.set_char_range(Some(all));
                state.store(ui.ctx(), field.id);
                self.focus_name = false;
            }
            if field.changed() {
                changed = true;
            }
            // An empty name is given back the one the environment has.
            if field.lost_focus() && self.name.trim().is_empty() {
                self.name = self.named.clone();
            }
            if ui.button("Close").clicked() {
                out.close = true;
            }
        });
        ui.add_space(4.0);
        if self.variables.ui(ui, theme, found, font) {
            changed = true;
        }
        if changed {
            if !self.name.trim().is_empty() {
                self.named = self.name.clone();
            }
            let variables = self
                .variables
                .described()
                .into_iter()
                .map(|r| Variable { key: r.key, value: r.value, enabled: r.enabled, description: r.description })
                .collect();
            out.changed = Some((self.named.clone(), variables));
        }
        out
    }
}
