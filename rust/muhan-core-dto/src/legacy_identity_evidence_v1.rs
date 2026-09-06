//! Closed, metadata-only LegacyIdentityEvidenceV1 canonical wire codec.
//!
//! This is an owned DTO boundary, not a Rust ABI for the C struct. It never
//! accepts player bytes, credentials, descriptors, pointers, tokens, or extensions.

use std::fmt;

pub const MAGIC: [u8; 8] = *b"MUDLIE\0\0";
pub const SCHEMA: u16 = 1;
pub const VERSION: u16 = 1;
pub const STORAGE_FORMAT: &str = "player-v1";
pub const SHA256_HEX_LEN: usize = 64;
pub const MAX_NAME_BYTES: usize = 14;
pub const MAX_PAYLOAD: usize = 1 + 1 + 1 + MAX_NAME_BYTES + 1 + 2 + 1 + SHA256_HEX_LEN + 1 + 9;
pub const HEADER_LEN: usize = 16;
pub const MAX_WIRE_LEN: usize = HEADER_LEN + MAX_PAYLOAD;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum Outcome {
    Ok,
    NotFound,
    Corrupt,
    IoError,
    InvalidInput,
}
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum Canonicalization {
    Canonical,
    Normalized,
    Invalid,
}
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct LegacyIdentityEvidenceV1 {
    pub outcome: Outcome,
    pub canonicalization: Canonicalization,
    pub canonical_name: String,
    pub legacy_shard: String,
    pub player_file_sha256: String,
    pub storage_format: String,
}
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct WireError;
impl fmt::Display for WireError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("invalid legacy identity evidence wire")
    }
}
impl std::error::Error for WireError {}

fn outcome_byte(value: Outcome) -> u8 {
    match value {
        Outcome::Ok => 0,
        Outcome::NotFound => 1,
        Outcome::Corrupt => 2,
        Outcome::IoError => 3,
        Outcome::InvalidInput => 4,
    }
}
fn parse_outcome(value: u8) -> Result<Outcome, WireError> {
    match value {
        0 => Ok(Outcome::Ok),
        1 => Ok(Outcome::NotFound),
        2 => Ok(Outcome::Corrupt),
        3 => Ok(Outcome::IoError),
        4 => Ok(Outcome::InvalidInput),
        _ => Err(WireError),
    }
}
fn canonicalization_byte(value: Canonicalization) -> u8 {
    match value {
        Canonicalization::Canonical => 0,
        Canonicalization::Normalized => 1,
        Canonicalization::Invalid => 2,
    }
}
fn parse_canonicalization(value: u8) -> Result<Canonicalization, WireError> {
    match value {
        0 => Ok(Canonicalization::Canonical),
        1 => Ok(Canonicalization::Normalized),
        2 => Ok(Canonicalization::Invalid),
        _ => Err(WireError),
    }
}
fn lower_hex(value: &[u8]) -> bool {
    value
        .iter()
        .all(|byte| byte.is_ascii_digit() || matches!(byte, b'a'..=b'f'))
}
fn nul_free(value: &[u8]) -> bool {
    !value.contains(&0)
}

fn valid(value: &LegacyIdentityEvidenceV1) -> bool {
    let name = value.canonical_name.as_bytes();
    let shard = value.legacy_shard.as_bytes();
    let digest = value.player_file_sha256.as_bytes();
    if !nul_free(name)
        || !nul_free(shard)
        || !nul_free(digest)
        || !nul_free(value.storage_format.as_bytes())
        || value.storage_format != STORAGE_FORMAT
    {
        return false;
    }
    match value.outcome {
        Outcome::InvalidInput => {
            value.canonicalization == Canonicalization::Invalid
                && name.is_empty()
                && shard.is_empty()
                && digest.is_empty()
        }
        Outcome::Ok | Outcome::NotFound | Outcome::Corrupt | Outcome::IoError => {
            value.canonicalization != Canonicalization::Invalid
                && !name.is_empty()
                && name.len() <= MAX_NAME_BYTES
                && shard.len() == 2
                && lower_hex(shard)
                && match value.outcome {
                    Outcome::Ok => digest.len() == SHA256_HEX_LEN && lower_hex(digest),
                    _ => digest.is_empty(),
                }
        }
    }
}

/// Fixed field order: outcome, canonicalization, canonical name, shard,
/// SHA-256, storage format. Every field length is explicit and bounded.
pub fn encode(value: &LegacyIdentityEvidenceV1) -> Result<Vec<u8>, WireError> {
    if !valid(value) {
        return Err(WireError);
    }
    let name = value.canonical_name.as_bytes();
    let shard = value.legacy_shard.as_bytes();
    let digest = value.player_file_sha256.as_bytes();
    let storage = value.storage_format.as_bytes();
    let payload = 1 + 1 + 1 + name.len() + 1 + shard.len() + 1 + digest.len() + 1 + storage.len();
    if payload > MAX_PAYLOAD {
        return Err(WireError);
    }
    let mut wire = Vec::with_capacity(HEADER_LEN + payload);
    wire.extend_from_slice(&MAGIC);
    wire.extend_from_slice(&SCHEMA.to_be_bytes());
    wire.extend_from_slice(&VERSION.to_be_bytes());
    wire.extend_from_slice(&(payload as u32).to_be_bytes());
    wire.push(outcome_byte(value.outcome));
    wire.push(canonicalization_byte(value.canonicalization));
    for field in [name, shard, digest, storage] {
        wire.push(u8::try_from(field.len()).map_err(|_| WireError)?);
        wire.extend_from_slice(field);
    }
    Ok(wire)
}

fn take<'a>(wire: &'a [u8], at: &mut usize, len: usize) -> Result<&'a [u8], WireError> {
    let end = at.checked_add(len).ok_or(WireError)?;
    let output = wire.get(*at..end).ok_or(WireError)?;
    *at = end;
    Ok(output)
}
fn text(wire: &[u8]) -> Result<String, WireError> {
    if !nul_free(wire) {
        return Err(WireError);
    }
    String::from_utf8(wire.to_vec()).map_err(|_| WireError)
}

/// Decodes only complete canonical bytes. It does not canonicalize or shard.
pub fn decode(wire: &[u8]) -> Result<LegacyIdentityEvidenceV1, WireError> {
    if wire.len() < HEADER_LEN || wire[..8] != MAGIC {
        return Err(WireError);
    }
    if u16::from_be_bytes([wire[8], wire[9]]) != SCHEMA
        || u16::from_be_bytes([wire[10], wire[11]]) != VERSION
    {
        return Err(WireError);
    }
    let payload = u32::from_be_bytes([wire[12], wire[13], wire[14], wire[15]]) as usize;
    if payload > MAX_PAYLOAD || wire.len() != HEADER_LEN.checked_add(payload).ok_or(WireError)? {
        return Err(WireError);
    }
    let mut at = HEADER_LEN;
    let outcome = parse_outcome(*wire.get(at).ok_or(WireError)?)?;
    at += 1;
    let canonicalization = parse_canonicalization(*wire.get(at).ok_or(WireError)?)?;
    at += 1;
    let mut next = || -> Result<&[u8], WireError> {
        let len = *wire.get(at).ok_or(WireError)? as usize;
        at += 1;
        take(wire, &mut at, len)
    };
    let canonical_name = text(next()?)?;
    let legacy_shard = text(next()?)?;
    let player_file_sha256 = text(next()?)?;
    let storage_format = text(next()?)?;
    if at != wire.len() {
        return Err(WireError);
    }
    let value = LegacyIdentityEvidenceV1 {
        outcome,
        canonicalization,
        canonical_name,
        legacy_shard,
        player_file_sha256,
        storage_format,
    };
    if !valid(&value) {
        return Err(WireError);
    }
    Ok(value)
}

#[cfg(test)]
mod tests {
    use super::*;

    const IMPORTER_BINDING_FIXTURE: &str = include_str!(
        "../../../tests/fixtures/legacy_identity_evidence_importer_binding_v1.fixture"
    );

    fn hex(source: &str) -> Vec<u8> {
        let source = source.trim();
        (0..source.len())
            .step_by(2)
            .map(|i| u8::from_str_radix(&source[i..i + 2], 16).unwrap())
            .collect()
    }

    fn fixture_field<'a>(source: &'a str, key: &str) -> &'a str {
        let prefix = format!("{key}=");
        let mut matches = source.lines().filter_map(|line| line.strip_prefix(&prefix));
        let value = matches.next().expect("fixture field must be present");
        assert!(matches.next().is_none(), "fixture field must be unique");
        value
    }

    fn assert_closed_importer_binding_fixture(source: &str) {
        let mut keys = Vec::new();
        for line in source.lines() {
            let (key, value) = line.split_once('=').expect("fixture lines are key=value");
            assert!(
                !key.is_empty() && !value.is_empty(),
                "fixture fields are nonempty"
            );
            keys.push(key);
        }
        keys.sort_unstable();
        assert_eq!(
            keys,
            [
                "canonical_name",
                "canonicalization",
                "contract_version",
                "inventory_byte_size",
                "inventory_canonical_name_key",
                "inventory_expected_shard",
                "inventory_name",
                "inventory_observed_shard",
                "inventory_relative_path",
                "inventory_sha256",
                "legacy_shard",
                "outcome",
                "player_file_sha256",
                "storage_format",
                "wire_hex",
            ]
        );
    }
    fn valid() -> LegacyIdentityEvidenceV1 {
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
    #[test]
    fn shared_c_golden_is_exact_and_round_trips() {
        let golden = hex(include_str!(
            "../../../tests/fixtures/legacy_identity_evidence_wire_v1_ok.hex"
        ));
        assert_eq!(encode(&valid()).unwrap(), golden);
        assert_eq!(decode(&golden).unwrap(), valid());
    }
    #[test]
    fn strict_wire_decode_matches_the_versioned_importer_binding_contract() {
        assert_closed_importer_binding_fixture(IMPORTER_BINDING_FIXTURE);
        assert_eq!(
            fixture_field(IMPORTER_BINDING_FIXTURE, "contract_version"),
            "1"
        );

        let wire = hex(fixture_field(IMPORTER_BINDING_FIXTURE, "wire_hex"));
        assert_eq!(
            wire,
            hex(include_str!(
                "../../../tests/fixtures/legacy_identity_evidence_wire_v1_ok.hex"
            ))
        );
        let decoded = decode(&wire).expect("contract wire must strict-decode");
        assert_eq!(encode(&decoded).unwrap(), wire);

        assert_eq!(decoded.outcome, Outcome::Ok);
        assert_eq!(fixture_field(IMPORTER_BINDING_FIXTURE, "outcome"), "ok");
        assert_eq!(decoded.canonicalization, Canonicalization::Normalized);
        assert_eq!(
            fixture_field(IMPORTER_BINDING_FIXTURE, "canonicalization"),
            "normalized"
        );
        assert_eq!(
            decoded.canonical_name,
            fixture_field(IMPORTER_BINDING_FIXTURE, "canonical_name")
        );
        assert_eq!(
            decoded.legacy_shard,
            fixture_field(IMPORTER_BINDING_FIXTURE, "legacy_shard")
        );
        assert_eq!(
            decoded.player_file_sha256,
            fixture_field(IMPORTER_BINDING_FIXTURE, "player_file_sha256")
        );
        assert_eq!(
            decoded.storage_format,
            fixture_field(IMPORTER_BINDING_FIXTURE, "storage_format")
        );

        assert_eq!(
            fixture_field(IMPORTER_BINDING_FIXTURE, "inventory_name"),
            decoded.canonical_name
        );
        assert_eq!(
            fixture_field(IMPORTER_BINDING_FIXTURE, "inventory_canonical_name_key"),
            decoded.canonical_name
        );
        assert_eq!(
            fixture_field(IMPORTER_BINDING_FIXTURE, "inventory_observed_shard"),
            decoded.legacy_shard
        );
        assert_eq!(
            fixture_field(IMPORTER_BINDING_FIXTURE, "inventory_expected_shard"),
            decoded.legacy_shard
        );
        assert_eq!(
            fixture_field(IMPORTER_BINDING_FIXTURE, "inventory_relative_path"),
            format!("player/{}/{}", decoded.legacy_shard, decoded.canonical_name)
        );
        assert_eq!(
            fixture_field(IMPORTER_BINDING_FIXTURE, "inventory_sha256"),
            decoded.player_file_sha256
        );
        assert_eq!(
            fixture_field(IMPORTER_BINDING_FIXTURE, "inventory_byte_size")
                .parse::<u64>()
                .expect("inventory byte size is numeric"),
            1
        );
    }
    #[test]
    fn shared_invalid_input_golden_is_accepted_without_identity_or_digest() {
        let golden = hex(include_str!(
            "../../../tests/fixtures/legacy_identity_evidence_wire_v1_invalid_input.hex"
        ));
        let decoded = decode(&golden).unwrap();
        assert_eq!(decoded.outcome, Outcome::InvalidInput);
        assert!(
            decoded.canonical_name.is_empty()
                && decoded.legacy_shard.is_empty()
                && decoded.player_file_sha256.is_empty()
        );
        assert_eq!(encode(&decoded).unwrap(), golden);
    }
    #[test]
    fn malformed_unknown_and_noncanonical_inputs_fail_closed() {
        let mut wire = encode(&valid()).unwrap();
        wire[11] = 2;
        assert!(decode(&wire).is_err());
        let mut wire = encode(&valid()).unwrap();
        wire[16] = 99;
        assert!(decode(&wire).is_err());
        let mut invalid = valid();
        invalid.player_file_sha256.clear();
        assert!(encode(&invalid).is_err());
        let mut invalid = valid();
        invalid.legacy_shard = "GG".into();
        assert!(encode(&invalid).is_err());
    }
    #[test]
    fn canonical_text_fields_reject_embedded_nuls_on_encode_and_decode() {
        for fixture in [
            include_str!("../../../tests/fixtures/legacy_identity_evidence_wire_v1_nul_name.hex"),
            include_str!("../../../tests/fixtures/legacy_identity_evidence_wire_v1_nul_shard.hex"),
            include_str!("../../../tests/fixtures/legacy_identity_evidence_wire_v1_nul_digest.hex"),
            include_str!(
                "../../../tests/fixtures/legacy_identity_evidence_wire_v1_nul_storage.hex"
            ),
        ] {
            assert!(decode(&hex(fixture)).is_err());
        }
        let mut invalid = valid();
        invalid.canonical_name = "Al\0ice".into();
        assert!(encode(&invalid).is_err());
        let mut invalid = valid();
        invalid.legacy_shard = "3\0".into();
        assert!(encode(&invalid).is_err());
        let mut invalid = valid();
        invalid.player_file_sha256.replace_range(1..1, "\0");
        assert!(encode(&invalid).is_err());
        let mut invalid = valid();
        invalid.storage_format = "player\0-v1".into();
        assert!(encode(&invalid).is_err());
    }
}
