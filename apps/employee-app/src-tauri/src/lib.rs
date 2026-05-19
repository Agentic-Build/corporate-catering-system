// T-Bite employee app — Tauri 2 runtime.
//
// The whole UI is the SvelteKit static SPA in ../build; this Rust layer
// only wires the native plugins:
//   - deep-link : registers the `tbite://` scheme so the OIDC callback
//                 (backend B4) can hand the auth token back to the app.
//   - opener    : opens the system browser for the OIDC login flow.
//   - stronghold: secure on-device token storage (mobile only).
//
// `run()` is shared by the desktop binary (main.rs) and the mobile
// entrypoint below.

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    let mut builder = tauri::Builder::default()
        .plugin(tauri_plugin_opener::init())
        .plugin(tauri_plugin_deep_link::init());

    // Secure storage plugin is mobile-only (see Cargo.toml target cfg).
    #[cfg(mobile)]
    {
        builder = builder.plugin(tauri_plugin_stronghold::Builder::new(|_pass| {
            // TODO(M5): derive the stronghold key from a device-bound
            // secret instead of a fixed value.
            vec![0u8; 32]
        }).build());
    }

    builder
        .run(tauri::generate_context!())
        .expect("error while running T-Bite employee app");
}
