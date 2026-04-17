use corporate_catering_system::persistence::{
    apply_sql_migrations, build_operational_pg_rw_pool_from_env,
};

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let pool = build_operational_pg_rw_pool_from_env().await?;
    apply_sql_migrations(&pool).await?;
    println!("sql_migrations_applied=true");
    Ok(())
}
