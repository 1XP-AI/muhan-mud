//! Read-only verifier for the C-owned `PlayerSnapshotV1` immutable artifact.
//!
//! This module deliberately has no artifact discovery, filesystem access,
//! FFI, or mutation capability.  Callers provide exactly one complete C
//! artifact and may use the result only for shadow observation; the legacy C
//! path remains authoritative unless a separate integration explicitly says
//! otherwise.

use super::player_snapshot_v1::{verify_player_snapshot_post_save_shadow_v1, ReplayVerificationV1};
use super::{sha256, DIGEST_LENGTH};

pub const PLAYER_SNAPSHOT_V1_ARTIFACT_FORMAT: &str = "player-snapshot-v1";
pub const PLAYER_SNAPSHOT_V1_ARTIFACT_HEADER_MAX: usize = 2048;
pub const PLAYER_SNAPSHOT_V1_ARTIFACT_MAX_SNAPSHOT_OCTETS: usize = 4 * 1024 * 1024 + 48;
pub const PLAYER_SNAPSHOT_V1_ARTIFACT_FILE_MAX: usize =
    PLAYER_SNAPSHOT_V1_ARTIFACT_HEADER_MAX + PLAYER_SNAPSHOT_V1_ARTIFACT_MAX_SNAPSHOT_OCTETS;
pub const PLAYER_SNAPSHOT_V1_ARTIFACT_SHADOW_VERIFICATION_FORMAT: &str =
    "player-snapshot-v1-artifact-shadow-verification";
pub const PLAYER_SNAPSHOT_V1_ARTIFACT_SHADOW_VERIFICATION_VERSION: u16 = 1;
pub const PLAYER_SNAPSHOT_V1_ARTIFACT_SHADOW_VERIFICATION_ALGORITHM: &str = "sha-256";

const HEADER_NAMES: [&str; 15] = [
    "version",
    "world_id",
    "character_id",
    "command_id",
    "canonical_name_hex",
    "request_sha256",
    "source_post_sha256",
    "writer_instance_id",
    "writer_epoch",
    "writer_revision",
    "storage_format",
    "snapshot_format",
    "source_octets",
    "snapshot_sha256",
    "snapshot_octets",
];

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PlayerSnapshotV1ArtifactShadowVerification {
    /// Immutable C artifact-header facts, copied only after the complete
    /// header and payload have validated.  This remains read-only metadata;
    /// no identity is used to select, write, or project an artifact.
    pub world_id: String,
    pub character_id: String,
    pub command_id: String,
    pub canonical_name_hex: String,
    pub request_sha256: String,
    pub source_post_sha256: String,
    pub writer_instance_id: String,
    pub writer_epoch: u64,
    pub writer_revision: u64,
    pub storage_format: u64,
    pub snapshot_format: String,
    pub source_octets: u64,
    pub snapshot_sha256: [u8; DIGEST_LENGTH],
    pub snapshot_octets: usize,
    pub replay: ReplayVerificationV1,
}

/// Deliberately uninformative to callers handling untrusted artifact bytes.
/// The CLI maps every failure to one fixed rejection result.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct InvalidPlayerSnapshotV1Artifact;

type Result<T> = std::result::Result<T, InvalidPlayerSnapshotV1Artifact>;

struct Header<'a> {
    world_id: &'a str,
    character_id: &'a str,
    command_id: &'a str,
    canonical_name_hex: &'a str,
    request_sha256: &'a str,
    source_post_sha256: &'a str,
    writer_instance_id: &'a str,
    writer_epoch: u64,
    writer_revision: u64,
    storage_format: u64,
    snapshot_format: &'a str,
    source_octets: u64,
    snapshot_sha256: [u8; DIGEST_LENGTH],
    snapshot_sha256_text: &'a str,
    snapshot_octets: usize,
}

fn lower_hex(value: &str) -> bool {
    value
        .bytes()
        .all(|byte| byte.is_ascii_digit() || matches!(byte, b'a'..=b'f'))
}

fn uuid(value: &str) -> bool {
    value.len() == 36
        && value.bytes().enumerate().all(|(index, byte)| {
            if matches!(index, 8 | 13 | 18 | 23) {
                byte == b'-'
            } else {
                byte.is_ascii_digit() || matches!(byte, b'a'..=b'f')
            }
        })
}

fn world(value: &str) -> bool {
    let mut bytes = value.bytes();
    matches!(bytes.next(), Some(b'a'..=b'z'))
        && value.len() <= 64
        && bytes.all(|byte| {
            byte.is_ascii_lowercase() || byte.is_ascii_digit() || matches!(byte, b'_' | b'-')
        })
}

fn canonical_name_hex(value: &str) -> bool {
    (2..=28).contains(&value.len()) && value.len() % 2 == 0 && lower_hex(value)
}

fn decimal(value: &str) -> Result<u64> {
    if value.is_empty() || value.len() > 19 || value.starts_with('0') {
        return Err(InvalidPlayerSnapshotV1Artifact);
    }
    let mut parsed = 0u64;
    for byte in value.bytes() {
        if !byte.is_ascii_digit() {
            return Err(InvalidPlayerSnapshotV1Artifact);
        }
        parsed = parsed
            .checked_mul(10)
            .and_then(|number| number.checked_add(u64::from(byte - b'0')))
            .filter(|number| *number <= i64::MAX as u64)
            .ok_or(InvalidPlayerSnapshotV1Artifact)?;
    }
    if parsed == 0 {
        return Err(InvalidPlayerSnapshotV1Artifact);
    }
    Ok(parsed)
}

fn digest(value: &str) -> Result<[u8; DIGEST_LENGTH]> {
    if value.len() != DIGEST_LENGTH * 2 || !lower_hex(value) {
        return Err(InvalidPlayerSnapshotV1Artifact);
    }
    let mut output = [0u8; DIGEST_LENGTH];
    for (index, byte) in output.iter_mut().enumerate() {
        *byte = u8::from_str_radix(&value[index * 2..index * 2 + 2], 16)
            .map_err(|_| InvalidPlayerSnapshotV1Artifact)?;
    }
    Ok(output)
}

fn digest_hex(value: &[u8]) -> String {
    const HEX: &[u8; 16] = b"0123456789abcdef";
    let mut output = String::with_capacity(value.len() * 2);
    for byte in value {
        output.push(HEX[(byte >> 4) as usize] as char);
        output.push(HEX[(byte & 15) as usize] as char);
    }
    output
}

fn canonical_header(header: &Header<'_>) -> String {
    format!(
        "version=1\nworld_id={}\ncharacter_id={}\ncommand_id={}\ncanonical_name_hex={}\nrequest_sha256={}\nsource_post_sha256={}\nwriter_instance_id={}\nwriter_epoch={}\nwriter_revision={}\nstorage_format={}\nsnapshot_format={}\nsource_octets={}\nsnapshot_sha256={}\nsnapshot_octets={}\n\n",
        header.world_id,
        header.character_id,
        header.command_id,
        header.canonical_name_hex,
        header.request_sha256,
        header.source_post_sha256,
        header.writer_instance_id,
        header.writer_epoch,
        header.writer_revision,
        header.storage_format,
        header.snapshot_format,
        header.source_octets,
        header.snapshot_sha256_text,
        header.snapshot_octets,
    )
}

fn parse_header(artifact: &[u8]) -> Result<(Header<'_>, usize)> {
    if artifact.len() < 3 || artifact.len() > PLAYER_SNAPSHOT_V1_ARTIFACT_FILE_MAX {
        return Err(InvalidPlayerSnapshotV1Artifact);
    }
    let boundary = artifact
        .windows(2)
        .position(|window| window == b"\n\n")
        .ok_or(InvalidPlayerSnapshotV1Artifact)?;
    let header_length = boundary + 2;
    if header_length > PLAYER_SNAPSHOT_V1_ARTIFACT_HEADER_MAX
        || artifact[..header_length].contains(&0)
    {
        return Err(InvalidPlayerSnapshotV1Artifact);
    }
    let mut cursor = std::str::from_utf8(&artifact[..boundary + 1])?;
    let mut values = [""; HEADER_NAMES.len()];
    for (index, name) in HEADER_NAMES.iter().enumerate() {
        let prefix = format!("{name}=");
        let rest = cursor
            .strip_prefix(&prefix)
            .ok_or(InvalidPlayerSnapshotV1Artifact)?;
        let newline = rest.find('\n').ok_or(InvalidPlayerSnapshotV1Artifact)?;
        values[index] = &rest[..newline];
        cursor = &rest[newline + 1..];
    }
    if !cursor.is_empty() || values[0] != "1" {
        return Err(InvalidPlayerSnapshotV1Artifact);
    }
    let header = Header {
        world_id: values[1],
        character_id: values[2],
        command_id: values[3],
        canonical_name_hex: values[4],
        request_sha256: values[5],
        source_post_sha256: values[6],
        writer_instance_id: values[7],
        writer_epoch: decimal(values[8])?,
        writer_revision: decimal(values[9])?,
        storage_format: decimal(values[10])?,
        snapshot_format: values[11],
        source_octets: decimal(values[12])?,
        snapshot_sha256: digest(values[13])?,
        snapshot_sha256_text: values[13],
        snapshot_octets: usize::try_from(decimal(values[14])?)
            .map_err(|_| InvalidPlayerSnapshotV1Artifact)?,
    };
    if !world(header.world_id)
        || !uuid(header.character_id)
        || !uuid(header.command_id)
        || !canonical_name_hex(header.canonical_name_hex)
        || digest(header.request_sha256).is_err()
        || digest(header.source_post_sha256).is_err()
        || !uuid(header.writer_instance_id)
        || header.storage_format > i16::MAX as u64
        || header.snapshot_format != PLAYER_SNAPSHOT_V1_ARTIFACT_FORMAT
        || header.snapshot_octets > PLAYER_SNAPSHOT_V1_ARTIFACT_MAX_SNAPSHOT_OCTETS
        || canonical_header(&header).as_bytes() != &artifact[..header_length]
    {
        return Err(InvalidPlayerSnapshotV1Artifact);
    }
    Ok((header, header_length))
}

/// Verifies exactly one complete C-generated immutable artifact.
///
/// The parser mirrors C's strict ordered header and canonical rendering rules,
/// then delegates PlayerSnapshotV1 decoding and canonical re-encoding to the
/// existing DTO semantics.  It performs no filesystem or state operation.
pub fn verify_player_snapshot_v1_artifact_shadow(
    artifact: &[u8],
) -> Result<PlayerSnapshotV1ArtifactShadowVerification> {
    let (header, header_length) = parse_header(artifact)?;
    let snapshot = &artifact[header_length..];
    if snapshot.is_empty()
        || snapshot.len() != header.snapshot_octets
        || sha256(snapshot) != header.snapshot_sha256
    {
        return Err(InvalidPlayerSnapshotV1Artifact);
    }
    let replay = verify_player_snapshot_post_save_shadow_v1(snapshot, &header.snapshot_sha256)
        .map_err(|_| InvalidPlayerSnapshotV1Artifact)?;
    Ok(PlayerSnapshotV1ArtifactShadowVerification {
        world_id: header.world_id.to_owned(),
        character_id: header.character_id.to_owned(),
        command_id: header.command_id.to_owned(),
        canonical_name_hex: header.canonical_name_hex.to_owned(),
        request_sha256: header.request_sha256.to_owned(),
        source_post_sha256: header.source_post_sha256.to_owned(),
        writer_instance_id: header.writer_instance_id.to_owned(),
        writer_epoch: header.writer_epoch,
        writer_revision: header.writer_revision,
        storage_format: header.storage_format,
        snapshot_format: header.snapshot_format.to_owned(),
        source_octets: header.source_octets,
        snapshot_sha256: header.snapshot_sha256,
        snapshot_octets: snapshot.len(),
        replay,
    })
}

/// Stable, metadata-only result for a shadow observer.  It includes the
/// validated immutable header facts and excludes the snapshot payload.
pub fn format_player_snapshot_v1_artifact_shadow_verification(
    value: &PlayerSnapshotV1ArtifactShadowVerification,
) -> String {
    format!(
        "format={PLAYER_SNAPSHOT_V1_ARTIFACT_SHADOW_VERIFICATION_FORMAT}\nversion={PLAYER_SNAPSHOT_V1_ARTIFACT_SHADOW_VERIFICATION_VERSION}\nalgorithm={PLAYER_SNAPSHOT_V1_ARTIFACT_SHADOW_VERIFICATION_ALGORITHM}\nworld_id={}\ncharacter_id={}\ncommand_id={}\ncanonical_name_hex={}\nrequest_sha256={}\nsource_post_sha256={}\nwriter_instance_id={}\nwriter_epoch={}\nwriter_revision={}\nstorage_format={}\nsnapshot_format={}\nsource_octets={}\nsnapshot_sha256={}\nsnapshot_octets={}\ncanonical_octets={}\ninventory_node_count={}\n",
        value.world_id,
        value.character_id,
        value.command_id,
        value.canonical_name_hex,
        value.request_sha256,
        value.source_post_sha256,
        value.writer_instance_id,
        value.writer_epoch,
        value.writer_revision,
        value.storage_format,
        value.snapshot_format,
        value.source_octets,
        digest_hex(&value.snapshot_sha256),
        value.snapshot_octets,
        value.replay.canonical_octets,
        value.replay.inventory_node_count,
    )
}

impl From<std::str::Utf8Error> for InvalidPlayerSnapshotV1Artifact {
    fn from(_: std::str::Utf8Error) -> Self {
        Self
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::{encode, Kind, Record};

    #[test]
    fn decimal_matches_the_c_header_number_grammar() {
        assert_eq!(decimal("1"), Ok(1));
        assert_eq!(decimal("9223372036854775807"), Ok(i64::MAX as u64));
        for malformed in ["", "0", "01", "-1", "1x", "9223372036854775808"] {
            assert!(decimal(malformed).is_err(), "{malformed}");
        }
    }

    #[test]
    fn header_identifiers_match_the_c_lowercase_contract() {
        assert!(uuid("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"));
        assert!(!uuid("AAAAAAAA-aaaa-4aaa-8aaa-aaaaaaaaaaaa"));
        assert!(world("muhan-01"));
        assert!(!world("Muhan-01"));
        assert!(canonical_name_hex("4d336865726f"));
        assert!(!canonical_name_hex("4D"));
    }

    #[test]
    fn canonical_header_and_digest_do_not_admit_a_noncanonical_snapshot_schema() {
        let snapshot = encode(&Record::new(Kind::PlayerSnapshot, vec![]).unwrap()).unwrap();
        let snapshot_sha256 = sha256(&snapshot);
        let snapshot_sha256_text = digest_hex(&snapshot_sha256);
        let header = Header {
            world_id: "muhan-01",
            character_id: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
            command_id: "22222222-2222-4222-8222-222222222222",
            canonical_name_hex: "4d336865726f",
            request_sha256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
            source_post_sha256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
            writer_instance_id: "11111111-1111-4111-8111-111111111111",
            writer_epoch: 7,
            writer_revision: 2,
            storage_format: 1,
            snapshot_format: PLAYER_SNAPSHOT_V1_ARTIFACT_FORMAT,
            source_octets: 9,
            snapshot_sha256,
            snapshot_sha256_text: &snapshot_sha256_text,
            snapshot_octets: snapshot.len(),
        };
        let mut artifact = canonical_header(&header).into_bytes();
        artifact.extend_from_slice(&snapshot);
        assert!(verify_player_snapshot_v1_artifact_shadow(&artifact).is_err());
    }
}
