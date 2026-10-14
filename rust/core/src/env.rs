//! Named sets of variables which are put into requests with `{{name}}`
//! placeholders.

use std::collections::HashMap;
use std::path::Path;
use std::sync::LazyLock;

use chrono::{DateTime, Days, Local, Utc};
use regex::Regex;
use serde::{Deserialize, Serialize};

use crate::crypt::Keys;
use crate::{jsonfile, nullable};

/// A single named value in an environment.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct Variable {
    #[serde(default, deserialize_with = "nullable::de")]
    pub key: String,
    #[serde(default, deserialize_with = "nullable::de")]
    pub value: String,
    #[serde(default, deserialize_with = "nullable::de")]
    pub enabled: bool,
    /// What the variable is for. It is only for the reader: several
    /// variables can have one name, to keep the values it is switched
    /// between, and the description tells them apart.
    #[serde(default, deserialize_with = "nullable::de", skip_serializing_if = "String::is_empty")]
    pub description: String,
}

/// A named set of variables.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct Environment {
    #[serde(default, deserialize_with = "nullable::de")]
    pub name: String,
    #[serde(default, deserialize_with = "nullable::de")]
    pub variables: Vec<Variable>,
}

/// The environments that are saved to disk.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct Store {
    /// The index of the environment in use, or -1 for none.
    #[serde(default = "none_active")]
    pub active: i64,
    #[serde(default, deserialize_with = "nullable::de")]
    pub environments: Vec<Environment>,
}

fn none_active() -> i64 {
    -1
}

impl Default for Store {
    fn default() -> Self {
        Store { active: -1, environments: Vec::new() }
    }
}

static PLACEHOLDER: LazyLock<Regex> = LazyLock::new(|| Regex::new(r"\{\{\s*([^{}]*?)\s*\}\}").unwrap());

/// A function which puts the values of variables into text.
pub type Expander = Box<dyn Fn(&str) -> String + Send + Sync>;

impl Environment {
    /// Replaces each `{{key}}` placeholder in `s` with the value of the
    /// enabled variable of that name. If a key is defined more than once the
    /// last one wins. Unknown placeholders are left untouched. A value that
    /// is a call of a built-in function is worked out each time.
    pub fn expand(&self, s: &str) -> String {
        if !s.contains("{{") {
            return s.to_string();
        }
        self.expander()(s)
    }

    /// Returns a function which does as `expand` does to each text it is
    /// given, with the values as they are now. A value that is a call of a
    /// built-in function is worked out once, so it is the same wherever its
    /// variable is used.
    pub fn expander(&self) -> Expander {
        let at = Local::now();
        let mut values = HashMap::new();
        for v in &self.variables {
            if v.enabled && !v.key.is_empty() {
                let value = evaluate(&v.value, at).unwrap_or_else(|| v.value.clone());
                values.insert(v.key.clone(), value);
            }
        }
        Box::new(move |s: &str| {
            PLACEHOLDER
                .replace_all(s, |caps: &regex::Captures| match values.get(&caps[1]) {
                    Some(value) => value.clone(),
                    None => caps[0].to_string(),
                })
                .into_owned()
        })
    }

    /// Returns the value a placeholder's name stands for, if an enabled
    /// variable has the name: the last of them.
    pub fn lookup(&self, key: &str) -> Option<&str> {
        self.variables
            .iter()
            .rev()
            .find(|v| v.enabled && v.key == key)
            .map(|v| v.value.as_str())
    }

    /// Gives the variable with a key a value, and enables it. The variable
    /// is added if there is none. If several have the key, it is the one in
    /// use that is set: the last that is enabled, or the first of them if
    /// none is.
    pub fn set(&mut self, key: &str, value: &str) {
        let mut found: Option<usize> = None;
        for (i, v) in self.variables.iter().enumerate() {
            if v.key == key && (found.is_none() || v.enabled) {
                found = Some(i);
            }
        }
        match found {
            None => self.variables.push(Variable { key: key.into(), value: value.into(), enabled: true, description: String::new() }),
            Some(i) => {
                self.variables[i].value = value.into();
                self.variables[i].enabled = true;
            }
        }
    }
}

/// Returns the start and end byte offsets of each `{{key}}` placeholder in
/// `s`.
pub fn find_placeholders(s: &str) -> Vec<(usize, usize)> {
    PLACEHOLDER.find_iter(s).map(|m| (m.start(), m.end())).collect()
}

/// Returns each placeholder in `s` with its offsets and the name inside it.
pub fn placeholders(s: &str) -> Vec<(usize, usize, String)> {
    PLACEHOLDER
        .captures_iter(s)
        .map(|c| {
            let m = c.get(0).unwrap();
            (m.start(), m.end(), c[1].to_string())
        })
        .collect()
}

impl Store {
    /// Returns the active environment, if there is one.
    pub fn current(&self) -> Option<&Environment> {
        usize::try_from(self.active).ok().and_then(|i| self.environments.get(i))
    }

    pub fn current_mut(&mut self) -> Option<&mut Environment> {
        usize::try_from(self.active).ok().and_then(|i| self.environments.get_mut(i))
    }

    /// Puts the variables of the active environment into text.
    pub fn expand(&self, text: &str) -> String {
        match self.current() {
            Some(e) => e.expand(text),
            None => text.to_string(),
        }
    }

    /// Returns a function which puts the variables of the active environment
    /// into each text it is given. See `Environment::expander`.
    pub fn expander(&self) -> Expander {
        match self.current() {
            Some(e) => e.expander(),
            None => Box::new(|s: &str| s.to_string()),
        }
    }

    /// Puts the environments in alphabetical order, ignoring case. The
    /// active environment stays the active one.
    pub fn sort(&mut self) {
        let mut order: Vec<usize> = (0..self.environments.len()).collect();
        order.sort_by(|&a, &b| {
            self.environments[a].name.to_lowercase().cmp(&self.environments[b].name.to_lowercase())
        });
        let active = usize::try_from(self.active).ok();
        let mut sorted = Vec::with_capacity(order.len());
        let mut new_active = -1;
        for (to, &from) in order.iter().enumerate() {
            sorted.push(self.environments[from].clone());
            if Some(from) == active {
                new_active = to as i64;
            }
        }
        self.environments = sorted;
        self.active = new_active;
    }

    /// Reads a store from the JSON file at path, decrypting it with keys if
    /// it is encrypted. A missing file is not an error and gives an empty
    /// store.
    pub fn load_with(path: &Path, keys: Option<&Keys>) -> Result<Store, String> {
        let mut store: Store = jsonfile::load_with(path, keys)?.unwrap_or_default();
        if store.current().is_none() {
            store.active = -1;
        }
        store.sort();
        Ok(store)
    }

    /// Writes the store to the JSON file at path, encrypted with keys unless
    /// there are none.
    pub fn save_with(&self, path: &Path, keys: Option<&Keys>) -> Result<(), String> {
        jsonfile::save_with(path, self, keys)
    }
}

static CALL: LazyLock<Regex> = LazyLock::new(|| Regex::new(r"^\s*([a-z_]+)\(\s*(-?\d+)?\s*\)\s*$").unwrap());

/// Returns what the value of a variable stands for if it is a call of a
/// built-in function, which is worked out afresh each time, or `None` if it
/// is not one.
///
/// - `random_uuid()` is a random UUID.
/// - `iso_date(n)` is the date n days from today, as 2006-01-02.
/// - `iso_timestamp(n)` is the time n days from now in UTC, as
///   2006-01-02T15:04:05Z.
/// - `unix_timestamp(n)` is that time as seconds since the start of 1970.
///
/// n may be left out, for today or now, and may be below zero.
pub fn evaluate(value: &str, at: DateTime<Local>) -> Option<String> {
    let parts = CALL.captures(value)?;
    let given = parts.get(2).is_some();
    let days: i64 = match parts.get(2) {
        Some(n) => n.as_str().parse().ok()?,
        None => 0,
    };
    let shifted = || {
        if days >= 0 {
            at.checked_add_days(Days::new(days as u64))
        } else {
            at.checked_sub_days(Days::new(days.unsigned_abs()))
        }
    };
    match &parts[1] {
        "random_uuid" if !given => Some(random_uuid()),
        "iso_date" => Some(shifted()?.format("%Y-%m-%d").to_string()),
        "iso_timestamp" => Some(shifted()?.with_timezone(&Utc).format("%Y-%m-%dT%H:%M:%SZ").to_string()),
        "unix_timestamp" => Some(shifted()?.timestamp().to_string()),
        _ => None,
    }
}

/// Returns a random (version 4) UUID.
pub fn random_uuid() -> String {
    let mut b: [u8; 16] = rand::random();
    b[6] = (b[6] & 0x0f) | 0x40;
    b[8] = (b[8] & 0x3f) | 0x80;
    let hex = |s: &[u8]| s.iter().map(|x| format!("{x:02x}")).collect::<String>();
    format!("{}-{}-{}-{}-{}", hex(&b[0..4]), hex(&b[4..6]), hex(&b[6..8]), hex(&b[8..10]), hex(&b[10..16]))
}

#[cfg(test)]
mod tests {
    use super::*;
    use chrono::TimeZone;

    fn var(k: &str, v: &str, e: bool) -> Variable {
        Variable { key: k.into(), value: v.into(), enabled: e, description: String::new() }
    }

    #[test]
    fn expands() {
        let e = Environment {
            name: "x".into(),
            variables: vec![var("host", "a", true), var("host", "b", true), var("off", "x", false)],
        };
        assert_eq!(e.expand("{{host}}/{{ host }}/{{off}}/{{none}}"), "b/b/{{off}}/{{none}}");
    }

    #[test]
    fn functions_are_made_once_per_expander() {
        let e = Environment { name: "x".into(), variables: vec![var("id", "random_uuid()", true)] };
        let f = e.expander();
        let out = f("{{id}} {{id}}");
        let (a, b) = out.split_once(' ').unwrap();
        assert_eq!(a, b);
        assert_eq!(a.len(), 36);
        assert_ne!(f("{{id}}"), e.expand("{{id}}"));
    }

    #[test]
    fn evaluates() {
        let at = Local.with_ymd_and_hms(2024, 2, 28, 12, 0, 0).unwrap();
        assert_eq!(evaluate("iso_date(1)", at).unwrap(), "2024-02-29");
        assert_eq!(evaluate(" iso_date( -28 ) ", at).unwrap(), "2024-01-31");
        assert_eq!(evaluate("unix_timestamp()", at).unwrap(), at.timestamp().to_string());
        assert!(evaluate("random_uuid(1)", at).is_none());
        assert!(evaluate("nope()", at).is_none());
        assert!(evaluate("iso_date() x", at).is_none());
    }

    #[test]
    fn sets_the_variable_in_use() {
        let mut e = Environment { name: "x".into(), variables: vec![var("a", "1", false), var("a", "2", true), var("a", "3", false)] };
        e.set("a", "9");
        assert_eq!(e.variables[1].value, "9");
        e.set("b", "new");
        assert_eq!(e.variables[3], var("b", "new", true));
    }

    #[test]
    fn sorting_keeps_the_active_one() {
        let mut s = Store {
            active: 0,
            environments: vec![Environment { name: "b".into(), ..Default::default() }, Environment { name: "A".into(), ..Default::default() }],
        };
        s.sort();
        assert_eq!(s.environments[0].name, "A");
        assert_eq!(s.active, 1);
    }
}
