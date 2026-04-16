use std::collections::BTreeMap;
use std::sync::Arc;

use corporate_catering_system::identity::{
    ActorId, AuthenticatedActorContext, AuthenticationSource, PlantId, PlantScope, Role,
};
use corporate_catering_system::menu_supply_window::{
    MenuHealthTag, MenuImageUrl, MenuItemId, MenuSupplyPolicy, Money, OrderId,
    OrderLineItemRequest, OrderMutation, SpecialRequest, VendorMenuItem, VendorMenuItemDraft,
};
use corporate_catering_system::vendor_compliance::VendorId;
use corporate_catering_system::vendor_delivery_mapping::TaipeiBusinessMoment;
use corporate_catering_system::vendor_fulfillment::{
    FulfillmentArtifactReference, FulfillmentArtifactStore, FulfillmentArtifactType,
    FulfillmentBatchId, FulfillmentDeliveryStatus, VendorFulfillmentError, VendorFulfillmentPolicy,
};
use sha2::{Digest, Sha256};

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

fn vendor_operator_with_scope(plants: &[&str]) -> AuthenticatedActorContext {
    AuthenticatedActorContext::new(
        actor_id("vendor-fulfillment-operator"),
        Role::VendorOperator,
        restricted_scope(plants),
        AuthenticationSource::VendorAccountMfa,
    )
    .expect("vendor operator actor should be valid")
}

fn employee_with_scope(plants: &[&str]) -> AuthenticatedActorContext {
    AuthenticatedActorContext::new(
        actor_id("employee-fulfillment-operator"),
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

fn menu_item(
    menu_item_id_value: &str,
    vendor_id: &VendorId,
    delivery_epoch_day: i32,
) -> VendorMenuItem {
    VendorMenuItem::new(
        menu_item_id(menu_item_id_value),
        vendor_id.clone(),
        VendorMenuItemDraft::new(
            "Fulfillment Bento",
            "Purpose-built menu item for fulfillment tests.",
            "BENTO",
            vec![MenuHealthTag::HighProtein],
            Some(
                MenuImageUrl::parse(format!(
                    "s3://menu-assets/menu-images/{}/media/262144-deadbeef-fulfillment-bento.jpg",
                    vendor_id.as_str()
                ))
                .expect("menu image URL should be valid"),
            ),
            Money::new("TWD", 12000).expect("money should be valid"),
            40,
            delivery_epoch_day,
        )
        .expect("menu draft should be valid"),
    )
}

#[derive(Debug)]
struct FixtureFulfillmentArtifactStore {
    bucket: String,
}

impl FixtureFulfillmentArtifactStore {
    fn new(bucket: impl Into<String>) -> Result<Self, VendorFulfillmentError> {
        let bucket = bucket.into().trim().to_owned();
        if bucket.is_empty() {
            return Err(VendorFulfillmentError::ArtifactStorageConfiguration(
                "fixture fulfillment artifact store bucket must not be empty".to_owned(),
            ));
        }
        Ok(Self { bucket })
    }
}

impl FulfillmentArtifactStore for FixtureFulfillmentArtifactStore {
    fn store_json_artifact(
        &self,
        vendor_id: &VendorId,
        batch_id: &FulfillmentBatchId,
        delivery_epoch_day: i32,
        artifact_type: FulfillmentArtifactType,
        payload: &[u8],
    ) -> Result<FulfillmentArtifactReference, VendorFulfillmentError> {
        let size_bytes = u64::try_from(payload.len()).expect("artifact payload length should fit");
        let digest = sha256_hex(payload);
        let artifact_file_stem = artifact_type
            .as_str()
            .to_ascii_lowercase()
            .replace('_', "-");
        let object_ref = format!(
            "s3://{}/fulfillment-artifacts/{}/{}/{}/{}-deadbeef-{}.json",
            self.bucket,
            vendor_id.as_str(),
            delivery_epoch_day,
            batch_id.as_str(),
            size_bytes,
            artifact_file_stem
        );
        FulfillmentArtifactReference::new(
            artifact_type,
            object_ref,
            "application/json",
            size_bytes,
            digest,
        )
    }
}

fn sha256_hex(payload: &[u8]) -> String {
    let mut digest = Sha256::new();
    digest.update(payload);
    let digest = digest.finalize();
    let mut output = String::with_capacity(digest.len() * 2);
    for byte in digest {
        output.push_str(format!("{byte:02x}").as_str());
    }
    output
}

fn fulfillment_policy() -> VendorFulfillmentPolicy {
    VendorFulfillmentPolicy::new(Arc::new(
        FixtureFulfillmentArtifactStore::new("fulfillment-exports")
            .expect("fixture artifact store should initialize"),
    ))
}

fn setup_policy_with_orders(
    delivery_epoch_day: i32,
) -> (
    MenuSupplyPolicy,
    VendorId,
    AuthenticatedActorContext,
    AuthenticatedActorContext,
) {
    let menu_supply = MenuSupplyPolicy::default();
    let vendor = vendor_id("ven-fulfillmenta1");
    let vendor_actor = vendor_operator_with_scope(&["fab-a", "fab-b"]);
    let employee_actor = employee_with_scope(&["fab-a", "fab-b"]);

    menu_supply
        .upsert_menu_item(
            &vendor_actor,
            menu_item("menu-fulfill-a1", &vendor, delivery_epoch_day),
        )
        .expect("menu a1 should be upserted");
    menu_supply
        .upsert_menu_item(
            &vendor_actor,
            menu_item("menu-fulfill-a2", &vendor, delivery_epoch_day),
        )
        .expect("menu a2 should be upserted");

    menu_supply
        .create_order(
            &employee_actor,
            order_id("ord-fulfill-001"),
            &vendor,
            &plant_id("fab-a"),
            delivery_epoch_day,
            vec![OrderLineItemRequest::new(
                menu_item_id("menu-fulfill-a1"),
                2,
                vec![SpecialRequest::NoUtensils],
            )
            .expect("line item should be valid")],
            taipei_moment(delivery_epoch_day.saturating_sub(1), 700),
        )
        .expect("order one should be created");

    menu_supply
        .create_order(
            &employee_actor,
            order_id("ord-fulfill-002"),
            &vendor,
            &plant_id("fab-b"),
            delivery_epoch_day,
            vec![OrderLineItemRequest::new(
                menu_item_id("menu-fulfill-a2"),
                1,
                vec![SpecialRequest::LessRice, SpecialRequest::SauceOnSide],
            )
            .expect("line item should be valid")],
            taipei_moment(delivery_epoch_day.saturating_sub(1), 701),
        )
        .expect("order two should be created");

    menu_supply
        .create_order(
            &employee_actor,
            order_id("ord-fulfill-003"),
            &vendor,
            &plant_id("fab-b"),
            delivery_epoch_day,
            vec![
                OrderLineItemRequest::new(menu_item_id("menu-fulfill-a1"), 1, vec![])
                    .expect("line item should be valid"),
            ],
            taipei_moment(delivery_epoch_day.saturating_sub(1), 702),
        )
        .expect("order three should be created");

    menu_supply
        .update_order(
            &employee_actor,
            &order_id("ord-fulfill-003"),
            OrderMutation::MarkFulfilled,
            taipei_moment(delivery_epoch_day, 660),
        )
        .expect("third order should be marked fulfilled");

    (menu_supply, vendor, vendor_actor, employee_actor)
}

#[test]
fn vendor_operations_board_aggregates_per_plant_special_needs_and_delivery_status() {
    let delivery_epoch_day = 210;
    let (menu_supply, vendor, vendor_actor, _) = setup_policy_with_orders(delivery_epoch_day);
    let fulfillment_policy = fulfillment_policy();

    let board_before_transition = fulfillment_policy
        .vendor_operations_board(
            &menu_supply,
            &vendor,
            delivery_epoch_day,
            taipei_moment(delivery_epoch_day, 720),
        )
        .expect("board snapshot should be generated");

    assert_eq!(board_before_transition.order_entries().len(), 3);
    assert_eq!(board_before_transition.plant_entries().len(), 2);

    let plant_by_id = board_before_transition
        .plant_entries()
        .iter()
        .map(|entry| (entry.plant_id().as_str().to_owned(), entry))
        .collect::<BTreeMap<_, _>>();

    let fab_a = plant_by_id.get("fab-a").expect("fab-a should be present");
    assert_eq!(fab_a.order_count(), 1);
    assert_eq!(fab_a.portion_count(), 2);
    assert_eq!(
        fab_a
            .special_request_counts()
            .get(&SpecialRequest::NoUtensils)
            .copied(),
        Some(2)
    );
    assert_eq!(
        fab_a
            .delivery_status_counts()
            .get(&FulfillmentDeliveryStatus::PendingPrep)
            .copied(),
        Some(1)
    );

    let fab_b = plant_by_id.get("fab-b").expect("fab-b should be present");
    assert_eq!(fab_b.order_count(), 2);
    assert_eq!(fab_b.portion_count(), 2);
    assert_eq!(
        fab_b
            .special_request_counts()
            .get(&SpecialRequest::LessRice)
            .copied(),
        Some(1)
    );
    assert_eq!(
        fab_b
            .special_request_counts()
            .get(&SpecialRequest::SauceOnSide)
            .copied(),
        Some(1)
    );
    assert_eq!(
        fab_b
            .delivery_status_counts()
            .get(&FulfillmentDeliveryStatus::PendingPrep)
            .copied(),
        Some(1)
    );
    assert_eq!(
        fab_b
            .delivery_status_counts()
            .get(&FulfillmentDeliveryStatus::Delivered)
            .copied(),
        Some(1)
    );

    let transition = fulfillment_policy
        .transition_delivery_status(
            &vendor_actor,
            &menu_supply,
            &order_id("ord-fulfill-001"),
            FulfillmentDeliveryStatus::Preparing,
            taipei_moment(delivery_epoch_day, 721),
        )
        .expect("transition should be accepted");
    assert_eq!(
        transition.audit_identity().operation_id(),
        "advanceVendorFulfillmentDeliveryStatus"
    );

    let board_after_transition = fulfillment_policy
        .vendor_operations_board(
            &menu_supply,
            &vendor,
            delivery_epoch_day,
            taipei_moment(delivery_epoch_day, 722),
        )
        .expect("board should update near real-time after transition");

    let transitioned_order = board_after_transition
        .order_entries()
        .iter()
        .find(|entry| entry.order_id().as_str() == "ord-fulfill-001")
        .expect("transitioned order should be present");
    assert_eq!(
        transitioned_order.delivery_status(),
        FulfillmentDeliveryStatus::Preparing
    );

    assert_eq!(board_after_transition.status_transitions().len(), 1);
    assert_eq!(
        board_after_transition.status_transitions()[0]
            .audit_identity()
            .actor_id(),
        vendor_actor.actor_id()
    );
}

#[test]
fn export_batches_are_immutable_and_generated_from_snapshot_state() {
    let delivery_epoch_day = 220;
    let (menu_supply, vendor, vendor_actor, employee_actor) =
        setup_policy_with_orders(delivery_epoch_day);
    let fulfillment_policy = fulfillment_policy();

    let first_batch = fulfillment_policy
        .create_export_batch(
            &vendor_actor,
            &menu_supply,
            &vendor,
            delivery_epoch_day,
            taipei_moment(delivery_epoch_day, 730),
        )
        .expect("first export batch should be created");

    assert_eq!(first_batch.batch_id().as_str(), "fbatch-220-000001");
    assert_eq!(first_batch.artifacts().artifacts().len(), 4);
    let first_daily_summary = first_batch
        .artifacts()
        .artifact(FulfillmentArtifactType::DailySummary)
        .expect("daily summary artifact should exist");
    assert!(first_daily_summary
        .object_ref()
        .contains("fbatch-220-000001"));
    assert_eq!(first_daily_summary.mime_type(), "application/json");
    assert_eq!(first_daily_summary.sha256().len(), 64);
    let first_daily_summary_sha = first_daily_summary.sha256().to_owned();

    menu_supply
        .create_order(
            &employee_actor,
            order_id("ord-fulfill-004"),
            &vendor,
            &plant_id("fab-a"),
            delivery_epoch_day,
            vec![OrderLineItemRequest::new(
                menu_item_id("menu-fulfill-a2"),
                1,
                vec![SpecialRequest::ExtraSpicy],
            )
            .expect("line item should be valid")],
            taipei_moment(delivery_epoch_day.saturating_sub(1), 703),
        )
        .expect("fourth order should be created after first batch");

    fulfillment_policy
        .transition_delivery_status(
            &vendor_actor,
            &menu_supply,
            &order_id("ord-fulfill-002"),
            FulfillmentDeliveryStatus::Packed,
            taipei_moment(delivery_epoch_day, 731),
        )
        .expect("second order should transition to packed");

    let immutable_first_batch = fulfillment_policy
        .batch_snapshot(first_batch.batch_id())
        .expect("first batch should remain readable");
    assert_eq!(
        immutable_first_batch
            .artifacts()
            .artifact(FulfillmentArtifactType::DailySummary)
            .expect("daily summary artifact should still exist")
            .sha256(),
        first_daily_summary_sha.as_str(),
        "first batch artifact checksum must stay immutable after live-state changes"
    );
    assert_eq!(immutable_first_batch.board().order_entries().len(), 3);

    let second_batch = fulfillment_policy
        .create_export_batch(
            &vendor_actor,
            &menu_supply,
            &vendor,
            delivery_epoch_day,
            taipei_moment(delivery_epoch_day, 732),
        )
        .expect("second export batch should be created from updated state");

    assert_eq!(second_batch.batch_id().as_str(), "fbatch-220-000002");
    assert_eq!(second_batch.artifacts().artifacts().len(), 4);
    let second_daily_summary = second_batch
        .artifacts()
        .artifact(FulfillmentArtifactType::DailySummary)
        .expect("daily summary artifact should exist");
    assert!(second_daily_summary
        .object_ref()
        .contains("fbatch-220-000002"));
    assert_ne!(
        second_daily_summary.sha256(),
        first_daily_summary_sha.as_str(),
        "updated state should produce a new daily summary artifact checksum"
    );
    for artifact_type in [
        FulfillmentArtifactType::DailySummary,
        FulfillmentArtifactType::PlantPartitionSheet,
        FulfillmentArtifactType::Labels,
        FulfillmentArtifactType::BasketList,
    ] {
        let artifact = second_batch
            .artifacts()
            .artifact(artifact_type)
            .expect("all export artifact references should be present");
        assert!(artifact.size_bytes() > 0);
        assert!(artifact
            .object_ref()
            .starts_with("s3://fulfillment-exports/"));
    }
}

#[test]
fn transition_rejects_orders_outside_vendor_scope() {
    let delivery_epoch_day = 230;
    let (menu_supply, _vendor, _, _) = setup_policy_with_orders(delivery_epoch_day);
    let fulfillment_policy = fulfillment_policy();
    let scoped_actor = vendor_operator_with_scope(&["fab-a"]);

    let error = fulfillment_policy
        .transition_delivery_status(
            &scoped_actor,
            &menu_supply,
            &order_id("ord-fulfill-002"),
            FulfillmentDeliveryStatus::Preparing,
            taipei_moment(delivery_epoch_day, 740),
        )
        .expect_err("out-of-scope plant transition should be rejected");

    assert!(matches!(
        error,
        VendorFulfillmentError::TargetPlantOutOfScope { .. }
    ));
}
