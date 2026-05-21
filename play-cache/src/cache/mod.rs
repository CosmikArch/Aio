// cache/mod.rs

pub mod index;
pub mod store;
pub mod evict;

pub use index::{add, lookup, stats};
pub use store::{load, save};
pub use evict::{enforce_cap, touch, measure_dir_bytes, max_bytes, cache_dir, DEFAULT_MAX_BYTES};
