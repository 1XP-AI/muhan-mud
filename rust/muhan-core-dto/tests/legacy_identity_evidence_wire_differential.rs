use muhan_core_dto::legacy_identity_evidence_v1::{
    decode, encode, Canonicalization, LegacyIdentityEvidenceV1, Outcome, STORAGE_FORMAT,
};
use std::{env, path::PathBuf, process::Command};

fn oracle() -> Option<PathBuf> {
    env::var_os("LEGACY_IDENTITY_EVIDENCE_WIRE_C_ORACLE").map(PathBuf::from)
}
fn hex(value: &[u8]) -> String {
    value.iter().map(|byte| format!("{byte:02x}")).collect()
}
fn unhex(value: &str) -> Vec<u8> {
    let value = value.trim();
    (0..value.len())
        .step_by(2)
        .map(|i| u8::from_str_radix(&value[i..i + 2], 16).unwrap())
        .collect()
}
fn run(oracle: &PathBuf, args: &[String]) -> String {
    let output = Command::new(oracle).args(args).output().unwrap();
    assert!(output.status.success());
    String::from_utf8(output.stdout).unwrap().trim().into()
}
fn value() -> LegacyIdentityEvidenceV1 {
    LegacyIdentityEvidenceV1 {
        outcome: Outcome::Ok,
        canonicalization: Canonicalization::Normalized,
        canonical_name: "Alice".into(),
        legacy_shard: "35".into(),
        player_file_sha256: "18f8d2eb4a387bbc1e37ec099a7326805739bc9c99ecf0f14b808a5bcb65bf49"
            .into(),
        storage_format: STORAGE_FORMAT.into(),
    }
}
fn nul_text_mutations(wire: &[u8]) -> Vec<Vec<u8>> {
    let mut at = 18;
    let mut mutations = Vec::new();
    for _ in 0..4 {
        let len = wire[at] as usize;
        at += 1;
        if len != 0 {
            let mut mutation = wire.to_vec();
            mutation[at] = 0;
            mutations.push(mutation);
        }
        at += len;
    }
    mutations
}

#[test]
fn c_and_rust_accept_and_produce_identical_closed_identity_evidence_bytes() {
    let Some(oracle) = oracle() else {
        eprintln!(
            "skipping C oracle; run scripts/run-legacy-identity-evidence-wire-differential.sh"
        );
        return;
    };
    let mut not_found = value();
    not_found.outcome = Outcome::NotFound;
    not_found.canonicalization = Canonicalization::Canonical;
    not_found.player_file_sha256.clear();
    let mut corrupt = not_found.clone();
    corrupt.outcome = Outcome::Corrupt;
    let mut io_error = not_found.clone();
    io_error.outcome = Outcome::IoError;
    let invalid_input = LegacyIdentityEvidenceV1 {
        outcome: Outcome::InvalidInput,
        canonicalization: Canonicalization::Invalid,
        canonical_name: String::new(),
        legacy_shard: String::new(),
        player_file_sha256: String::new(),
        storage_format: STORAGE_FORMAT.into(),
    };
    for (label, expected) in [
        ("ok", value()),
        ("not-found", not_found),
        ("corrupt", corrupt),
        ("io-error", io_error),
        ("invalid-input", invalid_input),
    ] {
        let c_wire = unhex(&run(&oracle, &["fixture".into(), label.into()]));
        let rust_wire = encode(&expected).unwrap();
        assert_eq!(
            c_wire, rust_wire,
            "C and Rust valid {label} encoding matches exactly"
        );
        assert_eq!(decode(&c_wire).unwrap(), expected);
        assert_eq!(
            run(&oracle, &["decode".into(), hex(&rust_wire)]),
            "0",
            "C accepts Rust {label} bytes"
        );
        assert_eq!(
            run(&oracle, &["roundtrip".into(), hex(&rust_wire)]),
            hex(&rust_wire),
            "C preserves Rust {label} bytes"
        );
    }
    let rust_wire = encode(&value()).unwrap();
    for mutation in [
        {
            let mut wire = rust_wire.clone();
            wire[11] = 2;
            wire
        },
        {
            let mut wire = rust_wire.clone();
            wire[16] = 99;
            wire
        },
        {
            let mut wire = rust_wire.clone();
            wire.push(0);
            wire
        },
    ] {
        assert!(
            decode(&mutation).is_err(),
            "Rust rejects malformed/noncanonical input"
        );
        assert_eq!(
            run(&oracle, &["decode".into(), hex(&mutation)]),
            "1",
            "C rejects the same malformed/noncanonical input"
        );
    }
    for mutation in nul_text_mutations(&rust_wire) {
        assert!(
            decode(&mutation).is_err(),
            "Rust rejects an embedded NUL in every canonical text field"
        );
        assert_eq!(
            run(&oracle, &["decode".into(), hex(&mutation)]),
            "1",
            "C rejects the same embedded-NUL canonical text vector"
        );
    }
}
