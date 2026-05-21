// lib.rs — play-cache library crate root.
//
// Two responsibilities:
//   1. Declare internal modules (visible to both this lib and the CLI binary).
//   2. Export a narrow C ABI surface for Go/CGO consumption.
//
// FFI design principles applied here:
//   • Opaque handle — callers never touch internals.
//   • Rust owns all allocations; Go never frees Rust memory directly.
//   • Strings cross the boundary as null-terminated C strings.
//   • Only four verbs: open, get, put, close (+ free_string).
//   • Every unsafe block is documented.
//
// LRU accounting
// --------------
// `CacheHandle` carries a `total_bytes` counter that is:
//   • Initialised by `evict::measure_dir_bytes` once at `cache_open`.
//   • Incremented by the on-disk file size in `cache_put` on a successful add.
//   • Decremented inside `evict::enforce_cap` as files are removed.
//
// This means the directory is scanned exactly once per process lifetime.
// Counter drift (from out-of-process file changes) is bounded to the current
// session and self-corrects on the next `cache_open`.

pub mod cache;
pub mod error;
pub mod model;
pub mod parser;

use std::ffi::{CStr, CString};
use std::os::raw::c_char;

// ---------------------------------------------------------------------------
// Opaque handle
// ---------------------------------------------------------------------------

/// Opaque cache state.  The pointer is allocated via `Box::into_raw` and must
/// be freed with `cache_close`.  cbindgen emits a forward declaration so Go
/// can hold a `*CacheHandle` without knowing its layout.
pub struct CacheHandle {
    entries:     Vec<model::CacheEntry>,
    /// Running total of on-disk audio bytes.  Maintained incrementally so we
    /// never re-scan the directory after `cache_open`.
    total_bytes: u64,
}

// ---------------------------------------------------------------------------
// FFI surface
// ---------------------------------------------------------------------------

/// Open the cache from `$HOME/.play/cache.json`.
///
/// Scans the audio directory once to initialise the `total_bytes` counter.
/// Returns an opaque handle on success, or NULL if the file cannot be loaded.
/// Must be paired with `cache_close`.
#[no_mangle]
pub extern "C" fn cache_open() -> *mut CacheHandle {
    let entries = match cache::load() {
        Ok(e) => e,
        Err(_) => return std::ptr::null_mut(),
    };

    // Measure directory size once at open time.  After this, total_bytes is
    // maintained incrementally — no further directory scans needed.
    let total_bytes = cache::cache_dir()
        .map(|d| cache::measure_dir_bytes(&d))
        .unwrap_or(0);

    Box::into_raw(Box::new(CacheHandle { entries, total_bytes }))
}

/// Look up entries whose id, title, artist, query, or source contains `query`
/// (case-insensitive substring).  An empty `query` returns all entries.
///
/// **Access promotion**: every matched entry has its `timestamp` updated to
/// `now` so that recently-read entries are not evicted first.  The updated
/// timestamps are persisted to disk so promotion survives across sessions.
///
/// Returns a JSON array string that **must** be freed with `cache_free_string`.
/// Returns NULL on invalid arguments or internal error.
///
/// # Safety
/// `handle` must be a valid pointer obtained from `cache_open`.
/// `query`  must be a valid null-terminated UTF-8 string.
#[no_mangle]
pub unsafe extern "C" fn cache_get(
    handle: *mut CacheHandle,   // mut: we promote timestamps
    query:  *const c_char,
) -> *mut c_char {
    if handle.is_null() || query.is_null() {
        return std::ptr::null_mut();
    }

    // SAFETY: caller guarantees `handle` is valid and `query` is null-terminated.
    let h = &mut *handle;
    let q = match CStr::from_ptr(query).to_str() {
        Ok(s) => s,
        Err(_) => return std::ptr::null_mut(),
    };

    let now = now_unix();
    let hits = cache::lookup(&h.entries, q);

    // Promote every matched entry so it isn't a premature eviction candidate.
    // We promote before serialising so the returned JSON carries the fresh
    // timestamps too.
    let mut any_promoted = false;
    for hit in &hits {
        if cache::touch(&mut h.entries, &hit.id, now) {
            any_promoted = true;
        }
    }

    // Persist promotions so LRU order survives process restarts.
    if any_promoted {
        let _ = cache::save(&h.entries);
    }

    let json = serde_json::to_string(&hits).unwrap_or_default();

    // CString::new fails only if `json` contains interior NULs, which
    // serde_json never produces for valid UTF-8 strings.
    match CString::new(json) {
        Ok(cs) => cs.into_raw(),
        Err(_) => std::ptr::null_mut(),
    }
}

/// Add or update an entry.  `entry_json` must be a JSON object whose fields
/// match `model::CacheEntry` (id, title, source, timestamp, hit_count,
/// artist, query).  Also accepts Go's alternate key names (video_id,
/// source_type, last_used).
///
/// Enforces the 500 MB size cap immediately after inserting via LRU eviction.
/// `total_bytes` is updated incrementally — no directory re-scan.
///
/// Persists the updated list to disk.
///
/// Returns 0 on success, -1 on error.
///
/// # Safety
/// `handle`     must be a valid pointer obtained from `cache_open`.
/// `entry_json` must be a valid null-terminated UTF-8 JSON string.
#[no_mangle]
pub unsafe extern "C" fn cache_put(
    handle:     *mut CacheHandle,
    entry_json: *const c_char,
) -> i32 {
    if handle.is_null() || entry_json.is_null() {
        return -1;
    }

    let h = &mut *handle;

    let s = match CStr::from_ptr(entry_json).to_str() {
        Ok(s) => s,
        Err(_) => return -1,
    };

    // We accept both Rust-conventional and Go-conventional field names here
    // by deserialising through EntryPayload (which carries the aliases).
    let payload: parser::json::EntryPayload = match serde_json::from_str(s) {
        Ok(p) => p,
        Err(_) => return -1,
    };

    let entry = payload.into_entry();

    // Probe the on-disk file size *before* adding to the index so we can
    // update total_bytes accurately.  New files may not exist yet (caller adds
    // the index entry before the download completes); in that case we account
    // for 0 bytes now and drift is corrected on the next cache_open.
    let dir = cache::cache_dir();
    if let Some(ref d) = dir {
        let file = d.join(format!("{}.m4a", entry.id));
        if let Ok(meta) = std::fs::metadata(&file) {
            h.total_bytes = h.total_bytes.saturating_add(meta.len());
        }
    }

    cache::add(&mut h.entries, entry);

    // Enforce the size cap using the pre-computed total — no directory scan.
    if let Some(ref d) = dir {
        let cap = cache::max_bytes();
        cache::enforce_cap(&mut h.entries, d, &mut h.total_bytes, cap);
    }

    match cache::save(&h.entries) {
        Ok(_) => 0,
        Err(_) => -1,
    }
}

/// Free a string returned by `cache_get`.
///
/// # Safety
/// `s` must have been returned by `cache_get` and not yet freed.
/// Passing NULL is safe (no-op).
#[no_mangle]
pub unsafe extern "C" fn cache_free_string(s: *mut c_char) {
    if !s.is_null() {
        // SAFETY: `s` was created by CString::into_raw in cache_get.
        drop(CString::from_raw(s));
    }
}

/// Close the handle and release all memory.
///
/// # Safety
/// `handle` must have been returned by `cache_open` and not yet closed.
/// Passing NULL is safe (no-op).
#[no_mangle]
pub unsafe extern "C" fn cache_close(handle: *mut CacheHandle) {
    if !handle.is_null() {
        // SAFETY: `handle` was created by Box::into_raw in cache_open.
        drop(Box::from_raw(handle));
    }
}

// ---------------------------------------------------------------------------
// Private helpers
// ---------------------------------------------------------------------------

/// Current Unix timestamp in seconds.
fn now_unix() -> i64 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_secs() as i64)
        .unwrap_or(0)
}
