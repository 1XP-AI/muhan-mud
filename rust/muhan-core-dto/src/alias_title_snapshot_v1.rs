//! AliasTitleSnapshotV1 is an offline, owned CDTO boundary for the legacy
//! alias/title sidecar. It neither parses files nor calls game runtime code;
//! the paired C characterization oracle is authoritative for legacy bytes.

use crate::{decode, encode, Error, Field, Kind, Record, TYPE_BOOL, TYPE_BYTES, TYPE_U16};

pub const SCHEMA: u16 = 1;
pub const MAX_ALIASES: usize = 50;
pub const ALIAS_MAX_BYTES: usize = 13;
pub const PROCESS_MAX_BYTES: usize = 253;
pub const TITLE_MAX_BYTES: usize = 78;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct AliasTitleEntryV1 {
    pub alias: Vec<u8>,
    pub process: Vec<u8>,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct AliasTitleSnapshotV1 {
    pub aliases: Vec<AliasTitleEntryV1>,
    /// `None` preserves the legacy distinction between no title and an empty
    /// title line, without treating either as text.
    pub title: Option<Vec<u8>>,
}

fn invalid(field_id: u16) -> Error {
    Error::InvalidFieldLength { field_id }
}

fn valid(value: &AliasTitleSnapshotV1) -> bool {
    value.aliases.len() <= MAX_ALIASES
        && value.aliases.iter().all(|entry| {
            !entry.alias.is_empty()
                && entry.alias.len() <= ALIAS_MAX_BYTES
                && entry.process.len() <= PROCESS_MAX_BYTES
        })
        && value
            .title
            .as_ref()
            .map_or(true, |title| title.len() <= TITLE_MAX_BYTES)
        && value.aliases.iter().enumerate().all(|(index, entry)| {
            value.aliases[..index]
                .iter()
                .all(|prior| prior.alias != entry.alias)
        })
}

fn list_bytes(entries: &[AliasTitleEntryV1]) -> Vec<u8> {
    let mut output = Vec::new();
    for entry in entries {
        output.push(entry.alias.len() as u8);
        output.extend_from_slice(&entry.alias);
        output.extend_from_slice(&(entry.process.len() as u16).to_be_bytes());
        output.extend_from_slice(&entry.process);
    }
    output
}

/// Encode exactly four ordered fields: schema, ordered aliases, title presence,
/// and title bytes. The enclosing CDTO digest is the canonical digest.
pub fn encode_alias_title_snapshot_v1(value: &AliasTitleSnapshotV1) -> Result<Vec<u8>, Error> {
    if !valid(value) {
        return Err(invalid(0));
    }
    let title_present = value.title.is_some();
    let title = value.title.clone().unwrap_or_default();
    encode(&Record::new(
        Kind::AliasTitleSnapshot,
        vec![
            Field::u16(1, SCHEMA),
            Field::bytes(2, list_bytes(&value.aliases)),
            Field::bool(3, title_present),
            Field::bytes(4, title),
        ],
    )?)
}

/// Decode only the closed canonical schema; no file syntax is accepted here.
pub fn decode_alias_title_snapshot_v1(wire: &[u8]) -> Result<AliasTitleSnapshotV1, Error> {
    let record = decode(wire)?;
    if record.kind != Kind::AliasTitleSnapshot || record.fields.len() != 4 {
        return Err(invalid(0));
    }
    let fields = record.fields();
    let expected = [
        (TYPE_U16, 2usize),
        (TYPE_BYTES, usize::MAX),
        (TYPE_BOOL, 1),
        (TYPE_BYTES, usize::MAX),
    ];
    for (index, (tag, length)) in expected.into_iter().enumerate() {
        let field = &fields[index];
        if field.id() != (index + 1) as u16
            || field.type_tag() != tag
            || (length != usize::MAX && field.value().len() != length)
        {
            return Err(invalid((index + 1) as u16));
        }
    }
    if u16::from_be_bytes([fields[0].value()[0], fields[0].value()[1]]) != SCHEMA {
        return Err(invalid(1));
    }
    let title_present = match fields[2].value()[0] {
        0 => false,
        1 => true,
        _ => return Err(invalid(3)),
    };
    if fields[3].value().len() > TITLE_MAX_BYTES
        || (!title_present && !fields[3].value().is_empty())
    {
        return Err(invalid(4));
    }
    let list = fields[1].value();
    let mut at = 0usize;
    let mut aliases = Vec::new();
    while at < list.len() {
        if aliases.len() == MAX_ALIASES || list.len() - at < 3 {
            return Err(invalid(2));
        }
        let alias_length = list[at] as usize;
        at += 1;
        if alias_length == 0 || alias_length > ALIAS_MAX_BYTES || list.len() - at < alias_length + 2
        {
            return Err(invalid(2));
        }
        let alias = list[at..at + alias_length].to_vec();
        at += alias_length;
        let process_length = u16::from_be_bytes([list[at], list[at + 1]]) as usize;
        at += 2;
        if process_length > PROCESS_MAX_BYTES || list.len() - at < process_length {
            return Err(invalid(2));
        }
        let process = list[at..at + process_length].to_vec();
        at += process_length;
        aliases.push(AliasTitleEntryV1 { alias, process });
    }
    let output = AliasTitleSnapshotV1 {
        aliases,
        title: title_present.then(|| fields[3].value().to_vec()),
    };
    if !valid(&output) {
        return Err(invalid(0));
    }
    Ok(output)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn preserves_order_and_empty_title_distinction() {
        let value = AliasTitleSnapshotV1 {
            aliases: vec![
                AliasTitleEntryV1 {
                    alias: b"n".to_vec(),
                    process: b"north".to_vec(),
                },
                AliasTitleEntryV1 {
                    alias: b"a".to_vec(),
                    process: b"attack target".to_vec(),
                },
            ],
            title: Some(Vec::new()),
        };
        let wire = encode_alias_title_snapshot_v1(&value).unwrap();
        assert_eq!(decode_alias_title_snapshot_v1(&wire).unwrap(), value);
    }
}
