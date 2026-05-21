// cache/store.rs — file-backed persistence layer.
//
// Reads and writes ~/.play/cache.json, which Go populates via
// `play cache export -` (JSON output).
//
// Design constraints (mirrors Zig spec §4.2):
//   • Load entire file into memory at startup.
//   • Rewrite file on every update (atomic via tmp → rename).
//   • No database.
//
// Wire format note: Go uses "video_id", "source_type", and "last_used" as
// JSON keys.  Serde `alias` attributes accept both spellings on read;
// we write Go's spelling on save so the file stays compatible.

use std::path::PathBuf;
use serde::{Deserialize, Serialize};
use crate::error::PlayCacheError;
use crate::model::CacheEntry;

// ---------------------------------------------------------------------------
// Path helpers
// ---------------------------------------------------------------------------

/// Resolve the cache file path: `$HOME/.play/cache.json`.
fn cache_path() -> Result<PathBuf, PlayCacheError> {
    let home = std::env::var("HOME").map_err(|_| PlayCacheError::NoHomeDir)?;
    Ok(PathBuf::from(home).join(".play").join("cache.json"))
}

// ---------------------------------------------------------------------------
// Wire types
// ---------------------------------------------------------------------------

/// Flat JSON shape as written by Go.  `serde(alias)` accepts both spellings
/// so neither the file format nor this struct needs to change if Go renames
/// a field.
#[derive(Deserialize)]
struct JsonEntry {
    /// Accepts "id" or "video_id".
    #[serde(alias = "video_id", default)]
    id: String,

    #[serde(default)]
    title: String,

    /// Accepts "source" or "source_type".
    #[serde(alias = "source_type", default)]
    source: String,

    /// Accepts "timestamp" or "last_used".
    #[serde(alias = "last_used", default)]
    timestamp: i64,

    #[serde(default)]
    hit_count: u64,

    #[serde(default)]
    artist: String,

    #[serde(default)]
    query: String,
}

/// Output shape — matches Go's field names for round-trip compatibility.
#[derive(Serialize)]
struct WireOut<'a> {
    video_id:    &'a str,
    title:       &'a str,
    source_type: &'a str,
    last_used:   i64,
    hit_count:   u64,
    artist:      &'a str,
    query:       &'a str,
}

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

/// Load all entries from the cache JSON file.
///
/// A missing or empty file is not an error — the cache is simply empty.
/// Callers may freely push into the returned `Vec` and pass it to `save`.
pub fn load() -> Result<Vec<CacheEntry>, PlayCacheError> {
    let path = cache_path()?;

    let raw = match std::fs::read(&path) {
        Ok(b) => b,
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => return Ok(Vec::new()),
        Err(e) => return Err(e.into()),
    };

    if raw.is_empty() {
        return Ok(Vec::new());
    }

    // Parse as array; ignore unknown fields from Go's richer ManifestEntry.
    let json_entries: Vec<JsonEntry> = serde_json::from_slice(&raw)?;

    let entries = json_entries
        .into_iter()
        .filter_map(|je| {
            if je.id.is_empty() {
                return None; // skip malformed rows
            }
            Some(CacheEntry {
                id:        je.id,
                title:     je.title,
                source:    je.source,
                timestamp: je.timestamp,
                hit_count: je.hit_count,
                artist:    je.artist,
                query:     je.query,
            })
        })
        .collect();

    Ok(entries)
}

/// Persist entries to the cache JSON file.
///
/// Writes to a sibling `.tmp` file first, then renames for atomicity —
/// identical to the Zig implementation.
pub fn save(entries: &[CacheEntry]) -> Result<(), PlayCacheError> {
    let path = cache_path()?;

    if let Some(dir) = path.parent() {
        std::fs::create_dir_all(dir)?;
    }

    let tmp = path.with_extension("tmp");

    let wire: Vec<WireOut> = entries
        .iter()
        .map(|e| WireOut {
            video_id:    &e.id,
            title:       &e.title,
            source_type: &e.source,
            last_used:   e.timestamp,
            hit_count:   e.hit_count,
            artist:      &e.artist,
            query:       &e.query,
        })
        .collect();

    let json = serde_json::to_vec_pretty(&wire)?;

    // Write to the tmp file, then atomically rename.  If either step fails
    // we attempt to remove the tmp file so it does not accumulate on disk.
    if let Err(e) = std::fs::write(&tmp, &json) {
        let _ = std::fs::remove_file(&tmp); // best-effort cleanup
        return Err(e.into());
    }
    if let Err(e) = std::fs::rename(&tmp, &path) {
        let _ = std::fs::remove_file(&tmp); // best-effort cleanup
        return Err(e.into());
    }

    Ok(())
}
