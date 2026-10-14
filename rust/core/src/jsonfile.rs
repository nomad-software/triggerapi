//! Reads and writes values as indented JSON files, which can be encrypted.

use std::io::Write;
use std::path::Path;

use serde::de::DeserializeOwned;
use serde::Serialize;

use crate::crypt::{self, Keys};

/// Reads the JSON file at path, decrypting it with keys if it is encrypted.
/// A missing file is not an error and gives `None`.
pub fn load_with<T: DeserializeOwned>(path: &Path, keys: Option<&Keys>) -> Result<Option<T>, String> {
    let data = match std::fs::read(path) {
        Ok(data) => data,
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => return Ok(None),
        Err(e) => return Err(format!("{}: {e}", path.display())),
    };
    let data = crypt::decrypt(keys, &data)?;
    serde_json::from_slice(&data).map(Some).map_err(|e| e.to_string())
}

/// Returns a value as the JSON the files hold: indented by two spaces, with
/// a line ending after it.
pub fn to_text<T: Serialize>(v: &T) -> Result<Vec<u8>, String> {
    let mut data = serde_json::to_vec_pretty(v).map_err(|e| e.to_string())?;
    data.push(b'\n');
    Ok(data)
}

/// Writes `v` to the JSON file at path, encrypted with keys unless there are
/// none, replacing it atomically.
pub fn save_with<T: Serialize>(path: &Path, v: &T, keys: Option<&Keys>) -> Result<(), String> {
    let data = crypt::encrypt(keys, &to_text(v)?)?;
    write(path, &data)
}

/// Has the file at path, which was written with the keys `from`, written
/// with the keys `to`: encrypted, decrypted, or encrypted with other keys. A
/// file that is missing, or already as it is to be, is left alone.
pub fn convert(path: &Path, from: Option<&Keys>, to: Option<&Keys>) -> Result<(), String> {
    let data = match std::fs::read(path) {
        Ok(data) => data,
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => return Ok(()),
        Err(e) => return Err(e.to_string()),
    };
    let mut plain = data.clone();
    if crypt::encrypted(&data) {
        plain = match crypt::decrypt(from, &data) {
            Ok(p) => p,
            Err(e) => {
                // A file which the keys it is to have can already read, as
                // one left by a conversion that did not finish, is not a
                // failure of this one.
                if to.is_some() && crypt::decrypt(to, &data).is_ok() {
                    return Ok(());
                }
                return Err(e);
            }
        };
        // A file encrypted with the keys it is to have is left as it is.
        if to.is_some() && crypt::decrypt(to, &data).is_ok() {
            return Ok(());
        }
    }
    let converted = crypt::encrypt(to, &plain)?;
    if converted == data {
        return Ok(());
    }
    write(path, &converted)
}

/// Replaces the file at path atomically. The file can be read and written by
/// the user alone.
pub fn write(path: &Path, data: &[u8]) -> Result<(), String> {
    let dir = path.parent().filter(|p| !p.as_os_str().is_empty()).unwrap_or(Path::new("."));
    let name = path.file_name().map(|n| n.to_string_lossy().into_owned()).unwrap_or_default();
    // A temporary file is made with just those permissions.
    let mut tmp = tempfile::Builder::new()
        .prefix(&format!("{name}."))
        .suffix(".tmp")
        .tempfile_in(dir)
        .map_err(|e| e.to_string())?;
    tmp.write_all(data).map_err(|e| e.to_string())?;
    tmp.persist(path).map_err(|e| e.to_string())?;
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::collections::BTreeMap;

    #[test]
    fn saves_and_loads() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("a.json");
        assert_eq!(load_with::<BTreeMap<String, i32>>(&path, None).unwrap(), None);
        let v = BTreeMap::from([("a".to_string(), 1)]);
        save_with(&path, &v, None).unwrap();
        assert_eq!(std::fs::read_to_string(&path).unwrap(), "{\n  \"a\": 1\n}\n");
        assert_eq!(load_with::<BTreeMap<String, i32>>(&path, None).unwrap(), Some(v.clone()));

        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            assert_eq!(std::fs::metadata(&path).unwrap().permissions().mode() & 0o777, 0o600);
        }

        let (public, private) = crypt::test_pair(dir.path(), "k", "");
        let keys = Keys::open(&public, &private, &mut || None).unwrap();
        convert(&path, None, Some(&keys)).unwrap();
        assert!(crypt::encrypted(&std::fs::read(&path).unwrap()));
        assert_eq!(load_with::<BTreeMap<String, i32>>(&path, Some(&keys)).unwrap(), Some(v.clone()));
        convert(&path, Some(&keys), None).unwrap();
        assert_eq!(std::fs::read_to_string(&path).unwrap(), "{\n  \"a\": 1\n}\n");
    }
}
