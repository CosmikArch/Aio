// parser/json.rs — stdin/stdout JSON protocol.
//
// Input:  { "cmd": "...", "query": "...", "entry": { ... } }
// Output: { "ok": true,  "data": ... }
//      or { "ok": false, "error": "..." }
//
// All stdout writes are centralised here.  No other module writes to stdout.

use serde::{Deserialize, Serialize};
use crate::model::CacheEntry;

/// Return the current Unix timestamp in seconds.
fn now_unix() -> i64 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_secs() as i64)
        .unwrap_or(0)
}

// ---------------------------------------------------------------------------
// Request
// ---------------------------------------------------------------------------

#[derive(Debug, Deserialize)]
pub struct Request {
    #[serde(default)]
    pub cmd: String,

    #[serde(default)]
    pub query: String,

    pub entry: Option<EntryPayload>,
}

/// Flat JSON shape for an entry supplied with the "add" command.
/// Accepts both Rust-conventional names and Go's exported names via aliases.
#[derive(Debug, Deserialize)]
pub struct EntryPayload {
    #[serde(alias = "video_id", default)]
    pub id: String,

    #[serde(default)]
    pub title: String,

    #[serde(alias = "source_type", default)]
    pub source: String,

    #[serde(alias = "last_used", default)]
    pub timestamp: i64,

    #[serde(default)]
    pub hit_count: u64,

    #[serde(default)]
    pub artist: String,

    #[serde(default)]
    pub query: String,
}

impl EntryPayload {
    /// Convert a parsed payload into an owned `CacheEntry`.
    ///
    /// If the caller did not supply a `timestamp` / `last_used` value the serde
    /// default of `0` (Unix epoch 1970) is replaced with the current time so
    /// that freshly-added entries are not the first LRU eviction candidates.
    pub fn into_entry(self) -> CacheEntry {
        let timestamp = if self.timestamp == 0 {
            now_unix()
        } else {
            self.timestamp
        };
        CacheEntry {
            id:        self.id,
            title:     self.title,
            source:    self.source,
            timestamp,
            hit_count: self.hit_count,
            artist:    self.artist,
            query:     self.query,
        }
    }
}

/// Parse a JSON request from raw bytes.
pub fn read_request(input: &[u8]) -> Result<Request, serde_json::Error> {
    serde_json::from_slice(input)
}

// ---------------------------------------------------------------------------
// Response
// ---------------------------------------------------------------------------

#[derive(Serialize)]
struct Response<'a, T: Serialize> {
    ok: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    data: Option<T>,
    #[serde(skip_serializing_if = "Option::is_none")]
    error: Option<&'a str>,
}

/// Serialise a success response.  Returns a fallback string on serialisation
/// failure — stdout must always receive valid JSON.
pub fn write_ok<T: Serialize>(data: T) -> String {
    serde_json::to_string(&Response { ok: true, data: Some(data), error: None })
        .unwrap_or_else(|_| r#"{"ok":false,"error":"serialisation failed"}"#.into())
}

/// Serialise an error response.
pub fn write_error(msg: &str) -> String {
    serde_json::to_string(&Response::<()> {
        ok: false,
        data: None,
        error: Some(msg),
    })
    .unwrap_or_else(|_| r#"{"ok":false,"error":"serialisation failed"}"#.into())
}

// ---------------------------------------------------------------------------
// Helpers used by main.rs
// ---------------------------------------------------------------------------

/// Result payload for the "add" command.
#[derive(Serialize)]
pub struct AddResult {
    pub saved: usize,
}
