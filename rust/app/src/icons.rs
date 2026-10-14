//! Icons drawn with the painter: a symbol in white on a rounded square of a
//! colour, as the Go program draws them. There are no image files.

use egui::epaint::{CubicBezierShape, PathShape, PathStroke, QuadraticBezierShape};
use egui::{pos2, vec2, Color32, CornerRadius, FontId, Painter, Pos2, Rect, Response, Sense, Shape, Stroke, StrokeKind, Ui};

/// The symbols of the icons.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Glyph {
    Globe,
    Folder,
    Document,
    Plane,
    Prompt,
    Eye,
    Magnifier,
    Tick,
    Cross,
    Disk,
    Plus,
    FolderPlus,
    PagePlus,
    Copy,
    Import,
    Pencil,
    Letters,
    Bin,
    Pin,
    Monitor,
    Braces,
    Arrows,
    Revolver,
}

/// Maps the 128 by 128 box the symbols are drawn in to a rectangle.
struct Space {
    rect: Rect,
}

impl Space {
    fn p(&self, x: f32, y: f32) -> Pos2 {
        let s = self.rect.width() / 128.0;
        pos2(self.rect.left() + x * s, self.rect.top() + y * s)
    }
    fn w(&self, width: f32) -> f32 {
        width * self.rect.width() / 128.0
    }
}

/// Draws an icon in a rectangle: the symbol in white on a rounded square of
/// the colour.
pub fn paint(painter: &Painter, rect: Rect, colour: Color32, glyph: Glyph) {
    let sp = Space { rect };
    let corner = sp.w(24.0);
    let inner = Rect::from_min_max(sp.p(4.0, 4.0), sp.p(124.0, 124.0));
    painter.rect_filled(inner, CornerRadius::same(corner.round() as u8), colour);
    symbol(painter, &sp, glyph, Color32::WHITE);
}

fn symbol(painter: &Painter, sp: &Space, glyph: Glyph, white: Color32) {
    let stroke = Stroke::new(sp.w(9.0).max(1.0), white);
    let line = |points: &[(f32, f32)]| {
        let pts: Vec<Pos2> = points.iter().map(|&(x, y)| sp.p(x, y)).collect();
        painter.add(Shape::Path(PathShape::line(pts, PathStroke::new(stroke.width, stroke.color))));
        for &(x, y) in points {
            // Round joins and caps.
            painter.circle_filled(sp.p(x, y), stroke.width / 2.0, stroke.color);
        }
    };
    let closed = |points: &[(f32, f32)]| {
        let mut pts: Vec<(f32, f32)> = points.to_vec();
        pts.push(points[0]);
        line(&pts);
    };
    let ellipse = |x: f32, y: f32, w: f32, h: f32| {
        let (cx, cy, rx, ry) = (x + w / 2.0, y + h / 2.0, w / 2.0, h / 2.0);
        let pts: Vec<Pos2> = (0..48)
            .map(|i| {
                let a = i as f32 / 48.0 * std::f32::consts::TAU;
                sp.p(cx + rx * a.cos(), cy + ry * a.sin())
            })
            .collect();
        painter.add(Shape::Path(PathShape::closed_line(pts, PathStroke::new(stroke.width, stroke.color))));
    };
    let quad = |a: (f32, f32), c: (f32, f32), b: (f32, f32)| {
        painter.add(QuadraticBezierShape::from_points_stroke(
            [sp.p(a.0, a.1), sp.p(c.0, c.1), sp.p(b.0, b.1)],
            false,
            Color32::TRANSPARENT,
            PathStroke::new(stroke.width, stroke.color),
        ));
    };
    let rounded = |x: f32, y: f32, w: f32, h: f32, r: f32| {
        painter.rect_stroke(
            Rect::from_min_max(sp.p(x, y), sp.p(x + w, y + h)),
            CornerRadius::same(sp.w(r).round() as u8),
            stroke,
            StrokeKind::Middle,
        );
    };
    let plus = |x: f32, y: f32, arm: f32| {
        line(&[(x - arm, y), (x + arm, y)]);
        line(&[(x, y - arm), (x, y + arm)]);
    };
    let text = |s: &str, size: f32, centre: (f32, f32)| {
        painter.text(sp.p(centre.0, centre.1), egui::Align2::CENTER_CENTER, s, FontId::proportional(sp.w(size)), white);
    };

    match glyph {
        Glyph::Globe => {
            ellipse(28.0, 28.0, 72.0, 72.0);
            ellipse(49.0, 28.0, 30.0, 72.0);
            line(&[(28.0, 64.0), (100.0, 64.0)]);
        }
        Glyph::Folder => folder(&closed),
        Glyph::Document => {
            closed(&[(38.0, 28.0), (72.0, 28.0), (90.0, 46.0), (90.0, 100.0), (38.0, 100.0)]);
            line(&[(72.0, 28.0), (72.0, 46.0), (90.0, 46.0)]);
            line(&[(51.0, 64.0), (77.0, 64.0)]);
            line(&[(51.0, 82.0), (77.0, 82.0)]);
        }
        Glyph::Plane => {
            closed(&[(26.0, 62.0), (102.0, 30.0), (78.0, 100.0), (60.0, 74.0)]);
            line(&[(60.0, 74.0), (102.0, 30.0)]);
        }
        Glyph::Prompt => {
            line(&[(30.0, 42.0), (56.0, 64.0), (30.0, 86.0)]);
            line(&[(66.0, 88.0), (98.0, 88.0)]);
        }
        Glyph::Eye => {
            quad((24.0, 64.0), (64.0, 20.0), (104.0, 64.0));
            quad((104.0, 64.0), (64.0, 108.0), (24.0, 64.0));
            painter.circle_filled(sp.p(64.0, 64.0), sp.w(10.0) + stroke.width / 2.0, white);
        }
        Glyph::Magnifier => {
            ellipse(28.0, 28.0, 50.0, 50.0);
            line(&[(72.0, 72.0), (100.0, 100.0)]);
        }
        Glyph::Tick => {
            let thick = Stroke::new(sp.w(14.0), white);
            painter.add(Shape::line(vec![sp.p(32.0, 68.0), sp.p(54.0, 90.0), sp.p(98.0, 40.0)], thick));
        }
        Glyph::Cross => {
            let thick = Stroke::new(sp.w(14.0), white);
            painter.line_segment([sp.p(38.0, 38.0), sp.p(90.0, 90.0)], thick);
            painter.line_segment([sp.p(90.0, 38.0), sp.p(38.0, 90.0)], thick);
        }
        Glyph::Disk => {
            closed(&[(30.0, 30.0), (84.0, 30.0), (98.0, 44.0), (98.0, 98.0), (30.0, 98.0)]);
            line(&[(46.0, 30.0), (46.0, 50.0), (74.0, 50.0), (74.0, 30.0)]);
            line(&[(46.0, 98.0), (46.0, 72.0), (82.0, 72.0), (82.0, 98.0)]);
        }
        Glyph::Plus => plus(64.0, 64.0, 30.0),
        Glyph::FolderPlus => {
            folder(&closed);
            plus(64.0, 72.0, 11.0);
        }
        Glyph::PagePlus => {
            closed(&[(36.0, 26.0), (72.0, 26.0), (92.0, 46.0), (92.0, 102.0), (36.0, 102.0)]);
            line(&[(72.0, 26.0), (72.0, 46.0), (92.0, 46.0)]);
            plus(64.0, 74.0, 12.0);
        }
        Glyph::Copy => {
            rounded(46.0, 44.0, 54.0, 58.0, 6.0);
            line(&[(30.0, 82.0), (30.0, 32.0)]);
            quad((30.0, 32.0), (30.0, 26.0), (36.0, 26.0));
            line(&[(36.0, 26.0), (82.0, 26.0)]);
        }
        Glyph::Import => {
            line(&[(76.0, 44.0), (76.0, 28.0), (46.0, 28.0), (28.0, 46.0), (28.0, 100.0), (76.0, 100.0), (76.0, 84.0)]);
            line(&[(46.0, 28.0), (46.0, 46.0), (28.0, 46.0)]);
            line(&[(104.0, 64.0), (52.0, 64.0)]);
            line(&[(68.0, 48.0), (52.0, 64.0), (68.0, 80.0)]);
        }
        Glyph::Pencil => {
            closed(&[(30.0, 98.0), (36.0, 74.0), (80.0, 30.0), (98.0, 48.0), (54.0, 92.0)]);
            line(&[(68.0, 42.0), (86.0, 60.0)]);
        }
        Glyph::Letters => {
            text("Aa", 56.0, (54.0, 61.0));
            line(&[(104.0, 34.0), (104.0, 94.0)]);
        }
        Glyph::Bin => {
            line(&[(30.0, 42.0), (98.0, 42.0)]);
            line(&[(52.0, 42.0), (52.0, 28.0), (76.0, 28.0), (76.0, 42.0)]);
            line(&[(40.0, 42.0), (46.0, 102.0), (82.0, 102.0), (88.0, 42.0)]);
            line(&[(56.0, 58.0), (57.0, 86.0)]);
            line(&[(72.0, 58.0), (71.0, 86.0)]);
        }
        Glyph::Pin => {
            let thick = Stroke::new(sp.w(12.0), white);
            for (a, b) in [((44.0, 26.0), (84.0, 26.0)), ((52.0, 26.0), (50.0, 62.0)), ((76.0, 26.0), (78.0, 62.0)), ((34.0, 64.0), (94.0, 64.0)), ((64.0, 64.0), (64.0, 104.0))] {
                painter.line_segment([sp.p(a.0, a.1), sp.p(b.0, b.1)], thick);
            }
        }
        Glyph::Monitor => {
            rounded(22.0, 28.0, 84.0, 54.0, 6.0);
            line(&[(64.0, 82.0), (64.0, 100.0)]);
            line(&[(44.0, 100.0), (84.0, 100.0)]);
        }
        Glyph::Braces => text("{ }", 64.0, (64.0, 60.0)),
        Glyph::Arrows => {
            line(&[(34.0, 48.0), (94.0, 48.0)]);
            line(&[(78.0, 32.0), (94.0, 48.0), (78.0, 64.0)]);
            line(&[(94.0, 82.0), (34.0, 82.0)]);
            line(&[(50.0, 66.0), (34.0, 82.0), (50.0, 98.0)]);
        }
        Glyph::Revolver => revolver(painter, sp, white),
    }
}

fn folder(closed: &dyn Fn(&[(f32, f32)])) {
    closed(&[(28.0, 40.0), (54.0, 40.0), (64.0, 52.0), (100.0, 52.0), (100.0, 92.0), (28.0, 92.0)]);
}

/// A revolver seen from the side, pointing right: the program is named after
/// its trigger.
fn revolver(painter: &Painter, sp: &Space, white: Color32) {
    let rect = |x: f32, y: f32, w: f32, h: f32, r: f32| {
        painter.rect_filled(Rect::from_min_max(sp.p(x, y), sp.p(x + w, y + h)), CornerRadius::same(sp.w(r).round() as u8), white);
    };
    rect(72.0, 30.0, 44.0, 14.0, 3.0);
    rect(107.0, 23.0, 5.0, 9.0, 0.0);
    rect(78.0, 47.0, 24.0, 5.0, 2.0);
    rect(32.0, 30.0, 44.0, 5.0, 0.0);
    rect(34.0, 67.0, 44.0, 5.0, 0.0);
    rect(30.0, 30.0, 9.0, 42.0, 0.0);
    rect(71.0, 30.0, 7.0, 42.0, 0.0);
    rect(42.0, 38.0, 26.0, 26.0, 5.0);
    painter.add(Shape::convex_polygon(
        vec![sp.p(36.0, 31.0), sp.p(27.0, 19.0), sp.p(21.0, 24.0), sp.p(28.0, 33.0)],
        white,
        Stroke::NONE,
    ));
    // The grip, curving down and back from the frame.
    let grip = [
        sp.p(30.0, 42.0),
        sp.p(47.0, 70.0),
        sp.p(41.0, 86.0),
        sp.p(42.0, 100.0),
        sp.p(40.0, 107.0),
        sp.p(34.0, 108.0),
        sp.p(22.0, 108.0),
        sp.p(16.0, 106.0),
        sp.p(15.0, 100.0),
        sp.p(20.0, 66.0),
    ];
    painter.add(Shape::convex_polygon(grip.to_vec(), white, Stroke::NONE));
    let pen = Stroke::new(sp.w(6.0), white);
    painter.add(Shape::line(vec![sp.p(45.0, 90.0), sp.p(62.0, 90.0)], pen));
    painter.add(CubicBezierShape::from_points_stroke(
        [sp.p(62.0, 90.0), sp.p(71.0, 90.0), sp.p(75.0, 84.0), sp.p(75.0, 73.0)],
        false,
        Color32::TRANSPARENT,
        pen,
    ));
    painter.add(QuadraticBezierShape::from_points_stroke([sp.p(60.0, 73.0), sp.p(63.0, 79.0), sp.p(59.0, 83.0)], false, Color32::TRANSPARENT, pen));
}

/// Draws a star of the colour of favourites: filled for a favourite, and in
/// outline for something that is not one but can be made one.
pub fn star(painter: &Painter, rect: Rect, colour: Color32, filled: bool) {
    let sp = Space { rect };
    let points: Vec<Pos2> = (0..10)
        .map(|i| {
            let radius = if i % 2 == 1 { 24.0 } else { 56.0 };
            let angle = std::f32::consts::PI / 5.0 * i as f32 - std::f32::consts::FRAC_PI_2;
            sp.p(64.0 + radius * angle.cos(), 68.0 + radius * angle.sin())
        })
        .collect();
    let stroke = PathStroke::new(sp.w(10.0), colour);
    if filled {
        // A star is not convex, so it is filled as a fan of triangles from
        // its middle.
        let centre = sp.p(64.0, 68.0);
        let mut mesh = egui::Mesh::default();
        mesh.colored_vertex(centre, colour);
        for p in &points {
            mesh.colored_vertex(*p, colour);
        }
        for i in 0..10u32 {
            mesh.add_triangle(0, 1 + i, 1 + (i + 1) % 10);
        }
        painter.add(Shape::mesh(mesh));
    }
    painter.add(Shape::Path(PathShape::closed_line(points, stroke)));
}

/// Draws a tick or a cross on a rounded square, which say how capturing a
/// value went.
pub fn mark(painter: &Painter, rect: Rect, colour: Color32, good: bool) {
    paint(painter, rect, colour, if good { Glyph::Tick } else { Glyph::Cross });
}

/// The size icons are shown at in lists and buttons.
pub const SIZE: f32 = 18.0;

/// A button that is an icon, named in its tooltip. It is drawn faded when it
/// cannot be used.
pub fn button(ui: &mut Ui, glyph: Glyph, colour: Color32, tip: &str, enabled: bool) -> Response {
    let size = vec2(SIZE + 8.0, SIZE + 8.0);
    let (rect, response) = ui.allocate_exact_size(size, if enabled { Sense::click() } else { Sense::hover() });
    if ui.is_rect_visible(rect) {
        if enabled && response.hovered() {
            ui.painter().rect_filled(rect, CornerRadius::same(4), ui.visuals().widgets.hovered.weak_bg_fill);
        }
        let colour = if enabled { colour } else { colour.gamma_multiply(0.35) };
        paint(ui.painter(), Rect::from_center_size(rect.center(), vec2(SIZE, SIZE)), colour, glyph);
    }
    let response = response.on_hover_text(tip);
    if enabled {
        response.on_hover_cursor(egui::CursorIcon::PointingHand)
    } else {
        response
    }
}

/// A button that is an icon and stays down while it is on.
pub fn toggle(ui: &mut Ui, on: &mut bool, glyph: Glyph, colour: Color32, tip: &str) -> Response {
    let size = vec2(SIZE + 8.0, SIZE + 8.0);
    let (rect, mut response) = ui.allocate_exact_size(size, Sense::click());
    if response.clicked() {
        *on = !*on;
        response.mark_changed();
    }
    if ui.is_rect_visible(rect) {
        let visuals = ui.visuals();
        if *on {
            ui.painter().rect_filled(rect, CornerRadius::same(4), visuals.selection.bg_fill.gamma_multiply(0.6));
        } else if response.hovered() {
            ui.painter().rect_filled(rect, CornerRadius::same(4), visuals.widgets.hovered.weak_bg_fill);
        }
        paint(ui.painter(), Rect::from_center_size(rect.center(), vec2(SIZE, SIZE)), colour, glyph);
    }
    response.on_hover_text(tip)
}

/// Returns the program's icon as an image, for the window.
pub fn app_icon() -> egui::IconData {
    eframe::icon_data::from_png_bytes(include_bytes!("../../../packaging/triggerapi.png")).unwrap_or_default()
}
