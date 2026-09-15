use muhan_core_dto::player_snapshot_normalized_v1::project_player_snapshot_v1_artifact;
use muhan_core_dto::player_snapshot_v1::{
    decode_player_snapshot_v1, encode_player_snapshot_v1, verify_player_snapshot_replay_v1,
};
use muhan_core_dto::{decode, encode, Field, Kind, Record};
use std::fmt::Write as _;
use std::io::Write;
use std::process::{Command, Output, Stdio};

const REJECTION: &[u8] = b"rejected: invalid player snapshot CDTO\n";

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

fn digest_hex(digest: &[u8]) -> String {
    const HEX: &[u8; 16] = b"0123456789abcdef";
    let mut output = String::with_capacity(digest.len() * 2);
    for &byte in digest {
        output.push(HEX[(byte >> 4) as usize] as char);
        output.push(HEX[(byte & 0x0f) as usize] as char);
    }
    output
}

fn artifact_digest(wire: &[u8]) -> String {
    digest_hex(
        &verify_player_snapshot_replay_v1(wire)
            .expect("fixture verifies")
            .canonical_digest,
    )
}

fn run_projection_runner(wire: &[u8], expected_digest: &str) -> Output {
    let mut child = Command::new(env!("CARGO_BIN_EXE_player_snapshot_v1_normalized_project"))
        .arg("--snapshot-sha256")
        .arg(expected_digest)
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
        .expect("normalized projection runner launches");
    child
        .stdin
        .take()
        .expect("runner stdin is piped")
        .write_all(wire)
        .expect("runner accepts fixture input");
    child.wait_with_output().expect("runner exits")
}

fn expected_output(wire: &[u8]) -> String {
    let projection = project_player_snapshot_v1_artifact(wire).expect("fixture projects");
    let mut output = format!(
        "{{\"format\":\"player-snapshot-v1-normalized-projection\",\"version\":1,\"algorithm\":\"sha-256\",\"canonical_digest\":\"{}\",\"player\":{{\"level\":{},\"hp_max\":{},\"hp_current\":{},\"mp_max\":{},\"mp_current\":{},\"experience\":{},\"gold\":{},\"daily\":[",
        digest_hex(&projection.canonical_digest()),
        projection.level,
        projection.hp_max,
        projection.hp_current,
        projection.mp_max,
        projection.mp_current,
        projection.experience,
        projection.gold,
    );
    for (index, daily) in projection.daily.iter().enumerate() {
        if index != 0 {
            output.push(',');
        }
        write!(
            output,
            "{{\"max\":{},\"current\":{},\"last_used\":{}}}",
            daily.max, daily.current, daily.last_used
        )
        .expect("writing a String cannot fail");
    }
    output.push_str("],\"timers\":[");
    for (index, timer) in projection.timers.iter().enumerate() {
        if index != 0 {
            output.push(',');
        }
        write!(
            output,
            "{{\"interval\":{},\"last_used\":{},\"misc\":{}}}",
            timer.interval, timer.last_used, timer.misc
        )
        .expect("writing a String cannot fail");
    }
    output.push_str("],\"items\":[");
    for (index, item) in projection.items.iter().enumerate() {
        if index != 0 {
            output.push(',');
        }
        let parent = item
            .parent_index
            .map_or_else(|| "null".to_owned(), |value| value.to_string());
        write!(
            output,
            "{{\"parent_index\":{parent},\"child_index\":{},\"value\":{},\"weight\":{},\"type_code\":{},\"adjustment\":{},\"shots_max\":{},\"shots_current\":{},\"ndice\":{},\"sdice\":{},\"pdice\":{},\"armor\":{},\"wear_flag\":{},\"magic_power\":{},\"magic_realm\":{},\"special\":{}}}",
            item.child_index,
            item.value,
            item.weight,
            item.type_code,
            item.adjustment,
            item.shots_max,
            item.shots_current,
            item.ndice,
            item.sdice,
            item.pdice,
            item.armor,
            item.wear_flag,
            item.magic_power,
            item.magic_realm,
            item.special,
        )
        .expect("writing a String cannot fail");
    }
    output.push_str("]}}\n");
    output
}

#[test]
fn normalized_projection_cli_is_versioned_machine_readable_and_byte_stable() {
    let wire = tree_fixture();
    let expected_digest = artifact_digest(&wire);

    let first = run_projection_runner(&wire, &expected_digest);
    let second = run_projection_runner(&wire, &expected_digest);

    assert!(first.status.success(), "runner accepts canonical fixture");
    assert_eq!(first.stdout, expected_output(&wire).as_bytes());
    assert_eq!(first.stdout, second.stdout, "repeated input is byte-stable");
    assert!(first.stderr.is_empty(), "successful run has no diagnostics");
}

#[test]
fn normalized_projection_cli_is_invariant_to_excluded_source_fields() {
    let wire = tree_fixture();
    let baseline = run_projection_runner(&wire, &artifact_digest(&wire));
    assert!(baseline.status.success());

    let mut snapshot = decode_player_snapshot_v1(&wire).expect("fixture decodes");
    snapshot.name[..12].copy_from_slice(b"NO_LEAK_TEXT");
    snapshot.description[..11].copy_from_slice(b"PRIVATE_BIO");
    snapshot.key[0][..10].copy_from_slice(b"KEY_SECRET");
    snapshot.flags[0] ^= 1;
    snapshot.quests[0] ^= 1;
    snapshot.inventory.nodes[0].object.name[..11].copy_from_slice(b"ITEM_SECRET");
    snapshot.inventory.nodes[0].object.key[0][..8].copy_from_slice(b"ITEM_KEY");
    snapshot.inventory.nodes[0].object.use_output[..10].copy_from_slice(b"USE_OUTPUT");
    snapshot.inventory.nodes[0].object.flags[0] ^= 1;
    let mutated = encode_player_snapshot_v1(&snapshot).expect("safe-field mutation encodes");

    let output = run_projection_runner(&mutated, &artifact_digest(&mutated));
    assert!(
        output.status.success(),
        "excluded fields do not prevent projection"
    );
    assert_eq!(
        output.stdout, baseline.stdout,
        "excluded values never cross the boundary"
    );
    let rendered = String::from_utf8(output.stdout).expect("report is UTF-8");
    for forbidden in [
        "NO_LEAK_TEXT",
        "PRIVATE_BIO",
        "KEY_SECRET",
        "ITEM_SECRET",
        "ITEM_KEY",
        "USE_OUTPUT",
        "flags",
        "quests",
    ] {
        assert!(!rendered.contains(forbidden), "output excludes {forbidden}");
    }
}

#[test]
fn normalized_projection_cli_rejects_invalid_and_tampered_artifacts_without_output() {
    let wire = canonical_fixture();
    let mut tampered = wire.clone();
    *tampered.last_mut().expect("fixture has bytes") ^= 1;

    let record = decode(&wire).expect("fixture is a generic envelope");
    let mut fields = record.fields().to_vec();
    fields[7] = Field::i8(8, -1);
    let invalid =
        encode(&Record::new(Kind::PlayerSnapshot, fields).expect("field metadata stays canonical"))
            .expect("outer digest is valid");

    for (name, malformed) in [("tampered", tampered), ("invalid", invalid)] {
        let output = run_projection_runner(&malformed, &"0".repeat(64));
        assert!(!output.status.success(), "{name} input is rejected");
        assert!(
            output.stdout.is_empty(),
            "{name} input has no partial output"
        );
        assert_eq!(
            output.stderr, REJECTION,
            "{name} input gets one generic rejection"
        );
    }
}

#[test]
fn normalized_projection_cli_rejects_expected_artifact_digest_mismatch_without_output() {
    let wire = canonical_fixture();
    let output = run_projection_runner(&wire, &"0".repeat(64));

    assert!(
        !output.status.success(),
        "mismatched expected artifact digest is rejected"
    );
    assert!(output.stdout.is_empty(), "mismatch has no partial output");
    assert_eq!(output.stderr, REJECTION);
}
