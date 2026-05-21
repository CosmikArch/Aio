// build.rs — invoked automatically by Cargo before compilation.
//
// Generates play_cache.h from the #[no_mangle] pub extern "C" surface in
// src/lib.rs.  The header lands next to Cargo.toml so Go can find it with
// a predictable relative path.
//
// To skip generation (e.g. in CI that only runs tests):
//   CBINDGEN_SKIP=1 cargo build

fn main() {
    if std::env::var("CBINDGEN_SKIP").is_ok() {
        return;
    }

    let crate_dir = std::env::var("CARGO_MANIFEST_DIR")
        .expect("CARGO_MANIFEST_DIR not set");

    cbindgen::Builder::new()
        .with_crate(&crate_dir)
        .with_config(
            cbindgen::Config::from_file("cbindgen.toml")
                .expect("cbindgen.toml missing or malformed"),
        )
        .generate()
        .expect("cbindgen failed to generate play_cache.h")
        .write_to_file("play_cache.h");
}
