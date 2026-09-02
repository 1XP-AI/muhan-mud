//! Closed, pointer-free player persistence envelope (wire kind 7).
use super::{
    array, daily_bytes, decode_object_graph_v1, encode, encode_object_graph_v1,
    fixed_string_is_canonical, i16_array_bytes, i64_array_bytes, read_daily, read_i16_array,
    read_i64_array, validate_object_graph_nodes, Error, Field, Kind, ObjectGraphV1, TYPE_BYTES,
    TYPE_I16, TYPE_I64, TYPE_I8, TYPE_U8,
};

pub const MAX_LIST_ITEMS: usize = 4096;

fn player_fixed_string_is_canonical(value: &[u8]) -> bool {
    value.contains(&0) && fixed_string_is_canonical(value)
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct LastTimeV1 {
    pub interval: i64,
    pub last_used: i64,
    pub misc: i16,
}

fn lasttime_bytes(values: &[LastTimeV1; 45]) -> Vec<u8> {
    let mut out = Vec::with_capacity(810);
    for value in values {
        out.extend_from_slice(&value.interval.to_be_bytes());
        out.extend_from_slice(&value.last_used.to_be_bytes());
        out.extend_from_slice(&value.misc.to_be_bytes());
    }
    out
}

fn read_lasttime(value: &[u8]) -> [LastTimeV1; 45] {
    std::array::from_fn(|index| LastTimeV1 {
        interval: i64::from_be_bytes(array(&value[index * 18..index * 18 + 8])),
        last_used: i64::from_be_bytes(array(&value[index * 18 + 8..index * 18 + 16])),
        misc: i16::from_be_bytes(array(&value[index * 18 + 16..index * 18 + 18])),
    })
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PlayerSnapshotV1 {
    pub name: [u8; 80],
    pub description: [u8; 80],
    pub talk: [u8; 80],
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
    pub daily: [super::DailyV1; 10],
    pub lasttime: [LastTimeV1; 45],
    pub inventory: ObjectGraphV1,
}
fn valid(v: &PlayerSnapshotV1) -> bool {
    player_fixed_string_is_canonical(&v.name)
        && player_fixed_string_is_canonical(&v.description)
        && player_fixed_string_is_canonical(&v.talk)
        && v.key.iter().all(|x| player_fixed_string_is_canonical(x))
        && v.type_code == 0
        && v.hp_current <= v.hp_max
        && v.mp_current <= v.mp_max
}

fn validate_inventory_profile(nodes: &[super::ObjectGraphNodeV1]) -> Result<(), Error> {
    let mut roots = 0usize;
    let mut child_counts = vec![0usize; nodes.len()];
    for node in nodes {
        let object = &node.object;
        if !player_fixed_string_is_canonical(&object.name)
            || !player_fixed_string_is_canonical(&object.description)
            || !object
                .key
                .iter()
                .all(|key| player_fixed_string_is_canonical(key))
            || !player_fixed_string_is_canonical(&object.use_output)
        {
            return Err(Error::InvalidFieldLength { field_id: 40 });
        }
        let count = match node.parent_index {
            None => &mut roots,
            Some(parent) => &mut child_counts[parent as usize],
        };
        *count += 1;
        if *count > MAX_LIST_ITEMS {
            return Err(Error::SizeLimitExceeded {
                limit: MAX_LIST_ITEMS,
            });
        }
    }
    Ok(())
}
pub fn encode_player_snapshot_v1(v: &PlayerSnapshotV1) -> Result<Vec<u8>, Error> {
    validate_object_graph_nodes(&v.inventory.nodes)?;
    validate_inventory_profile(&v.inventory.nodes)?;
    if !valid(v) {
        return Err(Error::InvalidFieldLength { field_id: 0 });
    }
    let f = vec![
        Field::bytes(1, v.name.to_vec()),
        Field::bytes(2, v.description.to_vec()),
        Field::bytes(3, v.talk.to_vec()),
        Field::bytes(4, v.key[0].to_vec()),
        Field::bytes(5, v.key[1].to_vec()),
        Field::bytes(6, v.key[2].to_vec()),
        Field::u8(7, v.level),
        Field::i8(8, v.type_code),
        Field::i8(9, v.class),
        Field::i8(10, v.race),
        Field::i8(11, v.numwander),
        Field::i16(12, v.alignment),
        Field::i8(13, v.strength),
        Field::i8(14, v.dexterity),
        Field::i8(15, v.constitution),
        Field::i8(16, v.intelligence),
        Field::i8(17, v.piety),
        Field::i16(18, v.hp_max),
        Field::i16(19, v.hp_current),
        Field::i16(20, v.mp_max),
        Field::i16(21, v.mp_current),
        Field::i8(22, v.armor),
        Field::i8(23, v.thaco),
        Field::i64(24, v.experience),
        Field::i64(25, v.gold),
        Field::i16(26, v.ndice),
        Field::i16(27, v.sdice),
        Field::i16(28, v.pdice),
        Field::i16(29, v.special),
        Field::bytes(30, i64_array_bytes(&v.proficiency)),
        Field::bytes(31, i64_array_bytes(&v.realm)),
        Field::bytes(32, v.spells.to_vec()),
        Field::bytes(33, v.flags.to_vec()),
        Field::bytes(34, v.quests.to_vec()),
        Field::i8(35, v.quest_num),
        Field::bytes(36, i16_array_bytes(&v.carry)),
        Field::i16(37, v.room_number),
        Field::bytes(38, daily_bytes(&v.daily)),
        Field::bytes(39, lasttime_bytes(&v.lasttime)),
        Field::bytes(40, encode_object_graph_v1(&v.inventory)?),
    ];
    encode(&super::Record::new(Kind::PlayerSnapshot, f)?)
}
pub fn decode_player_snapshot_v1(w: &[u8]) -> Result<PlayerSnapshotV1, Error> {
    let r = super::decode(w)?;
    if r.kind() != Kind::PlayerSnapshot || r.fields().len() != 40 {
        return Err(Error::InvalidFieldLength { field_id: 0 });
    }
    let expected = [
        (TYPE_BYTES, 80),
        (TYPE_BYTES, 80),
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
        (TYPE_BYTES, 810),
    ];
    for (i, (tag, len)) in expected.iter().enumerate() {
        let x = &r.fields()[i];
        if x.id() != (i + 1) as u16 || x.type_tag() != *tag || x.value().len() != *len {
            return Err(Error::InvalidFieldLength {
                field_id: (i + 1) as u16,
            });
        }
    }
    let graph_field = &r.fields()[39];
    if graph_field.id() != 40
        || graph_field.type_tag() != TYPE_BYTES
        || graph_field.value().is_empty()
    {
        return Err(Error::InvalidFieldLength { field_id: 40 });
    }
    let b = |i: usize| r.fields()[i].value();
    let inventory = decode_object_graph_v1(b(39))?;
    validate_inventory_profile(&inventory.nodes)?;
    if encode_object_graph_v1(&inventory)? != b(39) {
        return Err(Error::InvalidFieldLength { field_id: 40 });
    }
    let v = PlayerSnapshotV1 {
        name: array(b(0)),
        description: array(b(1)),
        talk: array(b(2)),
        key: [array(b(3)), array(b(4)), array(b(5))],
        level: b(6)[0],
        type_code: b(7)[0] as i8,
        class: b(8)[0] as i8,
        race: b(9)[0] as i8,
        numwander: b(10)[0] as i8,
        alignment: i16::from_be_bytes(array(b(11))),
        strength: b(12)[0] as i8,
        dexterity: b(13)[0] as i8,
        constitution: b(14)[0] as i8,
        intelligence: b(15)[0] as i8,
        piety: b(16)[0] as i8,
        hp_max: i16::from_be_bytes(array(b(17))),
        hp_current: i16::from_be_bytes(array(b(18))),
        mp_max: i16::from_be_bytes(array(b(19))),
        mp_current: i16::from_be_bytes(array(b(20))),
        armor: b(21)[0] as i8,
        thaco: b(22)[0] as i8,
        experience: i64::from_be_bytes(array(b(23))),
        gold: i64::from_be_bytes(array(b(24))),
        ndice: i16::from_be_bytes(array(b(25))),
        sdice: i16::from_be_bytes(array(b(26))),
        pdice: i16::from_be_bytes(array(b(27))),
        special: i16::from_be_bytes(array(b(28))),
        proficiency: read_i64_array(b(29)),
        realm: read_i64_array(b(30)),
        spells: array(b(31)),
        flags: array(b(32)),
        quests: array(b(33)),
        quest_num: b(34)[0] as i8,
        carry: read_i16_array(b(35)),
        room_number: i16::from_be_bytes(array(b(36))),
        daily: read_daily(b(37)),
        lasttime: read_lasttime(b(38)),
        inventory,
    };
    if !valid(&v) {
        return Err(Error::InvalidFieldLength { field_id: 0 });
    }
    Ok(v)
}
