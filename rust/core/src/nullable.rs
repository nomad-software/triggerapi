//! Reading the saved files as Go's encoding/json does, which takes `null`
//! for the zero value of a field rather than refusing it.

use serde::{Deserialize, Deserializer};

/// Reads a field that may be `null`, which is taken for its default.
pub fn de<'de, D, T>(deserializer: D) -> Result<T, D::Error>
where
    D: Deserializer<'de>,
    T: Default + Deserialize<'de>,
{
    Ok(Option::<T>::deserialize(deserializer)?.unwrap_or_default())
}

/// Reports if a value is its default, for leaving it out of a file as Go's
/// `omitempty` does.
pub fn is_default<T: Default + PartialEq>(value: &T) -> bool {
    *value == T::default()
}
