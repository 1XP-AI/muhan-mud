//! Closed AliasTitleSnapshotManifestV1 metadata boundary.  It has no file,
//! outbox, intake, runtime, database, or network integration.

use crate::alias_title_snapshot_v1::{
    decode_alias_title_snapshot_v1, encode_alias_title_snapshot_v1,
};
use crate::{
    decode, encode, Error, Field, Kind, Record, DIGEST_LENGTH, TYPE_BYTES, TYPE_TEXT, TYPE_U16,
    TYPE_U64,
};

pub const SCHEMA: u16 = 1;
pub const WORLD_ID_MAX: usize = 64;
pub const LEGACY_NAME_KEY_MAX: usize = 28;
pub const UUID_LENGTH: usize = 36;
pub const SNAPSHOT_WIRE_MAX: usize = 64 * 1024 + 16 + DIGEST_LENGTH;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct AliasTitleSnapshotManifestV1 {
    pub world_id: String,
    pub canonical_legacy_name_key: String,
    pub character_id: String,
    pub writer_instance_id: String,
    pub writer_epoch: u64,
    pub writer_revision: u64,
    pub command_id: String,
    pub correlation_id: String,
    pub event_id: String,
    /// Independently supplied and cross-checked against `snapshot_wire`.
    pub snapshot_octets: u64,
    pub snapshot_digest: [u8; DIGEST_LENGTH],
    pub snapshot_wire: Vec<u8>,
}

fn invalid(field_id: u16) -> Error {
    Error::InvalidFieldLength { field_id }
}

fn lower_hex(value: &str) -> bool {
    value
        .bytes()
        .all(|byte| byte.is_ascii_digit() || (b'a'..=b'f').contains(&byte))
}

fn world_id(value: &str) -> bool {
    !value.is_empty()
        && value.len() <= WORLD_ID_MAX
        && value.as_bytes()[0].is_ascii_lowercase()
        && value.bytes().skip(1).all(|byte| {
            byte.is_ascii_lowercase() || byte.is_ascii_digit() || byte == b'_' || byte == b'-'
        })
}

fn legacy_name_key(value: &str) -> bool {
    (2..=LEGACY_NAME_KEY_MAX).contains(&value.len()) && value.len() % 2 == 0 && lower_hex(value)
}

fn uuid(value: &str) -> bool {
    value.len() == UUID_LENGTH
        && value.bytes().enumerate().all(|(index, byte)| {
            if matches!(index, 8 | 13 | 18 | 23) {
                byte == b'-'
            } else {
                byte.is_ascii_digit() || (b'a'..=b'f').contains(&byte)
            }
        })
}

fn snapshot_canonical(value: &AliasTitleSnapshotManifestV1) -> bool {
    value.snapshot_octets == value.snapshot_wire.len() as u64
        && !value.snapshot_wire.is_empty()
        && value.snapshot_wire.len() <= SNAPSHOT_WIRE_MAX
        && value.snapshot_wire.len() >= DIGEST_LENGTH
        && value.snapshot_wire[value.snapshot_wire.len() - DIGEST_LENGTH..] == value.snapshot_digest
        && decode_alias_title_snapshot_v1(&value.snapshot_wire)
            .and_then(|snapshot| encode_alias_title_snapshot_v1(&snapshot))
            .map_or(false, |canonical| canonical == value.snapshot_wire)
}

fn valid(value: &AliasTitleSnapshotManifestV1) -> bool {
    world_id(&value.world_id)
        && legacy_name_key(&value.canonical_legacy_name_key)
        && uuid(&value.character_id)
        && uuid(&value.writer_instance_id)
        && uuid(&value.command_id)
        && uuid(&value.correlation_id)
        && uuid(&value.event_id)
        && value.writer_epoch != 0
        && value.writer_revision != 0
        && value.writer_epoch <= i64::MAX as u64
        && value.writer_revision <= i64::MAX as u64
        && snapshot_canonical(value)
}

/// Encode only the closed, ordered thirteen-field schema. Every identity and
/// snapshot fact is a required caller-provided value; none is derived.
pub fn encode_alias_title_snapshot_manifest_v1(
    value: &AliasTitleSnapshotManifestV1,
) -> Result<Vec<u8>, Error> {
    if !valid(value) {
        return Err(invalid(0));
    }
    encode(&Record::new(
        Kind::AliasTitleSnapshotManifest,
        vec![
            Field::u16(1, SCHEMA),
            Field::text(2, value.world_id.clone()),
            Field::text(3, value.canonical_legacy_name_key.clone()),
            Field::text(4, value.character_id.clone()),
            Field::text(5, value.writer_instance_id.clone()),
            Field::u64(6, value.writer_epoch),
            Field::u64(7, value.writer_revision),
            Field::text(8, value.command_id.clone()),
            Field::text(9, value.correlation_id.clone()),
            Field::text(10, value.event_id.clone()),
            Field::bytes(11, value.snapshot_wire.clone()),
            Field::bytes(12, value.snapshot_digest.to_vec()),
            Field::u64(13, value.snapshot_octets),
        ],
    )?)
}

/// Decode a manifest only if its exact nested snapshot bytes re-encode
/// identically and the separately supplied digest and length match.
pub fn decode_alias_title_snapshot_manifest_v1(
    wire: &[u8],
) -> Result<AliasTitleSnapshotManifestV1, Error> {
    let record = decode(wire)?;
    if record.kind() != Kind::AliasTitleSnapshotManifest || record.fields().len() != 13 {
        return Err(invalid(0));
    }
    let expected = [
        (1, TYPE_U16, Some(2)),
        (2, TYPE_TEXT, None),
        (3, TYPE_TEXT, None),
        (4, TYPE_TEXT, Some(UUID_LENGTH)),
        (5, TYPE_TEXT, Some(UUID_LENGTH)),
        (6, TYPE_U64, Some(8)),
        (7, TYPE_U64, Some(8)),
        (8, TYPE_TEXT, Some(UUID_LENGTH)),
        (9, TYPE_TEXT, Some(UUID_LENGTH)),
        (10, TYPE_TEXT, Some(UUID_LENGTH)),
        (11, TYPE_BYTES, None),
        (12, TYPE_BYTES, Some(DIGEST_LENGTH)),
        (13, TYPE_U64, Some(8)),
    ];
    let fields = record.fields();
    for (field, (id, tag, length)) in fields.iter().zip(expected) {
        if field.id() != id
            || field.type_tag() != tag
            || length.is_some_and(|n| field.value().len() != n)
        {
            return Err(invalid(id));
        }
    }
    if u16::from_be_bytes([fields[0].value()[0], fields[0].value()[1]]) != SCHEMA {
        return Err(invalid(1));
    }
    let text = |index: usize| {
        String::from_utf8(fields[index].value().to_vec()).map_err(|_| invalid((index + 1) as u16))
    };
    let snapshot_digest: [u8; DIGEST_LENGTH] =
        fields[11].value().try_into().map_err(|_| invalid(12))?;
    let value = AliasTitleSnapshotManifestV1 {
        world_id: text(1)?,
        canonical_legacy_name_key: text(2)?,
        character_id: text(3)?,
        writer_instance_id: text(4)?,
        writer_epoch: u64::from_be_bytes(fields[5].value().try_into().map_err(|_| invalid(6))?),
        writer_revision: u64::from_be_bytes(fields[6].value().try_into().map_err(|_| invalid(7))?),
        command_id: text(7)?,
        correlation_id: text(8)?,
        event_id: text(9)?,
        snapshot_wire: fields[10].value().to_vec(),
        snapshot_digest,
        snapshot_octets: u64::from_be_bytes(
            fields[12].value().try_into().map_err(|_| invalid(13))?,
        ),
    };
    if valid(&value) {
        Ok(value)
    } else {
        Err(invalid(0))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn hex(input: &str) -> Vec<u8> {
        (0..input.trim().len())
            .step_by(2)
            .map(|at| u8::from_str_radix(&input.trim()[at..at + 2], 16).unwrap())
            .collect()
    }

    #[test]
    fn c_fixture_round_trips_exactly() {
        let wire = hex(include_str!(
            "../../../tests/fixtures/alias_title_snapshot_manifest_v1_canonical.hex"
        ));
        let value = decode_alias_title_snapshot_manifest_v1(&wire).unwrap();
        assert_eq!(
            encode_alias_title_snapshot_manifest_v1(&value).unwrap(),
            wire
        );
    }

    #[test]
    fn independently_supplied_snapshot_facts_must_match() {
        let wire = hex(include_str!(
            "../../../tests/fixtures/alias_title_snapshot_manifest_v1_canonical.hex"
        ));
        let mut value = decode_alias_title_snapshot_manifest_v1(&wire).unwrap();
        value.snapshot_octets -= 1;
        assert!(encode_alias_title_snapshot_manifest_v1(&value).is_err());
        value.snapshot_octets += 1;
        value.snapshot_digest[0] ^= 1;
        assert!(encode_alias_title_snapshot_manifest_v1(&value).is_err());
    }
}
