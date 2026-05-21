// cache/index.rs — pure query and mutation logic over an in-memory entry list.
//
// No IO here.  All functions take the slice (or mutable Vec) by reference;
// callers own persistence (see store.rs).

use crate::model::{CacheEntry, Stats};

// ---------------------------------------------------------------------------
// Lookup
// ---------------------------------------------------------------------------

/// Return every entry whose id, title, artist, query, or source contains
/// `query` (case-insensitive substring match).
///
/// An empty `query` returns a clone of the full list (mirrors Zig behaviour).
pub fn lookup(entries: &[CacheEntry], query: &str) -> Vec<CacheEntry> {
    if query.is_empty() {
        return entries.to_vec();
    }

    // Lower-case the query once; field comparisons lower-case per field.
    let lq = query.to_lowercase();

    entries
        .iter()
        .filter(|e| {
            field_contains(&e.id, &lq)
                || field_contains(&e.title, &lq)
                || field_contains(&e.artist, &lq)
                || field_contains(&e.query, &lq)
                || field_contains(&e.source, &lq)
        })
        .cloned()
        .collect()
}

/// Case-insensitive substring check.  `lq` must already be lowercase.
#[inline]
fn field_contains(field: &str, lq: &str) -> bool {
    !field.is_empty() && field.to_lowercase().contains(lq)
}

// ---------------------------------------------------------------------------
// Stats
// ---------------------------------------------------------------------------

/// Compute aggregate statistics across all entries.  Pure; no allocation.
pub fn stats(entries: &[CacheEntry]) -> Stats {
    Stats {
        cached: entries.len(),
        total_hits: entries.iter().map(|e| e.hit_count).sum(),
    }
}

// ---------------------------------------------------------------------------
// Add / upsert
// ---------------------------------------------------------------------------

/// Append a new entry, or update an existing one with the same id (upsert).
///
/// On upsert:
///   - Metadata fields (title, source, artist, query) are taken from `entry`.
///   - `hit_count` is preserved from the existing row so a caller that passes
///     the serde default (0) does not silently wipe the accumulated count.
///   - `timestamp` is always refreshed to the value in `entry`; callers must
///     set it to `now` (Unix seconds) so LRU order stays current.
///
/// Matches Go's `ON CONFLICT … DO UPDATE SET … last_used = excluded.last_used`
/// semantics and the Zig implementation.
pub fn add(entries: &mut Vec<CacheEntry>, entry: CacheEntry) {
    match entries.iter().position(|e| e.id == entry.id) {
        Some(i) => {
            // Preserve the accumulated hit count — the caller supplies 0 as the
            // serde default and must never silently wipe the real value.
            let preserved_hits = entries[i].hit_count;
            entries[i] = CacheEntry {
                hit_count: preserved_hits,
                ..entry
            };
        }
        None => entries.push(entry),
    }
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

#[cfg(test)]
mod tests {
    use super::*;
    use crate::model::CacheEntry;

    fn make(id: &str, title: &str, artist: &str) -> CacheEntry {
        CacheEntry {
            id: id.into(),
            title: title.into(),
            source: "search".into(),
            timestamp: 0,
            hit_count: 1,
            artist: artist.into(),
            query: String::new(),
        }
    }

    #[test]
    fn lookup_empty_query_returns_all() {
        let entries = vec![make("a", "Title A", ""), make("b", "Title B", "")];
        assert_eq!(lookup(&entries, "").len(), 2);
    }

    #[test]
    fn lookup_case_insensitive() {
        let entries = vec![make("dQw4w9WgXcQ", "Never Gonna Give You Up", "Rick Astley")];
        assert_eq!(lookup(&entries, "rick").len(), 1);
        assert_eq!(lookup(&entries, "RICK").len(), 1);
        assert_eq!(lookup(&entries, "zxzx").len(), 0);
    }

    #[test]
    fn add_upserts_on_same_id() {
        let mut entries = vec![make("x", "Old Title", "")];
        add(&mut entries, make("x", "New Title", ""));
        assert_eq!(entries.len(), 1);
        assert_eq!(entries[0].title, "New Title");
    }

    /// Bug fix: upsert must NOT wipe the existing hit_count when the incoming
    /// entry carries the serde default of 0.
    #[test]
    fn add_upsert_preserves_hit_count() {
        let mut existing = make("x", "Title", "");
        existing.hit_count = 42;
        let mut entries = vec![existing];

        // Simulate the caller supplying hit_count = 0 (the serde default).
        let mut incoming = make("x", "Title v2", "");
        incoming.hit_count = 0;
        add(&mut entries, incoming);

        assert_eq!(entries.len(), 1);
        assert_eq!(entries[0].hit_count, 42, "hit_count must be preserved on upsert");
        assert_eq!(entries[0].title, "Title v2", "metadata must be updated on upsert");
    }

    /// Bug fix: upsert must refresh the timestamp so LRU order is current.
    #[test]
    fn add_upsert_refreshes_timestamp() {
        let mut existing = make("x", "Title", "");
        existing.timestamp = 1_000_000;
        let mut entries = vec![existing];

        let mut incoming = make("x", "Title", "");
        incoming.timestamp = 9_999_999;
        add(&mut entries, incoming);

        assert_eq!(entries[0].timestamp, 9_999_999, "timestamp must be refreshed on upsert");
    }

    #[test]
    fn add_appends_new_id() {
        let mut entries = vec![make("x", "X", "")];
        add(&mut entries, make("y", "Y", ""));
        assert_eq!(entries.len(), 2);
    }

    #[test]
    fn stats_sums_hits() {
        let mut e = make("a", "A", "");
        e.hit_count = 3;
        let mut f = make("b", "B", "");
        f.hit_count = 7;
        let s = stats(&[e, f]);
        assert_eq!(s.cached, 2);
        assert_eq!(s.total_hits, 10);
    }
}
