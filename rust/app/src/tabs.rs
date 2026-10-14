//! The row of tabs above the request: one for each pinned request, and at
//! most one that is not pinned, which shows whichever request was last
//! selected in the sidebar. The tabs share the one request UI.

use egui::{vec2, Align2, CornerRadius, FontId, Rect, Sense, Ui};
use triggerapi_core::collection::Store;
use triggerapi_core::rest;

use crate::icons::{self, Glyph};
use crate::response::Replay;
use crate::theme::{Kind, Theme};

/// What the tab of a request that is not saved is called.
pub const UNTITLED: &str = "Untitled";

#[derive(Clone)]
pub struct Tab {
    pub id: u64,
    /// The request shown in the tab, or none for one that is not saved.
    pub request: Option<u64>,
    pub pinned: bool,
    /// What had been typed in the request that is not saved.
    pub draft: Option<rest::Request>,
    /// The response the request was last given, shown again when the tab
    /// is gone back to.
    pub replay: Option<Replay>,
}

pub struct Tabs {
    pub tabs: Vec<Tab>,
    pub current: usize,
    hovered: Option<u64>,
    next_id: u64,
}

impl Tabs {
    pub fn new() -> Tabs {
        let mut tabs = Tabs { tabs: Vec::new(), current: 0, hovered: None, next_id: 1 };
        tabs.reset(&[]);
        tabs
    }

    fn make(&mut self, request: Option<u64>, pinned: bool) -> Tab {
        self.next_id += 1;
        Tab { id: self.next_id, request, pinned, draft: None, replay: None }
    }

    pub fn current(&self) -> &Tab {
        &self.tabs[self.current.min(self.tabs.len() - 1)]
    }

    pub fn current_mut(&mut self) -> &mut Tab {
        let at = self.current.min(self.tabs.len() - 1);
        &mut self.tabs[at]
    }

    pub fn by_id(&mut self, id: u64) -> Option<&mut Tab> {
        self.tabs.iter_mut().find(|t| t.id == id)
    }

    /// Returns the pinned requests, in the order of their tabs.
    pub fn pinned(&self) -> Vec<u64> {
        self.tabs.iter().filter(|t| t.pinned).filter_map(|t| t.request).collect()
    }

    /// Replaces the tabs with one for each pinned request, or one for a
    /// request that is not saved if there are none, and shows the first.
    pub fn reset(&mut self, pinned: &[u64]) {
        self.tabs.clear();
        for r in pinned {
            let tab = self.make(Some(*r), true);
            self.tabs.push(tab);
        }
        if self.tabs.is_empty() {
            let tab = self.make(None, false);
            self.tabs.push(tab);
        }
        self.current = 0;
    }

    /// Shows a request in its pinned tab, or else in the tab that is not
    /// pinned, which is made if there is none, and returns its index.
    pub fn open(&mut self, request: Option<u64>) -> usize {
        if let Some(r) = request {
            if let Some(at) = self.tabs.iter().position(|t| t.pinned && t.request == Some(r)) {
                self.current = at;
                return at;
            }
        }
        let at = match self.tabs.iter().position(|t| !t.pinned) {
            Some(at) => at,
            None => {
                let tab = self.make(None, false);
                self.tabs.push(tab);
                self.tabs.len() - 1
            }
        };
        if self.tabs[at].request != request {
            let tab = &mut self.tabs[at];
            tab.request = request;
            tab.draft = None;
            tab.replay = None;
        }
        self.current = at;
        at
    }

    /// Pins a tab, or unpins it: one unpinned while shown becomes the tab
    /// that is not pinned, and otherwise it is closed.
    fn toggle(&mut self, at: usize) {
        if !self.tabs[at].pinned {
            if self.tabs[at].request.is_some() {
                self.tabs[at].pinned = true;
            }
            return;
        }
        if at == self.current {
            let id = self.tabs[at].id;
            if let Some(other) = self.tabs.iter().position(|t| !t.pinned) {
                self.tabs.remove(other);
            }
            let at = self.tabs.iter().position(|t| t.id == id).unwrap();
            self.tabs[at].pinned = false;
            self.current = at;
        } else {
            let id = self.tabs[self.current].id;
            self.tabs.remove(at);
            self.current = self.tabs.iter().position(|t| t.id == id).unwrap_or(0);
        }
    }

    /// Closes the pinned tab of a request that has been removed, and empties
    /// the tab that is not pinned if it showed it.
    pub fn remove(&mut self, request: u64) {
        let current_id = self.current().id;
        self.tabs.retain(|t| !(t.pinned && t.request == Some(request)));
        for t in &mut self.tabs {
            if t.request == Some(request) {
                t.request = None;
                t.draft = None;
                t.replay = None;
            }
        }
        if self.tabs.is_empty() {
            let tab = self.make(None, false);
            self.tabs.push(tab);
        }
        self.current = self.tabs.iter().position(|t| t.id == current_id).unwrap_or(0).min(self.tabs.len() - 1);
    }

    /// Draws the tabs, and returns the index of one chosen, with that of the
    /// tab that was shown before.
    pub fn ui(&mut self, ui: &mut Ui, store: &Store, theme: &Theme) -> Option<(usize, usize)> {
        let mut chosen = None;
        let mut toggled = None;
        let mut hovered = None;
        egui::ScrollArea::horizontal().id_salt("request tabs").max_height(30.0).show(ui, |ui| {
            ui.horizontal(|ui| {
                for (i, tab) in self.tabs.iter().enumerate() {
                    let request = tab.request.and_then(|r| store.request(r));
                    let name = request.map(|r| r.name.clone()).unwrap_or_else(|| UNTITLED.into());
                    let grpc = request.is_some_and(|r| r.is_grpc());
                    let font = FontId::proportional(14.0);
                    let text_width = ui.fonts(|f| f.layout_no_wrap(name.clone(), font.clone(), egui::Color32::WHITE).size().x).min(220.0);
                    let size = vec2(text_width + 44.0, 26.0);
                    let (rect, response) = ui.allocate_exact_size(size, Sense::click());
                    let visuals = ui.visuals();
                    let selected = i == self.current;
                    let fill = if selected {
                        visuals.selection.bg_fill.gamma_multiply(0.5)
                    } else if response.hovered() {
                        visuals.widgets.hovered.weak_bg_fill
                    } else {
                        visuals.widgets.inactive.weak_bg_fill
                    };
                    ui.painter().rect_filled(rect, CornerRadius { nw: 5, ne: 5, sw: 0, se: 0 }, fill);
                    if selected {
                        ui.painter().line_segment([rect.left_bottom(), rect.right_bottom()], (2.0, visuals.selection.stroke.color));
                    }
                    let button = Rect::from_center_size(rect.left_center() + vec2(15.0, 0.0), vec2(18.0, 18.0));
                    let over_button = ui.input(|inp| inp.pointer.hover_pos()).is_some_and(|p| button.contains(p));
                    if response.hovered() {
                        hovered = Some(tab.id);
                    }
                    // The button before the name: a pin on the tab that is
                    // not pinned, which pins it; on a pinned tab the icon of
                    // its type, which becomes a bin while the pointer is over
                    // the tab, to unpin it with.
                    let (glyph, colour, tip) = if !tab.pinned {
                        let colour = theme.colour(Kind::Action);
                        let colour = if tab.request.is_some() { colour } else { colour.gamma_multiply(0.35) };
                        (Glyph::Pin, colour, "Pin this tab, to keep this request open in it".to_string())
                    } else if self.hovered == Some(tab.id) {
                        (Glyph::Bin, theme.colour(Kind::Remove), "Unpin this tab".to_string())
                    } else if grpc {
                        (Glyph::Arrows, theme.colour(Kind::Grpc), "gRPC request, pinned".to_string())
                    } else {
                        (Glyph::Braces, theme.colour(Kind::Rest), "REST request, pinned".to_string())
                    };
                    icons::paint(ui.painter(), Rect::from_center_size(button.center(), vec2(15.0, 15.0)), colour, glyph);
                    let name_rect = Rect::from_min_max(rect.left_top() + vec2(28.0, 0.0), rect.right_bottom() - vec2(6.0, 0.0));
                    let galley = ui.fonts(|f| {
                        let mut job = egui::text::LayoutJob::simple_singleline(name.clone(), font.clone(), visuals.text_color());
                        job.wrap = egui::text::TextWrapping::truncate_at_width(name_rect.width());
                        f.layout_job(job)
                    });
                    ui.painter().galley(name_rect.left_center() - vec2(0.0, galley.size().y / 2.0), galley, visuals.text_color());
                    let _ = Align2::LEFT_CENTER;
                    let tip_text = if over_button { tip } else if tab.request.is_some() { name } else { "A request that has not been saved".into() };
                    let response = response.on_hover_text(tip_text);
                    if response.clicked() {
                        if over_button {
                            toggled = Some(i);
                        } else if i != self.current {
                            chosen = Some(i);
                        }
                    }
                }
            });
        });
        self.hovered = hovered;
        // Pinning and unpinning leave the request on show as it is.
        if let Some(at) = toggled {
            self.toggle(at);
            return None;
        }
        chosen.map(|to| (self.current, to))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn pins_and_unpins() {
        let mut t = Tabs::new();
        assert_eq!(t.open(Some(1)), 0);
        t.toggle(0);
        assert!(t.tabs[0].pinned);
        // Another request opens in a new tab that is not pinned.
        assert_eq!(t.open(Some(2)), 1);
        assert_eq!(t.open(Some(1)), 0);
        // Unpinning the tab on show takes the place of the other.
        t.toggle(0);
        assert_eq!(t.tabs.len(), 1);
        assert_eq!(t.tabs[0].request, Some(1));
        t.toggle(0);
        t.open(Some(3));
        t.current = 1;
        t.toggle(0);
        assert_eq!(t.tabs.len(), 1);
        assert_eq!(t.tabs[0].request, Some(3));
        t.remove(3);
        assert_eq!(t.tabs[0].request, None);
    }
}
