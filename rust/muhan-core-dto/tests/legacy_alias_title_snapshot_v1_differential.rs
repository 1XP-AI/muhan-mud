//! Default-off characterization corpus for the legacy alias/title sidecar.
//!
//! C alone admits legacy file bytes. Rust consumes only the CDTO snapshot that
//! C emitted, preserving legacy C authority while pinning the port boundary.

use muhan_core_dto::alias_title_snapshot_v1::{
    decode_alias_title_snapshot_v1, encode_alias_title_snapshot_v1, AliasTitleSnapshotV1,
    ALIAS_MAX_BYTES, MAX_ALIASES, PROCESS_MAX_BYTES, TITLE_MAX_BYTES,
};
use std::collections::HashSet;
use std::env;
use std::fs;
use std::path::{Path, PathBuf};
use std::process::Command;

const CORPUS: &str = include_str!("../../../tests/fixtures/alias_title_snapshot_v1_corpus_v1.tsv");

#[derive(Debug)]
struct Case<'a> {
    id: &'a str,
    class: &'a str,
    legacy_fixture: &'a str,
    canonical_fixture: &'a str,
    digest: &'a str,
}

fn corpus() -> Vec<Case<'static>> {
    let mut rows = Vec::new();
    let mut ids = HashSet::new();
    let mut header = false;
    for line in CORPUS.lines() {
        if let Some(value) = line.strip_prefix("# format=") {
            assert_eq!(value, "alias-title-snapshot-v1-corpus;version=1");
            header = true;
            continue;
        }
        if line.starts_with('#') || line.is_empty() {
            continue;
        }
        let columns: Vec<_> = line.split('\t').collect();
        assert_eq!(columns.len(), 5, "corpus row has five columns: {line}");
        let row = Case {
            id: columns[0],
            class: columns[1],
            legacy_fixture: columns[2],
            canonical_fixture: columns[3],
            digest: columns[4],
        };
        assert!(ids.insert(row.id), "unique fixture id: {}", row.id);
        assert!(matches!(row.class, "accept" | "reject"));
        if row.class == "accept" {
            assert!(row.canonical_fixture.ends_with(".hex"));
            assert_eq!(row.digest.len(), 64);
            assert!(row.digest.bytes().all(|byte| byte.is_ascii_hexdigit()));
        } else {
            assert_eq!(row.canonical_fixture, "-");
            assert_eq!(row.digest, "-");
        }
        rows.push(row);
    }
    assert!(header, "version-pinned corpus header");
    assert_eq!(rows.len(), 7, "corpus growth requires explicit review");
    assert_eq!(rows.iter().filter(|row| row.class == "accept").count(), 3);
    assert_eq!(rows.iter().filter(|row| row.class == "reject").count(), 4);
    rows
}

fn oracle() -> Option<PathBuf> {
    env::var_os("LEGACY_ALIAS_TITLE_SNAPSHOT_V1_C_ORACLE").map(PathBuf::from)
}

fn run(oracle: &Path, fixture: &Path) -> String {
    let output = Command::new(oracle)
        .args(["classify", fixture.to_str().expect("fixture path is UTF-8")])
        .output()
        .expect("C AliasTitleSnapshotV1 oracle must launch");
    assert!(
        output.status.success(),
        "C oracle fixture {:?} failed: {}",
        fixture,
        String::from_utf8_lossy(&output.stderr)
    );
    String::from_utf8(output.stdout)
        .expect("C oracle returns ASCII")
        .trim()
        .to_owned()
}

fn unhex(value: &str) -> Vec<u8> {
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

fn fixtures() -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR")).join("../../tests/fixtures")
}

#[test]
fn c_authoritative_alias_title_corpus_has_a_pinned_rust_boundary() {
    let Some(oracle) = oracle() else {
        eprintln!(
            "skipping C AliasTitleSnapshotV1 corpus; run scripts/run-legacy-alias-title-snapshot-v1-differential.sh"
        );
        return;
    };
    let root = fixtures();
    for row in corpus() {
        let result = run(&oracle, &root.join(row.legacy_fixture));
        if row.class == "reject" {
            assert_eq!(result, "reject", "{} must not publish a snapshot", row.id);
            continue;
        }
        let c_wire = unhex(result.strip_prefix("accept ").expect("accepted C output"));
        let expected = unhex(
            &fs::read_to_string(root.join(row.canonical_fixture)).expect("pinned CDTO fixture"),
        );
        assert_eq!(c_wire, expected, "{} C canonical bytes", row.id);
        assert_eq!(
            hex(&c_wire[c_wire.len() - 32..]),
            row.digest,
            "{} CDTO canonical digest",
            row.id
        );
        let snapshot = decode_alias_title_snapshot_v1(&c_wire)
            .expect("C-authoritative canonical CDTO must decode in Rust");
        assert_eq!(
            encode_alias_title_snapshot_v1(&snapshot).unwrap(),
            expected,
            "{} Rust re-encoding must be byte stable",
            row.id
        );
        match row.id {
            "valid-ordering" => {
                assert_eq!(
                    snapshot
                        .aliases
                        .iter()
                        .map(|entry| entry.alias.as_slice())
                        .collect::<Vec<_>>(),
                    [b"n".as_slice(), b"a".as_slice()],
                    "aliases retain persisted order"
                );
                assert_eq!(snapshot.title, Some(b"the Swift".to_vec()));
            }
            "valid-boundary" => {
                assert_eq!(snapshot.aliases.len(), 1);
                assert_eq!(snapshot.aliases[0].alias.len(), ALIAS_MAX_BYTES);
                assert_eq!(snapshot.aliases[0].process.len(), PROCESS_MAX_BYTES);
                assert_eq!(snapshot.title.as_ref().unwrap().len(), TITLE_MAX_BYTES);
                assert!(MAX_ALIASES >= snapshot.aliases.len());
            }
            "valid-max-count" => {
                assert_eq!(snapshot.aliases.len(), MAX_ALIASES);
                assert!(snapshot.title.is_none());
            }
            other => panic!("unexpected accepted corpus member: {other}"),
        }
    }
}

#[test]
fn rust_boundary_rejects_noncanonical_alias_identity() {
    let duplicate = AliasTitleSnapshotV1 {
        aliases: vec![
            muhan_core_dto::alias_title_snapshot_v1::AliasTitleEntryV1 {
                alias: b"n".to_vec(),
                process: b"north".to_vec(),
            },
            muhan_core_dto::alias_title_snapshot_v1::AliasTitleEntryV1 {
                alias: b"n".to_vec(),
                process: b"northeast".to_vec(),
            },
        ],
        title: None,
    };
    assert!(
        encode_alias_title_snapshot_v1(&duplicate).is_err(),
        "duplicate aliases cannot gain a canonical digest"
    );
}
