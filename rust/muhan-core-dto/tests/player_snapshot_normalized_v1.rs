use muhan_core_dto::player_snapshot_normalized_v1::{
    project_player_snapshot_v1_artifact, NormalizedDailyV1, NormalizedItemV1, NormalizedTimerV1,
};
use muhan_core_dto::player_snapshot_v1::{decode_player_snapshot_v1, encode_player_snapshot_v1};
use muhan_core_dto::{decode, encode, Field, Kind, Record};

fn fixture_hex(source: &str) -> Vec<u8> {
    let text = source.trim();
    assert_eq!(text.len() % 2, 0, "fixture has whole hex octets");
    (0..text.len())
        .step_by(2)
        .map(|index| u8::from_str_radix(&text[index..index + 2], 16).expect("fixture hex"))
        .collect()
}

fn canonical_fixture() -> Vec<u8> {
    fixture_hex(include_str!(
        "../../../tests/fixtures/player_snapshot_v1_canonical.hex"
    ))
}

fn tree_fixture() -> Vec<u8> {
    fixture_hex(include_str!(
        "../../../tests/fixtures/player_snapshot_v1_tree_inventory.hex"
    ))
}

fn persisted_graph_fixture() -> Vec<u8> {
    fixture_hex(include_str!(
        "../../../tests/fixtures/player_snapshot_v1_legacy_decoder_persisted_graph.hex"
    ))
}

#[test]
fn canonical_fixture_maps_only_the_explicit_safe_scalar_and_timer_allowlist() {
    let wire = canonical_fixture();
    let snapshot = decode_player_snapshot_v1(&wire).expect("fixture decodes");
    let normalized = project_player_snapshot_v1_artifact(&wire).expect("fixture projects");

    assert_eq!(normalized.level, snapshot.level);
    assert_eq!(normalized.hp_max, snapshot.hp_max);
    assert_eq!(normalized.hp_current, snapshot.hp_current);
    assert_eq!(normalized.mp_max, snapshot.mp_max);
    assert_eq!(normalized.mp_current, snapshot.mp_current);
    assert_eq!(normalized.experience, snapshot.experience);
    assert_eq!(normalized.gold, snapshot.gold);
    assert_eq!(
        normalized.daily,
        snapshot.daily.map(|daily| NormalizedDailyV1 {
            max: daily.max,
            current: daily.current,
            last_used: daily.last_used,
        })
    );
    assert_eq!(
        normalized.timers,
        snapshot.lasttime.map(|timer| NormalizedTimerV1 {
            interval: timer.interval,
            last_used: timer.last_used,
            misc: timer.misc,
        })
    );
    assert!(normalized.items.is_empty());
}

#[test]
fn tree_fixture_preserves_canonical_root_and_child_item_topology_and_numeric_fields() {
    let wire = tree_fixture();
    let snapshot = decode_player_snapshot_v1(&wire).expect("tree decodes");
    let normalized = project_player_snapshot_v1_artifact(&wire).expect("tree projects");

    assert_eq!(normalized.items.len(), 5);
    assert_eq!(
        normalized
            .items
            .iter()
            .map(|item| (item.parent_index, item.child_index))
            .collect::<Vec<_>>(),
        vec![
            (None, 0),
            (Some(0), 0),
            (Some(1), 0),
            (Some(0), 1),
            (None, 1)
        ]
    );
    assert_eq!(
        normalized.items[0].value,
        snapshot.inventory.nodes[0].object.value
    );
    assert_eq!(
        normalized.items[2].shots_current,
        snapshot.inventory.nodes[2].object.shots_current
    );
    let object = &snapshot.inventory.nodes[0].object;
    assert_eq!(
        normalized.items[0],
        NormalizedItemV1 {
            parent_index: None,
            child_index: 0,
            value: object.value,
            weight: object.weight,
            type_code: object.type_code,
            adjustment: object.adjustment,
            shots_max: object.shots_max,
            shots_current: object.shots_current,
            ndice: object.ndice,
            sdice: object.sdice,
            pdice: object.pdice,
            armor: object.armor,
            wear_flag: object.wear_flag,
            magic_power: object.magic_power,
            magic_realm: object.magic_realm,
            special: object.special,
        }
    );
}

#[test]
fn persisted_graph_fixture_preserves_multiple_roots_in_canonical_order() {
    let normalized =
        project_player_snapshot_v1_artifact(&persisted_graph_fixture()).expect("graph projects");

    assert_eq!(
        normalized
            .items
            .iter()
            .map(|item| (item.parent_index, item.child_index))
            .collect::<Vec<_>>(),
        vec![(None, 0), (Some(0), 0), (Some(0), 1), (None, 1)]
    );
}

#[test]
fn projection_digest_is_canonical_deterministic_and_insensitive_to_excluded_fields() {
    let wire = tree_fixture();
    let first = project_player_snapshot_v1_artifact(&wire).expect("first projection");
    let second = project_player_snapshot_v1_artifact(&wire).expect("second projection");
    assert_eq!(first, second);
    assert_eq!(first.canonical_digest(), second.canonical_digest());

    let mut snapshot = decode_player_snapshot_v1(&wire).expect("fixture decodes");
    snapshot.name[0] ^= 1;
    snapshot.key[0][0] ^= 1;
    snapshot.flags[0] ^= 1;
    snapshot.inventory.nodes[0].object.name[0] ^= 1;
    snapshot.inventory.nodes[0].object.key[0][0] ^= 1;
    snapshot.inventory.nodes[0].object.flags[0] ^= 1;
    snapshot.inventory.nodes[0].object.quest_num ^= 1;
    let sensitive_mutation = encode_player_snapshot_v1(&snapshot).expect("mutation remains valid");
    let projected_mutation =
        project_player_snapshot_v1_artifact(&sensitive_mutation).expect("mutation projects");
    assert_eq!(
        projected_mutation, first,
        "excluded source fields never cross the boundary"
    );
    assert_eq!(
        projected_mutation.canonical_digest(),
        first.canonical_digest()
    );

    snapshot.hp_current -= 1;
    let allowed_mutation =
        encode_player_snapshot_v1(&snapshot).expect("allowed mutation remains valid");
    let projected_allowed =
        project_player_snapshot_v1_artifact(&allowed_mutation).expect("allowed mutation projects");
    assert_ne!(
        projected_allowed.canonical_digest(),
        first.canonical_digest()
    );
}

#[test]
fn projection_rejects_tampered_and_digest_valid_invalid_artifacts() {
    let wire = canonical_fixture();
    let mut tampered = wire.clone();
    *tampered.last_mut().expect("fixture has bytes") ^= 1;
    assert!(project_player_snapshot_v1_artifact(&tampered).is_err());

    let record = decode(&wire).expect("fixture has generic envelope");
    let mut fields = record.fields().to_vec();
    fields[7] = Field::i8(8, -1);
    let invalid =
        encode(&Record::new(Kind::PlayerSnapshot, fields).expect("mutation is a generic envelope"))
            .expect("mutation has a valid outer digest");
    assert!(project_player_snapshot_v1_artifact(&invalid).is_err());
}
