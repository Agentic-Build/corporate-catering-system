use std::sync::Arc;

use corporate_catering_system::identity::{
    ActorId, AuthenticatedActorContext, AuthenticationSource, PlantId, PlantScope, Role,
};
use corporate_catering_system::menu_supply_window::{
    MenuHealthTag, MenuImageUrl, MenuItemId, MenuSupplyPolicy, Money, OrderId,
    OrderLineItemRequest, SpecialRequest, VendorMenuItem, VendorMenuItemDraft,
};
use corporate_catering_system::transport::http::HttpVendorFulfillmentExecutionGateway;
use corporate_catering_system::vendor_compliance::VendorId;
use corporate_catering_system::vendor_delivery_mapping::TaipeiBusinessMoment;
use corporate_catering_system::vendor_fulfillment::{
    FulfillmentBatchId, FulfillmentDeliveryStatus, InMemoryFulfillmentArtifactStore,
    VendorFulfillmentError, VendorFulfillmentPolicy,
};

fn actor_id(value: &str) -> ActorId {
    ActorId::parse(value).expect("actor id should be valid")
}

fn plant_id(value: &str) -> PlantId {
    PlantId::parse(value).expect("plant id should be valid")
}

fn restricted_scope(plants: &[&str]) -> PlantScope {
    PlantScope::restricted(plants.iter().map(|plant| plant_id(plant)).collect())
        .expect("restricted scope should be valid")
}

fn vendor_operator(plants: &[&str]) -> AuthenticatedActorContext {
    AuthenticatedActorContext::new(
        actor_id("vendor-http-fulfillment"),
        Role::VendorOperator,
        restricted_scope(plants),
        AuthenticationSource::VendorAccountMfa,
    )
    .expect("vendor operator actor should be valid")
}

fn employee_actor(plants: &[&str]) -> AuthenticatedActorContext {
    AuthenticatedActorContext::new(
        actor_id("employee-http-fulfillment"),
        Role::Employee,
        restricted_scope(plants),
        AuthenticationSource::CorporateSso,
    )
    .expect("employee actor should be valid")
}

fn vendor_id(value: &str) -> VendorId {
    VendorId::parse(value).expect("vendor id should be valid")
}

fn menu_item_id(value: &str) -> MenuItemId {
    MenuItemId::parse(value).expect("menu item id should be valid")
}

fn order_id(value: &str) -> OrderId {
    OrderId::parse(value).expect("order id should be valid")
}

fn taipei_moment(epoch_day: i32, minute_of_day: u16) -> TaipeiBusinessMoment {
    TaipeiBusinessMoment::new(epoch_day, minute_of_day).expect("Taipei business moment is valid")
}

fn fulfillment_policy() -> VendorFulfillmentPolicy {
    VendorFulfillmentPolicy::new(Arc::new(
        InMemoryFulfillmentArtifactStore::new("fulfillment-exports")
            .expect("in-memory artifact store should initialize"),
    ))
}

fn ensure_test_otel_endpoint() {
    std::env::set_var("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:4317");
}

fn menu_item(
    menu_item_id_value: &str,
    vendor_id: &VendorId,
    delivery_epoch_day: i32,
) -> VendorMenuItem {
    VendorMenuItem::new(
        menu_item_id(menu_item_id_value),
        vendor_id.clone(),
        VendorMenuItemDraft::new(
            "HTTP Fulfillment Bento",
            "Menu item for HTTP fulfillment execution tests.",
            "BENTO",
            vec![MenuHealthTag::HighProtein],
            Some(
                MenuImageUrl::parse(format!(
                    "s3://menu-assets/menu-images/{}/media/262144-deadbeef-http-fulfillment-bento.jpg",
                    vendor_id.as_str()
                ))
                .expect("menu image URL should be valid"),
            ),
            Money::new("TWD", 13000).expect("money should be valid"),
            20,
            delivery_epoch_day,
        )
        .expect("menu draft should be valid"),
    )
}

#[test]
fn http_gateway_drives_vendor_fulfillment_board_status_transition_and_export_batches() {
    ensure_test_otel_endpoint();
    let delivery_epoch_day = 320;
    let vendor = vendor_id("ven-http-fulfillmenta1");
    let vendor_actor = vendor_operator(&["fab-a", "fab-b"]);
    let employee = employee_actor(&["fab-a", "fab-b"]);

    let menu_supply = MenuSupplyPolicy::default();
    menu_supply
        .upsert_menu_item(
            &vendor_actor,
            menu_item("menu-http-fulfill-a1", &vendor, delivery_epoch_day),
        )
        .expect("menu item should be upserted");

    menu_supply
        .create_order(
            &employee,
            order_id("ord-http-fulfill-001"),
            &vendor,
            &plant_id("fab-a"),
            delivery_epoch_day,
            vec![OrderLineItemRequest::new(
                menu_item_id("menu-http-fulfill-a1"),
                1,
                vec![SpecialRequest::NoUtensils],
            )
            .expect("line item should be valid")],
            taipei_moment(delivery_epoch_day.saturating_sub(1), 700),
        )
        .expect("order should be created");

    let fulfillment_policy = fulfillment_policy();
    let gateway = HttpVendorFulfillmentExecutionGateway::new(&fulfillment_policy, &menu_supply);

    let board = gateway
        .execute_vendor_operations_board(
            &vendor,
            delivery_epoch_day,
            taipei_moment(delivery_epoch_day, 701),
        )
        .expect("board should be readable");
    assert_eq!(board.order_entries().len(), 1);
    assert_eq!(
        board.order_entries()[0].delivery_status(),
        FulfillmentDeliveryStatus::PendingPrep
    );

    gateway
        .execute_transition_delivery_status(
            &vendor_actor,
            &order_id("ord-http-fulfill-001"),
            FulfillmentDeliveryStatus::Preparing,
            taipei_moment(delivery_epoch_day, 702),
        )
        .expect("status transition should be accepted");

    let updated_board = gateway
        .execute_vendor_operations_board(
            &vendor,
            delivery_epoch_day,
            taipei_moment(delivery_epoch_day, 703),
        )
        .expect("updated board should be readable");
    assert_eq!(
        updated_board.order_entries()[0].delivery_status(),
        FulfillmentDeliveryStatus::Preparing
    );

    let batch = gateway
        .execute_create_export_batch(
            &vendor_actor,
            &vendor,
            delivery_epoch_day,
            taipei_moment(delivery_epoch_day, 704),
        )
        .expect("batch should be created");
    assert_eq!(batch.batch_id().as_str(), "fbatch-320-000001");

    let loaded_batch = gateway
        .execute_get_export_batch(
            &FulfillmentBatchId::parse(batch.batch_id().as_str().to_owned())
                .expect("batch id should parse"),
        )
        .expect("batch should be retrievable");
    assert_eq!(loaded_batch.batch_id().as_str(), "fbatch-320-000001");
}

#[test]
fn http_gateway_rejects_status_transition_for_order_outside_actor_scope() {
    ensure_test_otel_endpoint();
    let delivery_epoch_day = 321;
    let vendor = vendor_id("ven-http-fulfillmentb1");
    let menu_supply = MenuSupplyPolicy::default();
    let employee = employee_actor(&["fab-a", "fab-b"]);

    let broad_actor = vendor_operator(&["fab-a", "fab-b"]);
    menu_supply
        .upsert_menu_item(
            &broad_actor,
            menu_item("menu-http-fulfill-b1", &vendor, delivery_epoch_day),
        )
        .expect("menu item should be upserted");
    menu_supply
        .create_order(
            &employee,
            order_id("ord-http-fulfill-002"),
            &vendor,
            &plant_id("fab-b"),
            delivery_epoch_day,
            vec![OrderLineItemRequest::new(
                menu_item_id("menu-http-fulfill-b1"),
                1,
                vec![SpecialRequest::LessRice],
            )
            .expect("line item should be valid")],
            taipei_moment(delivery_epoch_day.saturating_sub(1), 700),
        )
        .expect("order should be created");

    let scoped_actor = vendor_operator(&["fab-a"]);
    let fulfillment_policy = fulfillment_policy();
    let gateway = HttpVendorFulfillmentExecutionGateway::new(&fulfillment_policy, &menu_supply);

    let error = gateway
        .execute_transition_delivery_status(
            &scoped_actor,
            &order_id("ord-http-fulfill-002"),
            FulfillmentDeliveryStatus::Preparing,
            taipei_moment(delivery_epoch_day, 710),
        )
        .expect_err("transition should fail when plant is outside actor scope");

    assert!(matches!(
        error,
        VendorFulfillmentError::TargetPlantOutOfScope { .. }
    ));
}
