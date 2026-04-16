use std::collections::BTreeSet;
use std::sync::{Arc, Barrier};
use std::thread;

use corporate_catering_system::identity::{
    ActorId, AuthenticatedActorContext, AuthenticationSource, PlantId, PlantScope, Role,
};
use corporate_catering_system::menu_supply_window::{
    MenuHealthTag, MenuImageUrl, MenuItemId, MenuSupplyPolicy, MenuSupplyWindowError, Money,
    OrderId, OrderLifecycleState, OrderLineItemRequest, OrderMutation, OrderTimelineEventType,
    SpecialRequest, VendorMenuItem, VendorMenuItemDraft, VendorOrderingPolicyOverride,
};
use corporate_catering_system::vendor_compliance::VendorId;
use corporate_catering_system::vendor_delivery_mapping::TaipeiBusinessMoment;

fn actor_id(value: &str) -> ActorId {
    ActorId::parse(value).expect("actor id should be valid")
}

fn plant_id(value: &str) -> PlantId {
    PlantId::parse(value).expect("plant id should be valid")
}

fn restricted_scope(plants: &[&str]) -> PlantScope {
    let plant_ids = plants.iter().map(|plant| plant_id(plant)).collect();
    PlantScope::restricted(plant_ids).expect("restricted scope should be valid")
}

fn vendor_operator() -> AuthenticatedActorContext {
    AuthenticatedActorContext::new(
        actor_id("vendor-menu-supply-operator"),
        Role::VendorOperator,
        restricted_scope(&["fab-a"]),
        AuthenticationSource::VendorAccountMfa,
    )
    .expect("vendor operator should be valid")
}

fn employee_actor() -> AuthenticatedActorContext {
    AuthenticatedActorContext::new(
        actor_id("employee-menu-supply"),
        Role::Employee,
        restricted_scope(&["fab-a"]),
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
    max_daily_quantity: u16,
    delivery_epoch_day: i32,
) -> VendorMenuItem {
    menu_item_with_overrides(
        menu_item_id_value,
        vendor_id,
        max_daily_quantity,
        delivery_epoch_day,
        VendorOrderingPolicyOverride::default(),
    )
}

fn menu_item_with_overrides(
    menu_item_id_value: &str,
    vendor_id: &VendorId,
    max_daily_quantity: u16,
    delivery_epoch_day: i32,
    policy_override: VendorOrderingPolicyOverride,
) -> VendorMenuItem {
    VendorMenuItem::new(
        menu_item_id(menu_item_id_value),
        vendor_id.clone(),
        VendorMenuItemDraft::new(
            "Roasted Chicken Bento",
            "Low sodium roasted chicken with mixed vegetables.",
            "BENTO",
            vec![MenuHealthTag::HighProtein],
            Some(
                MenuImageUrl::parse("https://cdn.example.com/menu/roasted-chicken-bento.jpg")
                    .expect("menu image URL should be valid"),
            ),
            Money::new("TWD", 14500).expect("money should be valid"),
            max_daily_quantity,
            delivery_epoch_day,
        )
        .expect("menu draft should be valid")
        .with_ordering_policy_overrides(policy_override),
    )
}

fn menu_item_with_metadata(
    menu_item_id_value: &str,
    vendor_id: &VendorId,
    max_daily_quantity: u16,
    delivery_epoch_day: i32,
    menu_type: &str,
    health_tags: Vec<MenuHealthTag>,
) -> VendorMenuItem {
    VendorMenuItem::new(
        menu_item_id(menu_item_id_value),
        vendor_id.clone(),
        VendorMenuItemDraft::new(
            "Discovery Menu",
            "Discovery menu description",
            menu_type,
            health_tags,
            Some(
                MenuImageUrl::parse("https://cdn.example.com/menu/discovery-menu.jpg")
                    .expect("menu image URL should be valid"),
            ),
            Money::new("TWD", 12000).expect("money should be valid"),
            max_daily_quantity,
            delivery_epoch_day,
        )
        .expect("menu draft should be valid"),
    )
}

#[test]
fn vendors_can_manage_menu_price_image_and_daily_quota() {
    let policy = MenuSupplyPolicy::default();
    let vendor = vendor_id("ven-menuwindowa1");

    policy
        .upsert_menu_item(
            &vendor_operator(),
            menu_item("menu-supply-a1", &vendor, 120, 40),
        )
        .expect("vendor should be allowed to upsert menu item");

    let stored_item = policy
        .menu_item(&menu_item_id("menu-supply-a1"))
        .expect("state lock should not fail")
        .expect("menu item should exist");

    assert_eq!(stored_item.vendor_id(), &vendor);
    assert_eq!(stored_item.price().currency(), "TWD");
    assert_eq!(stored_item.price().amount_minor(), 14500);
    assert_eq!(stored_item.max_daily_quantity(), 120);
    assert_eq!(stored_item.delivery_epoch_day(), 40);
    assert_eq!(
        stored_item
            .image_url()
            .expect("image URL should be present")
            .as_str(),
        "https://cdn.example.com/menu/roasted-chicken-bento.jpg"
    );
}

#[test]
fn quota_accounting_prevents_oversell_under_concurrent_ordering() {
    let policy = MenuSupplyPolicy::default();
    let vendor = vendor_id("ven-menuwindowb1");
    let menu_item_id_value = menu_item_id("menu-supply-b1");

    policy
        .upsert_menu_item(
            &vendor_operator(),
            menu_item(menu_item_id_value.as_str(), &vendor, 5, 11),
        )
        .expect("menu item upsert should succeed");

    let barrier = Arc::new(Barrier::new(11));
    let mut handles = Vec::new();

    for index in 0..10 {
        let policy_clone = policy.clone();
        let vendor_clone = vendor.clone();
        let menu_item_id_clone = menu_item_id_value.clone();
        let barrier_clone = Arc::clone(&barrier);
        handles.push(thread::spawn(move || {
            barrier_clone.wait();
            let line_item =
                OrderLineItemRequest::new(menu_item_id_clone, 1, vec![SpecialRequest::NoUtensils])
                    .expect("line item should be valid");
            policy_clone.create_order(
                order_id(format!("ord-atomic-{index:02}").as_str()),
                &vendor_clone,
                &plant_id("fab-a"),
                11,
                vec![line_item],
                taipei_moment(10, 600),
            )
        }));
    }

    barrier.wait();

    let mut success_count = 0;
    let mut failure_count = 0;
    for handle in handles {
        match handle
            .join()
            .expect("concurrent order task should not panic")
        {
            Ok(()) => success_count += 1,
            Err(MenuSupplyWindowError::QuotaExceeded { .. }) => failure_count += 1,
            Err(error) => panic!("unexpected error from concurrent quota reservation: {error}"),
        }
    }

    assert_eq!(success_count, 5, "quota should allow exactly five orders");
    assert_eq!(
        failure_count, 5,
        "remaining concurrent orders should be rejected"
    );
    assert_eq!(
        policy
            .remaining_quantity(&menu_item_id_value)
            .expect("state lock should succeed"),
        Some(0)
    );
}

#[test]
fn preorder_window_and_cutoff_rules_enforce_default_and_bounded_vendor_overrides() {
    let policy = MenuSupplyPolicy::default();
    let vendor = vendor_id("ven-menuwindowc1");

    policy
        .upsert_menu_item(
            &vendor_operator(),
            menu_item("menu-supply-c1", &vendor, 30, 20),
        )
        .expect("menu item upsert should succeed");

    let default_window_error = policy
        .create_order(
            order_id("ord-window-default-reject"),
            &vendor,
            &plant_id("fab-a"),
            20,
            vec![
                OrderLineItemRequest::new(menu_item_id("menu-supply-c1"), 1, vec![])
                    .expect("line item should be valid"),
            ],
            taipei_moment(12, 500),
        )
        .expect_err("8-day-ahead order must fail under default 7-day window");
    assert!(matches!(
        default_window_error,
        MenuSupplyWindowError::PreorderWindowClosed { .. }
    ));

    policy
        .upsert_menu_item(
            &vendor_operator(),
            menu_item_with_overrides(
                "menu-supply-c2",
                &vendor,
                30,
                17,
                VendorOrderingPolicyOverride {
                    preorder_open_days_ahead: Some(3),
                    modify_cancel_cutoff_minute_of_day: Some(16 * 60),
                },
            ),
        )
        .expect("menu item upsert should succeed");
    let effective_policy = policy
        .effective_vendor_ordering_policy(&vendor)
        .expect("state lock should not fail");
    assert_eq!(effective_policy.preorder_open_days_ahead(), 3);
    assert_eq!(
        effective_policy.modify_cancel_cutoff_minute_of_day(),
        16 * 60
    );

    let overridden_window_error = policy
        .create_order(
            order_id("ord-window-override-reject"),
            &vendor,
            &plant_id("fab-a"),
            17,
            vec![
                OrderLineItemRequest::new(menu_item_id("menu-supply-c2"), 1, vec![])
                    .expect("line item should be valid"),
            ],
            taipei_moment(13, 500),
        )
        .expect_err("4-day-ahead order must fail after vendor narrowed window to 3 days");
    assert!(matches!(
        overridden_window_error,
        MenuSupplyWindowError::PreorderWindowClosed { .. }
    ));

    policy
        .upsert_menu_item(
            &vendor_operator(),
            menu_item("menu-supply-c3", &vendor, 30, 16),
        )
        .expect("menu item upsert should succeed");

    let cutoff_error = policy
        .create_order(
            order_id("ord-cutoff-reject"),
            &vendor,
            &plant_id("fab-a"),
            16,
            vec![
                OrderLineItemRequest::new(menu_item_id("menu-supply-c3"), 1, vec![])
                    .expect("line item should be valid"),
            ],
            taipei_moment(15, (16 * 60) + 1),
        )
        .expect_err("order must fail after previous-day vendor cutoff");
    assert!(matches!(
        cutoff_error,
        MenuSupplyWindowError::ModifyCancelCutoffPassed { .. }
    ));

    let out_of_bounds_override = policy
        .upsert_menu_item(
            &vendor_operator(),
            menu_item_with_overrides(
                "menu-supply-c4",
                &vendor,
                30,
                18,
                VendorOrderingPolicyOverride {
                    preorder_open_days_ahead: Some(8),
                    modify_cancel_cutoff_minute_of_day: None,
                },
            ),
        )
        .expect_err("vendor preorder override above policy max should fail");
    assert!(matches!(
        out_of_bounds_override,
        MenuSupplyWindowError::VendorOverrideOutOfBounds { .. }
    ));
}

#[test]
fn update_and_cancel_respect_cutoff_and_release_allocated_quota() {
    let policy = MenuSupplyPolicy::default();
    let vendor = vendor_id("ven-menuwindowd1");

    policy
        .upsert_menu_item(
            &vendor_operator(),
            menu_item("menu-supply-d1", &vendor, 4, 40),
        )
        .expect("menu item upsert should succeed");

    policy
        .create_order(
            order_id("ord-update-001"),
            &vendor,
            &plant_id("fab-a"),
            40,
            vec![
                OrderLineItemRequest::new(menu_item_id("menu-supply-d1"), 2, vec![])
                    .expect("line item should be valid"),
            ],
            taipei_moment(39, 800),
        )
        .expect("order creation should reserve quota");

    assert_eq!(
        policy
            .remaining_quantity(&menu_item_id("menu-supply-d1"))
            .expect("state lock should not fail"),
        Some(2)
    );

    policy
        .update_order(
            &order_id("ord-update-001"),
            OrderMutation::ReplaceLineItems {
                line_items: vec![OrderLineItemRequest::new(
                    menu_item_id("menu-supply-d1"),
                    3,
                    vec![],
                )
                .expect("line item should be valid")],
            },
            taipei_moment(39, 900),
        )
        .expect("order update should be allowed before cutoff");

    assert_eq!(
        policy
            .remaining_quantity(&menu_item_id("menu-supply-d1"))
            .expect("state lock should not fail"),
        Some(1)
    );

    policy
        .update_order(
            &order_id("ord-update-001"),
            OrderMutation::Cancel,
            taipei_moment(39, 901),
        )
        .expect("order cancel should release quota before cutoff");

    assert_eq!(
        policy
            .remaining_quantity(&menu_item_id("menu-supply-d1"))
            .expect("state lock should not fail"),
        Some(4)
    );

    policy
        .create_order(
            order_id("ord-update-002"),
            &vendor,
            &plant_id("fab-a"),
            40,
            vec![
                OrderLineItemRequest::new(menu_item_id("menu-supply-d1"), 1, vec![])
                    .expect("line item should be valid"),
            ],
            taipei_moment(39, 1000),
        )
        .expect("order creation should still be allowed before default 17:00 cutoff");

    let update_after_cutoff_error = policy
        .update_order(
            &order_id("ord-update-002"),
            OrderMutation::Cancel,
            taipei_moment(39, 1020),
        )
        .expect_err("update at cutoff boundary should be blocked");
    assert!(matches!(
        update_after_cutoff_error,
        MenuSupplyWindowError::ModifyCancelCutoffPassed { .. }
    ));
}

#[test]
fn lifecycle_timeline_covers_modification_cancel_sold_out_and_refund_states() {
    let policy = MenuSupplyPolicy::default();
    let vendor = vendor_id("ven-menuwindow-life-01");

    policy
        .upsert_menu_item(
            &vendor_operator(),
            menu_item("menu-supply-life-a1", &vendor, 6, 55),
        )
        .expect("menu item upsert should succeed");

    policy
        .create_order(
            order_id("ord-life-001"),
            &vendor,
            &plant_id("fab-a"),
            55,
            vec![
                OrderLineItemRequest::new(menu_item_id("menu-supply-life-a1"), 2, vec![])
                    .expect("line item should be valid"),
            ],
            taipei_moment(54, 900),
        )
        .expect("order create should reserve inventory");

    policy
        .update_order(
            &order_id("ord-life-001"),
            OrderMutation::ReplaceLineItems {
                line_items: vec![OrderLineItemRequest::new(
                    menu_item_id("menu-supply-life-a1"),
                    3,
                    vec![],
                )
                .expect("line item should be valid")],
            },
            taipei_moment(54, 901),
        )
        .expect("replace line items should succeed before cutoff");
    policy
        .update_order(
            &order_id("ord-life-001"),
            OrderMutation::Cancel,
            taipei_moment(54, 902),
        )
        .expect("cancel should succeed before cutoff");
    policy
        .update_order(
            &order_id("ord-life-001"),
            OrderMutation::MarkRefundPending,
            taipei_moment(54, 903),
        )
        .expect("refund pending transition should succeed");
    policy
        .update_order(
            &order_id("ord-life-001"),
            OrderMutation::MarkRefunded,
            taipei_moment(54, 904),
        )
        .expect("refunded transition should succeed");

    let refunded_snapshot = policy
        .order_snapshot(&order_id("ord-life-001"))
        .expect("snapshot query should succeed")
        .expect("order must exist after cancellation for audit timeline");
    assert_eq!(refunded_snapshot.state(), OrderLifecycleState::Refunded);
    assert_eq!(
        refunded_snapshot
            .timeline()
            .iter()
            .map(|event| event.event_type())
            .collect::<Vec<_>>(),
        vec![
            OrderTimelineEventType::Created,
            OrderTimelineEventType::Modified,
            OrderTimelineEventType::Cancelled,
            OrderTimelineEventType::RefundPending,
            OrderTimelineEventType::Refunded,
        ]
    );
    assert!(!refunded_snapshot.inventory_reserved());
    assert_eq!(
        policy
            .remaining_quantity(&menu_item_id("menu-supply-life-a1"))
            .expect("remaining quantity query should succeed"),
        Some(6)
    );

    policy
        .create_order(
            order_id("ord-life-002"),
            &vendor,
            &plant_id("fab-a"),
            55,
            vec![
                OrderLineItemRequest::new(menu_item_id("menu-supply-life-a1"), 2, vec![])
                    .expect("line item should be valid"),
            ],
            taipei_moment(54, 905),
        )
        .expect("second order create should reserve inventory");
    policy
        .update_order(
            &order_id("ord-life-002"),
            OrderMutation::MarkSoldOut,
            taipei_moment(55, 700),
        )
        .expect("sold-out transition should release inventory once");

    let sold_out_snapshot = policy
        .order_snapshot(&order_id("ord-life-002"))
        .expect("snapshot query should succeed")
        .expect("order must exist for sold-out audit");
    assert_eq!(sold_out_snapshot.state(), OrderLifecycleState::SoldOut);
    assert_eq!(
        sold_out_snapshot
            .timeline()
            .iter()
            .map(|event| event.event_type())
            .collect::<Vec<_>>(),
        vec![
            OrderTimelineEventType::Created,
            OrderTimelineEventType::SoldOut
        ]
    );
    assert!(!sold_out_snapshot.inventory_reserved());
    assert_eq!(
        policy
            .remaining_quantity(&menu_item_id("menu-supply-life-a1"))
            .expect("remaining quantity query should succeed"),
        Some(6)
    );
}

#[test]
fn inventory_reservation_release_and_create_are_idempotent() {
    let policy = MenuSupplyPolicy::default();
    let vendor = vendor_id("ven-menuwindow-idemp-01");

    policy
        .upsert_menu_item(
            &vendor_operator(),
            menu_item("menu-supply-idemp-a1", &vendor, 4, 66),
        )
        .expect("menu item upsert should succeed");

    let create_line_items =
        vec![
            OrderLineItemRequest::new(menu_item_id("menu-supply-idemp-a1"), 2, vec![])
                .expect("line item should be valid"),
        ];

    policy
        .create_order(
            order_id("ord-idemp-001"),
            &vendor,
            &plant_id("fab-a"),
            66,
            create_line_items.clone(),
            taipei_moment(65, 900),
        )
        .expect("first create should succeed");
    policy
        .create_order(
            order_id("ord-idemp-001"),
            &vendor,
            &plant_id("fab-a"),
            66,
            create_line_items,
            taipei_moment(65, 901),
        )
        .expect("duplicate create with same payload should be idempotent");
    assert_eq!(
        policy
            .remaining_quantity(&menu_item_id("menu-supply-idemp-a1"))
            .expect("remaining quantity query should succeed"),
        Some(2)
    );

    let create_conflict = policy
        .create_order(
            order_id("ord-idemp-001"),
            &vendor,
            &plant_id("fab-a"),
            66,
            vec![
                OrderLineItemRequest::new(menu_item_id("menu-supply-idemp-a1"), 1, vec![])
                    .expect("line item should be valid"),
            ],
            taipei_moment(65, 902),
        )
        .expect_err("same order id with different payload must be rejected");
    assert!(matches!(
        create_conflict,
        MenuSupplyWindowError::OrderAlreadyExists(_)
    ));

    policy
        .update_order(
            &order_id("ord-idemp-001"),
            OrderMutation::Cancel,
            taipei_moment(65, 903),
        )
        .expect("cancel should release inventory");
    policy
        .update_order(
            &order_id("ord-idemp-001"),
            OrderMutation::Cancel,
            taipei_moment(65, 904),
        )
        .expect("duplicate cancel should be idempotent");
    assert_eq!(
        policy
            .remaining_quantity(&menu_item_id("menu-supply-idemp-a1"))
            .expect("remaining quantity query should succeed"),
        Some(4)
    );
}

#[test]
fn invalid_lifecycle_transition_returns_explicit_domain_error() {
    let policy = MenuSupplyPolicy::default();
    let vendor = vendor_id("ven-menuwindow-life-02");

    policy
        .upsert_menu_item(
            &vendor_operator(),
            menu_item("menu-supply-life-b1", &vendor, 3, 70),
        )
        .expect("menu item upsert should succeed");

    policy
        .create_order(
            order_id("ord-life-003"),
            &vendor,
            &plant_id("fab-a"),
            70,
            vec![
                OrderLineItemRequest::new(menu_item_id("menu-supply-life-b1"), 1, vec![])
                    .expect("line item should be valid"),
            ],
            taipei_moment(69, 800),
        )
        .expect("order should be created");
    policy
        .update_order(
            &order_id("ord-life-003"),
            OrderMutation::MarkFulfilled,
            taipei_moment(70, 721),
        )
        .expect("fulfillment transition should succeed");
    let replay_fulfillment_error = policy
        .update_order(
            &order_id("ord-life-003"),
            OrderMutation::MarkFulfilled,
            taipei_moment(70, 721),
        )
        .expect_err("fulfillment must be single-use to prevent replay");
    assert!(matches!(
        replay_fulfillment_error,
        MenuSupplyWindowError::InvalidOrderLifecycleTransition { .. }
    ));

    let transition_error = policy
        .update_order(
            &order_id("ord-life-003"),
            OrderMutation::Cancel,
            taipei_moment(70, 722),
        )
        .expect_err("cancel after fulfillment must be rejected");
    assert!(matches!(
        transition_error,
        MenuSupplyWindowError::InvalidOrderLifecycleTransition { .. }
    ));
}

#[test]
fn special_requests_are_controlled_and_risk_limited() {
    let duplicate_request_error = OrderLineItemRequest::new(
        menu_item_id("menu-special-a1"),
        1,
        vec![SpecialRequest::NoUtensils, SpecialRequest::NoUtensils],
    )
    .expect_err("duplicate special requests should be rejected");
    assert!(matches!(
        duplicate_request_error,
        MenuSupplyWindowError::DuplicateSpecialRequest(SpecialRequest::NoUtensils)
    ));

    let too_many_request_error = OrderLineItemRequest::new(
        menu_item_id("menu-special-a2"),
        1,
        vec![
            SpecialRequest::LessRice,
            SpecialRequest::NoGreenOnion,
            SpecialRequest::SauceOnSide,
            SpecialRequest::ExtraSpicy,
        ],
    )
    .expect_err("special requests should be bounded to controlled set size");
    assert!(matches!(
        too_many_request_error,
        MenuSupplyWindowError::TooManySpecialRequests { maximum: 3 }
    ));

    let unauthorized_menu_mutation_error = MenuSupplyPolicy::default()
        .upsert_menu_item(
            &employee_actor(),
            menu_item(
                "menu-special-unauthorized",
                &vendor_id("ven-menuwindowe1"),
                10,
                60,
            ),
        )
        .expect_err("employee must not be allowed to mutate vendor menu state");
    assert!(matches!(
        unauthorized_menu_mutation_error,
        MenuSupplyWindowError::UnauthorizedRole {
            expected: Role::VendorOperator,
            actual: Role::Employee,
        }
    ));
}

#[test]
fn order_snapshot_retains_plant_and_controlled_special_request_structure() {
    let policy = MenuSupplyPolicy::default();
    let vendor = vendor_id("ven-menuwindow-special-struct");

    policy
        .upsert_menu_item(
            &vendor_operator(),
            menu_item("menu-special-struct-a1", &vendor, 15, 90),
        )
        .expect("menu item upsert should succeed");

    policy
        .create_order(
            order_id("ord-special-struct-001"),
            &vendor,
            &plant_id("fab-b"),
            90,
            vec![OrderLineItemRequest::new(
                menu_item_id("menu-special-struct-a1"),
                2,
                vec![SpecialRequest::NoUtensils, SpecialRequest::SauceOnSide],
            )
            .expect("line item should be valid")],
            taipei_moment(89, 700),
        )
        .expect("order should be created");

    let snapshot = policy
        .order_snapshot(&order_id("ord-special-struct-001"))
        .expect("snapshot query should succeed")
        .expect("snapshot should exist");
    assert_eq!(snapshot.plant_id(), &plant_id("fab-b"));
    assert_eq!(
        snapshot
            .special_requests_by_menu_item()
            .get(&menu_item_id("menu-special-struct-a1"))
            .expect("special request map should have menu item"),
        &BTreeSet::from([SpecialRequest::NoUtensils, SpecialRequest::SauceOnSide])
    );
}

#[test]
fn employee_discovery_snapshot_is_multi_day_and_uses_exact_inventory_with_cutoff_context() {
    let policy = MenuSupplyPolicy::default();
    let deliverable_vendor = vendor_id("ven-menuwindow-disc-a1");
    let hidden_vendor = vendor_id("ven-menuwindow-disc-b1");

    policy
        .upsert_menu_item(
            &vendor_operator(),
            menu_item_with_metadata(
                "menu-disc-a1",
                &deliverable_vendor,
                5,
                81,
                "BENTO",
                vec![MenuHealthTag::HighProtein],
            ),
        )
        .expect("deliverable day-1 menu should be upserted");
    policy
        .upsert_menu_item(
            &vendor_operator(),
            menu_item_with_metadata(
                "menu-disc-a2",
                &deliverable_vendor,
                10,
                83,
                "SALAD",
                vec![MenuHealthTag::Vegan],
            ),
        )
        .expect("deliverable day-3 menu should be upserted");
    policy
        .upsert_menu_item(
            &vendor_operator(),
            menu_item_with_metadata(
                "menu-disc-hidden-b1",
                &hidden_vendor,
                8,
                82,
                "NOODLE",
                vec![MenuHealthTag::LowCalorie],
            ),
        )
        .expect("hidden vendor menu should be upserted");

    policy
        .create_order(
            order_id("ord-disc-001"),
            &deliverable_vendor,
            &plant_id("fab-a"),
            81,
            vec![
                OrderLineItemRequest::new(menu_item_id("menu-disc-a1"), 2, vec![])
                    .expect("line item should be valid"),
            ],
            taipei_moment(80, 600),
        )
        .expect("order reservation should consume exact inventory");

    let visible_vendors = BTreeSet::from([deliverable_vendor.clone()]);
    let discovery = policy
        .employee_discovery_snapshot(&visible_vendors, taipei_moment(80, 600))
        .expect("discovery snapshot should succeed");

    assert_eq!(
        discovery.len(),
        2,
        "only deliverable vendor items should remain"
    );
    assert!(discovery
        .iter()
        .all(|entry| entry.menu_item().vendor_id() == &deliverable_vendor));
    assert_eq!(
        discovery
            .iter()
            .map(|entry| entry.menu_item().delivery_epoch_day())
            .collect::<Vec<_>>(),
        vec![81, 83]
    );
    assert_eq!(discovery[0].remaining_quantity(), 3);
    assert_eq!(discovery[1].remaining_quantity(), 10);
    assert!(discovery.iter().all(|entry| entry.preorder_open()));

    let discovery_after_cutoff = policy
        .employee_discovery_snapshot(&visible_vendors, taipei_moment(80, 1030))
        .expect("discovery snapshot should succeed after cutoff");
    let day_81 = discovery_after_cutoff
        .iter()
        .find(|entry| entry.menu_item().delivery_epoch_day() == 81)
        .expect("day 81 menu should exist");
    let day_83 = discovery_after_cutoff
        .iter()
        .find(|entry| entry.menu_item().delivery_epoch_day() == 83)
        .expect("day 83 menu should exist");
    assert!(
        !day_81.preorder_open(),
        "day-81 preorder should close after day-80 cutoff"
    );
    assert!(
        day_83.preorder_open(),
        "farther day should remain preorder-open"
    );
}
