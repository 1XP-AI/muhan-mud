//! S1 portable side of the legacy-player decoder oracle.
//!
//! The paired shell harness generates this exact CDTO byte sequence only by
//! serializing a native legacy record and passing it through read_crt_player.
use muhan_core_dto::player_snapshot_v1::{
    decode_player_snapshot_v1, encode_player_snapshot_v1, verify_player_snapshot_replay_v1,
};
use muhan_core_dto::{decode, encode, Field, Kind, Record};

fn fixture(text: &str) -> Vec<u8> {
    let text = text.trim();
    assert_eq!(text.len() % 2, 0, "fixture has whole octets");
    (0..text.len())
        .step_by(2)
        .map(|index| u8::from_str_radix(&text[index..index + 2], 16).expect("fixture hex"))
        .collect()
}

fn negative_snapshot_corpus(wire: &[u8]) -> [(&'static str, Vec<u8>); 3] {
    let mut trailing = wire.to_vec();
    trailing.push(0x7f);

    let record = decode(wire).expect("C-derived fixture has a generic envelope");
    let mut fields = record.fields().to_vec();
    fields[7] = Field::i8(8, -1);
    let canonical_invalid_type = encode(
        &Record::new(Kind::PlayerSnapshot, fields)
            .expect("test-only invalid player type remains a generic CDTO record"),
    )
    .expect("test-only invalid player type has a valid envelope digest");

    [
        ("truncated", wire[..wire.len() - 1].to_vec()),
        ("trailing-octet", trailing),
        ("canonical-invalid-player-type", canonical_invalid_type),
    ]
}

#[test]
fn bounded_legacy_decoder_fixture_is_a_canonical_portable_snapshot() {
    let wire = fixture(include_str!(
        "../../../tests/fixtures/player_snapshot_v1_legacy_decoder_canonical.hex"
    ));
    let snapshot = decode_player_snapshot_v1(&wire).expect("native decoder fixture must decode");
    assert_eq!(&snapshot.name[..11], b"s1-fixture\0");
    assert_eq!((snapshot.hp_max, snapshot.hp_current), (20, 20));
    assert_eq!((snapshot.mp_max, snapshot.mp_current), (10, 10));
    assert_eq!(snapshot.inventory.nodes.len(), 2);
    assert_eq!(snapshot.inventory.nodes[0].object.shots_current, 2);
    assert_eq!(snapshot.inventory.nodes[1].parent_index, Some(0));
    assert_eq!(
        encode_player_snapshot_v1(&snapshot).expect("reencode"),
        wire
    );
}

#[test]
fn minimal_legacy_decoder_fixture_is_a_canonical_empty_inventory_snapshot() {
    let wire = fixture(include_str!(
        "../../../tests/fixtures/player_snapshot_v1_legacy_decoder_minimal.hex"
    ));
    let snapshot = decode_player_snapshot_v1(&wire).expect("minimal fixture must decode");
    assert_eq!(&snapshot.name[..11], b"s1-minimal\0");
    assert!(snapshot.description.iter().all(|byte| *byte == 0));
    assert_eq!((snapshot.hp_max, snapshot.hp_current), (1, 1));
    assert_eq!((snapshot.mp_max, snapshot.mp_current), (1, 1));
    assert!(snapshot.inventory.nodes.is_empty());
    assert_eq!(
        encode_player_snapshot_v1(&snapshot).expect("reencode"),
        wire
    );
}

#[test]
fn persisted_graph_legacy_decoder_fixture_normalizes_and_orders_the_forest() {
    let wire = fixture(include_str!(
        "../../../tests/fixtures/player_snapshot_v1_legacy_decoder_persisted_graph.hex"
    ));
    let snapshot = decode_player_snapshot_v1(&wire).expect("persisted graph fixture must decode");
    let nodes = &snapshot.inventory.nodes;

    assert_eq!(&snapshot.name[..13], b"s1-persisted\0");
    assert_eq!((snapshot.hp_max, snapshot.hp_current), (50, 50));
    assert_eq!((snapshot.mp_max, snapshot.mp_current), (21, 21));
    assert_eq!(
        (snapshot.experience, snapshot.gold),
        (987_654_321, 7_654_321)
    );
    assert_eq!(nodes.len(), 4);
    assert_eq!(nodes[0].parent_index, None);
    assert_eq!(nodes[1].parent_index, Some(0));
    assert_eq!(nodes[2].parent_index, Some(0));
    assert_eq!(nodes[3].parent_index, None);
    assert_eq!(
        nodes
            .iter()
            .map(|node| node.child_index)
            .collect::<Vec<_>>(),
        [0, 0, 1, 1]
    );
    assert_eq!(&nodes[0].object.name[..11], b"s1-satchel\0");
    assert_eq!(nodes[0].object.shots_current, 2);
    assert_eq!(nodes[2].object.shots_current, 3);
    assert!(nodes[0].object.name[11..].iter().all(|byte| *byte == 0));
    assert!(nodes[0].object.description[9..]
        .iter()
        .all(|byte| *byte == 0));
    assert!(nodes[0].object.key[0][4..].iter().all(|byte| *byte == 0));
    assert!(nodes[0].object.use_output[9..]
        .iter()
        .all(|byte| *byte == 0));
    assert_eq!(
        encode_player_snapshot_v1(&snapshot).expect("reencode"),
        wire
    );
}

#[test]
fn c_derived_negative_snapshot_corpus_rejects_without_a_snapshot() {
    let wire = fixture(include_str!(
        "../../../tests/fixtures/player_snapshot_v1_legacy_decoder_canonical.hex"
    ));

    for (name, malformed) in negative_snapshot_corpus(&wire) {
        assert!(
            decode_player_snapshot_v1(&malformed).is_err(),
            "{name} must not return a partial PlayerSnapshotV1"
        );
        assert!(
            verify_player_snapshot_replay_v1(&malformed).is_err(),
            "{name} must not produce a replay report"
        );
    }
}
