//! Saved requests organised into folders.
//!
//! Folders and requests carry an `id`, which is not saved: it is given to
//! each when it is made or read, and lets the UI refer to one while others
//! are added, moved and removed.

use std::path::Path;
use std::sync::atomic::{AtomicU64, Ordering};

use serde::{Deserialize, Serialize};

use crate::crypt::Keys;
use crate::{jsonfile, nullable, rest, rpc};

/// The kind of a saved request.
pub const TYPE_REST: &str = "rest";
pub const TYPE_GRPC: &str = "grpc";

static NEXT_ID: AtomicU64 = AtomicU64::new(1);

/// Returns an id that nothing has yet.
pub fn new_id() -> u64 {
    NEXT_ID.fetch_add(1, Ordering::Relaxed)
}

/// A saved request. Only the field for its type is set: `rest` for a REST
/// request, whose fields are stored alongside the name, and `grpc` for a
/// gRPC one.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct Request {
    #[serde(skip)]
    pub id: u64,
    #[serde(default, deserialize_with = "nullable::de")]
    pub name: String,
    #[serde(rename = "type", default, deserialize_with = "nullable::de")]
    pub kind: String,
    #[serde(flatten)]
    pub rest: Option<rest::Request>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub grpc: Option<rpc::Request>,
}

impl Request {
    /// Returns a REST request.
    pub fn new_rest(name: impl Into<String>, request: rest::Request) -> Request {
        Request { id: new_id(), name: name.into(), kind: TYPE_REST.into(), rest: Some(request), grpc: None }
    }

    /// Returns a gRPC request.
    pub fn new_grpc(name: impl Into<String>, request: rpc::Request) -> Request {
        Request { id: new_id(), name: name.into(), kind: TYPE_GRPC.into(), rest: None, grpc: Some(request) }
    }

    pub fn is_grpc(&self) -> bool {
        self.kind == TYPE_GRPC
    }

    /// Makes the request hold exactly the field for its type. Requests saved
    /// before there were types are REST requests.
    fn normalise(&mut self) {
        if self.id == 0 {
            self.id = new_id();
        }
        if self.kind == TYPE_GRPC {
            self.rest = None;
            if self.grpc.is_none() {
                self.grpc = Some(rpc::Request::default());
            }
            return;
        }
        self.kind = TYPE_REST.into();
        self.grpc = None;
        if self.rest.is_none() {
            self.rest = Some(rest::Request::default());
        }
    }

    /// Returns a copy of the request which has an id of its own.
    pub fn duplicate(&self) -> Request {
        let mut copy = self.clone();
        copy.id = new_id();
        copy
    }
}

/// A named group of requests. It can hold other folders, to any depth. Its
/// folders come before its requests wherever its contents are listed.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct Folder {
    #[serde(skip)]
    pub id: u64,
    #[serde(default, deserialize_with = "nullable::de")]
    pub name: String,
    /// The folder is shown closed.
    #[serde(default, deserialize_with = "nullable::de", skip_serializing_if = "nullable::is_default")]
    pub collapsed: bool,
    /// A folder at the top level is one of the favourites. It is cleared when
    /// the folder is moved inside another.
    #[serde(default, deserialize_with = "nullable::de", skip_serializing_if = "nullable::is_default")]
    pub favourite: bool,
    #[serde(default, deserialize_with = "nullable::de", skip_serializing_if = "Vec::is_empty")]
    pub folders: Vec<Folder>,
    #[serde(default, deserialize_with = "nullable::de", skip_serializing_if = "Vec::is_empty")]
    pub requests: Vec<Request>,
}

impl Folder {
    pub fn new(name: impl Into<String>) -> Folder {
        Folder { id: new_id(), name: name.into(), ..Default::default() }
    }

    /// Appends a new folder inside the folder, and returns its id.
    pub fn add_folder(&mut self, name: &str) -> u64 {
        let f = Folder::new(name);
        let id = f.id;
        self.folders.push(f);
        id
    }

    /// Appends a request to the folder, and returns its id.
    pub fn add(&mut self, mut r: Request) -> u64 {
        if r.id == 0 {
            r.id = new_id();
        }
        let id = r.id;
        self.requests.push(r);
        id
    }

    /// Closes every folder inside the folder, leaving the folder itself as
    /// it is.
    pub fn collapse(&mut self) {
        for sub in &mut self.folders {
            sub.collapsed = true;
            sub.collapse();
        }
    }

    /// Returns the requests in the folder and in every folder inside it.
    pub fn all(&self) -> Vec<&Request> {
        let mut out: Vec<&Request> = self.requests.iter().collect();
        for sub in &self.folders {
            out.extend(sub.all());
        }
        out
    }

    /// Returns the number of folders inside the folder, at any depth.
    pub fn descendants(&self) -> usize {
        self.folders.len() + self.folders.iter().map(Folder::descendants).sum::<usize>()
    }

    /// Returns a copy of the folder, and of everything in it, with ids of
    /// their own.
    pub fn duplicate(&self) -> Folder {
        let mut copy = self.clone();
        copy.renew_ids();
        copy
    }

    fn renew_ids(&mut self) {
        self.id = new_id();
        for r in &mut self.requests {
            r.id = new_id();
        }
        for f in &mut self.folders {
            f.renew_ids();
        }
    }

    fn sort(&mut self) {
        self.folders.sort_by_key(|f| f.name.to_lowercase());
        self.requests.sort_by_key(|r| r.name.to_lowercase());
        for sub in &mut self.folders {
            sub.sort();
        }
    }

    /// Gives ids to what has none, and makes each request hold the field for
    /// its type.
    fn normalise(&mut self) {
        if self.id == 0 {
            self.id = new_id();
        }
        for sub in &mut self.folders {
            sub.normalise();
        }
        for r in &mut self.requests {
            r.normalise();
        }
    }

    fn contains_folder(&self, id: u64) -> bool {
        self.id == id || self.folders.iter().any(|f| f.contains_folder(id))
    }

    fn find_folder(&self, id: u64) -> Option<&Folder> {
        if self.id == id {
            return Some(self);
        }
        self.folders.iter().find_map(|f| f.find_folder(id))
    }

    fn find_folder_mut(&mut self, id: u64) -> Option<&mut Folder> {
        if self.id == id {
            return Some(self);
        }
        self.folders.iter_mut().find_map(|f| f.find_folder_mut(id))
    }

    fn holder_of_request(&self, id: u64) -> Option<&Folder> {
        if self.requests.iter().any(|r| r.id == id) {
            return Some(self);
        }
        self.folders.iter().find_map(|f| f.holder_of_request(id))
    }
}

/// The folders that are saved to disk. Only folders are at its top level.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct Store {
    #[serde(default, deserialize_with = "nullable::de")]
    pub folders: Vec<Folder>,
}

impl Store {
    /// Appends a new folder at the top level, and returns its id.
    pub fn add_folder(&mut self, name: &str) -> u64 {
        let f = Folder::new(name);
        let id = f.id;
        self.folders.push(f);
        id
    }

    /// Puts the folders and the requests in them in alphabetical order,
    /// ignoring case. Folders still come before requests.
    pub fn sort(&mut self) {
        self.folders.sort_by_key(|f| f.name.to_lowercase());
        for f in &mut self.folders {
            f.sort();
        }
    }

    /// Returns every request of the store.
    pub fn all(&self) -> Vec<&Request> {
        self.folders.iter().flat_map(|f| f.all()).collect()
    }

    pub fn folder(&self, id: u64) -> Option<&Folder> {
        self.folders.iter().find_map(|f| f.find_folder(id))
    }

    pub fn folder_mut(&mut self, id: u64) -> Option<&mut Folder> {
        self.folders.iter_mut().find_map(|f| f.find_folder_mut(id))
    }

    pub fn request(&self, id: u64) -> Option<&Request> {
        let holder = self.folder_of(id)?;
        holder.requests.iter().find(|r| r.id == id)
    }

    pub fn request_mut(&mut self, id: u64) -> Option<&mut Request> {
        let holder = self.folder_of(id)?.id;
        self.folder_mut(holder)?.requests.iter_mut().find(|r| r.id == id)
    }

    /// Returns the folder that holds a request, or `None` if the request is
    /// not in the store.
    pub fn folder_of(&self, request: u64) -> Option<&Folder> {
        self.folders.iter().find_map(|f| f.holder_of_request(request))
    }

    /// Returns the id of the folder that holds a folder, `Some(None)` if it
    /// is at the top level, and `None` if it is not in the store.
    pub fn parent_of(&self, folder: u64) -> Option<Option<u64>> {
        if self.folders.iter().any(|f| f.id == folder) {
            return Some(None);
        }
        fn find(in_: &Folder, id: u64) -> Option<u64> {
            if in_.folders.iter().any(|f| f.id == id) {
                return Some(in_.id);
            }
            in_.folders.iter().find_map(|f| find(f, id))
        }
        self.folders.iter().find_map(|f| find(f, folder)).map(Some)
    }

    /// Returns the names of the folders a request is in, outermost first,
    /// followed by its own name. It is `None` if the request is not in the
    /// store.
    pub fn path(&self, request: u64) -> Option<Vec<String>> {
        fn find(in_: &Folder, id: u64, mut path: Vec<String>) -> Option<Vec<String>> {
            path.push(in_.name.clone());
            if let Some(r) = in_.requests.iter().find(|r| r.id == id) {
                path.push(r.name.clone());
                return Some(path);
            }
            in_.folders.iter().find_map(|f| find(f, id, path.clone()))
        }
        self.folders.iter().find_map(|f| find(f, request, Vec::new()))
    }

    /// Returns the id of the request at a path, as `path` gives it. Where
    /// names are shared it is the first that is found.
    pub fn find(&self, path: &[String]) -> Option<u64> {
        fn find(folders: &[Folder], path: &[String]) -> Option<u64> {
            if path.len() < 2 {
                return None;
            }
            for folder in folders.iter().filter(|f| f.name == path[0]) {
                if path.len() == 2 {
                    if let Some(r) = folder.requests.iter().find(|r| r.name == path[1]) {
                        return Some(r.id);
                    }
                } else if let Some(found) = find(&folder.folders, &path[1..]) {
                    return Some(found);
                }
            }
            None
        }
        find(&self.folders, path)
    }

    /// Takes a request out of the store, returning it.
    pub fn take_request(&mut self, id: u64) -> Option<Request> {
        let holder = self.folder_of(id)?.id;
        let folder = self.folder_mut(holder)?;
        let at = folder.requests.iter().position(|r| r.id == id)?;
        Some(folder.requests.remove(at))
    }

    /// Takes a folder, with everything in it, out of the store, returning
    /// it.
    pub fn take_folder(&mut self, id: u64) -> Option<Folder> {
        match self.parent_of(id)? {
            None => {
                let at = self.folders.iter().position(|f| f.id == id)?;
                Some(self.folders.remove(at))
            }
            Some(parent) => {
                let parent = self.folder_mut(parent)?;
                let at = parent.folders.iter().position(|f| f.id == id)?;
                Some(parent.folders.remove(at))
            }
        }
    }

    /// Moves a request into a folder, at the end. It reports false, having
    /// done nothing, if either is not in the store.
    pub fn move_request(&mut self, request: u64, to: u64) -> bool {
        if self.folder(to).is_none() || self.folder_of(request).is_none() {
            return false;
        }
        let r = self.take_request(request).unwrap();
        self.folder_mut(to).unwrap().requests.push(r);
        true
    }

    /// Moves a folder into another, or to the top level if that is `None`,
    /// at the end. Only a folder at the top level can be a favourite, so
    /// one moved into another is no longer. It reports false, having done
    /// nothing, if the folder is not in the store or would be moved inside
    /// itself.
    pub fn move_folder(&mut self, folder: u64, to: Option<u64>) -> bool {
        let Some(f) = self.folder(folder) else { return false };
        if let Some(to) = to {
            if f.contains_folder(to) || self.folder(to).is_none() {
                return false;
            }
        }
        let mut f = self.take_folder(folder).unwrap();
        match to {
            None => self.folders.push(f),
            Some(to) => {
                f.favourite = false;
                self.folder_mut(to).unwrap().folders.push(f);
            }
        }
        true
    }

    /// Reads a store from the JSON file at path, decrypting it with keys if
    /// it is encrypted. A missing file is not an error and gives an empty
    /// store.
    pub fn load_with(path: &Path, keys: Option<&Keys>) -> Result<Store, String> {
        let mut store: Store = jsonfile::load_with(path, keys)?.unwrap_or_default();
        for f in &mut store.folders {
            f.normalise();
        }
        store.sort();
        Ok(store)
    }

    /// Writes the store to the JSON file at path, encrypted with keys unless
    /// there are none.
    pub fn save_with(&self, path: &Path, keys: Option<&Keys>) -> Result<(), String> {
        jsonfile::save_with(path, self, keys)
    }

    /// Gives ids to what has none, after folders have been added from
    /// elsewhere.
    pub fn normalise(&mut self) {
        for f in &mut self.folders {
            f.normalise();
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn reads_and_writes_the_saved_format() {
        // Requests from before there were types are REST requests, with
        // their fields beside the name.
        let text = r#"{"folders":[{"name":"b","requests":[{"name":"r","method":"GET","url":"x","auth":{}},{"name":"g","type":"grpc","grpc":{"url":"h:1","method":"a.B/C","auth":{}}}]},{"name":"A","folders":null,"favourite":true}]}"#;
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("requests.json");
        std::fs::write(&path, text).unwrap();
        let store = Store::load_with(&path, None).unwrap();
        assert_eq!(store.folders[0].name, "A");
        assert!(store.folders[0].favourite);
        let b = &store.folders[1];
        assert_eq!(b.requests[0].name, "g");
        assert!(b.requests[0].rest.is_none() && b.requests[0].grpc.is_some());
        assert_eq!(b.requests[1].kind, TYPE_REST);
        assert_eq!(b.requests[1].rest.as_ref().unwrap().url, "x");

        store.save_with(&path, None).unwrap();
        let written = std::fs::read_to_string(&path).unwrap();
        let value: serde_json::Value = serde_json::from_str(&written).unwrap();
        assert_eq!(
            value,
            serde_json::json!({"folders": [
                {"name": "A", "favourite": true},
                {"name": "b", "requests": [
                    {"name": "g", "type": "grpc", "grpc": {"url": "h:1", "method": "a.B/C", "auth": {}}},
                    {"name": "r", "type": "rest", "method": "GET", "url": "x", "auth": {}}
                ]}
            ]})
        );
        assert!(!written.contains("collapsed"), "{written}");
    }

    #[test]
    fn moves_and_finds() {
        let mut s = Store::default();
        let a = s.add_folder("a");
        let b = s.add_folder("b");
        let c = s.folder_mut(a).unwrap().add_folder("c");
        let r = s.folder_mut(c).unwrap().add(Request::new_rest("r", rest::Request::default()));
        assert_eq!(s.path(r).unwrap(), ["a", "c", "r"]);
        assert_eq!(s.find(&["a".into(), "c".into(), "r".into()]), Some(r));
        assert_eq!(s.parent_of(c), Some(Some(a)));
        assert_eq!(s.parent_of(a), Some(None));
        assert!(!s.move_folder(a, Some(c)));
        s.folder_mut(a).unwrap().favourite = true;
        assert!(s.move_folder(a, Some(b)));
        assert!(!s.folder(a).unwrap().favourite);
        assert!(s.move_request(r, b));
        assert_eq!(s.folder_of(r).unwrap().id, b);
        assert!(s.move_folder(c, None));
        assert_eq!(s.folders.len(), 2);
    }
}
