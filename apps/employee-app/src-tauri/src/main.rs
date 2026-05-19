// Desktop / dev entrypoint. On mobile, Tauri uses the `mobile_entry_point`
// in lib.rs instead; this binary keeps `cargo run` working on the host.
#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

fn main() {
    tbite_employee_app_lib::run();
}
