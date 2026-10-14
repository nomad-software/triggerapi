//! Program state that is kept between runs, in the same file as the Go
//! program keeps it.

use std::path::{Path, PathBuf};
use std::sync::LazyLock;

use regex::Regex;
use serde::{Deserialize, Serialize};

use crate::{jsonfile, nullable};

const DEFAULT_WIDTH: i64 = 1024;
const DEFAULT_HEIGHT: i64 = 768;
const MIN_SIZE: i64 = 200;
pub const DEFAULT_RESPONSE_FONT: &str = "Iosevka Term Curly";

/// The longest a request can be given, in seconds.
pub const MAX_TIMEOUT: i64 = 3600;

/// The state that is saved to disk.
#[derive(Debug, Clone, PartialEq, Default, Serialize, Deserialize)]
pub struct Settings {
    /// The size of the window when it is not maximised.
    #[serde(default, deserialize_with = "nullable::de")]
    pub width: i64,
    #[serde(default, deserialize_with = "nullable::de")]
    pub height: i64,
    #[serde(default, deserialize_with = "nullable::de")]
    pub maximized: bool,

    /// The sizes the sidebar and the response were dragged to. The default
    /// layout is used if one is zero.
    #[serde(rename = "sidebarWidth", default, deserialize_with = "nullable::de", skip_serializing_if = "nullable::is_default")]
    pub sidebar_width: i64,
    #[serde(rename = "responseHeight", default, deserialize_with = "nullable::de", skip_serializing_if = "nullable::is_default")]
    pub response_height: i64,

    /// The font family the response body is shown in.
    #[serde(rename = "responseFont", default, deserialize_with = "nullable::de")]
    pub response_font: String,

    /// How long a request can take, in seconds, with no limit if it is
    /// zero.
    #[serde(default, deserialize_with = "nullable::de", skip_serializing_if = "nullable::is_default")]
    pub timeout: i64,
    /// Has a REST response that redirects shown as it is.
    #[serde(rename = "noRedirects", default, deserialize_with = "nullable::de", skip_serializing_if = "nullable::is_default")]
    pub no_redirects: bool,

    /// The folder the workspaces are kept in. The default is used if it is
    /// empty.
    #[serde(rename = "workspacesDir", default, deserialize_with = "nullable::de", skip_serializing_if = "String::is_empty")]
    pub workspaces_dir: String,
    /// The name of the workspace in use.
    #[serde(default, deserialize_with = "nullable::de", skip_serializing_if = "String::is_empty")]
    pub workspace: String,

    /// The paths of the files of a pair of SSH keys, which the files of
    /// every workspace are encrypted with.
    #[serde(rename = "publicKey", default, deserialize_with = "nullable::de", skip_serializing_if = "String::is_empty")]
    pub public_key: String,
    #[serde(rename = "privateKey", default, deserialize_with = "nullable::de", skip_serializing_if = "String::is_empty")]
    pub private_key: String,

    /// The requests that are kept open in tabs of their own.
    #[serde(default, deserialize_with = "nullable::de", skip_serializing_if = "Vec::is_empty")]
    pub pinned: Vec<Pin>,

    /// The colours JSON is highlighted in.
    #[serde(rename = "jsonColours", default, deserialize_with = "nullable::de", skip_serializing_if = "nullable::is_default")]
    pub json_colours: JsonColours,

    /// The colours of the icons and of placeholders.
    #[serde(default, deserialize_with = "nullable::de", skip_serializing_if = "nullable::is_default")]
    pub colours: Colours,
}

/// Names a request that is kept open in a tab.
#[derive(Debug, Clone, PartialEq, Eq, Default, Serialize, Deserialize)]
pub struct Pin {
    /// The name of the workspace the request is in.
    #[serde(default, deserialize_with = "nullable::de")]
    pub workspace: String,
    /// The names of the folders the request is in, outermost first, followed
    /// by its own name.
    #[serde(default, deserialize_with = "nullable::de")]
    pub path: Vec<String>,
}

/// The colours of the parts of JSON, as #rrggbb. The default colour for the
/// theme is used for a part that is empty.
#[derive(Debug, Clone, PartialEq, Eq, Default, Serialize, Deserialize)]
pub struct JsonColours {
    #[serde(default, deserialize_with = "nullable::de", skip_serializing_if = "String::is_empty")]
    pub key: String,
    #[serde(default, deserialize_with = "nullable::de", skip_serializing_if = "String::is_empty")]
    pub string: String,
    #[serde(default, deserialize_with = "nullable::de", skip_serializing_if = "String::is_empty")]
    pub number: String,
    #[serde(default, deserialize_with = "nullable::de", skip_serializing_if = "String::is_empty")]
    pub literal: String,
}

macro_rules! colours {
    ($( $(#[$doc:meta])* $field:ident = $json:literal ),* $(,)?) => {
        /// The colours of the icons and of placeholders, as #rrggbb. The
        /// default colour is used for one that is empty.
        #[derive(Debug, Clone, PartialEq, Eq, Default, Serialize, Deserialize)]
        pub struct Colours {
            $(
                $(#[$doc])*
                #[serde(rename = $json, default, deserialize_with = "nullable::de", skip_serializing_if = "String::is_empty")]
                pub $field: String,
            )*
        }

        impl Colours {
            fn fields_mut(&mut self) -> Vec<&mut String> {
                vec![$(&mut self.$field),*]
            }
        }
    };
}

colours! {
    /// The colours of the tabs of the sidebar, of the buttons of each and of
    /// what they list.
    workspaces = "workspaces",
    environments = "environments",
    requests = "requests",
    /// The colours of the requests of each type.
    rest = "rest",
    grpc = "grpc",
    /// The colour of the buttons which remove something.
    remove = "remove",
    /// The colour of the buttons beside the tabs of a panel.
    action = "action",
    /// The colour of the buttons which send a request.
    send = "send",
    /// The colours of placeholders the active environment defines, and of
    /// those it does not.
    found = "found",
    not_found = "notFound",
    /// The colour of the text of errors and of failed statuses.
    error = "error",
    /// The colours of the marks of capturing.
    captured = "captured",
    not_captured = "notCaptured",
    /// The colour that marks what was found in a response.
    r#match = "match",
    /// The colour of the star of a favourite folder.
    favourite = "favourite",
}

static HEX: LazyLock<Regex> = LazyLock::new(|| Regex::new(r"^#[0-9a-fA-F]{6}$").unwrap());

fn empty_invalid(colours: Vec<&mut String>) {
    for c in colours {
        if !HEX.is_match(c) {
            c.clear();
        }
    }
}

/// Returns where the settings file is kept: in a folder for the program in
/// the place the system has for the configuration of programs.
pub fn path() -> Option<PathBuf> {
    dirs::config_dir().map(|d| d.join("triggerapi").join("settings.json"))
}

/// Makes the folder the settings file is kept in.
pub fn prepare(path: &Path) -> std::io::Result<()> {
    match path.parent() {
        Some(dir) if !dir.as_os_str().is_empty() => std::fs::create_dir_all(dir),
        _ => Ok(()),
    }
}

impl Settings {
    /// Reads the settings from the JSON file at path. Defaults are used if
    /// the file is missing or unreadable, and for any value that is
    /// unusable.
    pub fn load(path: &Path) -> Settings {
        let mut s: Settings = jsonfile::load_with(path, None).ok().flatten().unwrap_or_default();
        if s.width < MIN_SIZE || s.height < MIN_SIZE {
            s.width = DEFAULT_WIDTH;
            s.height = DEFAULT_HEIGHT;
        }
        s.timeout = s.timeout.clamp(0, MAX_TIMEOUT);
        s.sidebar_width = s.sidebar_width.max(0);
        s.response_height = s.response_height.max(0);
        let j = &mut s.json_colours;
        empty_invalid(vec![&mut j.key, &mut j.string, &mut j.number, &mut j.literal]);
        empty_invalid(s.colours.fields_mut());
        if s.response_font.is_empty() {
            s.response_font = DEFAULT_RESPONSE_FONT.into();
        }
        s
    }

    /// Writes the settings to the JSON file at path.
    pub fn save(&self, path: &Path) -> Result<(), String> {
        jsonfile::save_with(path, self, None)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn loads_with_defaults() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("settings.json");
        std::fs::write(&path, r##"{"width":10,"timeout":99999,"colours":{"rest":"#123456","grpc":"red"},"pinned":[{"workspace":"W","path":["a","b"]}],"unknown":1}"##).unwrap();
        let s = Settings::load(&path);
        assert_eq!((s.width, s.height), (1024, 768));
        assert_eq!(s.timeout, MAX_TIMEOUT);
        assert_eq!(s.colours.rest, "#123456");
        assert_eq!(s.colours.grpc, "");
        assert_eq!(s.response_font, DEFAULT_RESPONSE_FONT);
        assert_eq!(s.pinned[0].path, vec!["a", "b"]);
        s.save(&path).unwrap();
        assert_eq!(Settings::load(&path), s);
        assert!(!std::fs::read_to_string(&path).unwrap().contains("jsonColours"));
    }
}
