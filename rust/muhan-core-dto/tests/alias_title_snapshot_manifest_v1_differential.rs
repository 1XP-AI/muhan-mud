use muhan_core_dto::alias_title_snapshot_manifest_v1::{
    decode_alias_title_snapshot_manifest_v1, encode_alias_title_snapshot_manifest_v1,
};
use std::{env, path::PathBuf, process::Command};

fn oracle() -> Option<PathBuf> {
    env::var_os("ALIAS_TITLE_SNAPSHOT_MANIFEST_V1_C_ORACLE").map(PathBuf::from)
}

fn hex(value: &[u8]) -> String {
    value.iter().map(|byte| format!("{byte:02x}")).collect()
}

fn unhex(value: &str) -> Vec<u8> {
    let value = value.trim();
    (0..value.len())
        .step_by(2)
        .map(|index| u8::from_str_radix(&value[index..index + 2], 16).unwrap())
        .collect()
}

fn run(oracle: &PathBuf, arguments: &[String]) -> String {
    let output = Command::new(oracle).args(arguments).output().unwrap();
    assert!(
        output.status.success(),
        "{}",
        String::from_utf8_lossy(&output.stderr)
    );
    String::from_utf8(output.stdout).unwrap().trim().to_owned()
}

#[test]
fn c_and_rust_preserve_the_pinned_manifest_fixture_exactly() {
    let Some(oracle) = oracle() else {
        eprintln!(
            "skipping C oracle; run scripts/run-alias-title-snapshot-manifest-v1-differential.sh"
        );
        return;
    };
    let fixture = unhex(include_str!(
        "../../../tests/fixtures/alias_title_snapshot_manifest_v1_canonical.hex"
    ));
    assert_eq!(unhex(&run(&oracle, &["fixture".into()])), fixture);
    let rust = decode_alias_title_snapshot_manifest_v1(&fixture).unwrap();
    let rust_wire = encode_alias_title_snapshot_manifest_v1(&rust).unwrap();
    assert_eq!(rust_wire, fixture);
    assert_eq!(run(&oracle, &["decode".into(), hex(&rust_wire)]), "0");
    assert_eq!(
        unhex(&run(&oracle, &["roundtrip".into(), hex(&rust_wire)])),
        fixture
    );
}
