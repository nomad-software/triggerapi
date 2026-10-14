//! Encrypts and decrypts the saved files with a pair of SSH keys, using age,
//! as armoured text, so that the files are those the Go program writes.

use std::io::{Read, Write};
use std::path::{Path, PathBuf};
use std::str::FromStr;
use std::sync::Arc;

use age::armor::{ArmoredReader, ArmoredWriter, Format};
use age::secrecy::SecretString;

/// What a file that age has encrypted can start with: armoured, as they are
/// written here, or not.
const INTROS: &[&[u8]] = &[b"-----BEGIN AGE ENCRYPTED FILE-----", b"age-encryption.org/v1\n"];

/// A pair of SSH keys: the public one, which data is encrypted to, and the
/// private one, which decrypts it. Where there are no keys (`None`), data
/// that is not encrypted is left as it is, and data that is cannot be read.
#[derive(Clone)]
pub struct Keys {
    recipient: Option<Arc<age::ssh::Recipient>>,
    identity: Option<Arc<age::ssh::Identity>>,
    /// Why the keys cannot be used, if they cannot.
    err: Option<String>,
}

impl std::fmt::Debug for Keys {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("Keys").field("err", &self.err).finish_non_exhaustive()
    }
}

/// Returns a path with the `~` it starts with, if it does, replaced by the
/// home folder of the user.
pub fn home(path: &str) -> PathBuf {
    if let Some(rest) = path.strip_prefix('~') {
        if rest.is_empty() || rest.starts_with(std::path::is_separator) {
            if let Some(dir) = dirs::home_dir() {
                return dir.join(rest.trim_start_matches(std::path::is_separator));
            }
        }
    }
    PathBuf::from(path)
}

fn read_recipient(public: &str) -> Result<age::ssh::Recipient, String> {
    let path = home(public);
    let line = std::fs::read_to_string(&path).map_err(|e| format!("the public key could not be read: {e}"))?;
    age::ssh::Recipient::from_str(line.trim())
        .map_err(|e| format!("{public} is not a public key that can be used: {e:?}"))
}

impl Keys {
    /// Reads a pair of keys from the files of the public and of the private
    /// key, whose paths can start with `~` for the home folder, and checks
    /// that one decrypts what the other encrypts. The passphrase is asked
    /// for if the private key is protected by one, and not again
    /// afterwards; `passphrase` returns `None` if the user gives none.
    pub fn open(public: &str, private: &str, passphrase: &mut dyn FnMut() -> Option<String>) -> Result<Keys, String> {
        if public.is_empty() || private.is_empty() {
            return Err("both a public and a private key are needed".into());
        }
        let recipient = read_recipient(public)?;

        let pem = std::fs::read(home(private)).map_err(|e| format!("the private key could not be read: {e}"))?;
        let identity = age::ssh::Identity::from_buffer(&pem[..], Some(private.to_string()))
            .map_err(|e| format!("{private} is not a private key that can be used: {e}"))?;
        let identity = match identity {
            age::ssh::Identity::Unencrypted(_) => identity,
            age::ssh::Identity::Encrypted(key) => {
                let given = passphrase().ok_or("no passphrase was given for the private key")?;
                let key = key
                    .decrypt(SecretString::from(given))
                    .map_err(|_| format!("{private} is not a private key that can be used: the passphrase is not right"))?;
                age::ssh::Identity::from(key)
            }
            age::ssh::Identity::Unsupported(k) => {
                return Err(format!("{private} is not a private key that can be used: {k:?}"));
            }
        };

        let keys = Keys { recipient: Some(Arc::new(recipient)), identity: Some(Arc::new(identity)), err: None };
        keys.check()?;
        Ok(keys)
    }

    /// Reads the public key of someone else from its file. The keys returned
    /// encrypt to it, and decrypt nothing, as that takes the private key.
    pub fn recipient(public: &str) -> Result<Keys, String> {
        if public.is_empty() {
            return Err("a public key is needed".into());
        }
        Ok(Keys { recipient: Some(Arc::new(read_recipient(public)?)), identity: None, err: None })
    }

    /// Returns keys which fail, for the reason given, to encrypt or decrypt
    /// anything. They stand for keys the settings name which could not be
    /// opened, so that nothing is then saved without being encrypted.
    pub fn unusable(err: impl Into<String>) -> Keys {
        Keys { recipient: None, identity: None, err: Some(err.into()) }
    }

    /// Returns why the keys cannot be used, if they cannot.
    pub fn err(&self) -> Option<&str> {
        self.err.as_deref()
    }

    /// Encrypts something and decrypts it again, which fails if the keys
    /// are not a pair.
    fn check(&self) -> Result<(), String> {
        const SAMPLE: &[u8] = b"triggerapi";
        let encrypted = self.encrypt_data(SAMPLE)?;
        match self.decrypt_data(&encrypted) {
            Err(e) if e.contains("No matching keys") || e.contains("no-matching-keys") || e.contains("NoMatchingKeys") => {
                Err("the private key is not the one that goes with the public key".into())
            }
            Err(e) => Err(e),
            Ok(d) if d != SAMPLE => Err("the keys do not decrypt what they encrypt".into()),
            Ok(_) => Ok(()),
        }
    }

    fn encrypt_data(&self, data: &[u8]) -> Result<Vec<u8>, String> {
        if let Some(e) = &self.err {
            return Err(e.clone());
        }
        let recipient = self.recipient.as_ref().ok_or("there is no public key to encrypt with")?;
        let fail = |e: &dyn std::fmt::Display| format!("encrypting: {e}");
        let encryptor = age::Encryptor::with_recipients(std::iter::once(recipient.as_ref() as &dyn age::Recipient))
            .map_err(|e| fail(&e))?;
        let mut out = Vec::new();
        let armoured = ArmoredWriter::wrap_output(&mut out, Format::AsciiArmor).map_err(|e| fail(&e))?;
        let mut writer = encryptor.wrap_output(armoured).map_err(|e| fail(&e))?;
        writer.write_all(data).map_err(|e| fail(&e))?;
        writer.finish().and_then(|a| a.finish()).map_err(|e| fail(&e))?;
        Ok(out)
    }

    fn decrypt_data(&self, data: &[u8]) -> Result<Vec<u8>, String> {
        let identity = self
            .identity
            .as_ref()
            .ok_or("it is encrypted, and there is no private key to decrypt it with")?;
        let decryptor = age::Decryptor::new(ArmoredReader::new(data)).map_err(|e| format!("decrypting: {e}"))?;
        let mut reader = decryptor
            .decrypt(std::iter::once(identity.as_ref() as &dyn age::Identity))
            .map_err(|e| match e {
                age::DecryptError::NoMatchingKeys => "decrypting: NoMatchingKeys".to_string(),
                e => format!("decrypting: {e}"),
            })?;
        let mut out = Vec::new();
        reader.read_to_end(&mut out).map_err(|e| format!("decrypting: {e}"))?;
        Ok(out)
    }
}

/// Reports if data is encrypted.
pub fn encrypted(data: &[u8]) -> bool {
    INTROS.iter().any(|intro| data.starts_with(intro))
}

/// Returns data encrypted to the public key, as text. With no keys it is
/// returned as it is.
pub fn encrypt(keys: Option<&Keys>, data: &[u8]) -> Result<Vec<u8>, String> {
    match keys {
        None => Ok(data.to_vec()),
        Some(k) => k.encrypt_data(data),
    }
}

/// Returns what data was before it was encrypted. Data that is not
/// encrypted is returned as it is, so that files from before there were
/// keys can still be read.
pub fn decrypt(keys: Option<&Keys>, data: &[u8]) -> Result<Vec<u8>, String> {
    if let Some(e) = keys.and_then(|k| k.err.clone()) {
        return Err(e);
    }
    if !encrypted(data) {
        return Ok(data.to_vec());
    }
    match keys {
        None => Err("it is encrypted, and the settings have no keys to decrypt it with".into()),
        Some(k) => k.decrypt_data(data).map_err(|e| {
            if e.ends_with("NoMatchingKeys") {
                "decrypting: no identity matched any of the recipients".to_string()
            } else {
                e
            }
        }),
    }
}

/// Writes a new pair of Ed25519 keys to files named `name` and `name.pub` in
/// a folder with `ssh-keygen`, for tests, and returns the paths of the
/// public and of the private key.
pub fn test_pair(dir: &Path, name: &str, passphrase: &str) -> (String, String) {
    let private = dir.join(name);
    let status = std::process::Command::new("ssh-keygen")
        .args(["-q", "-t", "ed25519", "-N", passphrase, "-C", "", "-f"])
        .arg(&private)
        .status()
        .expect("ssh-keygen runs");
    assert!(status.success());
    (format!("{}.pub", private.display()), private.display().to_string())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn round_trips_and_checks_pairs() {
        let dir = tempfile::tempdir().unwrap();
        let (public, private) = test_pair(dir.path(), "a", "");
        let keys = Keys::open(&public, &private, &mut || None).unwrap();
        let data = encrypt(Some(&keys), b"hello").unwrap();
        assert!(data.starts_with(b"-----BEGIN AGE ENCRYPTED FILE-----"));
        assert!(encrypted(&data));
        assert_eq!(decrypt(Some(&keys), &data).unwrap(), b"hello");
        assert_eq!(decrypt(Some(&keys), b"plain").unwrap(), b"plain");
        assert!(decrypt(None, &data).is_err());

        let (other_public, _) = test_pair(dir.path(), "b", "");
        let err = Keys::open(&other_public, &private, &mut || None).unwrap_err();
        assert_eq!(err, "the private key is not the one that goes with the public key");

        let theirs = Keys::recipient(&other_public).unwrap();
        let for_them = encrypt(Some(&theirs), b"x").unwrap();
        assert!(decrypt(Some(&keys), &for_them).is_err());
    }

    #[test]
    fn asks_for_a_passphrase_once() {
        let dir = tempfile::tempdir().unwrap();
        let (public, private) = test_pair(dir.path(), "p", "secret");
        let mut asked = 0;
        let keys = Keys::open(&public, &private, &mut || {
            asked += 1;
            Some("secret".into())
        })
        .unwrap();
        let data = encrypt(Some(&keys), b"x").unwrap();
        assert_eq!(decrypt(Some(&keys), &data).unwrap(), b"x");
        assert_eq!(asked, 1);
        assert!(Keys::open(&public, &private, &mut || Some("wrong".into())).is_err());
    }
}
