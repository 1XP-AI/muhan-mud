use std::collections::HashMap;
use std::fs;
use std::path::Path;

/// A field in the current, audited legacy fixture ABI.
///
/// This module is a deliberately read-only projection over the fixture grammar;
/// it does not recognize production saves, compressed payloads, or text encodings
/// beyond UTF-8 strings embedded in the test corpus.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct LegacyScalarField {
    pub offset: usize,
    pub width: usize,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum LegacyByteOrder {
    LittleEndian,
    BigEndian,
}

/// The explicit record layout supplied by an audited fixture/oracle pairing.
/// It is not inferred from a file and is not a portable legacy-save ABI.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct LegacyPlayerAbi {
    pub byte_order: LegacyByteOrder,
    pub creature_bytes: usize,
    pub object_bytes: usize,
    pub player_name_offset: usize,
    pub player_name_bytes: usize,
    pub player_level: LegacyScalarField,
    pub player_gold: LegacyScalarField,
    pub player_hp_max: LegacyScalarField,
    pub player_hp_current: LegacyScalarField,
    pub player_mp_max: LegacyScalarField,
    pub player_mp_current: LegacyScalarField,
    pub item_name_offset: usize,
    pub item_name_bytes: usize,
    pub item_description_offset: usize,
    pub item_description_bytes: usize,
    pub item_value: LegacyScalarField,
    pub item_weight: LegacyScalarField,
    pub item_type: LegacyScalarField,
    pub item_shots_max: LegacyScalarField,
    pub item_shots_current: LegacyScalarField,
}

/// Limits that make malformed fixture input fail before allocating an unbounded tree.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct LegacyProjectionLimits {
    pub max_input_bytes: usize,
    pub max_root_items: usize,
    pub max_items_per_container: usize,
    pub max_total_items: usize,
    pub max_depth: usize,
}

impl Default for LegacyProjectionLimits {
    fn default() -> Self {
        Self {
            max_input_bytes: 128 * 1024,
            max_root_items: 200,
            max_items_per_container: 4096,
            max_total_items: 4096,
            max_depth: 64,
        }
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum LegacyProjectionErrorKind {
    Abi,
    InputTooLarge,
    Truncated,
    InvalidCount,
    ItemLimit,
    DepthLimit,
    InvalidText,
    TrailingBytes,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct LegacyProjectionError {
    kind: LegacyProjectionErrorKind,
}

impl LegacyProjectionError {
    pub fn kind(self) -> LegacyProjectionErrorKind {
        self.kind
    }
}

impl std::fmt::Display for LegacyProjectionError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "legacy player projection failed: {:?}", self.kind)
    }
}

impl std::error::Error for LegacyProjectionError {}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ItemShadow {
    pub name: String,
    pub description: String,
    pub value: i64,
    pub weight: i64,
    pub type_code: i64,
    pub shots_max: i64,
    pub shots_current: i64,
    pub contents: Vec<ItemShadow>,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PlayerShadow {
    pub canonical_name: String,
    pub name_sha1: String,
    pub shard: String,
    /// A diagnostic fixture-relative path; this crate never opens it.
    pub relative_source_path: String,
    pub level: u8,
    pub gold: i64,
    pub hp_max: i64,
    pub hp_current: i64,
    pub mp_max: i64,
    pub mp_current: i64,
    pub inventory: Vec<ItemShadow>,
}

/// Closed, metadata-only identity evidence derived from an audited fixture projection.
///
/// This fixture-ABI-only view contains only a canonical name, that name's already-derived
/// SHA-1 digest and shard, plus the raw unsigned legacy level. It is not a production legacy
/// decoder, a source of authority, or a representation of a save, account, credential, or
/// runtime state.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct LegacyPlayerShadowIdentityV1 {
    canonical_name: String,
    name_sha1: String,
    shard: String,
    level: u8,
}

impl LegacyPlayerShadowIdentityV1 {
    /// The fixture-canonicalized player name.
    #[must_use]
    pub fn canonical_name(&self) -> &str {
        &self.canonical_name
    }

    /// The SHA-1 digest already derived from the canonical name by the fixture projection.
    #[must_use]
    pub fn name_sha1(&self) -> &str {
        &self.name_sha1
    }

    /// The two-hex-character shard already derived from the name digest.
    #[must_use]
    pub fn shard(&self) -> &str {
        &self.shard
    }

    /// The raw legacy unsigned-byte level.
    #[must_use]
    pub const fn level(&self) -> u8 {
        self.level
    }
}

/// Closed, fixture-ABI-only player locator derived from an audited projection.
///
/// This value contains only the fixture-canonicalized name, its already-derived
/// SHA-1 digest, and the digest-derived shard. It is neither a save decoder nor
/// a representation of player state, source paths, credentials, or runtime data.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct LegacyPlayerShadowLocatorV1 {
    canonical_name: String,
    name_sha1: String,
    shard: String,
}

impl LegacyPlayerShadowLocatorV1 {
    /// The fixture-canonicalized player name.
    #[must_use]
    pub fn canonical_name(&self) -> &str {
        &self.canonical_name
    }

    /// The SHA-1 digest already derived from the canonical name by the fixture projection.
    #[must_use]
    pub fn name_sha1(&self) -> &str {
        &self.name_sha1
    }

    /// The two-hex-character shard already derived from the name digest.
    #[must_use]
    pub fn shard(&self) -> &str {
        &self.shard
    }
}

impl PlayerShadow {
    /// Derives closed fixture-ABI-only metadata evidence from this pure projection.
    ///
    /// This intentionally copies no source path, raw bytes, inventory, statistics other than
    /// level, credential, database, runtime, writer, or deployment data.
    #[must_use]
    pub fn legacy_player_shadow_identity_v1(&self) -> LegacyPlayerShadowIdentityV1 {
        LegacyPlayerShadowIdentityV1 {
            canonical_name: self.canonical_name.clone(),
            name_sha1: self.name_sha1.clone(),
            shard: self.shard.clone(),
            level: self.level,
        }
    }

    /// Derives a closed fixture-ABI-only locator from this pure projection.
    ///
    /// This intentionally copies no level, source path, raw bytes, inventory,
    /// credential, database, runtime, writer, or deployment data.
    #[must_use]
    pub fn legacy_player_shadow_locator_v1(&self) -> LegacyPlayerShadowLocatorV1 {
        LegacyPlayerShadowLocatorV1 {
            canonical_name: self.canonical_name.clone(),
            name_sha1: self.name_sha1.clone(),
            shard: self.shard.clone(),
        }
    }

    /// A stable, deliberately small test/oracle representation, not a storage format.
    pub fn oracle_line(&self) -> String {
        format!(
            "OK|name={}|sha1={}|shard={}|source={}|level={}|gold={}|hp={}/{}|mp={}/{}|items={}",
            self.canonical_name,
            self.name_sha1,
            self.shard,
            self.relative_source_path,
            self.level,
            self.gold,
            self.hp_current,
            self.hp_max,
            self.mp_current,
            self.mp_max,
            oracle_items(&self.inventory),
        )
    }
}

fn oracle_items(items: &[ItemShadow]) -> String {
    let mut out = String::new();
    for (index, item) in items.iter().enumerate() {
        if index != 0 {
            out.push(',');
        }
        out.push_str(&format!(
            "{}(value={},weight={},type={},shots={}/{},description={})",
            item.name,
            item.value,
            item.weight,
            item.type_code,
            item.shots_current,
            item.shots_max,
            item.description,
        ));
        if !item.contents.is_empty() {
            out.push('{');
            out.push_str(&oracle_items(&item.contents));
            out.push('}');
        }
    }
    out
}

/// Projects one raw fixture player blob into a pure DTO without performing I/O.
/// Any malformed sub-record fails the whole projection; no partial DTO is returned.
pub fn project_legacy_player_shadow(
    input: &[u8],
    abi: &LegacyPlayerAbi,
    limits: LegacyProjectionLimits,
) -> Result<PlayerShadow, LegacyProjectionError> {
    validate_abi(abi)?;
    if input.len() > limits.max_input_bytes {
        return Err(error(LegacyProjectionErrorKind::InputTooLarge));
    }
    let mut cursor = Cursor::new(input);
    let player = cursor.take(abi.creature_bytes)?;
    let raw_name = read_text(player, abi.player_name_offset, abi.player_name_bytes)?;
    if raw_name.is_empty() || raw_name.contains('/') || raw_name.contains('\\') {
        return Err(error(LegacyProjectionErrorKind::InvalidText));
    }
    let canonical_name = canonical_legacy_name(raw_name);
    let root_count = cursor.read_count(limits.max_root_items, abi.byte_order)?;
    let mut total_items = 0usize;
    let mut inventory = Vec::with_capacity(root_count);
    for _ in 0..root_count {
        inventory.push(read_item(&mut cursor, abi, limits, 1, &mut total_items)?);
    }
    if cursor.remaining() != 0 {
        return Err(error(LegacyProjectionErrorKind::TrailingBytes));
    }

    let name_sha1 = sha1_hex(canonical_name.as_bytes());
    let shard = name_sha1[..2].to_string();
    Ok(PlayerShadow {
        relative_source_path: format!("player/{shard}/{canonical_name}"),
        canonical_name,
        name_sha1,
        shard,
        level: read_unsigned_byte(player, abi.player_level)?,
        gold: read_scalar(player, abi.player_gold, abi.byte_order)?,
        hp_max: read_scalar(player, abi.player_hp_max, abi.byte_order)?,
        hp_current: read_scalar(player, abi.player_hp_current, abi.byte_order)?,
        mp_max: read_scalar(player, abi.player_mp_max, abi.byte_order)?,
        mp_current: read_scalar(player, abi.player_mp_current, abi.byte_order)?,
        inventory,
    })
}

/// Match `scripts/export-player-inventory.py:canonical_name_key`: fold ASCII
/// upper-case bytes, then capitalize the first byte only when it is ASCII.
fn canonical_legacy_name(raw_name: String) -> String {
    let mut bytes = raw_name.into_bytes();
    for byte in &mut bytes {
        if byte.is_ascii_uppercase() {
            *byte = byte.to_ascii_lowercase();
        }
    }
    if let Some(first) = bytes.first_mut() {
        if first.is_ascii_lowercase() {
            *first = first.to_ascii_uppercase();
        }
    }
    // ASCII-only changes preserve valid UTF-8.
    String::from_utf8(bytes).expect("ASCII case folding preserves UTF-8")
}

fn read_item(
    cursor: &mut Cursor<'_>,
    abi: &LegacyPlayerAbi,
    limits: LegacyProjectionLimits,
    depth: usize,
    total_items: &mut usize,
) -> Result<ItemShadow, LegacyProjectionError> {
    if depth > limits.max_depth {
        return Err(error(LegacyProjectionErrorKind::DepthLimit));
    }
    *total_items = total_items
        .checked_add(1)
        .ok_or_else(|| error(LegacyProjectionErrorKind::ItemLimit))?;
    if *total_items > limits.max_total_items {
        return Err(error(LegacyProjectionErrorKind::ItemLimit));
    }
    let record = cursor.take(abi.object_bytes)?;
    let child_count = cursor.read_count(limits.max_items_per_container, abi.byte_order)?;
    let mut contents = Vec::with_capacity(child_count);
    for _ in 0..child_count {
        contents.push(read_item(cursor, abi, limits, depth + 1, total_items)?);
    }
    Ok(ItemShadow {
        name: read_text(record, abi.item_name_offset, abi.item_name_bytes)?,
        description: read_text(
            record,
            abi.item_description_offset,
            abi.item_description_bytes,
        )?,
        value: read_scalar(record, abi.item_value, abi.byte_order)?,
        weight: read_scalar(record, abi.item_weight, abi.byte_order)?,
        type_code: read_scalar(record, abi.item_type, abi.byte_order)?,
        shots_max: read_scalar(record, abi.item_shots_max, abi.byte_order)?,
        shots_current: read_scalar(record, abi.item_shots_current, abi.byte_order)?,
        contents,
    })
}

fn validate_abi(abi: &LegacyPlayerAbi) -> Result<(), LegacyProjectionError> {
    if abi.creature_bytes == 0
        || abi.object_bytes == 0
        || abi.player_name_bytes == 0
        || abi.item_name_bytes == 0
        || abi.item_description_bytes == 0
    {
        return Err(error(LegacyProjectionErrorKind::Abi));
    }
    validate_range(
        abi.creature_bytes,
        abi.player_name_offset,
        abi.player_name_bytes,
    )?;
    if abi.player_level.width != 1 {
        return Err(error(LegacyProjectionErrorKind::Abi));
    }
    for field in [
        abi.player_level,
        abi.player_gold,
        abi.player_hp_max,
        abi.player_hp_current,
        abi.player_mp_max,
        abi.player_mp_current,
    ] {
        validate_scalar(abi.creature_bytes, field)?;
    }
    validate_range(abi.object_bytes, abi.item_name_offset, abi.item_name_bytes)?;
    validate_range(
        abi.object_bytes,
        abi.item_description_offset,
        abi.item_description_bytes,
    )?;
    for field in [
        abi.item_value,
        abi.item_weight,
        abi.item_type,
        abi.item_shots_max,
        abi.item_shots_current,
    ] {
        validate_scalar(abi.object_bytes, field)?;
    }
    Ok(())
}

fn validate_scalar(
    record_len: usize,
    field: LegacyScalarField,
) -> Result<(), LegacyProjectionError> {
    if !matches!(field.width, 1 | 2 | 4 | 8) {
        return Err(error(LegacyProjectionErrorKind::Abi));
    }
    validate_range(record_len, field.offset, field.width)
}

fn validate_range(
    record_len: usize,
    offset: usize,
    width: usize,
) -> Result<(), LegacyProjectionError> {
    if offset
        .checked_add(width)
        .filter(|end| *end <= record_len)
        .is_none()
    {
        return Err(error(LegacyProjectionErrorKind::Abi));
    }
    Ok(())
}

fn read_text(record: &[u8], offset: usize, width: usize) -> Result<String, LegacyProjectionError> {
    let bytes = &record[offset..offset + width];
    let end = bytes
        .iter()
        .position(|byte| *byte == 0)
        .ok_or_else(|| error(LegacyProjectionErrorKind::InvalidText))?;
    std::str::from_utf8(&bytes[..end])
        .map(str::to_owned)
        .map_err(|_| error(LegacyProjectionErrorKind::InvalidText))
}

fn read_scalar(
    record: &[u8],
    field: LegacyScalarField,
    byte_order: LegacyByteOrder,
) -> Result<i64, LegacyProjectionError> {
    let bytes = &record[field.offset..field.offset + field.width];
    let value = match field.width {
        1 => i8::from_ne_bytes([bytes[0]]) as i64,
        2 => match byte_order {
            LegacyByteOrder::LittleEndian => {
                i16::from_le_bytes(bytes.try_into().expect("validated scalar width")) as i64
            }
            LegacyByteOrder::BigEndian => {
                i16::from_be_bytes(bytes.try_into().expect("validated scalar width")) as i64
            }
        },
        4 => match byte_order {
            LegacyByteOrder::LittleEndian => {
                i32::from_le_bytes(bytes.try_into().expect("validated scalar width")) as i64
            }
            LegacyByteOrder::BigEndian => {
                i32::from_be_bytes(bytes.try_into().expect("validated scalar width")) as i64
            }
        },
        8 => match byte_order {
            LegacyByteOrder::LittleEndian => {
                i64::from_le_bytes(bytes.try_into().expect("validated scalar width"))
            }
            LegacyByteOrder::BigEndian => {
                i64::from_be_bytes(bytes.try_into().expect("validated scalar width"))
            }
        },
        _ => return Err(error(LegacyProjectionErrorKind::Abi)),
    };
    Ok(value)
}

fn read_unsigned_byte(
    record: &[u8],
    field: LegacyScalarField,
) -> Result<u8, LegacyProjectionError> {
    if field.width != 1 {
        return Err(error(LegacyProjectionErrorKind::Abi));
    }
    Ok(record[field.offset])
}

fn error(kind: LegacyProjectionErrorKind) -> LegacyProjectionError {
    LegacyProjectionError { kind }
}

struct Cursor<'a> {
    bytes: &'a [u8],
    offset: usize,
}

impl<'a> Cursor<'a> {
    fn new(bytes: &'a [u8]) -> Self {
        Self { bytes, offset: 0 }
    }

    fn take(&mut self, count: usize) -> Result<&'a [u8], LegacyProjectionError> {
        let end = self
            .offset
            .checked_add(count)
            .ok_or_else(|| error(LegacyProjectionErrorKind::Truncated))?;
        let slice = self
            .bytes
            .get(self.offset..end)
            .ok_or_else(|| error(LegacyProjectionErrorKind::Truncated))?;
        self.offset = end;
        Ok(slice)
    }

    fn read_count(
        &mut self,
        max: usize,
        byte_order: LegacyByteOrder,
    ) -> Result<usize, LegacyProjectionError> {
        let bytes = self.take(4)?;
        let count = match byte_order {
            LegacyByteOrder::LittleEndian => {
                i32::from_le_bytes(bytes.try_into().expect("four-byte count"))
            }
            LegacyByteOrder::BigEndian => {
                i32::from_be_bytes(bytes.try_into().expect("four-byte count"))
            }
        };
        if count < 0 || count as usize > max {
            return Err(error(LegacyProjectionErrorKind::InvalidCount));
        }
        Ok(count as usize)
    }

    fn remaining(&self) -> usize {
        self.bytes.len() - self.offset
    }
}

fn sha1_hex(input: &[u8]) -> String {
    let mut h = [
        0x6745_2301u32,
        0xefcd_ab89,
        0x98ba_dcfe,
        0x1032_5476,
        0xc3d2_e1f0,
    ];
    let bit_len = (input.len() as u64).wrapping_mul(8);
    let mut padded = input.to_vec();
    padded.push(0x80);
    while !(padded.len() + 8).is_multiple_of(64) {
        padded.push(0);
    }
    padded.extend_from_slice(&bit_len.to_be_bytes());
    for block in padded.chunks_exact(64) {
        let mut words = [0u32; 80];
        for (index, word) in words[..16].iter_mut().enumerate() {
            *word = u32::from_be_bytes(
                block[index * 4..index * 4 + 4]
                    .try_into()
                    .expect("sha1 block"),
            );
        }
        for index in 16..80 {
            words[index] =
                (words[index - 3] ^ words[index - 8] ^ words[index - 14] ^ words[index - 16])
                    .rotate_left(1);
        }
        let (mut a, mut b, mut c, mut d, mut e) = (h[0], h[1], h[2], h[3], h[4]);
        for (index, word) in words.iter().enumerate() {
            let (f, k) = match index {
                0..=19 => ((b & c) | ((!b) & d), 0x5a82_7999),
                20..=39 => (b ^ c ^ d, 0x6ed9_eba1),
                40..=59 => ((b & c) | (b & d) | (c & d), 0x8f1b_bcdc),
                _ => (b ^ c ^ d, 0xca62_c1d6),
            };
            let next = a
                .rotate_left(5)
                .wrapping_add(f)
                .wrapping_add(e)
                .wrapping_add(k)
                .wrapping_add(*word);
            e = d;
            d = c;
            c = b.rotate_left(30);
            b = a;
            a = next;
        }
        h[0] = h[0].wrapping_add(a);
        h[1] = h[1].wrapping_add(b);
        h[2] = h[2].wrapping_add(c);
        h[3] = h[3].wrapping_add(d);
        h[4] = h[4].wrapping_add(e);
    }
    let mut output = String::with_capacity(40);
    for word in h {
        use std::fmt::Write;
        write!(&mut output, "{word:08x}").expect("writing to string");
    }
    output
}

pub type AliasMap = HashMap<String, String>;

pub fn legacy_path_hex(path_bytes: &[u8]) -> String {
    let mut s = String::with_capacity(path_bytes.len() * 2);
    for b in path_bytes {
        s.push_str(&format!("{:02X}", b));
    }
    s
}

pub fn load_alias_tsv<P: AsRef<Path>>(path: P) -> std::io::Result<AliasMap> {
    let text = fs::read_to_string(path)?;
    let mut map = AliasMap::new();

    for (idx, line) in text.lines().enumerate() {
        if idx == 0 && line.starts_with("legacy_path_hex\t") {
            continue;
        }
        if line.trim().is_empty() {
            continue;
        }
        let mut it = line.split('\t');
        let legacy_hex = it.next().unwrap_or("").trim();
        let _legacy_cp949 = it.next().unwrap_or("");
        let normalized = it.next().unwrap_or("").trim();
        if !legacy_hex.is_empty() && !normalized.is_empty() {
            map.insert(legacy_hex.to_string(), normalized.to_string());
        }
    }

    Ok(map)
}

pub fn resolve_from_map<'a>(map: &'a AliasMap, legacy_path_bytes: &[u8]) -> Option<&'a str> {
    let key = legacy_path_hex(legacy_path_bytes);
    map.get(&key).map(String::as_str)
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::io::Write;
    use std::path::{Path, PathBuf};
    use std::process::Command;

    #[test]
    fn legacy_player_projection_contract_is_available() {
        let _ = project_legacy_player_shadow;
    }

    fn fixture_abi() -> LegacyPlayerAbi {
        LegacyPlayerAbi {
            byte_order: LegacyByteOrder::LittleEndian,
            creature_bytes: 1952,
            object_bytes: 376,
            player_name_offset: 0,
            player_name_bytes: 80,
            player_level: LegacyScalarField {
                offset: 318,
                width: 1,
            },
            player_gold: LegacyScalarField {
                offset: 352,
                width: 8,
            },
            player_hp_max: LegacyScalarField {
                offset: 332,
                width: 2,
            },
            player_hp_current: LegacyScalarField {
                offset: 334,
                width: 2,
            },
            player_mp_max: LegacyScalarField {
                offset: 336,
                width: 2,
            },
            player_mp_current: LegacyScalarField {
                offset: 338,
                width: 2,
            },
            item_name_offset: 0,
            item_name_bytes: 80,
            item_description_offset: 80,
            item_description_bytes: 80,
            item_value: LegacyScalarField {
                offset: 304,
                width: 8,
            },
            item_weight: LegacyScalarField {
                offset: 312,
                width: 2,
            },
            item_type: LegacyScalarField {
                offset: 314,
                width: 1,
            },
            item_shots_max: LegacyScalarField {
                offset: 316,
                width: 2,
            },
            item_shots_current: LegacyScalarField {
                offset: 318,
                width: 2,
            },
        }
    }

    fn repo_root() -> PathBuf {
        Path::new(env!("CARGO_MANIFEST_DIR"))
            .join("../..")
            .canonicalize()
            .unwrap()
    }

    fn compiled_oracle(temp: &tempfile::TempDir) -> PathBuf {
        let root = repo_root();
        let executable = temp.path().join("legacy_player_oracle");
        let status = Command::new("cc")
            .args(["-std=c11", "-I"])
            .arg(root.join("src"))
            .arg(root.join("tests/harness/legacy_player_oracle.c"))
            .arg("-o")
            .arg(&executable)
            .status()
            .expect("C compiler is required for the legacy oracle test");
        assert!(
            status.success(),
            "legacy oracle must compile against the audited ABI"
        );
        executable
    }

    fn fixture(oracle: &Path, directory: &Path, name: &str) -> (Vec<u8>, String) {
        let path = directory.join(format!("{name}.bin"));
        let emitted = Command::new(oracle)
            .args(["emit", name])
            .arg(&path)
            .status()
            .unwrap();
        assert!(emitted.success(), "oracle must emit {name}");
        let expected = Command::new(oracle)
            .arg("project")
            .arg(&path)
            .output()
            .unwrap();
        assert!(expected.status.success());
        (
            std::fs::read(path).unwrap(),
            String::from_utf8(expected.stdout)
                .unwrap()
                .trim_end()
                .to_owned(),
        )
    }

    fn c_projection_line(oracle: &Path, path: &Path) -> String {
        let expected = Command::new(oracle)
            .arg("project")
            .arg(path)
            .output()
            .unwrap();
        assert!(expected.status.success());
        String::from_utf8(expected.stdout)
            .unwrap()
            .trim_end()
            .to_owned()
    }

    fn c_identity_line(oracle: &Path, path: &Path) -> String {
        let expected = Command::new(oracle)
            .arg("identity")
            .arg(path)
            .output()
            .unwrap();
        assert!(expected.status.success());
        String::from_utf8(expected.stdout)
            .unwrap()
            .trim_end()
            .to_owned()
    }

    fn c_locator_line(oracle: &Path, path: &Path) -> String {
        c_identity_line(oracle, path)
            .split_once("|level=")
            .expect("C identity oracle must include level evidence")
            .0
            .to_owned()
    }

    fn identity_line(identity: &LegacyPlayerShadowIdentityV1) -> String {
        format!(
            "OK|name={}|sha1={}|shard={}|level={}",
            identity.canonical_name(),
            identity.name_sha1(),
            identity.shard(),
            identity.level(),
        )
    }

    fn locator_line(locator: &LegacyPlayerShadowLocatorV1) -> String {
        format!(
            "OK|name={}|sha1={}|shard={}",
            locator.canonical_name(),
            locator.name_sha1(),
            locator.shard(),
        )
    }

    #[test]
    fn legacy_player_shadow_identity_v1_is_deterministic_fixture_metadata() {
        let temp = tempfile::tempdir().unwrap();
        let oracle = compiled_oracle(&temp);
        let (bytes, _) = fixture(&oracle, temp.path(), "mixed-case");
        let player =
            project_legacy_player_shadow(&bytes, &fixture_abi(), LegacyProjectionLimits::default())
                .unwrap();

        let first = player.legacy_player_shadow_identity_v1();
        let second = player.legacy_player_shadow_identity_v1();

        assert_eq!(first, second);
        assert_eq!(first.canonical_name(), "Alice");
        assert_eq!(
            first.name_sha1(),
            "35318264c9a98faf79965c270ac80c5606774df1"
        );
        assert_eq!(first.shard(), "35");
        assert_eq!(first.level(), 17);
    }

    #[test]
    fn legacy_player_shadow_identity_v1_matches_c_oracle_at_u8_boundaries() {
        let temp = tempfile::tempdir().unwrap();
        let oracle = compiled_oracle(&temp);
        let (mut bytes, _) = fixture(&oracle, temp.path(), "high-level");
        let abi = fixture_abi();
        let path = temp.path().join("identity-level.bin");

        for level in [0, 1, 127, 128, 255] {
            bytes[abi.player_level.offset] = level;
            std::fs::write(&path, &bytes).unwrap();
            let player =
                project_legacy_player_shadow(&bytes, &abi, LegacyProjectionLimits::default())
                    .unwrap();
            let identity = player.legacy_player_shadow_identity_v1();

            assert_eq!(identity.canonical_name(), "Alice", "level={level}");
            assert_eq!(identity.level(), level, "level={level}");
            assert_eq!(
                identity_line(&identity),
                c_identity_line(&oracle, &path),
                "level={level}"
            );
        }
    }

    #[test]
    fn legacy_player_shadow_locator_v1_is_deterministic_fixture_metadata() {
        let temp = tempfile::tempdir().unwrap();
        let oracle = compiled_oracle(&temp);
        let (bytes, _) = fixture(&oracle, temp.path(), "mixed-case");
        let player =
            project_legacy_player_shadow(&bytes, &fixture_abi(), LegacyProjectionLimits::default())
                .unwrap();

        let first = player.legacy_player_shadow_locator_v1();
        let second = player.legacy_player_shadow_locator_v1();

        assert_eq!(first, second);
        assert_eq!(first.canonical_name(), "Alice");
        assert_eq!(
            first.name_sha1(),
            "35318264c9a98faf79965c270ac80c5606774df1"
        );
        assert_eq!(first.shard(), "35");
    }

    #[test]
    fn legacy_player_shadow_locator_v1_matches_the_three_c_backed_fields_exactly() {
        let temp = tempfile::tempdir().unwrap();
        let oracle = compiled_oracle(&temp);

        for fixture_name in ["empty", "mixed-case", "high-level", "nested"] {
            let (bytes, _) = fixture(&oracle, temp.path(), fixture_name);
            let path = temp.path().join(format!("{fixture_name}.bin"));
            let player = project_legacy_player_shadow(
                &bytes,
                &fixture_abi(),
                LegacyProjectionLimits::default(),
            )
            .unwrap();

            assert_eq!(
                locator_line(&player.legacy_player_shadow_locator_v1()),
                c_locator_line(&oracle, &path),
                "fixture={fixture_name}"
            );
        }
    }

    #[test]
    fn changing_level_changes_identity_evidence_but_not_locator_values() {
        let temp = tempfile::tempdir().unwrap();
        let oracle = compiled_oracle(&temp);
        let (mut bytes, _) = fixture(&oracle, temp.path(), "high-level");
        let abi = fixture_abi();
        let path = temp.path().join("locator-level.bin");

        bytes[abi.player_level.offset] = 17;
        std::fs::write(&path, &bytes).unwrap();
        let low_level =
            project_legacy_player_shadow(&bytes, &abi, LegacyProjectionLimits::default()).unwrap();
        let low_identity = low_level.legacy_player_shadow_identity_v1();
        let low_locator = low_level.legacy_player_shadow_locator_v1();
        assert_eq!(
            identity_line(&low_identity),
            c_identity_line(&oracle, &path)
        );

        bytes[abi.player_level.offset] = u8::MAX;
        std::fs::write(&path, &bytes).unwrap();
        let high_level =
            project_legacy_player_shadow(&bytes, &abi, LegacyProjectionLimits::default()).unwrap();
        let high_identity = high_level.legacy_player_shadow_identity_v1();
        let high_locator = high_level.legacy_player_shadow_locator_v1();

        assert_ne!(low_identity, high_identity);
        assert_eq!(
            identity_line(&high_identity),
            c_identity_line(&oracle, &path)
        );
        assert_eq!(low_locator, high_locator);
        assert_eq!(locator_line(&high_locator), c_locator_line(&oracle, &path));
    }

    #[test]
    fn legacy_player_empty_fixture_matches_c_oracle_and_is_idempotent() {
        let temp = tempfile::tempdir().unwrap();
        let oracle = compiled_oracle(&temp);
        let (bytes, c_line) = fixture(&oracle, temp.path(), "empty");

        let first =
            project_legacy_player_shadow(&bytes, &fixture_abi(), LegacyProjectionLimits::default())
                .unwrap();
        let second =
            project_legacy_player_shadow(&bytes, &fixture_abi(), LegacyProjectionLimits::default())
                .unwrap();

        assert_eq!(first, second);
        assert_eq!(first.oracle_line(), c_line);
        assert_eq!(first.canonical_name, "Alice");
        assert_eq!(first.name_sha1, "35318264c9a98faf79965c270ac80c5606774df1");
        assert_eq!(first.shard, "35");
        assert_eq!(first.relative_source_path, "player/35/Alice");
        assert!(first.inventory.is_empty());
    }

    #[test]
    fn legacy_player_mixed_case_fixture_canonicalizes_before_dto_path_and_shard() {
        let temp = tempfile::tempdir().unwrap();
        let oracle = compiled_oracle(&temp);
        let (bytes, c_line) = fixture(&oracle, temp.path(), "mixed-case");

        let player =
            project_legacy_player_shadow(&bytes, &fixture_abi(), LegacyProjectionLimits::default())
                .unwrap();

        assert_eq!(player.oracle_line(), c_line);
        assert_eq!(player.canonical_name, "Alice");
        assert_eq!(player.name_sha1, "35318264c9a98faf79965c270ac80c5606774df1");
        assert_eq!(player.shard, "35");
        assert_eq!(player.relative_source_path, "player/35/Alice");
    }

    #[test]
    fn legacy_player_high_bit_levels_match_the_unsigned_c_oracle_exactly() {
        let temp = tempfile::tempdir().unwrap();
        let oracle = compiled_oracle(&temp);
        let (mut bytes, c_line) = fixture(&oracle, temp.path(), "high-level");
        let abi = fixture_abi();

        let player =
            project_legacy_player_shadow(&bytes, &abi, LegacyProjectionLimits::default()).unwrap();
        assert_eq!(player.level, u8::MAX);
        assert_eq!(player.oracle_line(), c_line);

        let path = temp.path().join("high-bit-level.bin");
        for level in 128u8..=u8::MAX {
            bytes[abi.player_level.offset] = level;
            std::fs::write(&path, &bytes).unwrap();
            let player =
                project_legacy_player_shadow(&bytes, &abi, LegacyProjectionLimits::default())
                    .unwrap();
            assert_eq!(player.level, level, "level={level}");
            assert_eq!(
                player.oracle_line(),
                c_projection_line(&oracle, &path),
                "level={level}"
            );
        }
    }

    #[test]
    fn legacy_player_nested_fixture_matches_c_oracle_item_contract() {
        let temp = tempfile::tempdir().unwrap();
        let oracle = compiled_oracle(&temp);
        let (bytes, c_line) = fixture(&oracle, temp.path(), "nested");

        let player =
            project_legacy_player_shadow(&bytes, &fixture_abi(), LegacyProjectionLimits::default())
                .unwrap();

        assert_eq!(player.oracle_line(), c_line);
        assert_eq!(player.inventory.len(), 1);
        assert_eq!(player.inventory[0].name, "Satchel");
        assert_eq!(player.inventory[0].description, "a canvas satchel");
        assert_eq!(player.inventory[0].value, 75);
        assert_eq!(player.inventory[0].contents[0].name, "Coin");
        assert_eq!(player.inventory[0].contents[0].value, 1);
    }

    #[test]
    fn malformed_fixture_has_no_partial_dto_and_matches_c_error_class() {
        let temp = tempfile::tempdir().unwrap();
        let oracle = compiled_oracle(&temp);
        for (name, expected_kind, expected_c) in [
            (
                "truncated",
                LegacyProjectionErrorKind::Truncated,
                "ERR|truncated",
            ),
            (
                "invalid-count",
                LegacyProjectionErrorKind::InvalidCount,
                "ERR|invalid-count",
            ),
        ] {
            let (bytes, c_line) = fixture(&oracle, temp.path(), name);
            let result = project_legacy_player_shadow(
                &bytes,
                &fixture_abi(),
                LegacyProjectionLimits::default(),
            );
            assert_eq!(result.unwrap_err().kind(), expected_kind, "{name}");
            assert_eq!(c_line, expected_c, "{name}");
        }
    }

    #[test]
    fn projection_enforces_an_input_byte_ceiling_before_decoding() {
        let temp = tempfile::tempdir().unwrap();
        let oracle = compiled_oracle(&temp);
        let (bytes, _) = fixture(&oracle, temp.path(), "empty");
        let limits = LegacyProjectionLimits {
            max_input_bytes: bytes.len() - 1,
            ..LegacyProjectionLimits::default()
        };

        let result = project_legacy_player_shadow(&bytes, &fixture_abi(), limits);

        assert_eq!(
            result.unwrap_err().kind(),
            LegacyProjectionErrorKind::InputTooLarge
        );
    }

    #[test]
    fn test_hex() {
        assert_eq!(legacy_path_hex(b"objmon/talk"), "6F626A6D6F6E2F74616C6B");
    }

    #[test]
    fn test_load_and_resolve() {
        let mut f = tempfile::NamedTempFile::new().unwrap();
        writeln!(
            f,
            "legacy_path_hex\tlegacy_path_cp949\tnormalized_utf8_path\tblob_sha1\n6F626A\tobj\tobj_utf8\tdeadbeef"
        )
        .unwrap();

        let map = load_alias_tsv(f.path()).unwrap();
        assert_eq!(resolve_from_map(&map, b"obj"), Some("obj_utf8"));
        assert_eq!(resolve_from_map(&map, b"none"), None);
    }
}
