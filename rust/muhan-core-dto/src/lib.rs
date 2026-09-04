//! Language-neutral canonical DTO envelope codec for the clone-only port.
//!
//! This crate deliberately has no FFI, allocator, game-state, or async-runtime
//! dependency. It accepts only fully-owned wire values.

use std::error::Error as StdError;
use std::fmt;

pub const MAGIC: [u8; 8] = *b"MUHCDTO\0";
pub const WIRE_VERSION: u16 = 1;
pub const DIGEST_LENGTH: usize = 32;
const PREFIX_LENGTH: usize = 16;
const FIELD_HEADER_LENGTH: usize = 7;
/// Matches the C decoder's allocation guard.  A u16 field ID space cannot
/// contain more canonical unique fields, but retain the cap explicitly so a
/// malformed envelope never grows an unbounded decoded vector.
pub const MAX_FIELDS: usize = 65_536;

/// A schema kind with an explicit per-kind payload cap.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
#[repr(u16)]
pub enum Kind {
    Creature = 1,
    Object = 2,
    Room = 3,
    Session = 4,
    AbiFingerprint = 5,
    ObjectGraph = 6,
    PlayerSnapshot = 7,
}

impl Kind {
    pub const fn payload_limit(self) -> usize {
        match self {
            Self::Creature => 4 * 1024 * 1024,
            Self::Object => 2 * 1024 * 1024,
            Self::Room => 8 * 1024 * 1024,
            Self::Session => 1024 * 1024,
            Self::AbiFingerprint => 1024 * 1024,
            Self::ObjectGraph => 4 * 1024 * 1024,
            Self::PlayerSnapshot => 4 * 1024 * 1024,
        }
    }

    const fn wire_value(self) -> u16 {
        self as u16
    }

    fn from_wire(value: u16) -> Result<Self, Error> {
        match value {
            1 => Ok(Self::Creature),
            2 => Ok(Self::Object),
            3 => Ok(Self::Room),
            4 => Ok(Self::Session),
            5 => Ok(Self::AbiFingerprint),
            6 => Ok(Self::ObjectGraph),
            7 => Ok(Self::PlayerSnapshot),
            _ => Err(Error::UnknownKind { kind: value }),
        }
    }
}

/// The largest legal whole envelope: a Room payload plus prefix and digest.
pub const MAX_ENVELOPE_SIZE: usize = Kind::Room.payload_limit() + PREFIX_LENGTH + DIGEST_LENGTH;

pub mod player_snapshot_v1;

pub const TYPE_U8: u8 = 1;
pub const TYPE_U16: u8 = 2;
pub const TYPE_U32: u8 = 3;
pub const TYPE_U64: u8 = 4;
pub const TYPE_I8: u8 = 5;
pub const TYPE_I16: u8 = 6;
pub const TYPE_I32: u8 = 7;
pub const TYPE_I64: u8 = 8;
pub const TYPE_BYTES: u8 = 9;
pub const TYPE_TEXT: u8 = 10;
pub const TYPE_BOOL: u8 = 11;
pub const OPTIONAL_TYPE_BIT: u8 = 0x80;

/// A canonical field. Values remain raw so extension data is losslessly retained.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Field {
    id: u16,
    type_tag: u8,
    value: Vec<u8>,
}

impl Field {
    pub fn bytes(id: u16, value: Vec<u8>) -> Self {
        Self::raw(id, TYPE_BYTES, value)
    }

    pub fn optional_bytes(id: u16, value: Vec<u8>) -> Self {
        Self::raw(id, TYPE_BYTES | OPTIONAL_TYPE_BIT, value)
    }

    pub fn text(id: u16, value: String) -> Self {
        Self::raw(id, TYPE_TEXT, value.into_bytes())
    }

    pub fn u8(id: u16, value: u8) -> Self {
        Self::raw(id, TYPE_U8, vec![value])
    }

    pub fn u16(id: u16, value: u16) -> Self {
        Self::raw(id, TYPE_U16, value.to_be_bytes().to_vec())
    }

    pub fn u32(id: u16, value: u32) -> Self {
        Self::raw(id, TYPE_U32, value.to_be_bytes().to_vec())
    }

    pub fn u64(id: u16, value: u64) -> Self {
        Self::raw(id, TYPE_U64, value.to_be_bytes().to_vec())
    }

    pub fn i8(id: u16, value: i8) -> Self {
        Self::raw(id, TYPE_I8, value.to_be_bytes().to_vec())
    }

    pub fn i16(id: u16, value: i16) -> Self {
        Self::raw(id, TYPE_I16, value.to_be_bytes().to_vec())
    }

    pub fn i32(id: u16, value: i32) -> Self {
        Self::raw(id, TYPE_I32, value.to_be_bytes().to_vec())
    }

    pub fn i64(id: u16, value: i64) -> Self {
        Self::raw(id, TYPE_I64, value.to_be_bytes().to_vec())
    }

    pub fn bool(id: u16, value: bool) -> Self {
        Self::raw(id, TYPE_BOOL, vec![u8::from(value)])
    }

    /// Unknown tags are allowed only after setting the optional high bit.
    pub fn optional_raw(id: u16, type_tag: u8, value: Vec<u8>) -> Self {
        Self::raw(id, type_tag | OPTIONAL_TYPE_BIT, value)
    }

    pub fn raw(id: u16, type_tag: u8, value: Vec<u8>) -> Self {
        Self {
            id,
            type_tag,
            value,
        }
    }

    pub const fn id(&self) -> u16 {
        self.id
    }
    pub const fn type_tag(&self) -> u8 {
        self.type_tag
    }
    pub fn value(&self) -> &[u8] {
        &self.value
    }
}

/// A parsed CDTO record, before game-specific schema interpretation.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Record {
    kind: Kind,
    fields: Vec<Field>,
}

/// One explicit daily-use tuple. This is encoded field-by-field as two bytes
/// plus a big-endian i64, never as a native C struct image.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct DailyV1 {
    pub max: u8,
    pub current: u8,
    pub last_used: i64,
}

/// Detached, clone-only safe projection of a legacy `creature`. Passwords,
/// talk state, file descriptors, `lasttime`, ready slots, inventory, and every
/// pointer/link are intentionally outside this fixed 37-field schema.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct CreatureV1 {
    pub name: [u8; 80],
    pub description: [u8; 80],
    pub key: [[u8; 20]; 3],
    pub level: u8,
    pub type_code: i8,
    pub class: i8,
    pub race: i8,
    pub numwander: i8,
    pub alignment: i16,
    pub strength: i8,
    pub dexterity: i8,
    pub constitution: i8,
    pub intelligence: i8,
    pub piety: i8,
    pub hp_max: i16,
    pub hp_current: i16,
    pub mp_max: i16,
    pub mp_current: i16,
    pub armor: i8,
    pub thaco: i8,
    pub experience: i64,
    pub gold: i64,
    pub ndice: i16,
    pub sdice: i16,
    pub pdice: i16,
    pub special: i16,
    pub proficiency: [i64; 5],
    pub realm: [i64; 4],
    pub spells: [u8; 16],
    pub flags: [u8; 8],
    pub quests: [u8; 16],
    pub quest_num: i8,
    pub carry: [i16; 10],
    pub room_number: i16,
    pub daily: [DailyV1; 10],
}

fn fixed_string_is_canonical(value: &[u8]) -> bool {
    let mut terminated = false;
    for &byte in value {
        if terminated && byte != 0 {
            return false;
        }
        if byte == 0 {
            terminated = true;
        }
    }
    true
}

fn creature_is_canonical(value: &CreatureV1) -> bool {
    fixed_string_is_canonical(&value.name)
        && fixed_string_is_canonical(&value.description)
        && value.key.iter().all(|key| fixed_string_is_canonical(key))
        && value.hp_current <= value.hp_max
        && value.mp_current <= value.mp_max
        && value.daily.iter().all(|daily| daily.current <= daily.max)
}

fn i64_array_bytes(values: &[i64]) -> Vec<u8> {
    values
        .iter()
        .flat_map(|value| value.to_be_bytes())
        .collect()
}

fn i16_array_bytes(values: &[i16]) -> Vec<u8> {
    values
        .iter()
        .flat_map(|value| value.to_be_bytes())
        .collect()
}

fn daily_bytes(values: &[DailyV1; 10]) -> Vec<u8> {
    let mut output = Vec::with_capacity(100);
    for daily in values {
        output.push(daily.max);
        output.push(daily.current);
        output.extend_from_slice(&daily.last_used.to_be_bytes());
    }
    output
}

fn read_i64_array<const N: usize>(value: &[u8]) -> [i64; N] {
    std::array::from_fn(|index| i64::from_be_bytes(array(&value[index * 8..(index + 1) * 8])))
}

fn read_i16_array<const N: usize>(value: &[u8]) -> [i16; N] {
    std::array::from_fn(|index| i16::from_be_bytes(array(&value[index * 2..(index + 1) * 2])))
}

fn read_daily(value: &[u8]) -> [DailyV1; 10] {
    std::array::from_fn(|index| DailyV1 {
        max: value[index * 10],
        current: value[index * 10 + 1],
        last_used: i64::from_be_bytes(array(&value[index * 10 + 2..index * 10 + 10])),
    })
}

/// Encode the explicit safe CreatureV1 projection. It deliberately has no
/// password, pointer, inventory, transport, or talk-state field.
pub fn encode_creature_v1(input: &CreatureV1) -> Result<Vec<u8>, Error> {
    if !creature_is_canonical(input) {
        return Err(Error::InvalidFieldLength { field_id: 0 });
    }
    let fields = vec![
        Field::bytes(1, input.name.to_vec()),
        Field::bytes(2, input.description.to_vec()),
        Field::bytes(3, input.key[0].to_vec()),
        Field::bytes(4, input.key[1].to_vec()),
        Field::bytes(5, input.key[2].to_vec()),
        Field::u8(6, input.level),
        Field::i8(7, input.type_code),
        Field::i8(8, input.class),
        Field::i8(9, input.race),
        Field::i8(10, input.numwander),
        Field::i16(11, input.alignment),
        Field::i8(12, input.strength),
        Field::i8(13, input.dexterity),
        Field::i8(14, input.constitution),
        Field::i8(15, input.intelligence),
        Field::i8(16, input.piety),
        Field::i16(17, input.hp_max),
        Field::i16(18, input.hp_current),
        Field::i16(19, input.mp_max),
        Field::i16(20, input.mp_current),
        Field::i8(21, input.armor),
        Field::i8(22, input.thaco),
        Field::i64(23, input.experience),
        Field::i64(24, input.gold),
        Field::i16(25, input.ndice),
        Field::i16(26, input.sdice),
        Field::i16(27, input.pdice),
        Field::i16(28, input.special),
        Field::bytes(29, i64_array_bytes(&input.proficiency)),
        Field::bytes(30, i64_array_bytes(&input.realm)),
        Field::bytes(31, input.spells.to_vec()),
        Field::bytes(32, input.flags.to_vec()),
        Field::bytes(33, input.quests.to_vec()),
        Field::i8(34, input.quest_num),
        Field::bytes(35, i16_array_bytes(&input.carry)),
        Field::i16(36, input.room_number),
        Field::bytes(37, daily_bytes(&input.daily)),
    ];
    encode(&Record::new(Kind::Creature, fields)?)
}

/// Decode a closed CreatureV1 schema and reject noncanonical padding or
/// current/max state rather than silently changing canonical bytes.
pub fn decode_creature_v1(wire: &[u8]) -> Result<CreatureV1, Error> {
    let record = decode(wire)?;
    if record.kind != Kind::Creature || record.fields.len() != 37 {
        return Err(Error::InvalidFieldLength { field_id: 0 });
    }
    let expected = [
        (TYPE_BYTES, 80usize),
        (TYPE_BYTES, 80),
        (TYPE_BYTES, 20),
        (TYPE_BYTES, 20),
        (TYPE_BYTES, 20),
        (TYPE_U8, 1),
        (TYPE_I8, 1),
        (TYPE_I8, 1),
        (TYPE_I8, 1),
        (TYPE_I8, 1),
        (TYPE_I16, 2),
        (TYPE_I8, 1),
        (TYPE_I8, 1),
        (TYPE_I8, 1),
        (TYPE_I8, 1),
        (TYPE_I8, 1),
        (TYPE_I16, 2),
        (TYPE_I16, 2),
        (TYPE_I16, 2),
        (TYPE_I16, 2),
        (TYPE_I8, 1),
        (TYPE_I8, 1),
        (TYPE_I64, 8),
        (TYPE_I64, 8),
        (TYPE_I16, 2),
        (TYPE_I16, 2),
        (TYPE_I16, 2),
        (TYPE_I16, 2),
        (TYPE_BYTES, 40),
        (TYPE_BYTES, 32),
        (TYPE_BYTES, 16),
        (TYPE_BYTES, 8),
        (TYPE_BYTES, 16),
        (TYPE_I8, 1),
        (TYPE_BYTES, 20),
        (TYPE_I16, 2),
        (TYPE_BYTES, 100),
    ];
    for (index, (tag, length)) in expected.iter().enumerate() {
        let field = &record.fields[index];
        if field.id != (index + 1) as u16 || field.type_tag != *tag || field.value.len() != *length
        {
            return Err(Error::InvalidFieldLength {
                field_id: (index + 1) as u16,
            });
        }
    }
    let bytes = |index: usize| record.fields[index].value.as_slice();
    let output = CreatureV1 {
        name: array(bytes(0)),
        description: array(bytes(1)),
        key: [array(bytes(2)), array(bytes(3)), array(bytes(4))],
        level: bytes(5)[0],
        type_code: bytes(6)[0] as i8,
        class: bytes(7)[0] as i8,
        race: bytes(8)[0] as i8,
        numwander: bytes(9)[0] as i8,
        alignment: i16::from_be_bytes(array(bytes(10))),
        strength: bytes(11)[0] as i8,
        dexterity: bytes(12)[0] as i8,
        constitution: bytes(13)[0] as i8,
        intelligence: bytes(14)[0] as i8,
        piety: bytes(15)[0] as i8,
        hp_max: i16::from_be_bytes(array(bytes(16))),
        hp_current: i16::from_be_bytes(array(bytes(17))),
        mp_max: i16::from_be_bytes(array(bytes(18))),
        mp_current: i16::from_be_bytes(array(bytes(19))),
        armor: bytes(20)[0] as i8,
        thaco: bytes(21)[0] as i8,
        experience: i64::from_be_bytes(array(bytes(22))),
        gold: i64::from_be_bytes(array(bytes(23))),
        ndice: i16::from_be_bytes(array(bytes(24))),
        sdice: i16::from_be_bytes(array(bytes(25))),
        pdice: i16::from_be_bytes(array(bytes(26))),
        special: i16::from_be_bytes(array(bytes(27))),
        proficiency: read_i64_array(bytes(28)),
        realm: read_i64_array(bytes(29)),
        spells: array(bytes(30)),
        flags: array(bytes(31)),
        quests: array(bytes(32)),
        quest_num: bytes(33)[0] as i8,
        carry: read_i16_array(bytes(34)),
        room_number: i16::from_be_bytes(array(bytes(35))),
        daily: read_daily(bytes(36)),
    };
    if !creature_is_canonical(&output) {
        return Err(Error::InvalidFieldLength { field_id: 0 });
    }
    Ok(output)
}

/// Clone-only projection of one detached legacy `object`. Fixed legacy char
/// arrays remain raw 80/20-byte fields, including NUL padding. This is not a
/// gameplay or persistence type: nested/attached object graphs are deliberately
/// outside ObjectV1 and are rejected by the C exporter.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ObjectV1 {
    pub name: [u8; 80],
    pub description: [u8; 80],
    pub key: [[u8; 20]; 3],
    pub use_output: [u8; 80],
    pub value: i64,
    pub weight: i16,
    pub type_code: i8,
    pub adjustment: i8,
    pub shots_max: i16,
    pub shots_current: i16,
    pub ndice: i16,
    pub sdice: i16,
    pub pdice: i16,
    pub armor: i8,
    pub wear_flag: i8,
    pub magic_power: i8,
    pub magic_realm: i8,
    pub special: i16,
    pub flags: [u8; 8],
    pub quest_num: i8,
}

/// Encode the explicit, pointer-free 22-field ObjectV1 projection.
pub fn encode_object_v1(input: &ObjectV1) -> Result<Vec<u8>, Error> {
    if input.shots_current > input.shots_max {
        return Err(Error::InvalidFieldLength { field_id: 12 });
    }
    let fields = vec![
        Field::bytes(1, input.name.to_vec()),
        Field::bytes(2, input.description.to_vec()),
        Field::bytes(3, input.key[0].to_vec()),
        Field::bytes(4, input.key[1].to_vec()),
        Field::bytes(5, input.key[2].to_vec()),
        Field::bytes(6, input.use_output.to_vec()),
        Field::i64(7, input.value),
        Field::i16(8, input.weight),
        Field::i8(9, input.type_code),
        Field::i8(10, input.adjustment),
        Field::i16(11, input.shots_max),
        Field::i16(12, input.shots_current),
        Field::i16(13, input.ndice),
        Field::i16(14, input.sdice),
        Field::i16(15, input.pdice),
        Field::i8(16, input.armor),
        Field::i8(17, input.wear_flag),
        Field::i8(18, input.magic_power),
        Field::i8(19, input.magic_realm),
        Field::i16(20, input.special),
        Field::bytes(21, input.flags.to_vec()),
        Field::i8(22, input.quest_num),
    ];
    encode(&Record::new(Kind::Object, fields)?)
}

/// Decode a C ObjectV1 envelope and reject missing, added, or type-mismatched
/// fields rather than silently accepting schema drift.
pub fn decode_object_v1(wire: &[u8]) -> Result<ObjectV1, Error> {
    let record = decode(wire)?;
    if record.kind != Kind::Object || record.fields.len() != 22 {
        return Err(Error::InvalidFieldLength { field_id: 0 });
    }
    let expected = [
        (TYPE_BYTES, 80usize),
        (TYPE_BYTES, 80),
        (TYPE_BYTES, 20),
        (TYPE_BYTES, 20),
        (TYPE_BYTES, 20),
        (TYPE_BYTES, 80),
        (TYPE_I64, 8),
        (TYPE_I16, 2),
        (TYPE_I8, 1),
        (TYPE_I8, 1),
        (TYPE_I16, 2),
        (TYPE_I16, 2),
        (TYPE_I16, 2),
        (TYPE_I16, 2),
        (TYPE_I16, 2),
        (TYPE_I8, 1),
        (TYPE_I8, 1),
        (TYPE_I8, 1),
        (TYPE_I8, 1),
        (TYPE_I16, 2),
        (TYPE_BYTES, 8),
        (TYPE_I8, 1),
    ];
    for (index, (tag, length)) in expected.iter().enumerate() {
        let field = &record.fields[index];
        if field.id != (index + 1) as u16 || field.type_tag != *tag || field.value.len() != *length
        {
            return Err(Error::InvalidFieldLength {
                field_id: (index + 1) as u16,
            });
        }
    }
    let bytes = |index: usize| record.fields[index].value.as_slice();
    let output = ObjectV1 {
        name: array(bytes(0)),
        description: array(bytes(1)),
        key: [array(bytes(2)), array(bytes(3)), array(bytes(4))],
        use_output: array(bytes(5)),
        value: i64::from_be_bytes(array(bytes(6))),
        weight: i16::from_be_bytes(array(bytes(7))),
        type_code: bytes(8)[0] as i8,
        adjustment: bytes(9)[0] as i8,
        shots_max: i16::from_be_bytes(array(bytes(10))),
        shots_current: i16::from_be_bytes(array(bytes(11))),
        ndice: i16::from_be_bytes(array(bytes(12))),
        sdice: i16::from_be_bytes(array(bytes(13))),
        pdice: i16::from_be_bytes(array(bytes(14))),
        armor: bytes(15)[0] as i8,
        wear_flag: bytes(16)[0] as i8,
        magic_power: bytes(17)[0] as i8,
        magic_realm: bytes(18)[0] as i8,
        special: i16::from_be_bytes(array(bytes(19))),
        flags: array(bytes(20)),
        quest_num: bytes(21)[0] as i8,
    };
    if output.shots_current > output.shots_max {
        return Err(Error::InvalidFieldLength { field_id: 12 });
    }
    Ok(output)
}

/// Maximum object nesting and node count accepted by the synthetic-only graph
/// codec.  These are logical limits, separate from the CDTO envelope limit.
pub const OBJECT_GRAPH_V1_MAX_DEPTH: usize = 64;
pub const OBJECT_GRAPH_V1_MAX_NODES: usize = 8192;
pub const OBJECT_GRAPH_V1_NODE_LENGTH: usize = 349;

/// One preorder ObjectGraphV1 node.  `parent_index` and `child_index` are
/// explicit wire identities: the root parent is `None`, and its child index is
/// its root-list position.  The type deliberately has no address-like field.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ObjectGraphNodeV1 {
    pub object: ObjectV1,
    pub parent_index: Option<u32>,
    pub child_index: u32,
}

/// A detached ordered object forest.  Nodes are stored in deterministic
/// preorder; there is no native list tag, room/creature parent, or allocator
/// identity in this value.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ObjectGraphV1 {
    pub nodes: Vec<ObjectGraphNodeV1>,
}

fn object_graph_object_is_canonical(value: &ObjectV1) -> bool {
    fixed_string_is_canonical(&value.name)
        && fixed_string_is_canonical(&value.description)
        && value.key.iter().all(|key| fixed_string_is_canonical(key))
        && fixed_string_is_canonical(&value.use_output)
        && value.shots_current <= value.shots_max
}

fn graph_error(index: usize) -> Error {
    Error::InvalidFieldLength {
        field_id: u16::try_from(index + 2).expect("ObjectGraphV1 field ids fit in u16"),
    }
}

fn object_graph_node_bytes(index: usize, node: &ObjectGraphNodeV1) -> Result<Vec<u8>, Error> {
    if !object_graph_object_is_canonical(&node.object) {
        return Err(graph_error(index));
    }
    let parent = node.parent_index.unwrap_or(u32::MAX);
    if parent != u32::MAX && parent as usize >= index {
        return Err(graph_error(index));
    }
    let mut value = Vec::with_capacity(OBJECT_GRAPH_V1_NODE_LENGTH);
    value.extend_from_slice(&(index as u32).to_be_bytes());
    value.extend_from_slice(&parent.to_be_bytes());
    value.extend_from_slice(&node.child_index.to_be_bytes());
    value.extend_from_slice(&node.object.name);
    value.extend_from_slice(&node.object.description);
    for key in &node.object.key {
        value.extend_from_slice(key);
    }
    value.extend_from_slice(&node.object.use_output);
    value.extend_from_slice(&node.object.value.to_be_bytes());
    value.extend_from_slice(&node.object.weight.to_be_bytes());
    value.extend_from_slice(&node.object.type_code.to_be_bytes());
    value.extend_from_slice(&node.object.adjustment.to_be_bytes());
    value.extend_from_slice(&node.object.shots_max.to_be_bytes());
    value.extend_from_slice(&node.object.shots_current.to_be_bytes());
    value.extend_from_slice(&node.object.ndice.to_be_bytes());
    value.extend_from_slice(&node.object.sdice.to_be_bytes());
    value.extend_from_slice(&node.object.pdice.to_be_bytes());
    value.extend_from_slice(&node.object.armor.to_be_bytes());
    value.extend_from_slice(&node.object.wear_flag.to_be_bytes());
    value.extend_from_slice(&node.object.magic_power.to_be_bytes());
    value.extend_from_slice(&node.object.magic_realm.to_be_bytes());
    value.extend_from_slice(&node.object.special.to_be_bytes());
    value.extend_from_slice(&node.object.flags);
    value.extend_from_slice(&node.object.quest_num.to_be_bytes());
    debug_assert_eq!(value.len(), OBJECT_GRAPH_V1_NODE_LENGTH);
    Ok(value)
}

fn validate_object_graph_nodes(nodes: &[ObjectGraphNodeV1]) -> Result<(), Error> {
    if nodes.len() > OBJECT_GRAPH_V1_MAX_NODES {
        return Err(Error::SizeLimitExceeded {
            limit: OBJECT_GRAPH_V1_MAX_NODES,
        });
    }
    let mut child_counts = vec![0u32; nodes.len()];
    let mut depths = vec![0usize; nodes.len()];
    let mut ancestors = Vec::with_capacity(nodes.len());
    let mut roots = 0u32;
    for (index, node) in nodes.iter().enumerate() {
        let expected = match node.parent_index {
            None => {
                ancestors.clear();
                depths[index] = 1;
                let value = roots;
                roots += 1;
                value
            }
            Some(parent) => {
                let parent = parent as usize;
                if parent >= index {
                    return Err(graph_error(index));
                }
                let Some(position) = ancestors.iter().position(|&ancestor| ancestor == parent)
                else {
                    return Err(graph_error(index));
                };
                ancestors.truncate(position + 1);
                depths[index] = depths[parent] + 1;
                let value = child_counts[parent];
                child_counts[parent] += 1;
                value
            }
        };
        if node.child_index != expected {
            return Err(graph_error(index));
        }
        if depths[index] > OBJECT_GRAPH_V1_MAX_DEPTH {
            return Err(Error::SizeLimitExceeded {
                limit: OBJECT_GRAPH_V1_MAX_DEPTH,
            });
        }
        object_graph_node_bytes(index, node)?;
        ancestors.push(index);
    }
    Ok(())
}

/// Encode a closed, canonical, pointer-free object forest.  Each node has an
/// explicit preorder index, parent index, and sibling index; legacy object
/// pointers, struct padding, room/creature attachment, and passwords cannot
/// be represented by this schema.
pub fn encode_object_graph_v1(input: &ObjectGraphV1) -> Result<Vec<u8>, Error> {
    validate_object_graph_nodes(&input.nodes)?;
    let mut fields = Vec::with_capacity(input.nodes.len() + 1);
    fields.push(Field::u32(1, input.nodes.len() as u32));
    for (index, node) in input.nodes.iter().enumerate() {
        fields.push(Field::bytes(
            u16::try_from(index + 2).expect("ObjectGraphV1 field ids fit in u16"),
            object_graph_node_bytes(index, node)?,
        ));
    }
    encode(&Record::new(Kind::ObjectGraph, fields)?)
}

/// Decode ObjectGraphV1 and fail closed on unknown fields, non-preorder parent
/// identities, invalid sibling positions, invalid fixed-string tails, or
/// depth/node limits.  The resulting value is allocation-only Rust data.
pub fn decode_object_graph_v1(wire: &[u8]) -> Result<ObjectGraphV1, Error> {
    let record = decode(wire)?;
    if record.kind != Kind::ObjectGraph || record.fields.is_empty() {
        return Err(Error::InvalidFieldLength { field_id: 0 });
    }
    let count_field = &record.fields[0];
    if count_field.id != 1 || count_field.type_tag != TYPE_U32 || count_field.value.len() != 4 {
        return Err(Error::InvalidFieldLength { field_id: 1 });
    }
    let count = read_u32(&count_field.value) as usize;
    if count > OBJECT_GRAPH_V1_MAX_NODES {
        return Err(Error::InvalidFieldLength { field_id: 1 });
    }
    if record.fields.len() != count + 1 {
        return Err(Error::InvalidFieldLength { field_id: 0 });
    }
    let mut nodes = Vec::with_capacity(count);
    for index in 0..count {
        let field = &record.fields[index + 1];
        if field.id != (index + 2) as u16
            || field.type_tag != TYPE_BYTES
            || field.value.len() != OBJECT_GRAPH_V1_NODE_LENGTH
        {
            return Err(graph_error(index));
        }
        let value = field.value.as_slice();
        if read_u32(&value[0..4]) as usize != index {
            return Err(graph_error(index));
        }
        let parent_raw = read_u32(&value[4..8]);
        let mut cursor = 12;
        let take = |cursor: &mut usize, length: usize| {
            let start = *cursor;
            *cursor += length;
            &value[start..start + length]
        };
        let object = ObjectV1 {
            name: array(take(&mut cursor, 80)),
            description: array(take(&mut cursor, 80)),
            key: [
                array(take(&mut cursor, 20)),
                array(take(&mut cursor, 20)),
                array(take(&mut cursor, 20)),
            ],
            use_output: array(take(&mut cursor, 80)),
            value: i64::from_be_bytes(array(take(&mut cursor, 8))),
            weight: i16::from_be_bytes(array(take(&mut cursor, 2))),
            type_code: i8::from_be_bytes(array(take(&mut cursor, 1))),
            adjustment: i8::from_be_bytes(array(take(&mut cursor, 1))),
            shots_max: i16::from_be_bytes(array(take(&mut cursor, 2))),
            shots_current: i16::from_be_bytes(array(take(&mut cursor, 2))),
            ndice: i16::from_be_bytes(array(take(&mut cursor, 2))),
            sdice: i16::from_be_bytes(array(take(&mut cursor, 2))),
            pdice: i16::from_be_bytes(array(take(&mut cursor, 2))),
            armor: i8::from_be_bytes(array(take(&mut cursor, 1))),
            wear_flag: i8::from_be_bytes(array(take(&mut cursor, 1))),
            magic_power: i8::from_be_bytes(array(take(&mut cursor, 1))),
            magic_realm: i8::from_be_bytes(array(take(&mut cursor, 1))),
            special: i16::from_be_bytes(array(take(&mut cursor, 2))),
            flags: array(take(&mut cursor, 8)),
            quest_num: i8::from_be_bytes(array(take(&mut cursor, 1))),
        };
        debug_assert_eq!(cursor, OBJECT_GRAPH_V1_NODE_LENGTH);
        if !object_graph_object_is_canonical(&object) {
            return Err(graph_error(index));
        }
        nodes.push(ObjectGraphNodeV1 {
            object,
            parent_index: (parent_raw != u32::MAX).then_some(parent_raw),
            child_index: read_u32(&value[8..12]),
        });
    }
    validate_object_graph_nodes(&nodes)?;
    Ok(ObjectGraphV1 { nodes })
}

fn array<const N: usize>(value: &[u8]) -> [u8; N] {
    value
        .try_into()
        .expect("ObjectV1 schema length was checked")
}

impl Record {
    pub fn new(kind: Kind, fields: Vec<Field>) -> Result<Self, Error> {
        validate_fields(&fields)?;
        Ok(Self { kind, fields })
    }

    pub const fn kind(&self) -> Kind {
        self.kind
    }
    pub fn fields(&self) -> &[Field] {
        &self.fields
    }
}

/// Stable malformed-input classes. These variants never contain source bytes,
/// text values, or digest values, so error output cannot leak DTO secrets.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum Error {
    Truncated { at: usize },
    InvalidMagic,
    UnsupportedVersion { version: u16 },
    UnknownKind { kind: u16 },
    SizeLimitExceeded { limit: usize },
    TrailingBytes,
    DigestMismatch,
    DuplicateField { field_id: u16 },
    OutOfOrderField { previous: u16, current: u16 },
    UnknownMandatoryType { type_tag: u8 },
    InvalidFieldLength { field_id: u16 },
    InvalidUtf8 { field_id: u16 },
    InvalidBoolean { field_id: u16 },
    LengthOverflow,
    NonCanonicalEncoding,
}

impl fmt::Display for Error {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        let label = match self {
            Self::Truncated { .. } => "truncated input",
            Self::InvalidMagic => "invalid magic",
            Self::UnsupportedVersion { .. } => "unsupported wire version",
            Self::UnknownKind { .. } => "unknown kind",
            Self::SizeLimitExceeded { .. } => "size limit exceeded",
            Self::TrailingBytes => "trailing bytes",
            Self::DigestMismatch => "payload digest mismatch",
            Self::DuplicateField { .. } => "duplicate field id",
            Self::OutOfOrderField { .. } => "out-of-order field id",
            Self::UnknownMandatoryType { .. } => "unknown mandatory type",
            Self::InvalidFieldLength { .. } => "invalid field length",
            Self::InvalidUtf8 { .. } => "invalid UTF-8 text field",
            Self::InvalidBoolean { .. } => "invalid boolean field",
            Self::LengthOverflow => "length overflow",
            Self::NonCanonicalEncoding => "non-canonical encoding",
        };
        formatter.write_str(label)
    }
}

impl StdError for Error {}

/// Encodes a record into the one CDTO v1 canonical byte sequence.
pub fn encode(record: &Record) -> Result<Vec<u8>, Error> {
    validate_fields(&record.fields)?;
    let payload_length = encoded_payload_length(&record.fields)?;
    if payload_length > record.kind.payload_limit() {
        return Err(Error::SizeLimitExceeded {
            limit: record.kind.payload_limit(),
        });
    }
    let total = PREFIX_LENGTH
        .checked_add(payload_length)
        .and_then(|value| value.checked_add(DIGEST_LENGTH))
        .ok_or(Error::LengthOverflow)?;
    if total > MAX_ENVELOPE_SIZE {
        return Err(Error::SizeLimitExceeded {
            limit: MAX_ENVELOPE_SIZE,
        });
    }
    let mut wire = Vec::with_capacity(total);
    wire.extend_from_slice(&MAGIC);
    wire.extend_from_slice(&WIRE_VERSION.to_be_bytes());
    wire.extend_from_slice(&record.kind.wire_value().to_be_bytes());
    wire.extend_from_slice(&(payload_length as u32).to_be_bytes());
    for field in &record.fields {
        wire.extend_from_slice(&field.id.to_be_bytes());
        wire.push(field.type_tag);
        wire.extend_from_slice(&(field.value.len() as u32).to_be_bytes());
        wire.extend_from_slice(&field.value);
    }
    wire.extend_from_slice(&sha256(&wire[PREFIX_LENGTH..]));
    Ok(wire)
}

/// Decodes and validates CDTO v1 without retaining source bytes in errors.
pub fn decode(wire: &[u8]) -> Result<Record, Error> {
    if wire.len() > MAX_ENVELOPE_SIZE {
        return Err(Error::SizeLimitExceeded {
            limit: MAX_ENVELOPE_SIZE,
        });
    }
    let mut cursor = 0;
    let magic = take(wire, &mut cursor, MAGIC.len())?;
    if magic != MAGIC {
        return Err(Error::InvalidMagic);
    }
    let version = read_u16(take(wire, &mut cursor, 2)?);
    if version != WIRE_VERSION {
        return Err(Error::UnsupportedVersion { version });
    }
    let kind = Kind::from_wire(read_u16(take(wire, &mut cursor, 2)?))?;
    let payload_length = read_u32(take(wire, &mut cursor, 4)?) as usize;
    if payload_length > kind.payload_limit() {
        return Err(Error::SizeLimitExceeded {
            limit: kind.payload_limit(),
        });
    }
    let payload_end = PREFIX_LENGTH
        .checked_add(payload_length)
        .ok_or(Error::LengthOverflow)?;
    let expected_end = payload_end
        .checked_add(DIGEST_LENGTH)
        .ok_or(Error::LengthOverflow)?;
    if wire.len() < expected_end {
        return Err(Error::Truncated { at: wire.len() });
    }
    if wire.len() > expected_end {
        return Err(Error::TrailingBytes);
    }
    let payload = &wire[PREFIX_LENGTH..payload_end];
    if sha256(payload).as_slice() != &wire[payload_end..expected_end] {
        return Err(Error::DigestMismatch);
    }
    let mut fields = Vec::new();
    let mut payload_cursor = 0;
    let mut previous = None;
    while payload_cursor < payload.len() {
        let id = read_u16(take(payload, &mut payload_cursor, 2)?);
        let type_tag = take(payload, &mut payload_cursor, 1)?[0];
        let value_length = read_u32(take(payload, &mut payload_cursor, 4)?) as usize;
        let value_end = payload_cursor
            .checked_add(value_length)
            .ok_or(Error::LengthOverflow)?;
        if value_end > payload.len() {
            return Err(Error::InvalidFieldLength { field_id: id });
        }
        let field = Field::raw(id, type_tag, payload[payload_cursor..value_end].to_vec());
        if let Some(previous_id) = previous {
            if field.id == previous_id {
                return Err(Error::DuplicateField { field_id: field.id });
            }
            if field.id < previous_id {
                return Err(Error::OutOfOrderField {
                    previous: previous_id,
                    current: field.id,
                });
            }
        }
        validate_type(&field)?;
        if fields.len() == MAX_FIELDS {
            return Err(Error::SizeLimitExceeded { limit: MAX_FIELDS });
        }
        previous = Some(field.id);
        fields.push(field);
        payload_cursor = value_end;
    }
    Ok(Record { kind, fields })
}

fn take<'a>(input: &'a [u8], cursor: &mut usize, length: usize) -> Result<&'a [u8], Error> {
    let end = cursor.checked_add(length).ok_or(Error::LengthOverflow)?;
    if end > input.len() {
        return Err(Error::Truncated { at: input.len() });
    }
    let result = &input[*cursor..end];
    *cursor = end;
    Ok(result)
}

fn read_u16(value: &[u8]) -> u16 {
    u16::from_be_bytes([value[0], value[1]])
}
fn read_u32(value: &[u8]) -> u32 {
    u32::from_be_bytes([value[0], value[1], value[2], value[3]])
}

fn encoded_payload_length(fields: &[Field]) -> Result<usize, Error> {
    fields.iter().try_fold(0usize, |length, field| {
        length
            .checked_add(FIELD_HEADER_LENGTH)
            .and_then(|total| total.checked_add(field.value.len()))
            .ok_or(Error::LengthOverflow)
    })
}

fn validate_fields(fields: &[Field]) -> Result<(), Error> {
    if fields.len() > MAX_FIELDS {
        return Err(Error::SizeLimitExceeded { limit: MAX_FIELDS });
    }
    let mut previous = None;
    for field in fields {
        if let Some(previous_id) = previous {
            if field.id == previous_id {
                return Err(Error::DuplicateField { field_id: field.id });
            }
            if field.id < previous_id {
                return Err(Error::OutOfOrderField {
                    previous: previous_id,
                    current: field.id,
                });
            }
        }
        previous = Some(field.id);
        validate_type(field)?;
    }
    Ok(())
}

fn validate_type(field: &Field) -> Result<(), Error> {
    let optional = field.type_tag & OPTIONAL_TYPE_BIT != 0;
    let base = field.type_tag & !OPTIONAL_TYPE_BIT;
    let length_ok = match base {
        TYPE_U8 | TYPE_I8 | TYPE_BOOL => field.value.len() == 1,
        TYPE_U16 | TYPE_I16 => field.value.len() == 2,
        TYPE_U32 | TYPE_I32 => field.value.len() == 4,
        TYPE_U64 | TYPE_I64 => field.value.len() == 8,
        TYPE_BYTES => true,
        TYPE_TEXT => std::str::from_utf8(&field.value)
            .map(|_| true)
            .map_err(|_| Error::InvalidUtf8 { field_id: field.id })?,
        _ if optional => return Ok(()),
        _ => {
            return Err(Error::UnknownMandatoryType {
                type_tag: field.type_tag,
            })
        }
    };
    if !length_ok {
        return if base == TYPE_BOOL {
            Err(Error::InvalidBoolean { field_id: field.id })
        } else {
            Err(Error::InvalidFieldLength { field_id: field.id })
        };
    }
    if base == TYPE_BOOL && !matches!(field.value[0], 0 | 1) {
        return Err(Error::InvalidBoolean { field_id: field.id });
    }
    Ok(())
}

// Dependency-free SHA-256, private because this is only an envelope integrity field.
fn sha256(input: &[u8]) -> [u8; DIGEST_LENGTH] {
    const INITIAL: [u32; 8] = [
        0x6a09_e667,
        0xbb67_ae85,
        0x3c6e_f372,
        0xa54f_f53a,
        0x510e_527f,
        0x9b05_688c,
        0x1f83_d9ab,
        0x5be0_cd19,
    ];
    const K: [u32; 64] = [
        0x428a_2f98,
        0x7137_4491,
        0xb5c0_fbcf,
        0xe9b5_dba5,
        0x3956_c25b,
        0x59f1_11f1,
        0x923f_82a4,
        0xab1c_5ed5,
        0xd807_aa98,
        0x1283_5b01,
        0x2431_85be,
        0x550c_7dc3,
        0x72be_5d74,
        0x80de_b1fe,
        0x9bdc_06a7,
        0xc19b_f174,
        0xe49b_69c1,
        0xefbe_4786,
        0x0fc1_9dc6,
        0x240c_a1cc,
        0x2de9_2c6f,
        0x4a74_84aa,
        0x5cb0_a9dc,
        0x76f9_88da,
        0x983e_5152,
        0xa831_c66d,
        0xb003_27c8,
        0xbf59_7fc7,
        0xc6e0_0bf3,
        0xd5a7_9147,
        0x06ca_6351,
        0x1429_2967,
        0x27b7_0a85,
        0x2e1b_2138,
        0x4d2c_6dfc,
        0x5338_0d13,
        0x650a_7354,
        0x766a_0abb,
        0x81c2_c92e,
        0x9272_2c85,
        0xa2bf_e8a1,
        0xa81a_664b,
        0xc24b_8b70,
        0xc76c_51a3,
        0xd192_e819,
        0xd699_0624,
        0xf40e_3585,
        0x106a_a070,
        0x19a4_c116,
        0x1e37_6c08,
        0x2748_774c,
        0x34b0_bcb5,
        0x391c_0cb3,
        0x4ed8_aa4a,
        0x5b9c_ca4f,
        0x682e_6ff3,
        0x748f_82ee,
        0x78a5_636f,
        0x84c8_7814,
        0x8cc7_0208,
        0x90be_fffa,
        0xa450_6ceb,
        0xbef9_a3f7,
        0xc671_78f2,
    ];
    let bit_length = (input.len() as u64).wrapping_mul(8);
    let padding = (120 - ((input.len() + 1) % 64)) % 64;
    let mut bytes = Vec::with_capacity(input.len() + 1 + padding + 8);
    bytes.extend_from_slice(input);
    bytes.push(0x80);
    bytes.resize(input.len() + 1 + padding, 0);
    bytes.extend_from_slice(&bit_length.to_be_bytes());
    let mut state = INITIAL;
    let (blocks, remainder) = bytes.as_chunks::<64>();
    debug_assert!(remainder.is_empty());
    for block in blocks {
        let mut words = [0u32; 64];
        let (chunks, remainder) = block.as_chunks::<4>();
        debug_assert!(remainder.is_empty());
        for (index, chunk) in chunks.iter().enumerate() {
            words[index] = u32::from_be_bytes(*chunk);
        }
        for index in 16..64 {
            let small0 = words[index - 15].rotate_right(7)
                ^ words[index - 15].rotate_right(18)
                ^ (words[index - 15] >> 3);
            let small1 = words[index - 2].rotate_right(17)
                ^ words[index - 2].rotate_right(19)
                ^ (words[index - 2] >> 10);
            words[index] = words[index - 16]
                .wrapping_add(small0)
                .wrapping_add(words[index - 7])
                .wrapping_add(small1);
        }
        let [mut a, mut b, mut c, mut d, mut e, mut f, mut g, mut h] = state;
        for index in 0..64 {
            let sum1 = e.rotate_right(6) ^ e.rotate_right(11) ^ e.rotate_right(25);
            let choice = (e & f) ^ ((!e) & g);
            let temp1 = h
                .wrapping_add(sum1)
                .wrapping_add(choice)
                .wrapping_add(K[index])
                .wrapping_add(words[index]);
            let sum0 = a.rotate_right(2) ^ a.rotate_right(13) ^ a.rotate_right(22);
            let temp2 = sum0.wrapping_add((a & b) ^ (a & c) ^ (b & c));
            h = g;
            g = f;
            f = e;
            e = d.wrapping_add(temp1);
            d = c;
            c = b;
            b = a;
            a = temp1.wrapping_add(temp2);
        }
        state[0] = state[0].wrapping_add(a);
        state[1] = state[1].wrapping_add(b);
        state[2] = state[2].wrapping_add(c);
        state[3] = state[3].wrapping_add(d);
        state[4] = state[4].wrapping_add(e);
        state[5] = state[5].wrapping_add(f);
        state[6] = state[6].wrapping_add(g);
        state[7] = state[7].wrapping_add(h);
    }
    let mut digest = [0u8; DIGEST_LENGTH];
    for (index, value) in state.iter().enumerate() {
        digest[index * 4..index * 4 + 4].copy_from_slice(&value.to_be_bytes());
    }
    digest
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn canonical_round_trip_has_a_digest() {
        const C_CREATURE_GOLDEN: &str = "4d55484344544f000001000100000017000109000000055465727261000203000000040000002abd42d992f5822c03c8cd5f3d1f5d59baf16d471022c81989109304a43cd57347";
        let record = Record::new(
            Kind::Creature,
            vec![Field::bytes(1, b"Terra".to_vec()), Field::u32(2, 42)],
        )
        .unwrap();
        let wire = encode(&record).unwrap();
        assert_eq!(wire, decode_hex(C_CREATURE_GOLDEN));
        assert_eq!(&wire[..8], MAGIC);
        assert_eq!(read_u16(&wire[8..10]), WIRE_VERSION);
        assert_eq!(decode(&wire).unwrap(), record);
        assert_eq!(encode(&decode(&wire).unwrap()).unwrap(), wire);
    }

    #[test]
    fn abi_fingerprint_has_a_dedicated_cross_language_kind() {
        let record = Record::new(Kind::AbiFingerprint, vec![Field::u16(1, 1)]).unwrap();
        let wire = encode(&record).unwrap();
        assert_eq!(&wire[10..12], &[0, 5]);
        assert_eq!(decode(&wire).unwrap().kind(), Kind::AbiFingerprint);
    }

    #[test]
    fn object_v1_is_pointer_free_and_schema_closed() {
        let object = ObjectV1 {
            name: object_fixed(b"bronze-key"),
            description: object_fixed(b"A weathered bronze key."),
            key: [
                object_fixed(b"key"),
                object_fixed(b"bronze"),
                object_fixed(b"quest"),
            ],
            use_output: object_fixed(b"The key turns.\n"),
            value: 0x0001_0203_0405,
            weight: -7,
            type_code: 4,
            adjustment: -2,
            shots_max: 9,
            shots_current: 7,
            ndice: 1,
            sdice: 8,
            pdice: -3,
            armor: -1,
            wear_flag: 3,
            magic_power: 6,
            magic_realm: 2,
            special: 77,
            flags: [0x55, 0, 0, 0, 0, 0, 0, 0xaa],
            quest_num: 12,
        };
        let wire = encode_object_v1(&object).unwrap();
        assert_eq!(decode_object_v1(&wire).unwrap(), object);
        let mut fields = decode(&wire).unwrap().fields().to_vec();
        fields.push(Field::optional_raw(23, 0x61, vec![1]));
        let drift = encode(&Record::new(Kind::Object, fields).unwrap()).unwrap();
        assert!(matches!(
            decode_object_v1(&drift),
            Err(Error::InvalidFieldLength { .. })
        ));
        let mut noncanonical = object;
        noncanonical.shots_current = noncanonical.shots_max + 1;
        assert!(matches!(
            encode_object_v1(&noncanonical),
            Err(Error::InvalidFieldLength { field_id: 12 })
        ));
        let mut noncanonical_fields = decode(&wire).unwrap().fields().to_vec();
        noncanonical_fields[11].value = vec![0, 10];
        let noncanonical_wire =
            encode(&Record::new(Kind::Object, noncanonical_fields).unwrap()).unwrap();
        assert!(matches!(
            decode_object_v1(&noncanonical_wire),
            Err(Error::InvalidFieldLength { field_id: 12 })
        ));
    }

    #[test]
    fn object_graph_v1_is_preorder_closed_and_padding_free() {
        let object = ObjectV1 {
            name: object_fixed(b"synthetic-bag"),
            description: object_fixed(b"synthetic object"),
            key: [object_fixed(b"bag"), [0; 20], [0; 20]],
            use_output: [0; 80],
            value: 11,
            weight: 1,
            type_code: 4,
            adjustment: 0,
            shots_max: 5,
            shots_current: 3,
            ndice: 1,
            sdice: 2,
            pdice: 0,
            armor: 0,
            wear_flag: 0,
            magic_power: 0,
            magic_realm: 0,
            special: 0,
            flags: [0; 8],
            quest_num: 0,
        };
        let graph = ObjectGraphV1 {
            nodes: vec![
                ObjectGraphNodeV1 {
                    object: object.clone(),
                    parent_index: None,
                    child_index: 0,
                },
                ObjectGraphNodeV1 {
                    object,
                    parent_index: Some(0),
                    child_index: 0,
                },
            ],
        };
        let wire = encode_object_graph_v1(&graph).unwrap();
        assert_eq!(decode_object_graph_v1(&wire).unwrap(), graph);
        assert_eq!(
            encode_object_graph_v1(&decode_object_graph_v1(&wire).unwrap()).unwrap(),
            wire
        );

        let mut non_preorder = graph.clone();
        non_preorder.nodes[1].parent_index = Some(1);
        assert!(matches!(
            encode_object_graph_v1(&non_preorder),
            Err(Error::InvalidFieldLength { .. })
        ));
        let closed_root_reentry = ObjectGraphV1 {
            nodes: vec![
                ObjectGraphNodeV1 {
                    object: graph.nodes[0].object.clone(),
                    parent_index: None,
                    child_index: 0,
                },
                ObjectGraphNodeV1 {
                    object: graph.nodes[1].object.clone(),
                    parent_index: Some(0),
                    child_index: 0,
                },
                ObjectGraphNodeV1 {
                    object: graph.nodes[0].object.clone(),
                    parent_index: None,
                    child_index: 1,
                },
                ObjectGraphNodeV1 {
                    object: graph.nodes[1].object.clone(),
                    parent_index: Some(0),
                    child_index: 1,
                },
            ],
        };
        assert!(matches!(
            encode_object_graph_v1(&closed_root_reentry),
            Err(Error::InvalidFieldLength { .. })
        ));
        let closed_sibling_reentry = ObjectGraphV1 {
            nodes: vec![
                ObjectGraphNodeV1 {
                    object: graph.nodes[0].object.clone(),
                    parent_index: None,
                    child_index: 0,
                },
                ObjectGraphNodeV1 {
                    object: graph.nodes[1].object.clone(),
                    parent_index: Some(0),
                    child_index: 0,
                },
                ObjectGraphNodeV1 {
                    object: graph.nodes[0].object.clone(),
                    parent_index: Some(0),
                    child_index: 1,
                },
                ObjectGraphNodeV1 {
                    object: graph.nodes[1].object.clone(),
                    parent_index: Some(1),
                    child_index: 0,
                },
            ],
        };
        assert!(matches!(
            encode_object_graph_v1(&closed_sibling_reentry),
            Err(Error::InvalidFieldLength { .. })
        ));
        let maximum = ObjectGraphV1 {
            nodes: (0..OBJECT_GRAPH_V1_MAX_NODES)
                .map(|index| ObjectGraphNodeV1 {
                    object: graph.nodes[0].object.clone(),
                    parent_index: None,
                    child_index: index as u32,
                })
                .collect(),
        };
        assert!(encode_object_graph_v1(&maximum).is_ok());
        let mut over_limit = maximum;
        over_limit.nodes.push(ObjectGraphNodeV1 {
            object: graph.nodes[0].object.clone(),
            parent_index: None,
            child_index: OBJECT_GRAPH_V1_MAX_NODES as u32,
        });
        assert!(matches!(
            encode_object_graph_v1(&over_limit),
            Err(Error::SizeLimitExceeded {
                limit: OBJECT_GRAPH_V1_MAX_NODES
            })
        ));
        let depth_64 = ObjectGraphV1 {
            nodes: (0..OBJECT_GRAPH_V1_MAX_DEPTH)
                .map(|index| ObjectGraphNodeV1 {
                    object: graph.nodes[0].object.clone(),
                    parent_index: index.checked_sub(1).map(|parent| parent as u32),
                    child_index: 0,
                })
                .collect(),
        };
        assert!(encode_object_graph_v1(&depth_64).is_ok());
        let mut depth_65 = depth_64.clone();
        depth_65.nodes.push(ObjectGraphNodeV1 {
            object: graph.nodes[0].object.clone(),
            parent_index: Some((OBJECT_GRAPH_V1_MAX_DEPTH - 1) as u32),
            child_index: 0,
        });
        assert!(matches!(
            encode_object_graph_v1(&depth_65),
            Err(Error::SizeLimitExceeded {
                limit: OBJECT_GRAPH_V1_MAX_DEPTH
            })
        ));
        let mut depth_fields = decode(&encode_object_graph_v1(&depth_64).unwrap())
            .unwrap()
            .fields()
            .to_vec();
        depth_fields[0] = Field::u32(1, (OBJECT_GRAPH_V1_MAX_DEPTH + 1) as u32);
        depth_fields.push(Field::bytes(
            (OBJECT_GRAPH_V1_MAX_DEPTH + 2) as u16,
            object_graph_node_bytes(OBJECT_GRAPH_V1_MAX_DEPTH, &depth_65.nodes[64]).unwrap(),
        ));
        let depth_wire = encode(&Record::new(Kind::ObjectGraph, depth_fields).unwrap()).unwrap();
        assert!(matches!(
            decode_object_graph_v1(&depth_wire),
            Err(Error::SizeLimitExceeded {
                limit: OBJECT_GRAPH_V1_MAX_DEPTH
            })
        ));
        let two_roots = ObjectGraphV1 {
            nodes: vec![
                ObjectGraphNodeV1 {
                    object: graph.nodes[0].object.clone(),
                    parent_index: None,
                    child_index: 0,
                },
                ObjectGraphNodeV1 {
                    object: graph.nodes[1].object.clone(),
                    parent_index: Some(0),
                    child_index: 0,
                },
                ObjectGraphNodeV1 {
                    object: graph.nodes[0].object.clone(),
                    parent_index: None,
                    child_index: 1,
                },
            ],
        };
        assert_eq!(
            decode_object_graph_v1(&encode_object_graph_v1(&two_roots).unwrap()).unwrap(),
            two_roots
        );
        let mut padded = graph;
        padded.nodes[0].object.name[20] = b'x';
        assert!(matches!(
            encode_object_graph_v1(&padded),
            Err(Error::InvalidFieldLength { .. })
        ));
    }

    #[test]
    fn creature_v1_is_closed_canonical_and_password_free() {
        let creature = creature_fixture();
        let wire = encode_creature_v1(&creature).unwrap();
        assert_eq!(decode_creature_v1(&wire).unwrap(), creature);
        assert_eq!(
            encode_creature_v1(&decode_creature_v1(&wire).unwrap()).unwrap(),
            wire
        );

        let mut current_over_max = decode(&wire).unwrap().fields().to_vec();
        current_over_max[17].value = vec![0, 124];
        let current_over_max =
            encode(&Record::new(Kind::Creature, current_over_max).unwrap()).unwrap();
        assert!(matches!(
            decode_creature_v1(&current_over_max),
            Err(Error::InvalidFieldLength { .. })
        ));

        let mut bad_padding = decode(&wire).unwrap().fields().to_vec();
        bad_padding[0].value[2] = 0;
        let bad_padding = encode(&Record::new(Kind::Creature, bad_padding).unwrap()).unwrap();
        assert!(matches!(
            decode_creature_v1(&bad_padding),
            Err(Error::InvalidFieldLength { .. })
        ));

        let mut noncanonical_daily = creature.clone();
        noncanonical_daily.daily[0].current = noncanonical_daily.daily[0].max + 1;
        assert!(matches!(
            encode_creature_v1(&noncanonical_daily),
            Err(Error::InvalidFieldLength { .. })
        ));
    }

    #[test]
    fn c_emitted_abi_fingerprint_golden_is_canonical() {
        /* Emitted by `cdto_v1_encode_abi_fingerprint` on the macOS/aarch64
         * test image.  It has ABI kind 5, not a Session surrogate. */
        const C_ABI_FINGERPRINT_HEX: &str = "4d55484344544f00000100050000014600010a000000056d61636f7300020a00000007616172636836340003040000000800000000000000080004040000000800000000000000080005040000000800000000000001780006040000000800000000000007a00007040000000800000000000002f8000804000000080000000000002460000904000000080000000000000158000a04000000080000000000000160000b04000000080000000000000168000c04000000080000000000000170000d040000000800000000000001f8000e04000000080000000000000780000f040000000800000000000007980010040000000800000000000002d80011040000000800000000000002e00012040000000800000000000002e80013040000000800000000000002f0001404000000080000000000000000001504000000080000000000000400001604000000080000000000002418cf2f331d7d5da5d4b32aef32dd57f9c33294b4e0a5411f80e76d49f621131147";
        let wire = decode_hex(C_ABI_FINGERPRINT_HEX);
        let record = decode(&wire).unwrap();
        assert_eq!(record.kind(), Kind::AbiFingerprint);
        assert_eq!(record.fields().len(), 22);
        assert_eq!(encode(&record).unwrap(), wire);
    }

    #[test]
    fn field_count_cap_matches_c_boundary_and_malformed_priority() {
        let fields: Vec<_> = (0..=u16::MAX)
            .map(|id| Field::bytes(id, Vec::new()))
            .collect();
        assert_eq!(fields.len(), MAX_FIELDS);
        let record = Record::new(Kind::Session, fields).unwrap();
        assert_eq!(decode(&encode(&record).unwrap()).unwrap(), record);

        let mut too_many = record.fields().to_vec();
        too_many.push(Field::bytes(u16::MAX, Vec::new()));
        assert_eq!(
            Record::new(Kind::Session, too_many),
            Err(Error::SizeLimitExceeded { limit: MAX_FIELDS })
        );

        let mut malformed: Vec<_> = (0..=u16::MAX)
            .map(|id| (id, TYPE_BYTES, Vec::new()))
            .collect();
        malformed.push((u16::MAX, TYPE_BYTES, Vec::new()));
        assert_eq!(
            decode(&record_wire(Kind::Session, &malformed)),
            Err(Error::DuplicateField { field_id: u16::MAX })
        );
    }

    #[test]
    fn known_and_unknown_optional_extensions_round_trip_raw() {
        let record = Record::new(
            Kind::Object,
            vec![
                Field::optional_bytes(4, vec![0, 0xff, 7]),
                Field::optional_raw(99, 0x61, vec![0x80, 2]),
            ],
        )
        .unwrap();
        let wire = encode(&record).unwrap();
        let decoded = decode(&wire).unwrap();
        assert_eq!(decoded.fields()[0].value(), [0, 0xff, 7]);
        assert_eq!(decoded.fields()[1].type_tag(), 0xe1);
        assert_eq!(encode(&decoded).unwrap(), wire);
    }

    #[test]
    fn malformed_corpus_is_stably_classified() {
        let record = Record::new(
            Kind::Session,
            vec![Field::u8(1, 3), Field::bytes(2, b"ok".to_vec())],
        )
        .unwrap();
        let valid = encode(&record).unwrap();
        let mut bad_magic = valid.clone();
        bad_magic[0] ^= 1;
        assert_eq!(decode(&bad_magic), Err(Error::InvalidMagic));
        let mut bad_version = valid.clone();
        bad_version[9] = 2;
        assert_eq!(
            decode(&bad_version),
            Err(Error::UnsupportedVersion { version: 2 })
        );
        let mut bad_digest = valid.clone();
        let last = bad_digest.len() - 1;
        bad_digest[last] ^= 1;
        assert_eq!(decode(&bad_digest), Err(Error::DigestMismatch));
        assert!(matches!(
            decode(&valid[..valid.len() - 1]),
            Err(Error::Truncated { .. })
        ));
        let mut trailing = valid.clone();
        trailing.push(0);
        assert_eq!(decode(&trailing), Err(Error::TrailingBytes));
        assert_eq!(
            decode(&vec![0; MAX_ENVELOPE_SIZE + 1]),
            Err(Error::SizeLimitExceeded {
                limit: MAX_ENVELOPE_SIZE
            })
        );
        assert_eq!(
            decode(&vec![0; MAX_ENVELOPE_SIZE + 1]),
            Err(Error::SizeLimitExceeded {
                limit: MAX_ENVELOPE_SIZE
            })
        );
        assert_eq!(
            decode(&record_wire(
                Kind::Session,
                &[(1, TYPE_U8, vec![1]), (1, TYPE_U8, vec![2])]
            )),
            Err(Error::DuplicateField { field_id: 1 })
        );
        assert_eq!(
            decode(&record_wire(
                Kind::Session,
                &[(2, TYPE_U8, vec![1]), (1, TYPE_U8, vec![2])]
            )),
            Err(Error::OutOfOrderField {
                previous: 2,
                current: 1
            })
        );
        assert_eq!(
            decode(&record_wire(Kind::Session, &[(1, 0x61, vec![1])])),
            Err(Error::UnknownMandatoryType { type_tag: 0x61 })
        );
    }

    #[test]
    fn field_validation_rejects_invalid_encodings_and_size_budgets() {
        assert_eq!(
            Record::new(Kind::Object, vec![Field::u16(2, 1), Field::u8(1, 2)]),
            Err(Error::OutOfOrderField {
                previous: 2,
                current: 1
            })
        );
        assert_eq!(
            Record::new(Kind::Object, vec![Field::raw(1, TYPE_TEXT, vec![0xff])]),
            Err(Error::InvalidUtf8 { field_id: 1 })
        );
        assert_eq!(
            Record::new(Kind::Object, vec![Field::raw(1, TYPE_BOOL, vec![2])]),
            Err(Error::InvalidBoolean { field_id: 1 })
        );
        let too_large = Record::new(
            Kind::Session,
            vec![Field::bytes(1, vec![0; Kind::Session.payload_limit()])],
        )
        .unwrap();
        assert_eq!(
            encode(&too_large),
            Err(Error::SizeLimitExceeded {
                limit: Kind::Session.payload_limit()
            })
        );
    }

    #[test]
    fn deterministic_randomized_round_trips_are_canonical() {
        let mut seed = 0x4d55_4843_4454_4f31u64;
        for _ in 0..400 {
            let count = (next(&mut seed) % 12 + 1) as u16;
            let mut fields = Vec::new();
            for id in 1..=count {
                let mut value = Vec::new();
                for _ in 0..(next(&mut seed) % 96) {
                    value.push(next(&mut seed) as u8);
                }
                fields.push(Field::bytes(id, value));
            }
            let first = encode(&Record::new(Kind::Creature, fields).unwrap()).unwrap();
            assert_eq!(encode(&decode(&first).unwrap()).unwrap(), first);
        }
    }

    #[test]
    fn sha256_matches_a_standard_vector() {
        assert_eq!(
            sha256(b"abc"),
            [
                0xba, 0x78, 0x16, 0xbf, 0x8f, 0x01, 0xcf, 0xea, 0x41, 0x41, 0x40, 0xde, 0x5d, 0xae,
                0x22, 0x23, 0xb0, 0x03, 0x61, 0xa3, 0x96, 0x17, 0x7a, 0x9c, 0xb4, 0x10, 0xff, 0x61,
                0xf2, 0x00, 0x15, 0xad
            ]
        );
    }

    fn record_wire(kind: Kind, fields: &[(u16, u8, Vec<u8>)]) -> Vec<u8> {
        let payload_length: usize = fields
            .iter()
            .map(|(_, _, value)| FIELD_HEADER_LENGTH + value.len())
            .sum();
        let mut wire = Vec::new();
        wire.extend_from_slice(&MAGIC);
        wire.extend_from_slice(&WIRE_VERSION.to_be_bytes());
        wire.extend_from_slice(&kind.wire_value().to_be_bytes());
        wire.extend_from_slice(&(payload_length as u32).to_be_bytes());
        for (id, tag, value) in fields {
            wire.extend_from_slice(&id.to_be_bytes());
            wire.push(*tag);
            wire.extend_from_slice(&(value.len() as u32).to_be_bytes());
            wire.extend_from_slice(value);
        }
        wire.extend_from_slice(&sha256(&wire[PREFIX_LENGTH..]));
        wire
    }

    fn decode_hex(input: &str) -> Vec<u8> {
        fn nibble(value: u8) -> u8 {
            match value {
                b'0'..=b'9' => value - b'0',
                b'a'..=b'f' => value - b'a' + 10,
                _ => panic!("test vector must be lowercase hexadecimal"),
            }
        }

        assert_eq!(input.len() % 2, 0);
        let (pairs, remainder) = input.as_bytes().as_chunks::<2>();
        assert!(remainder.is_empty());
        pairs
            .iter()
            .map(|pair| nibble(pair[0]) << 4 | nibble(pair[1]))
            .collect()
    }

    fn object_fixed<const N: usize>(prefix: &[u8]) -> [u8; N] {
        let mut value = [0; N];
        value[..prefix.len()].copy_from_slice(prefix);
        value
    }

    fn creature_fixture() -> CreatureV1 {
        CreatureV1 {
            name: object_fixed(b"synthetic-ranger"),
            description: object_fixed(b"Synthetic clone fixture."),
            key: [
                object_fixed(b"ranger"),
                object_fixed(b"synthetic"),
                object_fixed(b"test"),
            ],
            level: 255,
            type_code: -2,
            class: 3,
            race: 4,
            numwander: -1,
            alignment: -123,
            strength: 18,
            dexterity: 17,
            constitution: 16,
            intelligence: 15,
            piety: 14,
            hp_max: 123,
            hp_current: 99,
            mp_max: 77,
            mp_current: 66,
            armor: -4,
            thaco: 12,
            experience: 0x0001_0203_0405,
            gold: -0x0102_0304,
            ndice: 2,
            sdice: 7,
            pdice: -1,
            special: 44,
            proficiency: [-200, -99, 2, 103, 204],
            realm: [-110, -21, 68, 157],
            spells: std::array::from_fn(|index| (index as i8 - 8) as u8),
            flags: std::array::from_fn(|index| 0xa0 + index as u8),
            quests: std::array::from_fn(|index| 0x30 + index as u8),
            quest_num: 9,
            carry: std::array::from_fn(|index| index as i16 - 5),
            room_number: 31,
            daily: std::array::from_fn(|index| DailyV1 {
                max: index as u8 + 3,
                current: index as u8 + 1,
                last_used: 1000 + index as i64,
            }),
        }
    }

    fn next(seed: &mut u64) -> u64 {
        *seed ^= *seed << 13;
        *seed ^= *seed >> 7;
        *seed ^= *seed << 17;
        *seed
    }
}
