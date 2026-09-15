//! Offline v1 corpus at the audited legacy-player -> portable snapshot boundary.
//!
//! The C oracle first produces each profile's portable raw CDTO fixture from
//! legacy bytes. Both the C PlayerSnapshot decoder and the Rust DTO decoder
//! then consume that exact raw fixture (or an explicitly named mutation).
//! No admission/name/shard/identity metadata is represented here.

use muhan_core_dto::player_snapshot_v1::{
    decode_player_snapshot_v1, encode_player_snapshot_v1, verify_player_snapshot_replay_v1,
};
use muhan_core_dto::{decode, encode, Field, Kind, Record};
use std::collections::HashSet;
use std::env;
use std::fs;
use std::path::{Path, PathBuf};
use std::process::Command;

const ABI: &str = "legacy-player-snapshot-v1/raw-v1;endian=little;char=8;short=16;int=32;long=64;ptr=64;creature=1952;object=376;creature.level=318;creature.type=319;creature.hpmax=332;creature.hpcur=334;creature.mpmax=336;creature.mpcur=338;creature.gold=352;creature.first_obj=1920;object.value=304;object.shotsmax=316;object.shotscur=318;object.first_obj=344";
const CORPUS: &str =
    include_str!("../../../tests/fixtures/player_snapshot_v1_projection_corpus_v1.tsv");

#[derive(Debug)]
struct Case<'a> {
    id: &'a str,
    abi: &'a str,
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
    for line in CORPUS.lines() {
        if line.is_empty() {
            continue;
        }
        if let Some(header) = line.strip_prefix("# format=") {
            assert_eq!(
                header, "player-snapshot-v1-projection-corpus;version=1",
                "corpus format is version pinned"
            );
            saw_header = true;
            continue;
        }
        if line.starts_with('#') {
            continue;
        }
        let columns: Vec<_> = line.split('\t').collect();
        assert_eq!(
            columns.len(),
            7,
            "corpus row has seven pinned columns: {line}"
        );
        let row = Case {
            id: columns[0],
            abi: columns[1],
            profile: columns[2],
            c_class: columns[3],
            canonical_fixture: columns[4],
            canonical_sha256: columns[5],
            mutation: columns[6],
        };
        assert!(
            !row.id.is_empty() && ids.insert(row.id),
            "fixture id is unique: {}",
            row.id
        );
        assert_eq!(row.abi, ABI, "fixture={} has the audited ABI", row.id);
        assert!(
            matches!(row.profile, "rich" | "minimal" | "persisted-graph"),
            "fixture={} has a known profile",
            row.id
        );
        assert!(
            matches!(row.c_class, "accept" | "reject"),
            "fixture={} has a C class",
            row.id
        );
        if row.c_class == "accept" {
            assert!(row.canonical_fixture.ends_with(".hex"));
            assert_eq!(row.canonical_sha256.len(), 64);
            assert!(row
                .canonical_sha256
                .bytes()
                .all(|byte| byte.is_ascii_hexdigit()));
            assert_eq!(row.mutation, "canonical");
        } else {
            assert_eq!(row.canonical_fixture, "-");
            assert_eq!(row.canonical_sha256, "-");
            assert_ne!(row.mutation, "canonical");
        }
        rows.push(row);
    }
    assert!(saw_header, "corpus has a version header");
    assert_eq!(rows.len(), 6, "corpus growth must be reviewed deliberately");
    assert_eq!(
        rows.iter().filter(|row| row.c_class == "accept").count(),
        3,
        "all audited legacy profiles remain represented"
    );
    assert_eq!(
        rows.iter().filter(|row| row.c_class == "reject").count(),
        3,
        "three meaningful malformed raw snapshot mutations remain represented"
    );
    rows
}

fn oracle() -> Option<PathBuf> {
    env::var_os("LEGACY_PLAYER_SNAPSHOT_V1_C_ORACLE").map(PathBuf::from)
}

fn run(oracle: &Path, args: &[&str]) -> String {
    let output = Command::new(oracle)
        .args(args)
        .output()
        .expect("C PlayerSnapshot corpus oracle must launch");
    assert!(
        output.status.success(),
        "C PlayerSnapshot corpus oracle command {:?} failed: {}",
        args,
        String::from_utf8_lossy(&output.stderr)
    );
    String::from_utf8(output.stdout)
        .expect("C PlayerSnapshot corpus oracle returns ASCII")
        .trim()
        .to_owned()
}

fn decode_hex(value: &str) -> Vec<u8> {
    let value = value.trim();
    assert_eq!(value.len() % 2, 0, "hex fixture contains whole octets");
    (0..value.len())
        .step_by(2)
        .map(|index| u8::from_str_radix(&value[index..index + 2], 16).expect("fixture hex"))
        .collect()
}

fn hex(value: &[u8]) -> String {
    value.iter().map(|byte| format!("{byte:02x}")).collect()
}

fn canonical_fixture(name: &str) -> Vec<u8> {
    let root = Path::new(env!("CARGO_MANIFEST_DIR")).join("../../tests/fixtures");
    decode_hex(&fs::read_to_string(root.join(name)).expect("checked-in projection fixture"))
}

fn mutate(base: &[u8], mutation: &str) -> Vec<u8> {
    match mutation {
        "canonical" => base.to_vec(),
        "truncate-last" => base[..base.len() - 1].to_vec(),
        "append-7f" => [base, &[0x7f]].concat(),
        "canonical-invalid-player-type" => {
            let record = decode(base).expect("C-derived raw fixture retains a generic envelope");
            let mut fields = record.fields().to_vec();
            fields[7] = Field::i8(8, -1);
            encode(
                &Record::new(Kind::PlayerSnapshot, fields)
                    .expect("mutation remains a generic canonical CDTO record"),
            )
            .expect("mutation has an envelope digest")
        }
        other => panic!("unknown corpus mutation: {other}"),
    }
}

fn fixture_from_c_oracle(oracle: &Path, profile: &str) -> Vec<u8> {
    decode_hex(&run(oracle, &["fixture", profile]))
}

fn classify_c_portable_projection(
    oracle: &Path,
    directory: &Path,
    id: &str,
    wire: &[u8],
) -> String {
    let path = directory.join(format!("{id}.hex"));
    fs::write(&path, format!("{}\n", hex(wire))).expect("raw corpus fixture is writable");
    run(
        oracle,
        &[
            "project-portable",
            path.to_str().expect("temporary fixture path is UTF-8"),
        ],
    )
}

#[test]
fn versioned_offline_player_snapshot_projection_corpus_has_no_c_rust_divergence() {
    let Some(oracle) = oracle() else {
        eprintln!(
            "skipping C PlayerSnapshot projection corpus; run scripts/run-legacy-player-snapshot-v1-differential.sh"
        );
        return;
    };
    assert_eq!(
        run(&oracle, &["abi-fingerprint"]),
        ABI,
        "C ABI matches the corpus"
    );

    let temp = env::temp_dir().join(format!(
        "muhan-player-snapshot-projection-corpus-{}",
        std::process::id()
    ));
    fs::create_dir_all(&temp).expect("corpus temporary directory is writable");

    for row in corpus() {
        let source = fixture_from_c_oracle(&oracle, row.profile);
        let wire = mutate(&source, row.mutation);
        let c_result = classify_c_portable_projection(&oracle, &temp, row.id, &wire);
        let rust_result = decode_player_snapshot_v1(&wire);
        let c_accepts = c_result.starts_with("accept ");
        assert_eq!(
            c_accepts,
            row.c_class == "accept",
            "fixture={} C success/rejection class",
            row.id
        );
        assert_eq!(
            rust_result.is_ok(),
            c_accepts,
            "fixture={} C and Rust consume the same raw fixture with the same result",
            row.id
        );

        if row.c_class == "accept" {
            let expected = canonical_fixture(row.canonical_fixture);
            assert_eq!(
                source, expected,
                "fixture={} legacy C projection remains pinned to its expected bytes",
                row.id
            );
            assert_eq!(
                wire, expected,
                "fixture={} canonical raw bytes are pinned",
                row.id
            );
            let c_canonical = decode_hex(c_result.strip_prefix("accept ").unwrap());
            assert_eq!(
                c_canonical, expected,
                "fixture={} C canonical projection",
                row.id
            );
            let snapshot = rust_result.unwrap();
            assert_eq!(
                encode_player_snapshot_v1(&snapshot).unwrap(),
                expected,
                "fixture={} Rust canonical projection",
                row.id
            );
            let report = verify_player_snapshot_replay_v1(&wire).unwrap();
            assert_eq!(
                hex(&report.canonical_digest),
                row.canonical_sha256,
                "fixture={} canonical projection digest",
                row.id
            );
        } else {
            assert_eq!(
                c_result, "reject",
                "fixture={} C rejection is closed",
                row.id
            );
            assert!(
                verify_player_snapshot_replay_v1(&wire).is_err(),
                "fixture={} malformed raw fixture cannot publish a replay report",
                row.id
            );
        }
    }

    fs::remove_dir_all(temp).expect("corpus temporary directory is removable");
}
