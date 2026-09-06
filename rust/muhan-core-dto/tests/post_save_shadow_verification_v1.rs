//! Pure-Rust, post-save shadow checks over the audited snapshot corpus.
//!
//! This test deliberately consumes only portable CDTO envelopes. It never
//! reads legacy player files or writes a replay result anywhere.

use muhan_core_dto::player_snapshot_v1::{
    verify_player_snapshot_post_save_shadow_v1, verify_player_snapshot_replay_v1,
};
use muhan_core_dto::{encode, Error, Field, Kind, Record, OBJECT_GRAPH_V1_NODE_LENGTH};
use std::collections::HashSet;
use std::fs;
use std::path::Path;

const CORPUS: &str =
    include_str!("../../../tests/fixtures/player_snapshot_v1_projection_corpus_v1.tsv");

#[derive(Debug)]
struct Case<'a> {
    id: &'a str,
    profile: &'a str,
    c_class: &'a str,
    canonical_fixture: &'a str,
    canonical_sha256: &'a str,
    mutation: &'a str,
}

fn corpus() -> Vec<Case<'static>> {
    let mut rows = Vec::new();
    let mut ids = HashSet::new();
    let mut saw_header = false;

    for line in CORPUS.lines().filter(|line| !line.is_empty()) {
        if let Some(header) = line.strip_prefix("# format=") {
            assert_eq!(
                header, "player-snapshot-v1-projection-corpus;version=1",
                "corpus format remains pinned"
            );
            saw_header = true;
            continue;
        }
        if line.starts_with('#') {
            continue;
        }
        let columns: Vec<_> = line.split('\t').collect();
        assert_eq!(columns.len(), 7, "corpus row has seven columns: {line}");
        let row = Case {
            id: columns[0],
            profile: columns[2],
            c_class: columns[3],
            canonical_fixture: columns[4],
            canonical_sha256: columns[5],
            mutation: columns[6],
        };
        assert!(ids.insert(row.id), "fixture id is unique: {}", row.id);
        assert!(
            matches!(row.profile, "rich" | "minimal" | "persisted-graph"),
            "fixture={} has a supported profile",
            row.id
        );
        rows.push(row);
    }

    assert!(saw_header, "corpus has its version header");
    assert_eq!(rows.len(), 6, "corpus growth requires deliberate review");
    assert_eq!(
        rows.iter().filter(|row| row.c_class == "accept").count(),
        3,
        "rich, minimal, and persisted-graph canonical envelopes are covered"
    );
    rows
}

fn decode_hex(value: &str) -> Vec<u8> {
    let value = value.trim();
    assert_eq!(value.len() % 2, 0, "hex contains whole octets");
    (0..value.len())
        .step_by(2)
        .map(|index| u8::from_str_radix(&value[index..index + 2], 16).expect("fixture hex"))
        .collect()
}

fn digest(value: &str) -> [u8; 32] {
    decode_hex(value)
        .try_into()
        .expect("corpus SHA-256 has exactly 32 octets")
}

fn fixture(name: &str) -> Vec<u8> {
    let root = Path::new(env!("CARGO_MANIFEST_DIR")).join("../../tests/fixtures");
    decode_hex(&fs::read_to_string(root.join(name)).expect("checked-in CDTO fixture"))
}

fn profile_fixture(profile: &str) -> Vec<u8> {
    fixture(match profile {
        "rich" => "player_snapshot_v1_legacy_decoder_canonical.hex",
        "minimal" => "player_snapshot_v1_legacy_decoder_minimal.hex",
        "persisted-graph" => "player_snapshot_v1_legacy_decoder_persisted_graph.hex",
        other => panic!("unknown corpus profile: {other}"),
    })
}

fn mutate(wire: &[u8], mutation: &str) -> Vec<u8> {
    match mutation {
        "canonical" => wire.to_vec(),
        "truncate-last" => wire[..wire.len() - 1].to_vec(),
        "append-7f" => [wire, &[0x7f]].concat(),
        "canonical-invalid-player-type" => {
            let record = muhan_core_dto::decode(wire).expect("rich fixture envelope decodes");
            let mut fields = record.fields().to_vec();
            fields[7] = Field::i8(8, -1);
            encode(
                &Record::new(Kind::PlayerSnapshot, fields)
                    .expect("bad type remains a generic canonical CDTO record"),
            )
            .expect("bad type envelope retains a digest")
        }
        other => panic!("unknown corpus mutation: {other}"),
    }
}

#[test]
fn post_save_shadow_verifier_binds_the_pinned_corpus_to_replay_reports() {
    for row in corpus() {
        let source = profile_fixture(row.profile);
        let wire = mutate(&source, row.mutation);

        if row.c_class == "accept" {
            assert_eq!(wire, fixture(row.canonical_fixture), "fixture={}", row.id);
            let report =
                verify_player_snapshot_post_save_shadow_v1(&wire, &digest(row.canonical_sha256))
                    .expect("canonical corpus envelope verifies");
            assert_eq!(
                report,
                verify_player_snapshot_replay_v1(&wire).expect("replay report verifies"),
                "fixture={} keeps the exact replay report semantics",
                row.id
            );
        } else {
            let result = verify_player_snapshot_post_save_shadow_v1(&wire, &[0; 32]);
            match row.mutation {
                "truncate-last" => assert!(
                    matches!(result, Err(Error::Truncated { .. })),
                    "fixture={} rejects truncation",
                    row.id
                ),
                "append-7f" => assert_eq!(
                    result,
                    Err(Error::TrailingBytes),
                    "fixture={} rejects trailing bytes",
                    row.id
                ),
                "canonical-invalid-player-type" => assert_eq!(
                    result,
                    Err(Error::InvalidFieldLength { field_id: 0 }),
                    "fixture={} rejects a digest-valid bad player type",
                    row.id
                ),
                other => panic!("unexpected rejection mutation: {other}"),
            }
        }
    }
}

#[test]
fn post_save_shadow_verifier_rejects_digest_mismatch_and_malformed_graphs() {
    let wire = profile_fixture("rich");
    assert_eq!(
        verify_player_snapshot_post_save_shadow_v1(&wire, &[0; 32]),
        Err(Error::DigestMismatch),
        "a canonical envelope cannot be accepted under an unrelated SHA-256"
    );

    let invalid_graph = encode(
        &Record::new(
            Kind::ObjectGraph,
            vec![
                Field::u32(1, 1),
                Field::bytes(2, vec![0; OBJECT_GRAPH_V1_NODE_LENGTH]),
            ],
        )
        .expect("graph field metadata is canonical"),
    )
    .expect("invalid graph still has an envelope digest");
    let record = muhan_core_dto::decode(&profile_fixture("minimal")).expect("fixture decodes");
    let mut fields = record.fields().to_vec();
    fields[39] = Field::bytes(40, invalid_graph);
    let malformed_graph_snapshot =
        encode(&Record::new(Kind::PlayerSnapshot, fields).expect("outer record remains canonical"))
            .expect("outer envelope has a digest");
    assert_eq!(
        verify_player_snapshot_post_save_shadow_v1(&malformed_graph_snapshot, &[0; 32]),
        Err(Error::InvalidFieldLength { field_id: 2 })
    );
}
