//! End-to-end boundary test for the default-unwired artifact observer.
//!
//! `scripts/run-player-snapshot-v1-artifact-shadow-verifier-differential.sh`
//! builds the C producer and supplies it here.  The positive artifact is
//! emitted by `character_player_snapshot_v1_artifact_store`, not assembled by
//! this Rust test.

use std::env;
use std::io::Write;
use std::process::{Command, Output, Stdio};

fn producer() -> Option<String> {
    env::var("PLAYER_SNAPSHOT_V1_ARTIFACT_C_PRODUCER").ok()
}

fn c_produced_artifact() -> Vec<u8> {
    let output = Command::new(producer().expect("C artifact producer is configured"))
        .output()
        .expect("C artifact producer launches");
    assert!(output.status.success(), "C artifact producer succeeds");
    assert!(
        !output.stdout.is_empty(),
        "C artifact producer emitted an artifact"
    );
    output.stdout
}

fn verify(artifact: &[u8]) -> Output {
    let mut child = Command::new(env!(
        "CARGO_BIN_EXE_player_snapshot_v1_artifact_shadow_verify"
    ))
    .stdin(Stdio::piped())
    .stdout(Stdio::piped())
    .stderr(Stdio::piped())
    .spawn()
    .expect("shadow verifier launches");
    child
        .stdin
        .take()
        .expect("shadow verifier stdin is piped")
        .write_all(artifact)
        .expect("shadow verifier accepts fixture input");
    child.wait_with_output().expect("shadow verifier exits")
}

fn reject(artifact: &[u8]) {
    let output = verify(artifact);
    assert_eq!(output.status.code(), Some(1));
    assert!(output.stdout.is_empty());
    assert_eq!(
        output.stderr,
        b"rejected: invalid player snapshot artifact\n"
    );
}

#[test]
fn c_produced_artifact_is_observed_and_malformed_variants_fail_closed() {
    let Some(_) = producer() else {
        eprintln!(
            "skipping C artifact boundary differential; run scripts/run-player-snapshot-v1-artifact-shadow-verifier-differential.sh"
        );
        return;
    };
    let artifact = c_produced_artifact();

    let first = verify(&artifact);
    let second = verify(&artifact);
    assert!(first.status.success());
    assert_eq!(first, second, "observation result is deterministic");
    assert!(first.stderr.is_empty());
    let report = String::from_utf8(first.stdout).expect("report is machine-readable UTF-8");
    assert!(report.starts_with(
        "format=player-snapshot-v1-artifact-shadow-verification\nversion=1\nalgorithm=sha-256\n"
    ));
    assert!(report.ends_with("\n"));

    let mut noncanonical_header = artifact.clone();
    noncanonical_header.splice(0..0, b"ignored=x\n".iter().copied());
    reject(&noncanonical_header);

    let mut declared_length_mismatch = artifact.clone();
    let value_start = declared_length_mismatch
        .windows(b"snapshot_octets=".len())
        .position(|window| window == b"snapshot_octets=")
        .expect("fixture has snapshot length")
        + b"snapshot_octets=".len();
    declared_length_mismatch[value_start] = if declared_length_mismatch[value_start] == b'9' {
        b'8'
    } else {
        declared_length_mismatch[value_start] + 1
    };
    reject(&declared_length_mismatch);

    let mut bad_payload_digest = artifact.clone();
    let payload_offset = bad_payload_digest
        .windows(2)
        .position(|window| window == b"\n\n")
        .expect("fixture has header boundary")
        + 2;
    bad_payload_digest[payload_offset] ^= 1;
    reject(&bad_payload_digest);
}
