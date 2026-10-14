//! The workspaces, each of which has its own environments and requests. A
//! workspace is a folder, named after it, in the folder all the workspaces
//! are kept in, and holds a file for each.

use std::collections::BTreeMap;
use std::fs;
use std::io::{Read, Write};
use std::path::{Path, PathBuf};

use crate::crypt::{self, Keys};

/// The name of the workspace there is to begin with.
pub const DEFAULT: &str = "Default";

const ENVIRONMENTS_FILE: &str = "environments.json";
const REQUESTS_FILE: &str = "requests.json";

/// The most a file in an archive can hold, which keeps one made to be
/// enormous when unpacked from being read.
const MAX_ARCHIVED: u64 = 256 << 20;

/// Returns the folder the workspaces are kept in unless another is chosen:
/// a folder for the program in the place the system has for the data of
/// programs.
pub fn default_dir() -> Option<PathBuf> {
    dirs::data_dir().map(|d| d.join("triggerapi").join("workspaces"))
}

/// Returns the path of the file the environments of a workspace are kept
/// in.
pub fn environments(dir: &Path, name: &str) -> PathBuf {
    dir.join(name).join(ENVIRONMENTS_FILE)
}

/// Returns the path of the file the requests of a workspace are kept in.
pub fn requests(dir: &Path, name: &str) -> PathBuf {
    dir.join(name).join(REQUESTS_FILE)
}

/// Returns the names of the workspaces in a folder, in alphabetical order
/// ignoring case. There are none in a folder that does not exist.
pub fn list(dir: &Path) -> Result<Vec<String>, String> {
    let entries = match fs::read_dir(dir) {
        Ok(e) => e,
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => return Ok(Vec::new()),
        Err(e) => return Err(e.to_string()),
    };
    let mut names = Vec::new();
    for entry in entries.flatten() {
        let name = entry.file_name().to_string_lossy().into_owned();
        // Hidden folders are not workspaces, as none can be given such a
        // name.
        if entry.file_type().map(|t| t.is_dir()).unwrap_or(false) && !name.starts_with('.') {
            names.push(name);
        }
    }
    names.sort_by_key(|n| n.to_lowercase());
    Ok(names)
}

/// Reports why a workspace cannot be given a name, which has to be usable
/// as the name of a folder on any system.
pub fn valid(name: &str) -> Result<(), String> {
    if name.is_empty() {
        return Err("a workspace needs a name".into());
    }
    if name != name.trim() {
        return Err("the name of a workspace cannot start or end with a space".into());
    }
    if name.starts_with('.') || name.ends_with('.') {
        return Err("the name of a workspace cannot start or end with a dot".into());
    }
    if name.len() > 100 {
        return Err("the name of a workspace cannot be longer than 100 characters".into());
    }
    if name.chars().any(|c| c < ' ' || r#"/\:*?"<>|"#.contains(c)) {
        return Err(r#"the name of a workspace cannot contain any of / \ : * ? " < > |"#.into());
    }
    Ok(())
}

/// Reports if a folder has a workspace of a name, ignoring case, as some
/// systems do in the names of folders.
fn has(dir: &Path, name: &str) -> Result<bool, String> {
    Ok(list(dir)?.iter().any(|n| n.to_lowercase() == name.to_lowercase()))
}

/// Returns a name that no workspace in a folder has yet: the one given, or
/// that with a number after it.
pub fn unique(dir: &Path, name: &str) -> String {
    for n in 1.. {
        let candidate = if n > 1 { format!("{name} {n}") } else { name.to_string() };
        if !has(dir, &candidate).unwrap_or(false) {
            return candidate;
        }
    }
    unreachable!()
}

fn make_dir(path: &Path) -> Result<(), String> {
    let mut builder = fs::DirBuilder::new();
    builder.recursive(true);
    #[cfg(unix)]
    {
        use std::os::unix::fs::DirBuilderExt;
        builder.mode(0o700);
    }
    builder.create(path).map_err(|e| e.to_string())
}

/// Adds an empty workspace to a folder, which is made if need be.
pub fn create(dir: &Path, name: &str) -> Result<(), String> {
    valid(name)?;
    if has(dir, name)? {
        return Err(format!("there is already a workspace named {name}"));
    }
    make_dir(&dir.join(name))
}

/// Gives a workspace another name.
pub fn rename(dir: &Path, from: &str, to: &str) -> Result<(), String> {
    if from == to {
        return Ok(());
    }
    valid(to)?;
    // A change to the case of the letters alone is not a clash with itself.
    if from.to_lowercase() != to.to_lowercase() && has(dir, to)? {
        return Err(format!("there is already a workspace named {to}"));
    }
    fs::rename(dir.join(from), dir.join(to)).map_err(|e| e.to_string())
}

/// Deletes a workspace with its environments and requests. A folder which
/// has anything else in it is left alone, so that nothing is deleted which
/// was not put there by the program.
pub fn remove(dir: &Path, name: &str) -> Result<(), String> {
    valid(name)?;
    let folder = dir.join(name);
    let entries: Vec<_> = fs::read_dir(&folder).map_err(|e| e.to_string())?.flatten().collect();
    for entry in &entries {
        let file = entry.file_name().to_string_lossy().into_owned();
        if entry.file_type().map(|t| t.is_dir()).unwrap_or(true) || (file != ENVIRONMENTS_FILE && file != REQUESTS_FILE) {
            return Err(format!("its folder has other files in it, such as {file}"));
        }
    }
    for entry in &entries {
        fs::remove_file(entry.path()).map_err(|e| e.to_string())?;
    }
    fs::remove_dir(&folder).map_err(|e| e.to_string())
}

/// Makes a copy of a workspace, with its environments and requests, and
/// returns the name it was given.
pub fn duplicate(dir: &Path, name: &str) -> Result<String, String> {
    valid(name)?;
    if !has(dir, name)? {
        return Err(format!("there is no workspace named {name}"));
    }
    // A long name is cut short to leave room for what is added to it.
    let mut base = name.to_string();
    if base.len() > 80 {
        let mut cut = 80;
        while !base.is_char_boundary(cut) {
            cut -= 1;
        }
        base = base[..cut].trim().to_string();
    }
    let copied = unique(dir, &format!("{base} copy"));
    create(dir, &copied)?;
    import(dir, &copied, &environments(dir, name), &requests(dir, name))?;
    Ok(copied)
}

/// Copies the files of environments and requests from before there were
/// workspaces into a workspace. A file that does not exist is skipped, and
/// nothing already in the workspace is replaced.
pub fn import(dir: &Path, name: &str, environments_from: &Path, requests_from: &Path) -> Result<(), String> {
    let a = copy_file(environments_from, &environments(dir, name));
    let b = copy_file(requests_from, &requests(dir, name));
    a.and(b)
}

/// Copies every workspace of one folder into another, leaving the first as
/// it was. Nothing already in the second is replaced.
pub fn copy_all(from: &Path, to: &Path) -> Result<(), String> {
    for name in list(from)? {
        make_dir(&to.join(&name))?;
        import(to, &name, &environments(from, &name), &requests(from, &name))?;
    }
    Ok(())
}

/// Writes a file that can be read and written by the user alone, failing
/// if there is one already.
fn create_new(path: &Path, data: &[u8]) -> std::io::Result<()> {
    let mut options = fs::OpenOptions::new();
    options.write(true).create_new(true);
    #[cfg(unix)]
    {
        use std::os::unix::fs::OpenOptionsExt;
        options.mode(0o600);
    }
    let mut file = options.open(path)?;
    file.write_all(data)
}

/// Copies a file, unless there is none to copy or there is already one where
/// it would go.
fn copy_file(from: &Path, to: &Path) -> Result<(), String> {
    let data = match fs::read(from) {
        Ok(d) => d,
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => return Ok(()),
        Err(e) => return Err(e.to_string()),
    };
    match create_new(to, &data) {
        Ok(()) => Ok(()),
        Err(e) if e.kind() == std::io::ErrorKind::AlreadyExists => Ok(()),
        Err(e) => Err(e.to_string()),
    }
}

/// Writes every workspace of a folder to a zip file for someone else: each
/// file is decrypted with the keys it has and encrypted to those of whoever
/// the archive is for. The workspaces themselves are left as they are. In
/// the archive each workspace is a folder, named after it, holding its
/// files.
pub fn export(dir: &Path, archive: &Path, own: Option<&Keys>, recipient: Option<&Keys>) -> Result<(), String> {
    let (Some(own), Some(recipient)) = (own, recipient) else {
        return Err("keys are needed to decrypt the files and to encrypt them again".into());
    };
    let names = list(dir)?;
    if names.is_empty() {
        return Err("there are no workspaces to export".into());
    }

    // The archive is written beside where it is to be, and put there once
    // it is whole.
    let parent = archive.parent().filter(|p| !p.as_os_str().is_empty()).unwrap_or(Path::new("."));
    let tmp = tempfile::Builder::new()
        .prefix(&format!("{}.", archive.file_name().unwrap_or_default().to_string_lossy()))
        .suffix(".tmp")
        .tempfile_in(parent)
        .map_err(|e| e.to_string())?;
    let mut w = zip::ZipWriter::new(tmp.reopen().map_err(|e| e.to_string())?);
    let options = zip::write::SimpleFileOptions::default();
    for name in &names {
        // A workspace with no files yet is still one.
        w.add_directory(format!("{name}/"), options).map_err(|e| e.to_string())?;
        for file in [ENVIRONMENTS_FILE, REQUESTS_FILE] {
            let data = match fs::read(dir.join(name).join(file)) {
                Ok(d) => d,
                Err(e) if e.kind() == std::io::ErrorKind::NotFound => continue,
                Err(e) => return Err(format!("{file} of {name}: {e}")),
            };
            let plain = crypt::decrypt(Some(own), &data).map_err(|e| format!("{file} of {name}: {e}"))?;
            let encrypted = crypt::encrypt(Some(recipient), &plain).map_err(|e| format!("{file} of {name}: {e}"))?;
            w.start_file(format!("{name}/{file}"), options).map_err(|e| e.to_string())?;
            w.write_all(&encrypted).map_err(|e| e.to_string())?;
        }
    }
    w.finish().map_err(|e| e.to_string())?;
    tmp.persist(archive).map_err(|e| e.to_string())?;
    Ok(())
}

/// Adds the workspaces of an archive made by `export` to a folder, and
/// returns the names they were given. One whose name is taken is given
/// another, so nothing already there is replaced. The files have to be
/// readable with the keys, and nothing is added if any is not, or if the
/// archive holds anything but workspaces.
pub fn unpack(dir: &Path, archive: &Path, keys: Option<&Keys>) -> Result<Vec<String>, String> {
    let keys = keys.ok_or("keys are needed to read the files")?;
    let file = fs::File::open(archive).map_err(|e| format!("it is not a zip file that can be read: {e}"))?;
    let mut zip = zip::ZipArchive::new(file).map_err(|e| format!("it is not a zip file that can be read: {e}"))?;

    // Everything is read and checked before anything is written.
    let mut names: Vec<String> = Vec::new();
    let mut files: BTreeMap<String, Vec<(String, Vec<u8>)>> = BTreeMap::new();
    for i in 0..zip.len() {
        let mut entry = zip.by_index(i).map_err(|e| e.to_string())?;
        let entry_name = entry.name().to_string();
        let trimmed = entry_name.trim_end_matches('/');
        let (name, file) = trimmed.split_once('/').unwrap_or((trimmed, ""));
        let refused = || format!("it is not an export of workspaces: it has {entry_name:?} in it");
        if valid(name).is_err() {
            return Err(refused());
        }
        if !files.contains_key(name) {
            names.push(name.to_string());
            files.insert(name.to_string(), Vec::new());
        }
        if file.is_empty() && entry.is_dir() {
            continue;
        }
        if (file != ENVIRONMENTS_FILE && file != REQUESTS_FILE) || entry.is_dir() {
            return Err(refused());
        }
        let mut data = Vec::new();
        (&mut entry).take(MAX_ARCHIVED + 1).read_to_end(&mut data).map_err(|e| e.to_string())?;
        if data.len() as u64 > MAX_ARCHIVED {
            return Err(format!("{file} of {name}: it is too large"));
        }
        let plain = crypt::decrypt(Some(keys), &data)
            .map_err(|e| format!("{file} of {name}: it cannot be read with the keys of the settings: {e}"))?;
        let kept = if crypt::encrypted(&data) { data } else { crypt::encrypt(Some(keys), &plain)? };
        files.get_mut(name).unwrap().push((file.to_string(), kept));
    }
    if names.is_empty() {
        return Err("there are no workspaces in it".into());
    }

    let mut added = Vec::new();
    for name in names {
        let given = unique(dir, &name);
        create(dir, &given)?;
        added.push(given.clone());
        for (file, data) in &files[&name] {
            create_new(&dir.join(&given).join(file), data).map_err(|e| e.to_string())?;
        }
    }
    Ok(added)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn manages_workspaces() {
        let tmp = tempfile::tempdir().unwrap();
        let dir = tmp.path().join("workspaces");
        assert!(list(&dir).unwrap().is_empty());
        create(&dir, "b").unwrap();
        create(&dir, "A").unwrap();
        assert!(create(&dir, "a").is_err());
        assert!(valid("x/y").is_err() && valid(" x").is_err() && valid(".x").is_err());
        assert_eq!(list(&dir).unwrap(), vec!["A", "b"]);
        assert_eq!(unique(&dir, "a"), "a 2");
        fs::write(requests(&dir, "A"), "{}").unwrap();
        let copy = duplicate(&dir, "A").unwrap();
        assert_eq!(copy, "A copy");
        assert_eq!(fs::read_to_string(requests(&dir, "A copy")).unwrap(), "{}");
        rename(&dir, "A copy", "C").unwrap();
        remove(&dir, "C").unwrap();
        fs::write(dir.join("b").join("other"), "x").unwrap();
        assert!(remove(&dir, "b").is_err());
    }

    #[test]
    fn exports_and_unpacks() {
        let tmp = tempfile::tempdir().unwrap();
        let (pa, ka) = crypt::test_pair(tmp.path(), "mine", "");
        let (pb, kb) = crypt::test_pair(tmp.path(), "theirs", "");
        let mine = Keys::open(&pa, &ka, &mut || None).unwrap();
        let theirs = Keys::open(&pb, &kb, &mut || None).unwrap();
        let recipient = Keys::recipient(&pb).unwrap();

        let from = tmp.path().join("from");
        create(&from, "Hotel").unwrap();
        create(&from, "Empty").unwrap();
        fs::write(requests(&from, "Hotel"), crypt::encrypt(Some(&mine), b"{\"folders\":[]}").unwrap()).unwrap();
        let archive = tmp.path().join("export.zip");
        export(&from, &archive, Some(&mine), Some(&recipient)).unwrap();

        let to = tmp.path().join("to");
        assert!(unpack(&to, &archive, Some(&mine)).unwrap_err().contains("cannot be read with the keys"));
        create(&to, "Hotel").unwrap();
        let added = unpack(&to, &archive, Some(&theirs)).unwrap();
        assert_eq!(added, vec!["Empty", "Hotel 2"]);
        let data = fs::read(requests(&to, "Hotel 2")).unwrap();
        assert_eq!(crypt::decrypt(Some(&theirs), &data).unwrap(), b"{\"folders\":[]}");
    }
}
