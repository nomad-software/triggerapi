//! Setting out the JSON of a body by hand.

use crate::env::find_placeholders;

/// Returns JSON set out with each member and element on a line of its own,
/// indented by two spaces, as a response is shown. `{{name}}` placeholders
/// are left as they are, and may stand for a value as well as be inside a
/// string: `{"id": {{id}}}` can be set out though it is not JSON until the
/// placeholder has been replaced. Text that is nothing but space is
/// returned as it is.
pub fn format_json(text: &str) -> Result<String, String> {
    if text.trim().is_empty() {
        return Ok(text.to_string());
    }

    // Each placeholder is swapped for a number while the text is set out,
    // as a number is allowed both inside a string and as a value. The
    // numbers start with digits that are nowhere in the text, so that
    // putting the placeholders back changes nothing else.
    let mut prefix = String::from("7309185264");
    while text.contains(&prefix) {
        prefix.push('1');
    }

    let mut swapped = String::new();
    let mut back: Vec<(String, String)> = Vec::new();
    let mut last = 0;
    for (i, (start, end)) in find_placeholders(text).into_iter().enumerate() {
        let number = format!("{prefix}{i:06}");
        swapped.push_str(&text[last..start]);
        swapped.push_str(&number);
        back.push((number, text[start..end].to_string()));
        last = end;
    }
    swapped.push_str(&text[last..]);

    let mut out = crate::json::indent(swapped.trim(), "  ")?;
    for (number, placeholder) in back {
        out = out.replace(&number, &placeholder);
    }
    Ok(out)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn keeps_placeholders() {
        assert_eq!(
            format_json(r#"{"id": {{id}}, "name": "{{ name }}"}"#).unwrap(),
            "{\n  \"id\": {{id}},\n  \"name\": \"{{ name }}\"\n}"
        );
        assert!(format_json("{").is_err());
        assert_eq!(format_json("  ").unwrap(), "  ");
    }
}
