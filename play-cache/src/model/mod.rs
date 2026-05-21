// model/mod.rs — data types shared across the play-cache crate.
//
// Mirrors the Zig model.zig; field names use Rust conventions throughout.
// Serde derives cover both internal use and the JSON protocol output.

use serde::{Deserialize, Serialize};

/// A single cached media entry.  All strings are owned; the struct is cheaply
/// clone-able so lookup results can be returned as `Vec<CacheEntry>` without
/// lifetime gymnastics.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CacheEntry {
    /// YouTube video ID — index key and filename stem.
    pub id: String,
    /// Track / video title.
    pub title: String,
    /// Origin: "search" | "url" | "id"
    pub source: String,
    /// Unix epoch — last_used from the DB export.
    pub timestamp: i64,
    /// Aggregate hit count across all keys for this video.
    pub hit_count: u64,
    /// May be empty.
    pub artist: String,
    /// Human search string; may be empty.
    pub query: String,
}

/// Aggregate statistics across the in-memory entry list.
#[derive(Debug, Serialize)]
pub struct Stats {
    pub cached: usize,
    pub total_hits: u64,
}
