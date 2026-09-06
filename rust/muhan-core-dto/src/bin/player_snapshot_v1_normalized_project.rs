//! Version-pinned, Rust-only safe projection for one PlayerSnapshotV1 CDTO.
//!
//! The binary accepts one canonical artifact on stdin, binds it to an expected
//! artifact digest, and emits only the numeric `PlayerSnapshotNormalizedV1`
//! allowlist/topology plus fixed projection metadata. It never renders source
//! payload bytes, source text, keys, flags, credentials, paths, or sessions.

use muhan_core_dto::player_snapshot_normalized_v1::{
    project_player_snapshot_v1_artifact, PlayerSnapshotNormalizedV1,
    PLAYER_SNAPSHOT_NORMALIZED_V1_ALGORITHM, PLAYER_SNAPSHOT_NORMALIZED_V1_VERSION,
};
use muhan_core_dto::player_snapshot_v1::verify_player_snapshot_post_save_shadow_v1;
use muhan_core_dto::{DIGEST_LENGTH, MAX_ENVELOPE_SIZE};
use std::fmt::Write as _;
use std::io::{self, Read, Write};
use std::process::ExitCode;

const FORMAT: &str = "player-snapshot-v1-normalized-projection";
const REJECTION: &[u8] = b"rejected: invalid player snapshot CDTO\n";

fn reject(stderr: &mut impl Write) -> ExitCode {
    let _ = stderr.write_all(REJECTION);
    let _ = stderr.flush();
    ExitCode::from(1)
}

/// Accepts exactly one lowercase hexadecimal digest supplied by the immutable
/// artifact header. The digest is never included in successful output.
fn expected_snapshot_sha256(mut args: impl Iterator<Item = String>) -> Option<[u8; DIGEST_LENGTH]> {
    if args.next().as_deref() != Some("--snapshot-sha256") {
        return None;
    }
    let value = args.next()?;
    if args.next().is_some()
        || value.len() != DIGEST_LENGTH * 2
        || !value
            .bytes()
            .all(|byte| byte.is_ascii_digit() || (b'a'..=b'f').contains(&byte))
    {
        return None;
    }
    let mut digest = [0u8; DIGEST_LENGTH];
    for (index, byte) in digest.iter_mut().enumerate() {
        *byte = u8::from_str_radix(&value[index * 2..index * 2 + 2], 16).ok()?;
    }
    Some(digest)
}

fn hex(digest: &[u8]) -> String {
    const HEX: &[u8; 16] = b"0123456789abcdef";
    let mut output = String::with_capacity(digest.len() * 2);
    for &byte in digest {
        output.push(HEX[(byte >> 4) as usize] as char);
        output.push(HEX[(byte & 0x0f) as usize] as char);
    }
    output
}

/// Serializes the closed, safe projection as a one-line JSON document.
///
/// All strings are compile-time metadata or a hexadecimal digest. The player
/// body is exclusively the reviewed numeric allowlist and item topology; root
/// items use JSON `null` for their absent parent index.
fn format_projection(projection: &PlayerSnapshotNormalizedV1) -> String {
    let mut output = format!(
        "{{\"format\":\"{FORMAT}\",\"version\":{PLAYER_SNAPSHOT_NORMALIZED_V1_VERSION},\"algorithm\":\"{PLAYER_SNAPSHOT_NORMALIZED_V1_ALGORITHM}\",\"canonical_digest\":\"{}\",\"player\":{{\"level\":{},\"hp_max\":{},\"hp_current\":{},\"mp_max\":{},\"mp_current\":{},\"experience\":{},\"gold\":{},\"daily\":[",
        hex(&projection.canonical_digest()),
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
    output.push_str("]}}}\n");
    output
}

fn run_projection(
    input: &mut impl Read,
    stdout: &mut impl Write,
    stderr: &mut impl Write,
    expected_digest: &[u8; DIGEST_LENGTH],
) -> ExitCode {
    let mut wire = Vec::new();
    let max_input_octets =
        u64::try_from(MAX_ENVELOPE_SIZE).expect("CDTO envelope limit fits u64") + 1;
    if input.take(max_input_octets).read_to_end(&mut wire).is_err()
        || wire.len() > MAX_ENVELOPE_SIZE
    {
        return reject(stderr);
    }

    // Validate/canonicalize and bind the source artifact before projecting it.
    // Both operations complete before stdout is touched, so invalid or tampered
    // input cannot produce a partial projection.
    if verify_player_snapshot_post_save_shadow_v1(&wire, expected_digest).is_err() {
        return reject(stderr);
    }
    let projection = match project_player_snapshot_v1_artifact(&wire) {
        Ok(projection) => projection,
        Err(_) => return reject(stderr),
    };
    let rendered = format_projection(&projection);
    if stdout.write_all(rendered.as_bytes()).is_err() || stdout.flush().is_err() {
        return reject(stderr);
    }
    ExitCode::SUCCESS
}

fn main() -> ExitCode {
    let mut input = io::stdin().lock();
    let mut stdout = io::stdout().lock();
    let mut stderr = io::stderr().lock();
    let Some(expected_digest) = expected_snapshot_sha256(std::env::args().skip(1)) else {
        return reject(&mut stderr);
    };
    run_projection(&mut input, &mut stdout, &mut stderr, &expected_digest)
}

#[cfg(test)]
mod tests {
    use super::expected_snapshot_sha256;

    #[test]
    fn expected_digest_argument_is_closed_and_lowercase() {
        assert_eq!(
            expected_snapshot_sha256(["--snapshot-sha256".into(), "00".repeat(32)].into_iter()),
            Some([0; 32])
        );
        assert_eq!(
            expected_snapshot_sha256(["--snapshot-sha256".into(), "A0".repeat(32)].into_iter()),
            None
        );
        assert_eq!(
            expected_snapshot_sha256(["--snapshot-sha256".into(), "00".repeat(31)].into_iter()),
            None
        );
    }
}
