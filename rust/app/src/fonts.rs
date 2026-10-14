//! The font code is shown in, which is one of those installed, by the name
//! of its family, as the settings give it.

use std::sync::{Arc, OnceLock};

use egui::{FontData, FontDefinitions, FontFamily, FontId};

/// The family egui knows the code font by.
pub const CODE: &str = "code";

fn database() -> &'static fontdb::Database {
    static DB: OnceLock<fontdb::Database> = OnceLock::new();
    DB.get_or_init(|| {
        let mut db = fontdb::Database::new();
        db.load_system_fonts();
        db
    })
}

/// Returns the families of the fonts that are installed, in order.
pub fn families() -> Vec<String> {
    let mut names: Vec<String> = database().faces().filter_map(|f| f.families.first().map(|(n, _)| n.clone())).collect();
    names.sort_by_key(|n| n.to_lowercase());
    names.dedup();
    names
}

/// Gives egui the fonts: its own, with the code font of the family named,
/// in front of its own fixed width font, which stands in for it if it is
/// not installed.
pub fn install(ctx: &egui::Context, family: &str) {
    let mut fonts = FontDefinitions::default();
    let mut code: Vec<String> = Vec::new();
    let db = database();
    let query = fontdb::Query { families: &[fontdb::Family::Name(family)], ..Default::default() };
    if let Some(id) = db.query(&query) {
        if let Some(data) = db.with_face_data(id, |data, index| {
            let mut font = FontData::from_owned(data.to_vec());
            font.index = index;
            font
        }) {
            fonts.font_data.insert(CODE.into(), Arc::new(data));
            code.push(CODE.into());
        }
    }
    code.extend(fonts.families.get(&FontFamily::Monospace).cloned().unwrap_or_default());
    fonts.families.insert(FontFamily::Name(CODE.into()), code);
    ctx.set_fonts(fonts);
}

/// The font code is shown in.
pub fn code() -> FontId {
    FontId::new(13.0, FontFamily::Name(CODE.into()))
}
