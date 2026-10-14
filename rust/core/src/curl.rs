//! The curl command which makes a request, and running it.

use std::time::Instant;

use crate::rest::{self, Request, Response, AUTH_BASIC};

impl Request {
    /// Returns a curl command which makes the request, to be run in a POSIX
    /// shell. It is made from the request as built, so it sends the address,
    /// headers and body that `send` would. Placeholders should have been
    /// expanded first.
    pub fn curl(&self) -> Result<String, String> {
        let options = self.curl_options()?;
        // The first line is the command with the options that say how the
        // request is made, and its address. Each option after that has a
        // line.
        let mut lines = vec![String::from("curl")];
        for (i, option) in options.iter().enumerate() {
            let words: Vec<String> = option.iter().map(|w| shell_quote(w)).collect();
            if i == 0 {
                lines[0] += &format!(" {}", words.join(" "));
            } else {
                lines.push(words.join(" "));
            }
        }
        Ok(lines.join(" \\\n  "))
    }

    /// Returns the arguments of the curl command which makes the request, an
    /// option and its value together. The first holds the options which say
    /// how the request is made, followed by its address.
    fn curl_options(&self) -> Result<Vec<Vec<String>>, String> {
        let mut built = self.build()?;
        let address = built.url.to_string();
        let content = built.body.clone();

        // The command follows redirects if `send` would, and gives up when
        // it would.
        let mut first = Vec::new();
        if !self.options.no_redirects {
            first.push("--location".to_string());
        }
        if let Some(timeout) = self.options.timeout {
            first.push("--max-time".into());
            first.push(format_seconds(timeout.as_secs_f64()));
        }
        match (built.method.as_str(), content.is_empty()) {
            // This is what curl does when it is not told otherwise.
            ("GET", true) => {}
            ("HEAD", true) => first.push("--head".into()),
            _ => {
                first.push("--request".into());
                first.push(built.method.clone());
            }
        }
        // Braces and brackets in an address would be taken by curl for a
        // range of addresses.
        if address.contains(['{', '}', '[', ']']) {
            first.push("--globoff".into());
        }
        first.push(address);
        let mut options = vec![first];

        // Basic authorization is given as the name and password where it
        // can be, which is easier to read than the header made from them.
        if let Some(value) = self.auth.header() {
            if self.auth.kind == AUTH_BASIC
                && !self.auth.username.contains(':')
                && built.headers.get("Authorization") == Some(&vec![value])
            {
                built.headers.remove("Authorization");
                options.push(vec!["--user".into(), format!("{}:{}", self.auth.username, self.auth.password)]);
            }
        }

        let url_host = match (built.url.host_str(), built.url.port()) {
            (Some(h), Some(p)) => format!("{h}:{p}"),
            (Some(h), None) => h.to_string(),
            _ => String::new(),
        };
        if let Some(host) = &built.host {
            if !host.is_empty() && *host != url_host {
                options.push(vec!["--header".into(), format!("Host: {host}")]);
            }
        }
        for (name, values) in &built.headers {
            for value in values {
                // This is how curl is told to send a header with no value.
                let header = if value.is_empty() { format!("{name};") } else { format!("{name}: {value}") };
                options.push(vec!["--header".into(), header]);
            }
        }

        if !content.is_empty() {
            // Unlike --data, this does not read a file when the body starts
            // with an @.
            options.push(vec!["--data-raw".into(), content]);
        }
        Ok(options)
    }
}

/// Writes seconds as Go's `strconv.FormatFloat(s, 'f', -1, 64)` does.
fn format_seconds(seconds: f64) -> String {
    let text = format!("{seconds}");
    text.strip_suffix(".0").map(str::to_string).unwrap_or(text)
}

/// The exit status of curl when a request has taken longer than it was
/// allowed.
const CURL_TIMED_OUT: i32 = 28;

/// Makes the request by running the curl command that `curl` describes, and
/// returns the response it was given. The curl program must be installed.
/// Dropping the future stops curl.
pub async fn run_curl(request: &Request) -> Result<Response, String> {
    let options = request.curl_options()?;
    let program = which("curl").ok_or("curl is not installed, or is not on the PATH")?;

    // The headers are written to a file, which keeps them apart from the
    // body however the response is made up.
    let file = tempfile::Builder::new()
        .prefix("triggerapi-headers-")
        .tempfile()
        .map_err(|e| e.to_string())?;
    let path = file.path().to_path_buf();

    // The progress meter is left out, but not the reason for a failure.
    let mut args: Vec<String> = vec!["--silent".into(), "--show-error".into(), "--dump-header".into(), path.display().to_string()];
    let mut head = false;
    for option in options {
        head = head || option.iter().any(|o| o == "--head");
        args.extend(option);
    }

    let start = Instant::now();
    let output = tokio::process::Command::new(program)
        .args(&args)
        .kill_on_drop(true)
        .stdin(std::process::Stdio::null())
        .output()
        .await
        .map_err(|e| format!("curl: {e}"))?;
    let duration = start.elapsed();

    if output.status.code() == Some(CURL_TIMED_OUT) {
        return Err(rest::TIMED_OUT.into());
    }
    if !output.status.success() {
        let problem = String::from_utf8_lossy(&output.stderr).trim().to_string();
        if !problem.is_empty() {
            return Err(problem);
        }
        return Err(format!("curl: {}", output.status));
    }

    let headers = std::fs::read_to_string(&path).map_err(|e| e.to_string())?;
    let mut res = parse_curl_headers(&headers)?;
    // When only the headers are asked for, curl prints them as if they
    // were the body.
    if !head {
        res.body = output.stdout;
    }
    res.duration = duration;
    Ok(res)
}

/// Finds a program on the PATH.
fn which(program: &str) -> Option<std::path::PathBuf> {
    let path = std::env::var_os("PATH")?;
    std::env::split_paths(&path)
        .map(|dir| dir.join(program))
        .find(|candidate| candidate.is_file())
}

/// Reads the status and headers of a response from what curl wrote of
/// them. There is a set for each response on the way to the last, such as a
/// redirect, and it is the last that is read.
pub fn parse_curl_headers(dump: &str) -> Result<Response, String> {
    let mut last: Vec<&str> = Vec::new();
    let normalised = dump.replace("\r\n", "\n");
    for line in normalised.split('\n') {
        if line.starts_with("HTTP/") {
            last.clear();
        }
        if !line.is_empty() {
            last.push(line);
        }
    }
    if last.is_empty() || !last[0].starts_with("HTTP/") {
        return Err("curl did not report a response".into());
    }

    let (proto, status) = last[0].split_once(' ').unwrap_or((last[0], ""));
    let status = status.trim();
    let (code, text) = status.split_once(' ').unwrap_or((status, ""));
    let status_code: u16 = code.parse().unwrap_or(0);
    let mut res = Response {
        status: status.to_string(),
        status_code,
        proto: proto.to_string(),
        ..Default::default()
    };
    // Only HTTP/1 has the reason after the code.
    if text.is_empty() {
        res.status = rest::status_text(status_code);
    }
    // Named as `send` names them.
    if !proto.contains('.') {
        res.proto = format!("{proto}.0");
    }
    for line in &last[1..] {
        if let Some((name, value)) = line.split_once(':') {
            res.headers.push((name.trim().to_string(), value.trim().to_string()));
        }
    }
    Ok(res)
}

/// Returns `s` as a single word for a POSIX shell.
pub fn shell_quote(s: &str) -> String {
    let plain = |c: char| c.is_ascii_alphanumeric() || "-_./:=@%+,".contains(c);
    if !s.is_empty() && s.chars().all(plain) {
        return s.to_string();
    }
    format!("'{}'", s.replace('\'', r"'\''"))
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::rest::{Auth, KeyValue, Options};
    use std::time::Duration;

    #[test]
    fn makes_a_command() {
        let r = Request {
            method: "POST".into(),
            url: "example.com/a".into(),
            auth: Auth { kind: AUTH_BASIC.into(), username: "me".into(), password: "p w".into(), ..Default::default() },
            headers: vec![KeyValue::new("X-Empty", "", true)],
            body_type: "json".into(),
            body: "{\"a\":'1'}".into(),
            options: Options { timeout: Some(Duration::from_millis(1500)), no_redirects: false },
            ..Default::default()
        };
        let got = r.curl().unwrap();
        assert_eq!(
            got,
            "curl --location --max-time 1.5 --request POST http://example.com/a \\\n  --user 'me:p w' \\\n  --header 'Content-Type: application/json' \\\n  --header 'X-Empty;' \\\n  --data-raw '{\"a\":'\\''1'\\''}'"
        );
    }

    #[test]
    fn reads_the_last_response() {
        let res = parse_curl_headers("HTTP/1.1 301 Moved\r\nLocation: x\r\n\r\nHTTP/2 200\r\ncontent-type: text/plain\r\n\r\n").unwrap();
        assert_eq!(res.status, "200 OK");
        assert_eq!(res.proto, "HTTP/2.0");
        assert_eq!(res.headers, vec![("content-type".to_string(), "text/plain".to_string())]);
    }
}
