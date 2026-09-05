//! Explicit v2 verifier for one PlayerSnapshotV1 CDTO envelope.
//!
//! This binary is intentionally separate from the v1 runner. Production v1
//! relay wiring continues to invoke `player_snapshot_v1_replay_verify`.

use muhan_core_dto::player_snapshot_v1::{
    format_replay_verification_v2_report, verify_player_snapshot_replay_v2,
};
use std::io::{self, Read, Write};
use std::process::ExitCode;

const REJECTION: &[u8] = b"rejected: invalid player snapshot CDTO\n";

fn reject(stderr: &mut impl Write) -> ExitCode {
    let _ = stderr.write_all(REJECTION);
    let _ = stderr.flush();
    ExitCode::from(1)
}

fn main() -> ExitCode {
    let mut wire = Vec::new();
    let mut input = io::stdin().lock();
    let mut stdout = io::stdout().lock();
    let mut stderr = io::stderr().lock();
    if input.read_to_end(&mut wire).is_err() {
        return reject(&mut stderr);
    }
    let report = match verify_player_snapshot_replay_v2(&wire) {
        Ok(report) => report,
        Err(_) => return reject(&mut stderr),
    };
    let rendered = format_replay_verification_v2_report(&report);
    if stdout.write_all(rendered.as_bytes()).is_err() || stdout.flush().is_err() {
        return reject(&mut stderr);
    }
    ExitCode::SUCCESS
}
