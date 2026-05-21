// main.rs — play-cache CLI entry point.
//
// Flow (mirrors Zig spec §4.5):
//   1. Read JSON request from stdin.
//   2. Load cache file.
//   3. Dispatch command.
//   4. Write JSON response to stdout.
//   5. Exit.
//
// The heavy lifting lives in the library crate (lib.rs + sub-modules).
// This file is intentionally thin — it only handles IO and top-level error
// recovery so that the core logic stays testable without a process boundary.
//
// total_bytes accounting
// ----------------------
// The CLI process is short-lived (one request, one response, exit), so we
// scan the directory once per invocation in cmd_add rather than carrying a
// long-lived CacheHandle.  The cost is one readdir per "add" command.  That
// is acceptable: adds are rare relative to lookups, and lookups don't need
// the size at all.

use std::io::{self, Read, Write};

// Import from the library target of this same crate.
use play_cache::cache;
use play_cache::error::PlayCacheError;
use play_cache::model::CacheEntry;
use play_cache::parser::json::{self as proto, AddResult};

fn main() {
    // Read all of stdin up front — the request is small and we need it whole.
    let mut input = Vec::new();
    io::stdin()
        .read_to_end(&mut input)
        .expect("failed to read stdin");

    let response = run(&input).unwrap_or_else(|e| proto::write_error(&e.to_string()));

    let stdout = io::stdout();
    let mut lock = stdout.lock();
    let _ = lock.write_all(response.as_bytes());
    let _ = lock.write_all(b"\n");
}

fn run(input: &[u8]) -> Result<String, PlayCacheError> {
    // 1. Parse request.
    let req = proto::read_request(input).map_err(|_| {
        PlayCacheError::InvalidRequest("invalid JSON request".into())
    })?;

    if req.cmd.is_empty() {
        return Ok(proto::write_error("missing \"cmd\" field"));
    }

    // 2. Load cache file (empty Vec if file missing).
    let mut entries = cache::load()?;

    // 3. Dispatch.
    match req.cmd.as_str() {
        "lookup" => cmd_lookup(&mut entries, &req.query),
        "list"   => cmd_list(&entries),
        "stats"  => cmd_stats(&entries),
        "add"    => cmd_add(&mut entries, req.entry),
        other    => Ok(proto::write_error(&format!("unknown command: {other}"))),
    }
}

// ---------------------------------------------------------------------------
// Private helpers
// ---------------------------------------------------------------------------

fn now_unix() -> i64 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_secs() as i64)
        .unwrap_or(0)
}

// ---------------------------------------------------------------------------
// Command handlers
// ---------------------------------------------------------------------------

/// Look up entries matching `query` and promote their timestamps.
///
/// Promotion updates `timestamp` to `now` for every matched entry so that
/// recently-read tracks aren't treated as LRU eviction candidates.
/// Persists the updated timestamps so the order survives across invocations.
fn cmd_lookup(entries: &mut Vec<CacheEntry>, query: &str) -> Result<String, PlayCacheError> {
    let hits = cache::lookup(entries, query);

    let now = now_unix();
    let mut any_promoted = false;
    for hit in &hits {
        if cache::touch(entries, &hit.id, now) {
            any_promoted = true;
        }
    }

    if any_promoted {
        // Best-effort persist; a save failure must not fail a lookup.
        let _ = cache::save(entries);
    }

    Ok(proto::write_ok(&hits))
}

fn cmd_list(entries: &[CacheEntry]) -> Result<String, PlayCacheError> {
    Ok(proto::write_ok(entries))
}

fn cmd_stats(entries: &[CacheEntry]) -> Result<String, PlayCacheError> {
    Ok(proto::write_ok(cache::stats(entries)))
}

fn cmd_add(
    entries: &mut Vec<CacheEntry>,
    payload: Option<play_cache::parser::json::EntryPayload>,
) -> Result<String, PlayCacheError> {
    let p = payload.ok_or(PlayCacheError::MissingEntry)?;
    let entry = p.into_entry();

    cache::add(entries, entry);

    // Enforce the size cap.  The CLI process is short-lived so we do a single
    // directory scan here rather than maintaining a persistent counter.
    if let Some(dir) = cache::cache_dir() {
        let cap = cache::max_bytes();
        if cap > 0 {
            let mut total_bytes = cache::measure_dir_bytes(&dir);
            cache::enforce_cap(entries, &dir, &mut total_bytes, cap);
        }
    }

    cache::save(entries)?;
    Ok(proto::write_ok(AddResult { saved: entries.len() }))
}
