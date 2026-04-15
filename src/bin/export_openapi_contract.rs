use std::path::PathBuf;

use corporate_catering_system::contract::write_openapi_artifacts;

fn main() -> Result<(), Box<dyn std::error::Error>> {
    let output_dir = std::env::args()
        .nth(1)
        .map(PathBuf::from)
        .unwrap_or_else(|| PathBuf::from("contract/openapi"));

    let artifacts = write_openapi_artifacts(&output_dir)?;
    println!("openapi_json={}", artifacts.openapi_json.display());
    println!("openapi_yaml={}", artifacts.openapi_yaml.display());
    println!("docs_html={}", artifacts.docs_html.display());

    Ok(())
}
