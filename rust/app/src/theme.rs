//! The colours the user can choose, and the defaults of each.

use egui::Color32;
use triggerapi_core::settings::{Colours, JsonColours};

/// The colour of the buttons which send a request, and of the workspaces,
/// until another is chosen.
pub const PRIMARY: &str = "#e8590c";
/// The colour errors are shown in until another is chosen.
pub const FAILURE: &str = "#ef5350";

/// One of the colours of the icons, buttons and text that the user can
/// choose.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash)]
pub enum Kind {
    Workspaces,
    Environments,
    Requests,
    Rest,
    Grpc,
    Remove,
    Action,
    Send,
    Found,
    NotFound,
    Error,
    Captured,
    NotCaptured,
    Match,
    Favourite,
}

/// The colours, with what each is called in the settings and the colour it
/// is until another is chosen.
pub const KINDS: &[(Kind, &str, &str)] = &[
    (Kind::Workspaces, "Workspaces", PRIMARY),
    (Kind::Environments, "Environments", "#8e6fd8"),
    (Kind::Requests, "Requests", "#17807c"),
    (Kind::Rest, "REST", "#2e9e5b"),
    (Kind::Grpc, "gRPC", "#2f7fd1"),
    (Kind::Remove, "Delete", "#e03131"),
    (Kind::Action, "Action", "#0b4f6c"),
    (Kind::Send, "Send", PRIMARY),
    (Kind::Found, "Placeholders found", "#2e9e5b"),
    (Kind::NotFound, "Placeholders not found", FAILURE),
    (Kind::Error, "Errors", FAILURE),
    (Kind::Captured, "Captured", "#2e9e5b"),
    (Kind::NotCaptured, "Not captured", FAILURE),
    (Kind::Match, "Search matches", "#ffd43b"),
    (Kind::Favourite, "Favourites", "#fab005"),
];

/// The colours as they are listed in the settings: in sections, each a
/// heading above the colours of one sort.
pub const SECTIONS: &[(&str, &[Kind])] = &[
    ("Sidebar", &[Kind::Workspaces, Kind::Environments, Kind::Requests, Kind::Favourite]),
    ("Request types", &[Kind::Rest, Kind::Grpc]),
    ("Buttons", &[Kind::Send, Kind::Action, Kind::Remove]),
    ("Text", &[Kind::Found, Kind::NotFound, Kind::Error, Kind::Match]),
    ("Capture", &[Kind::Captured, Kind::NotCaptured]),
];

/// Returns the place in the settings that holds the colour of a kind.
pub fn custom(c: &mut Colours, kind: Kind) -> &mut String {
    match kind {
        Kind::Workspaces => &mut c.workspaces,
        Kind::Environments => &mut c.environments,
        Kind::Requests => &mut c.requests,
        Kind::Rest => &mut c.rest,
        Kind::Grpc => &mut c.grpc,
        Kind::Remove => &mut c.remove,
        Kind::Action => &mut c.action,
        Kind::Send => &mut c.send,
        Kind::Found => &mut c.found,
        Kind::NotFound => &mut c.not_found,
        Kind::Error => &mut c.error,
        Kind::Captured => &mut c.captured,
        Kind::NotCaptured => &mut c.not_captured,
        Kind::Match => &mut c.r#match,
        Kind::Favourite => &mut c.favourite,
    }
}

pub fn label(kind: Kind) -> &'static str {
    KINDS.iter().find(|k| k.0 == kind).map(|k| k.1).unwrap_or("")
}

pub fn default_hex(kind: Kind) -> &'static str {
    KINDS.iter().find(|k| k.0 == kind).map(|k| k.2).unwrap_or(PRIMARY)
}

/// Reads a colour written as #rrggbb.
pub fn parse(hex: &str) -> Option<Color32> {
    let h = hex.strip_prefix('#')?;
    if h.len() != 6 {
        return None;
    }
    let v = u32::from_str_radix(h, 16).ok()?;
    Some(Color32::from_rgb((v >> 16) as u8, (v >> 8) as u8, v as u8))
}

/// Writes a colour as #rrggbb.
pub fn hex(c: Color32) -> String {
    format!("#{:02x}{:02x}{:02x}", c.r(), c.g(), c.b())
}

/// The colours in effect: those chosen in the settings, or the defaults.
#[derive(Clone, Debug, Default)]
pub struct Theme {
    pub colours: Colours,
    pub json: JsonColours,
}

impl Theme {
    /// Returns the colour of a kind in effect.
    pub fn colour(&self, kind: Kind) -> Color32 {
        let mut c = self.colours.clone();
        let chosen = custom(&mut c, kind).clone();
        parse(&chosen).or_else(|| parse(default_hex(kind))).unwrap_or(Color32::GRAY)
    }

    /// Returns the colour of a kind of JSON token in effect, the default of
    /// which depends on whether the background is dark.
    pub fn json(&self, token: Token, dark: bool) -> Color32 {
        let chosen = match token {
            Token::Key => &self.json.key,
            Token::String => &self.json.string,
            Token::Number => &self.json.number,
            Token::Literal => &self.json.literal,
        };
        parse(chosen).unwrap_or_else(|| parse(json_default(token, dark)).unwrap())
    }
}

/// A kind of JSON token that is coloured.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Token {
    Key,
    String,
    Number,
    Literal,
}

pub const TOKENS: &[(Token, &str)] = &[
    (Token::Key, "Keys"),
    (Token::String, "Strings"),
    (Token::Number, "Numbers"),
    (Token::Literal, "true, false and null"),
];

/// The default colours of the tokens: those of the Monaco editor's light
/// theme on a light background, and others on a dark one.
pub fn json_default(token: Token, dark: bool) -> &'static str {
    match (token, dark) {
        (Token::Key, false) => "#a31515",
        (Token::String, false) => "#0451a5",
        (Token::Number, false) => "#098658",
        (Token::Literal, false) => "#0451a5",
        (Token::Key, true) => "#9bdafc",
        (Token::String, true) => "#a1b796",
        (Token::Number, true) => "#b0c8a4",
        (Token::Literal, true) => "#a1b796",
    }
}

pub fn json_custom(c: &mut JsonColours, token: Token) -> &mut String {
    match token {
        Token::Key => &mut c.key,
        Token::String => &mut c.string,
        Token::Number => &mut c.number,
        Token::Literal => &mut c.literal,
    }
}

/// Returns black or white, whichever can be read against a colour.
pub fn text_on(c: Color32) -> Color32 {
    let luma = 0.299 * c.r() as f32 + 0.587 * c.g() as f32 + 0.114 * c.b() as f32;
    if luma > 150.0 {
        Color32::BLACK
    } else {
        Color32::WHITE
    }
}

/// Returns a colour mixed half and half with another.
pub fn mix(a: Color32, b: Color32) -> Color32 {
    Color32::from_rgb(
        ((a.r() as u16 + b.r() as u16) / 2) as u8,
        ((a.g() as u16 + b.g() as u16) / 2) as u8,
        ((a.b() as u16 + b.b() as u16) / 2) as u8,
    )
}
