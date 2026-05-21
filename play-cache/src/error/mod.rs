// error/mod.rs — unified error type for the play-cache crate.
//
// Using thiserror keeps Display impls concise and the From conversions
// mechanical.  All public functions return `Result<_, PlayCacheError>`.

use thiserror::Error;

#[derive(Debug, Error)]
pub enum PlayCacheError {
    #[error("IO error: {0}")]
    Io(#[from] std::io::Error),

    #[error("JSON error: {0}")]
    Json(#[from] serde_json::Error),

    #[error("HOME environment variable not set")]
    NoHomeDir,

    #[error("{0}")]
    InvalidRequest(String),

    #[error("\"add\" requires an \"entry\" field")]
    MissingEntry,
}
