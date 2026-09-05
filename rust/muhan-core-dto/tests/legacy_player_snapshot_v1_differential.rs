//! S1 portable side of the legacy-player decoder oracle.
//!
//! The paired shell harness generates this exact CDTO byte sequence only by
//! serializing a native legacy record and passing it through read_crt_player.
use muhan_core_dto::player_snapshot_v1::{decode_player_snapshot_v1, encode_player_snapshot_v1};

fn fixture() -> Vec<u8> {
    let text =
        include_str!("../../../tests/fixtures/player_snapshot_v1_legacy_decoder_canonical.hex")
            .trim();
    assert_eq!(text.len() % 2, 0, "fixture has whole octets");
    (0..text.len())
        .step_by(2)
        .map(|index| u8::from_str_radix(&text[index..index + 2], 16).expect("fixture hex"))
        .collect()
}

#[test]
fn bounded_legacy_decoder_fixture_is_a_canonical_portable_snapshot() {
    let wire = fixture();
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
