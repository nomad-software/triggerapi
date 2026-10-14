//! The Requests tab of the sidebar: the saved requests in their folders,
//! with the top level folders under gRPC, REST and Mixed headings by the
//! requests they hold, worked out each time they are drawn and never saved.
//! Beneath the search box are tabs for every folder and for the favourites.

use egui::{pos2, vec2, Align2, Color32, CornerRadius, EventFilter, FontId, Id, Key, Rect, RichText, Sense, TextEdit, Ui};
use triggerapi_core::collection::{Folder, Request, Store, TYPE_GRPC};

use crate::icons::{self, Glyph};
use crate::theme::{Kind, Theme};

/// What is selected in the tree.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash)]
pub enum Sel {
    Folder(u64),
    Request(u64),
}

/// The heading a top level folder is listed under.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Group {
    Grpc,
    Rest,
    Mixed,
}

impl Group {
    fn label(self) -> &'static str {
        match self {
            Group::Grpc => "gRPC",
            Group::Rest => "REST",
            Group::Mixed => "Mixed",
        }
    }
}

/// Returns the heading a top level folder is listed under: the type of its
/// requests, at any depth, if they are all of one type, Mixed if they are
/// not, and none if it holds no requests.
pub fn group_of(folder: &Folder) -> Option<Group> {
    let mut group = None;
    for r in folder.all() {
        let g = if r.kind == TYPE_GRPC { Group::Grpc } else { Group::Rest };
        match group {
            None => group = Some(g),
            Some(existing) if existing != g => return Some(Group::Mixed),
            _ => {}
        }
    }
    group
}

/// Which folders are shown.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum View {
    All,
    Favourites,
}

/// A line of the tree as it is drawn.
enum Line {
    Heading(Group),
    Folder { id: u64, depth: usize, open: bool, children: bool, starrable: bool, favourite: bool, name: String },
    Request { id: u64, depth: usize, grpc: bool, name: String },
}

/// What the user asked for that the window carries out.
#[derive(Default)]
pub struct Output {
    /// A request was chosen, and is to be opened.
    pub open: Option<u64>,
    /// The store was changed, and is to be saved.
    pub save: bool,
    /// Add Request was pressed: the type is to be asked for.
    pub add_request: bool,
    /// The import button was pressed.
    pub import: bool,
    /// Removing this is to be confirmed, with the question to ask.
    pub confirm_remove: Option<(Sel, String)>,
    /// These were removed without asking.
    pub removed: Vec<u64>,
}

pub struct Tree {
    id: Id,
    pub view: View,
    pub search: String,
    pub selected: Option<Sel>,
    /// What is being renamed, with the name as it is being typed.
    renaming: Option<(Sel, String)>,
    rename_focus: bool,
    pub focus_tree: bool,
    pub focus_search: bool,
    scroll_to_selected: bool,
}

impl Tree {
    pub fn new() -> Tree {
        Tree {
            id: Id::new("request tree"),
            view: View::All,
            search: String::new(),
            selected: None,
            renaming: None,
            rename_focus: false,
            focus_tree: false,
            focus_search: false,
            scroll_to_selected: false,
        }
    }

    /// Shows the favourites if there are any, and otherwise every folder.
    pub fn start_view(&mut self, store: &Store) {
        self.view = if store.folders.iter().any(|f| f.favourite) { View::Favourites } else { View::All };
    }

    /// Returns the words searched for, in lower case.
    fn query(&self) -> Vec<String> {
        self.search.to_lowercase().split_whitespace().map(str::to_string).collect()
    }

    /// Reports if a folder or request is shown in the view, whatever the
    /// search: in the favourites only if its top level folder is one.
    fn in_view(&self, store: &Store, sel: Sel) -> bool {
        if self.view == View::All {
            return true;
        }
        let folder_id = match sel {
            Sel::Folder(id) => Some(id),
            Sel::Request(id) => store.folder_of(id).map(|f| f.id),
        };
        let Some(mut id) = folder_id else { return false };
        while let Some(Some(parent)) = store.parent_of(id) {
            id = parent;
        }
        store.folder(id).is_some_and(|f| f.favourite)
    }

    /// Selects a folder or request, showing every folder if it is not among
    /// the favourites being shown.
    pub fn reveal(&mut self, store: &Store, sel: Sel) {
        if !self.in_view(store, sel) {
            self.view = View::All;
        }
        self.selected = Some(sel);
        self.scroll_to_selected = true;
    }

    /// Selects a request, as choosing its tab does. One that is not among
    /// the favourites being shown is shown on the Requests tab.
    pub fn select_request(&mut self, store: &Store, request: Option<u64>) {
        match request {
            Some(id) if store.request(id).is_some() => self.reveal(store, Sel::Request(id)),
            _ => self.selected = None,
        }
    }

    /// Starts renaming what is selected.
    pub fn rename(&mut self, store: &Store) {
        let Some(sel) = self.selected else { return };
        let name = match sel {
            Sel::Folder(id) => store.folder(id).map(|f| f.name.clone()),
            Sel::Request(id) => store.request(id).map(|r| r.name.clone()),
        };
        if let Some(name) = name {
            self.renaming = Some((sel, name));
            self.rename_focus = true;
        }
    }

    /// Returns the folder something is to be added to: the selected folder,
    /// or the folder of the selected request.
    fn target_folder(&self, store: &Store) -> Option<u64> {
        match self.selected? {
            Sel::Folder(id) => Some(id),
            Sel::Request(id) => store.folder_of(id).map(|f| f.id),
        }
    }

    /// Shows something just put in the store, in its place in the order, and
    /// starts renaming it.
    fn added(&mut self, store: &mut Store, sel: Sel) {
        self.search.clear();
        store.sort();
        self.reveal(store, sel);
        self.rename(store);
    }

    /// Adds a folder inside the selected folder, or the folder of the
    /// selected request, or at the top level if nothing is selected.
    pub fn add_folder(&mut self, store: &mut Store) {
        let id = match self.target_folder(store).and_then(|t| store.folder_mut(t)) {
            Some(parent) => {
                parent.collapsed = false;
                parent.add_folder("New Folder")
            }
            None => store.add_folder("New Folder"),
        };
        self.added(store, Sel::Folder(id));
    }

    /// Adds a request to the selected folder, or the folder of the selected
    /// request. With nothing selected a folder is made at the top level to
    /// hold it. It returns the id of the request.
    pub fn add_request(&mut self, store: &mut Store, request: Request) -> u64 {
        let target = self.target_folder(store).filter(|t| store.folder(*t).is_some());
        let folder_id = target.unwrap_or_else(|| store.add_folder("New Folder"));
        let folder = store.folder_mut(folder_id).unwrap();
        folder.collapsed = false;
        let id = folder.add(request);
        self.added(store, Sel::Request(id));
        id
    }

    /// Makes a copy of the selected request beside it, or of the selected
    /// folder and everything in it. It returns the id of a request copied,
    /// which is to be opened.
    pub fn duplicate(&mut self, store: &mut Store) -> Option<u64> {
        match self.selected? {
            Sel::Request(id) => {
                let mut copy = store.request(id)?.duplicate();
                copy.name.push_str(" copy");
                let holder = store.folder_of(id)?.id;
                let new = store.folder_mut(holder)?.add(copy);
                self.added(store, Sel::Request(new));
                Some(new)
            }
            Sel::Folder(id) => {
                let mut copy = store.folder(id)?.duplicate();
                copy.name.push_str(" copy");
                let new = copy.id;
                match store.parent_of(id)? {
                    None => store.folders.push(copy),
                    Some(parent) => store.folder_mut(parent)?.folders.push(copy),
                }
                self.added(store, Sel::Folder(new));
                None
            }
        }
    }

    /// Adds a folder, with everything in it, to the top level, and selects
    /// it.
    pub fn adopt(&mut self, store: &mut Store, mut folder: Folder) {
        store.normalise();
        let id = folder.id;
        if id == 0 {
            folder = folder.duplicate();
        }
        let id = folder.id;
        store.folders.push(folder);
        store.normalise();
        self.search.clear();
        store.sort();
        self.reveal(store, Sel::Folder(id));
    }

    /// Shows what has been added to the store from outside the tree,
    /// keeping a request selected.
    pub fn extended(&mut self, store: &mut Store, selected: Option<u64>) {
        store.normalise();
        self.search.clear();
        store.sort();
        if let Some(id) = selected {
            self.reveal(store, Sel::Request(id));
        }
    }

    /// Returns what is to be selected once something is removed: what is
    /// after it beside it, or else before it, or else its folder.
    fn neighbour(&self, store: &Store, sel: Sel) -> Option<Sel> {
        let (siblings, parent): (Vec<Sel>, Option<u64>) = match sel {
            Sel::Request(id) => {
                let f = store.folder_of(id)?;
                (f.requests.iter().map(|r| Sel::Request(r.id)).collect(), Some(f.id))
            }
            Sel::Folder(id) => match store.parent_of(id)? {
                None => (store.folders.iter().map(|f| Sel::Folder(f.id)).collect(), None),
                Some(p) => (store.folder(p)?.folders.iter().map(|f| Sel::Folder(f.id)).collect(), Some(p)),
            },
        };
        let at = siblings.iter().position(|s| *s == sel)?;
        siblings
            .get(at + 1)
            .or_else(|| at.checked_sub(1).and_then(|i| siblings.get(i)))
            .copied()
            .filter(|s| self.in_view(store, *s))
            .or(parent.map(Sel::Folder))
    }

    /// Removes what is selected once the user has agreed to it, returning
    /// the requests that went with it, and selects what was beside it.
    pub fn remove(&mut self, store: &mut Store, sel: Sel) -> (Vec<u64>, Option<u64>) {
        let next = self.neighbour(store, sel);
        let removed = match sel {
            Sel::Request(id) => store.take_request(id).map(|r| vec![r.id]).unwrap_or_default(),
            Sel::Folder(id) => store.take_folder(id).map(|f| f.all().iter().map(|r| r.id).collect()).unwrap_or_default(),
        };
        self.selected = next;
        let open = match next {
            Some(Sel::Request(id)) => Some(id),
            _ => None,
        };
        (removed, open)
    }

    /// Asks to remove what is selected: a request or a folder that is not
    /// empty is confirmed first, and an empty folder is removed at once.
    fn ask_remove(&mut self, store: &mut Store, out: &mut Output) {
        let Some(sel) = self.selected else { return };
        match sel {
            Sel::Request(id) => {
                if let Some(r) = store.request(id) {
                    out.confirm_remove = Some((sel, format!("Remove the request {:?}?", r.name)));
                }
            }
            Sel::Folder(id) => {
                let Some(f) = store.folder(id) else { return };
                let (folders, requests) = (f.descendants(), f.all().len());
                if folders + requests == 0 {
                    let (removed, open) = self.remove(store, sel);
                    out.removed = removed;
                    out.open = open;
                    out.save = true;
                } else {
                    out.confirm_remove = Some((
                        sel,
                        format!(
                            "Remove the folder {:?} and everything in it? It holds {} and {}.",
                            f.name,
                            plural(folders, "folder"),
                            plural(requests, "request")
                        ),
                    ));
                }
            }
        }
    }

    /// Moves a folder or request dropped on a line: into the folder it is,
    /// or the folder of the request it is, or to the top level for a folder
    /// dropped on a heading or the space beneath.
    fn drop(&mut self, store: &mut Store, dragged: Sel, target: Option<Sel>) -> bool {
        let into = match target {
            Some(Sel::Folder(id)) => Some(id),
            Some(Sel::Request(id)) => store.folder_of(id).map(|f| f.id),
            None => None,
        };
        let moved = match dragged {
            Sel::Request(id) => match into {
                Some(to) if store.folder_of(id).map(|f| f.id) != Some(to) => store.move_request(id, to),
                _ => false,
            },
            Sel::Folder(id) => {
                if store.parent_of(id) == Some(into) {
                    false
                } else {
                    store.move_folder(id, into)
                }
            }
        };
        if moved {
            if let Some(to) = into.and_then(|t| store.folder_mut(t)) {
                to.collapsed = false;
            }
            store.sort();
            self.reveal(store, dragged);
        }
        moved
    }

    /// Lists the lines to draw.
    fn lines(&self, store: &Store) -> Vec<Line> {
        let words = self.query();
        let searching = !words.is_empty();
        let matches = |r: &Request| {
            let name = r.name.to_lowercase();
            words.iter().all(|w| name.contains(w.as_str()))
        };

        fn has_match(f: &Folder, matches: &dyn Fn(&Request) -> bool) -> bool {
            f.requests.iter().any(matches) || f.folders.iter().any(|s| has_match(s, matches))
        }

        fn fill(out: &mut Vec<Line>, f: &Folder, depth: usize, searching: bool, matches: &dyn Fn(&Request) -> bool, starrable: bool) {
            if searching && !has_match(f, matches) {
                return;
            }
            let children = !f.folders.is_empty() || !f.requests.is_empty();
            let open = searching || !f.collapsed;
            out.push(Line::Folder { id: f.id, depth, open, children, starrable, favourite: f.favourite, name: f.name.clone() });
            if !open {
                return;
            }
            for sub in &f.folders {
                fill(out, sub, depth + 1, searching, matches, false);
            }
            for r in &f.requests {
                if !searching || matches(r) {
                    out.push(Line::Request { id: r.id, depth: depth + 1, grpc: r.kind == TYPE_GRPC, name: r.name.clone() });
                }
            }
        }

        let top: Vec<&Folder> = store
            .folders
            .iter()
            .filter(|f| self.view == View::All || f.favourite)
            .collect();
        let mut out = Vec::new();
        for group in [Group::Grpc, Group::Rest, Group::Mixed] {
            let members: Vec<&&Folder> = top.iter().filter(|f| group_of(f) == Some(group)).collect();
            let visible = members.iter().any(|f| !searching || has_match(f, &matches));
            if members.is_empty() || !visible {
                continue;
            }
            out.push(Line::Heading(group));
            for f in members {
                fill(&mut out, f, 1, searching, &matches, true);
            }
        }
        for f in top.iter().filter(|f| group_of(f).is_none()) {
            fill(&mut out, f, 0, searching, &matches, false);
        }
        out
    }

    /// Draws the tab: the search, the views, the tree and its buttons.
    pub fn ui(&mut self, ui: &mut Ui, store: &mut Store, theme: &Theme) -> Output {
        let mut out = Output::default();
        let requests_colour = theme.colour(Kind::Requests);

        // The buttons are along the bottom.
        egui::TopBottomPanel::bottom("request tree buttons").frame(egui::Frame::NONE).show_inside(ui, |ui| {
            ui.add_space(4.0);
            ui.horizontal(|ui| {
                if icons::button(ui, Glyph::FolderPlus, requests_colour, "Add a folder", true).clicked() {
                    self.add_folder(store);
                    out.save = true;
                }
                if icons::button(ui, Glyph::PagePlus, requests_colour, "Add a request", true).clicked() {
                    out.add_request = true;
                }
                let some = self.selected.is_some();
                if icons::button(ui, Glyph::Copy, requests_colour, "Duplicate the selected folder or request", some).clicked() {
                    out.open = self.duplicate(store);
                    out.save = true;
                }
                if icons::button(ui, Glyph::Letters, requests_colour, "Rename the selected folder or request", some).clicked() {
                    self.rename(store);
                }
                if icons::button(ui, Glyph::Bin, theme.colour(Kind::Remove), "Remove the selected folder or request", some).clicked() {
                    self.ask_remove(store, &mut out);
                }
                ui.with_layout(egui::Layout::right_to_left(egui::Align::Center), |ui| {
                    if icons::button(ui, Glyph::Import, requests_colour, "Import a folder of requests from an OpenAPI document or a Postman collection", true).clicked() {
                        out.import = true;
                    }
                });
            });
        });

        // The search.
        let search = ui.add(
            TextEdit::singleline(&mut self.search)
                .hint_text("Search requests")
                .desired_width(f32::INFINITY)
                .id(self.id.with("search")),
        );
        if self.focus_search {
            search.request_focus();
            let mut state = TextEdit::load_state(ui.ctx(), search.id).unwrap_or_default();
            let all = egui::text::CCursorRange::two(egui::text::CCursor::new(0), egui::text::CCursor::new(self.search.chars().count()));
            state.cursor.set_char_range(Some(all));
            state.store(ui.ctx(), search.id);
            self.focus_search = false;
        }
        if search.lost_focus() && ui.input(|i| i.key_pressed(Key::Enter)) {
            self.browse(store);
        }

        // The views.
        ui.horizontal(|ui| {
            for (view, label, glyph) in [(View::All, "Requests", Some(Glyph::Folder)), (View::Favourites, "Favourites", None)] {
                let selected = self.view == view;
                let response = ui
                    .scope(|ui| {
                        ui.horizontal(|ui| {
                            let (rect, _) = ui.allocate_exact_size(vec2(14.0, 14.0), Sense::hover());
                            match glyph {
                                Some(g) => icons::paint(ui.painter(), rect, requests_colour, g),
                                None => icons::star(ui.painter(), rect, theme.colour(Kind::Favourite), true),
                            }
                            ui.selectable_label(selected, label)
                        })
                        .inner
                    })
                    .inner;
                if response.clicked() && !selected {
                    self.view = view;
                }
            }
        });
        ui.separator();

        let lines = self.lines(store);
        let tree_id = self.id.with("tree");
        let focused = ui.memory(|m| m.has_focus(tree_id));
        if focused {
            ui.memory_mut(|m| {
                m.set_focus_lock_filter(tree_id, EventFilter { horizontal_arrows: true, vertical_arrows: true, tab: false, escape: false })
            });
            self.keys(ui, store, &lines, &mut out);
        }
        if self.focus_tree {
            ui.memory_mut(|m| m.request_focus(tree_id));
            self.focus_tree = false;
        }

        let row_h = 22.0;
        let mut dropped: Option<(Sel, Option<Sel>)> = None;
        egui::ScrollArea::vertical().auto_shrink([false, false]).show(ui, |ui| {
            for line in &lines {
                let (rect, response) = ui.allocate_exact_size(vec2(ui.available_width(), row_h), Sense::click_and_drag());
                let painter = ui.painter_at(rect);
                let text_colour = ui.visuals().text_color();
                let indent = |depth: usize| rect.left() + 4.0 + depth as f32 * 16.0;
                match line {
                    Line::Heading(group) => {
                        let x = indent(0);
                        let icon = Rect::from_min_size(pos2(x, rect.center().y - 8.0), vec2(16.0, 16.0));
                        match group {
                            Group::Grpc => icons::paint(&painter, icon, theme.colour(Kind::Grpc), Glyph::Arrows),
                            Group::Rest => icons::paint(&painter, icon, theme.colour(Kind::Rest), Glyph::Braces),
                            Group::Mixed => icons::paint(&painter, icon, requests_colour, Glyph::Folder),
                        }
                        painter.text(pos2(x + 22.0, rect.center().y), Align2::LEFT_CENTER, group.label(), FontId::proportional(14.0), text_colour);
                        // Something dropped on a heading goes to the top
                        // level.
                        if let Some(payload) = response.dnd_release_payload::<Sel>() {
                            dropped = Some((*payload, None));
                        }
                    }
                    Line::Folder { id, depth, open, children, starrable, favourite, name } => {
                        let sel = Sel::Folder(*id);
                        self.row_background(ui, &response, rect, sel);
                        let x = indent(*depth);
                        if *children {
                            let arrow = if *open { "⏷" } else { "⏵" };
                            painter.text(pos2(x + 5.0, rect.center().y), Align2::CENTER_CENTER, arrow, FontId::proportional(11.0), text_colour);
                        }
                        let icon = Rect::from_min_size(pos2(x + 14.0, rect.center().y - 8.0), vec2(16.0, 16.0));
                        icons::paint(&painter, icon, requests_colour, Glyph::Folder);
                        let name_rect = Rect::from_min_max(pos2(x + 36.0, rect.top()), pos2(rect.right() - 24.0, rect.bottom()));
                        if !self.name_editor(ui, store, sel, name_rect, &mut out) {
                            painter.text(name_rect.left_center(), Align2::LEFT_CENTER, name, FontId::proportional(14.0), text_colour);
                        }
                        // The star shows while the pointer is over a folder
                        // under a heading.
                        let star = Rect::from_center_size(pos2(rect.right() - 12.0, rect.center().y), vec2(16.0, 16.0));
                        let over_star = ui.input(|i| i.pointer.hover_pos()).is_some_and(|p| star.expand(2.0).contains(p));
                        if *starrable && response.hovered() {
                            icons::star(&painter, star, theme.colour(Kind::Favourite), *favourite);
                        }
                        if response.clicked() {
                            if *starrable && over_star {
                                if let Some(f) = store.folder_mut(*id) {
                                    f.favourite = !f.favourite;
                                    out.save = true;
                                }
                            } else if *children && ui.input(|i| i.pointer.interact_pos()).is_some_and(|p| p.x < x + 12.0) {
                                self.set_open(store, *id, !*open, &mut out);
                            } else {
                                self.selected = Some(sel);
                                ui.memory_mut(|m| m.request_focus(tree_id));
                            }
                        }
                        if response.double_clicked() && *children && !over_star {
                            self.set_open(store, *id, !*open, &mut out);
                        }
                        self.drag(&response, sel, &mut dropped);
                    }
                    Line::Request { id, depth, grpc, name } => {
                        let sel = Sel::Request(*id);
                        self.row_background(ui, &response, rect, sel);
                        let x = indent(*depth);
                        let icon = Rect::from_min_size(pos2(x + 14.0, rect.center().y - 8.0), vec2(16.0, 16.0));
                        if *grpc {
                            icons::paint(&painter, icon, theme.colour(Kind::Grpc), Glyph::Arrows);
                        } else {
                            icons::paint(&painter, icon, theme.colour(Kind::Rest), Glyph::Braces);
                        }
                        let name_rect = Rect::from_min_max(pos2(x + 36.0, rect.top()), pos2(rect.right() - 4.0, rect.bottom()));
                        if !self.name_editor(ui, store, sel, name_rect, &mut out) {
                            painter.text(name_rect.left_center(), Align2::LEFT_CENTER, name, FontId::proportional(14.0), text_colour);
                        }
                        if response.clicked() {
                            self.selected = Some(sel);
                            out.open = Some(*id);
                            ui.memory_mut(|m| m.request_focus(tree_id));
                        }
                        self.drag(&response, sel, &mut dropped);
                    }
                }
                if self.scroll_to_selected && self.selected.is_some() && line_sel(line) == self.selected {
                    response.scroll_to_me(None);
                    self.scroll_to_selected = false;
                }
            }
            // The space beneath the lines: clicking it leaves nothing
            // selected, which is how a top level folder is added, and
            // something dropped there goes to the top level.
            let rest = ui.allocate_response(ui.available_size().max(vec2(0.0, 40.0)), Sense::click());
            if rest.clicked() {
                self.selected = None;
                self.renaming = None;
            }
            if let Some(payload) = rest.dnd_release_payload::<Sel>() {
                dropped = Some((*payload, None));
            }
        });
        self.scroll_to_selected = false;

        if let Some((dragged, target)) = dropped {
            if Some(dragged) != target && self.drop(store, dragged, target) {
                out.save = true;
            }
        }
        out
    }

    fn row_background(&self, ui: &Ui, response: &egui::Response, rect: Rect, sel: Sel) {
        let visuals = ui.visuals();
        if self.selected == Some(sel) {
            ui.painter().rect_filled(rect, CornerRadius::same(3), visuals.selection.bg_fill.gamma_multiply(0.7));
        } else if response.hovered() {
            ui.painter().rect_filled(rect, CornerRadius::same(3), visuals.widgets.hovered.weak_bg_fill);
        }
        if let Some(payload) = response.dnd_hover_payload::<Sel>() {
            if *payload != sel {
                ui.painter().rect_stroke(rect, CornerRadius::same(3), (1.5, visuals.selection.stroke.color), egui::StrokeKind::Inside);
            }
        }
    }

    fn drag(&self, response: &egui::Response, sel: Sel, dropped: &mut Option<(Sel, Option<Sel>)>) {
        if response.drag_started() && self.renaming.is_none() {
            response.dnd_set_drag_payload(sel);
        }
        if let Some(payload) = response.dnd_release_payload::<Sel>() {
            *dropped = Some((*payload, Some(sel)));
        }
    }

    /// Opens or closes a folder, which is recorded with it unless a search
    /// is on.
    fn set_open(&mut self, store: &mut Store, id: u64, open: bool, out: &mut Output) {
        if !self.query().is_empty() {
            return;
        }
        if let Some(f) = store.folder_mut(id) {
            if f.collapsed == open {
                f.collapsed = !open;
                out.save = true;
            }
        }
    }

    /// Draws the field a name is renamed in, if it is being renamed, and
    /// reports if it was drawn. A name emptied is given back the one it had.
    fn name_editor(&mut self, ui: &mut Ui, store: &mut Store, sel: Sel, rect: Rect, out: &mut Output) -> bool {
        let Some((renaming, text)) = &mut self.renaming else { return false };
        if *renaming != sel {
            return false;
        }
        let id = self.id.with(("rename", sel));
        let response = ui.put(rect, TextEdit::singleline(text).id(id).margin(vec2(2.0, 1.0)));
        if self.rename_focus {
            response.request_focus();
            let mut state = TextEdit::load_state(ui.ctx(), id).unwrap_or_default();
            let all = egui::text::CCursorRange::two(egui::text::CCursor::new(0), egui::text::CCursor::new(text.chars().count()));
            state.cursor.set_char_range(Some(all));
            state.store(ui.ctx(), id);
            self.rename_focus = false;
            return true;
        }
        if ui.input(|i| i.key_pressed(Key::Escape)) && (response.has_focus() || response.lost_focus()) {
            self.renaming = None;
            return true;
        }
        if response.lost_focus() || !response.has_focus() && !response.gained_focus() {
            let name = text.clone();
            self.renaming = None;
            if !name.trim().is_empty() {
                match sel {
                    Sel::Folder(id) => {
                        if let Some(f) = store.folder_mut(id) {
                            if f.name != name {
                                f.name = name;
                                out.save = true;
                            }
                        }
                    }
                    Sel::Request(id) => {
                        if let Some(r) = store.request_mut(id) {
                            if r.name != name {
                                r.name = name;
                                out.save = true;
                            }
                        }
                    }
                }
                store.sort();
                self.scroll_to_selected = true;
            }
            ui.memory_mut(|m| m.request_focus(self.id.with("tree")));
        }
        true
    }

    /// Moves through the tree with the keyboard without opening what is
    /// passed over: up and down, left to close a folder and right to open
    /// one. Return opens the selected request, or opens or closes the
    /// selected folder, and F2 renames.
    fn keys(&mut self, ui: &mut Ui, store: &mut Store, lines: &[Line], out: &mut Output) {
        if self.renaming.is_some() {
            return;
        }
        let rows: Vec<Sel> = lines.iter().filter_map(line_sel).collect();
        let at = self.selected.and_then(|s| rows.iter().position(|r| *r == s));
        let pressed = |k: Key| ui.input_mut(|i| i.consume_key(egui::Modifiers::NONE, k));
        if pressed(Key::ArrowDown) {
            let next = at.map(|a| (a + 1).min(rows.len().saturating_sub(1))).unwrap_or(0);
            if let Some(s) = rows.get(next) {
                self.selected = Some(*s);
                self.scroll_to_selected = true;
            }
        }
        if pressed(Key::ArrowUp) {
            let next = at.map(|a| a.saturating_sub(1)).unwrap_or(0);
            if let Some(s) = rows.get(next) {
                self.selected = Some(*s);
                self.scroll_to_selected = true;
            }
        }
        let folder_line = |id: u64| {
            lines.iter().find_map(|l| match l {
                Line::Folder { id: f, open, children, .. } if *f == id => Some((*open, *children)),
                _ => None,
            })
        };
        if pressed(Key::ArrowRight) {
            if let Some(Sel::Folder(id)) = self.selected {
                if let Some((false, true)) = folder_line(id) {
                    self.set_open(store, id, true, out);
                }
            }
        }
        if pressed(Key::ArrowLeft) {
            match self.selected {
                Some(Sel::Folder(id)) if matches!(folder_line(id), Some((true, true))) => self.set_open(store, id, false, out),
                Some(Sel::Folder(id)) => {
                    if let Some(Some(parent)) = store.parent_of(id) {
                        self.selected = Some(Sel::Folder(parent));
                    }
                }
                Some(Sel::Request(id)) => {
                    if let Some(f) = store.folder_of(id) {
                        self.selected = Some(Sel::Folder(f.id));
                    }
                }
                None => {}
            }
        }
        if pressed(Key::Enter) {
            match self.selected {
                Some(Sel::Request(id)) => out.open = Some(id),
                Some(Sel::Folder(id)) => {
                    if let Some((open, true)) = folder_line(id) {
                        self.set_open(store, id, !open, out);
                    }
                }
                None => {}
            }
        }
        if pressed(Key::F2) {
            self.rename(store);
        }
    }

    /// Puts the keyboard in the tree: at the first request found if there is
    /// a search, and otherwise where the selection is, or at the first
    /// folder. Nothing is opened by it.
    pub fn browse(&mut self, store: &Store) {
        let lines = self.lines(store);
        let first_request = lines.iter().find_map(|l| match l {
            Line::Request { id, .. } => Some(Sel::Request(*id)),
            _ => None,
        });
        let first = lines.iter().find_map(line_sel);
        let visible = |s: Sel| lines.iter().any(|l| line_sel(l) == Some(s));
        if !self.query().is_empty() {
            self.selected = first_request.or(first);
        } else if !self.selected.is_some_and(visible) {
            self.selected = first;
        }
        self.scroll_to_selected = true;
        self.focus_tree = true;
    }
}

fn line_sel(line: &Line) -> Option<Sel> {
    match line {
        Line::Heading(_) => None,
        Line::Folder { id, .. } => Some(Sel::Folder(*id)),
        Line::Request { id, .. } => Some(Sel::Request(*id)),
    }
}

/// Returns a count of things, such as "1 folder" or "3 folders".
pub fn plural(n: usize, thing: &str) -> String {
    if n == 1 {
        format!("1 {thing}")
    } else {
        format!("{n} {thing}s")
    }
}

#[allow(dead_code)]
fn unused(_: Color32, _: RichText) {}

#[cfg(test)]
mod tests {
    use super::*;
    use triggerapi_core::{rest, rpc};

    fn store() -> Store {
        let mut s = Store::default();
        let a = s.add_folder("Hotel");
        s.folder_mut(a).unwrap().add(Request::new_rest("Ping", rest::Request::default()));
        let b = s.add_folder("Service");
        s.folder_mut(b).unwrap().add(Request::new_grpc("Get", rpc::Request::default()));
        let m = s.add_folder("Mixed");
        s.folder_mut(m).unwrap().add(Request::new_grpc("G", rpc::Request::default()));
        s.folder_mut(m).unwrap().add(Request::new_rest("R", rest::Request::default()));
        s.add_folder("Empty");
        s.sort();
        s
    }

    fn names(lines: &[Line]) -> Vec<String> {
        lines
            .iter()
            .map(|l| match l {
                Line::Heading(g) => format!("# {}", g.label()),
                Line::Folder { name, .. } => format!("{name}/"),
                Line::Request { name, .. } => name.clone(),
            })
            .collect()
    }

    #[test]
    fn groups_under_headings() {
        let s = store();
        let t = Tree::new();
        assert_eq!(
            names(&t.lines(&s)),
            vec!["# gRPC", "Service/", "Get", "# REST", "Hotel/", "Ping", "# Mixed", "Mixed/", "G", "R", "Empty/"]
        );
    }

    #[test]
    fn searches_request_names_only() {
        let s = store();
        let mut t = Tree::new();
        t.search = "hotel".into();
        assert!(t.lines(&s).is_empty());
        t.search = "PING".into();
        assert_eq!(names(&t.lines(&s)), vec!["# REST", "Hotel/", "Ping"]);
    }

    #[test]
    fn favourites_and_adding() {
        let mut s = store();
        let mut t = Tree::new();
        t.start_view(&s);
        assert_eq!(t.view, View::All);
        s.folders.iter_mut().find(|f| f.name == "Hotel").unwrap().favourite = true;
        t.start_view(&s);
        assert_eq!(t.view, View::Favourites);
        assert_eq!(names(&t.lines(&s)), vec!["# REST", "Hotel/", "Ping"]);

        // With nothing selected a request gets a new folder of its own, and
        // the tree goes back to every folder so that it can be seen.
        let id = t.add_request(&mut s, Request::new_rest("New Request", rest::Request::default()));
        assert_eq!(s.folder_of(id).unwrap().name, "New Folder");
        assert_eq!(t.view, View::All);
        assert_eq!(s.folders.len(), 5);
    }
}
