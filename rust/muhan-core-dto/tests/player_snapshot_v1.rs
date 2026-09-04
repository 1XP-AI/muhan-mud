use muhan_core_dto::player_snapshot_v1::{
    decode_player_snapshot_v1, encode_player_snapshot_v1, LastTimeV1, PlayerSnapshotV1,
};
use muhan_core_dto::{
    decode, encode, DailyV1, Field, Kind, ObjectGraphNodeV1, ObjectGraphV1, ObjectV1, Record,
    TYPE_U8,
};

fn fixture() -> PlayerSnapshotV1 {
    PlayerSnapshotV1 {
        name: {
            let mut x = [0; 80];
            x[..4].copy_from_slice(b"MUD1");
            x
        },
        description: [0; 80],
        talk: [0; 80],
        key: [[0; 20]; 3],
        level: 7,
        type_code: 0,
        class: 2,
        race: 3,
        numwander: 4,
        alignment: -5,
        strength: 10,
        dexterity: 11,
        constitution: 12,
        intelligence: 13,
        piety: 14,
        hp_max: 100,
        hp_current: 99,
        mp_max: 80,
        mp_current: 79,
        armor: -2,
        thaco: 12,
        experience: 1234,
        gold: -55,
        ndice: 1,
        sdice: 2,
        pdice: 3,
        special: 4,
        proficiency: [1, 2, 3, 4, 5],
        realm: [6, 7, 8, 9],
        spells: [10; 16],
        flags: [11; 8],
        quests: [12; 16],
        quest_num: 2,
        carry: [13; 10],
        room_number: 42,
        daily: [DailyV1 {
            max: 1,
            current: 2,
            last_used: 77,
        }; 10],
        lasttime: std::array::from_fn(|index| LastTimeV1 {
            interval: if index % 4 == 0 {
                i64::MIN
            } else if index % 4 == 1 {
                i64::MAX
            } else if index % 4 == 2 {
                -(index as i64)
            } else {
                index as i64
            },
            last_used: if index % 4 == 0 {
                i64::MAX
            } else if index % 4 == 1 {
                i64::MIN
            } else if index % 4 == 2 {
                index as i64
            } else {
                -(index as i64)
            },
            misc: if index % 2 == 0 { i16::MIN } else { i16::MAX },
        }),
        inventory: ObjectGraphV1 { nodes: Vec::new() },
    }
}

fn canonical_c_fixture() -> Vec<u8> {
    fixture_hex(include_str!(
        "../../../tests/fixtures/player_snapshot_v1_canonical.hex"
    ))
}

fn canonical_c_inventory_fixture() -> Vec<u8> {
    fixture_hex(include_str!(
        "../../../tests/fixtures/player_snapshot_v1_one_inventory_item.hex"
    ))
}

fn canonical_c_tree_inventory_fixture() -> Vec<u8> {
    fixture_hex(include_str!(
        "../../../tests/fixtures/player_snapshot_v1_tree_inventory.hex"
    ))
}

fn fixture_hex(source: &str) -> Vec<u8> {
    let text = source.trim();
    assert_eq!(text.len() % 2, 0, "fixture has whole hex octets");
    (0..text.len())
        .step_by(2)
        .map(|index| u8::from_str_radix(&text[index..index + 2], 16).expect("fixture hex"))
        .collect()
}

#[test]
fn canonical_c_fixture_rereads_to_identical_rust_cdto_bytes() {
    let wire = canonical_c_fixture();
    let decoded = decode_player_snapshot_v1(&wire).expect("C fixture decodes in Rust");
    assert_eq!(&decoded.name[..8], b"Pvahero\0");
    assert_eq!(decoded.type_code, 0);
    assert!(decoded.inventory.nodes.is_empty());
    assert_eq!(
        encode_player_snapshot_v1(&decoded).expect("C fixture reencodes in Rust"),
        wire,
        "Rust must preserve every canonical byte emitted by C"
    );
}

#[test]
fn canonical_c_inventory_fixture_rereads_to_identical_rust_cdto_bytes() {
    let wire = canonical_c_inventory_fixture();
    let decoded = decode_player_snapshot_v1(&wire).expect("C inventory fixture decodes in Rust");
    assert_eq!(decoded.inventory.nodes.len(), 1);
    assert_eq!(decoded.inventory.nodes[0].parent_index, None);
    assert_eq!(
        encode_player_snapshot_v1(&decoded).expect("C inventory fixture reencodes in Rust"),
        wire,
        "Rust must preserve C's canonical inventory graph bytes"
    );
}

#[test]
fn canonical_c_tree_inventory_fixture_rereads_to_identical_rust_cdto_bytes() {
    let wire = canonical_c_tree_inventory_fixture();
    let decoded = decode_player_snapshot_v1(&wire).expect("C tree fixture decodes in Rust");
    let topology: Vec<(Option<u32>, u32)> = decoded
        .inventory
        .nodes
        .iter()
        .map(|node| (node.parent_index, node.child_index))
        .collect();
    assert_eq!(
        topology,
        vec![
            (None, 0),
            (Some(0), 0),
            (Some(1), 0),
            (Some(0), 1),
            (None, 1)
        ],
        "C preorder parent/sibling topology survives Rust decoding"
    );
    assert_eq!(
        encode_player_snapshot_v1(&decoded).expect("C tree fixture reencodes in Rust"),
        wire,
        "Rust must preserve C's canonical tree graph bytes"
    );
}

#[test]
fn snapshot_round_trip_preserves_all_safe_fields_and_allows_daily_overmax() {
    let input = fixture();
    let wire = encode_player_snapshot_v1(&input).expect("snapshot encodes");
    let output = decode_player_snapshot_v1(&wire).expect("snapshot decodes");
    assert_eq!(input, output);
}

#[test]
fn snapshot_level_is_canonical_raw_u8_across_projection_bounds() {
    for level in [0, 42, 255] {
        let mut input = fixture();
        input.level = level;
        let wire = encode_player_snapshot_v1(&input).expect("level fixture encodes");
        let envelope = decode(&wire).expect("level fixture has a CDTO envelope");
        let field = &envelope.fields()[6];
        assert_eq!((field.id(), field.type_tag()), (7, TYPE_U8));
        assert_eq!(field.value(), &[level]);

        let decoded = decode_player_snapshot_v1(&wire).expect("level fixture decodes");
        assert_eq!(decoded.level, level);
        assert_eq!(
            encode_player_snapshot_v1(&decoded).expect("level fixture reencodes"),
            wire,
            "field 7 must remain byte-stable for raw U8 level {level}"
        );
    }
}

#[test]
fn snapshot_rejects_hp_overmax_and_noncanonical_fixed_tail() {
    let mut input = fixture();
    input.hp_current = 101;
    assert!(encode_player_snapshot_v1(&input).is_err());
    let mut input = fixture();
    input.name[5] = 1;
    assert!(encode_player_snapshot_v1(&input).is_err());
    let mut input = fixture();
    input.name = [b'x'; 80];
    assert!(encode_player_snapshot_v1(&input).is_err());
}

#[test]
fn snapshot_rejects_non_player_type() {
    let mut input = fixture();
    input.type_code = -1;
    assert!(encode_player_snapshot_v1(&input).is_err());
    let valid = decode(&encode_player_snapshot_v1(&fixture()).unwrap()).unwrap();
    let mut fields = valid.fields().to_vec();
    fields[7] = Field::i8(8, -1);
    let malformed = encode(&Record::new(Kind::PlayerSnapshot, fields).unwrap()).unwrap();
    assert!(decode_player_snapshot_v1(&malformed).is_err());
}

#[test]
fn snapshot_rejects_level_type_length_and_envelope_changes() {
    let valid = decode(&encode_player_snapshot_v1(&fixture()).unwrap()).unwrap();

    let mut fields = valid.fields().to_vec();
    fields[6] = Field::i8(7, 42);
    let wrong_type = encode(&Record::new(Kind::PlayerSnapshot, fields).unwrap()).unwrap();
    assert!(decode_player_snapshot_v1(&wrong_type).is_err());

    let mut fields = valid.fields().to_vec();
    fields[6] = Field::raw(7, TYPE_U8, Vec::new());
    assert!(Record::new(Kind::PlayerSnapshot, fields).is_err());

    let wrong_envelope =
        encode(&Record::new(Kind::Session, valid.fields().to_vec()).unwrap()).unwrap();
    assert!(decode_player_snapshot_v1(&wrong_envelope).is_err());
}

#[test]
fn snapshot_rejects_inventory_field_metadata_changes() {
    let valid = decode(&encode_player_snapshot_v1(&fixture()).unwrap()).unwrap();
    let mut fields = valid.fields().to_vec();
    let graph = fields[39].value().to_vec();
    fields[39] = Field::bytes(41, graph.clone());
    let wrong_id = encode(&Record::new(Kind::PlayerSnapshot, fields).unwrap()).unwrap();
    assert!(decode_player_snapshot_v1(&wrong_id).is_err());

    let mut fields = valid.fields().to_vec();
    fields[39] = Field::optional_bytes(40, graph);
    let wrong_type = encode(&Record::new(Kind::PlayerSnapshot, fields).unwrap()).unwrap();
    assert!(decode_player_snapshot_v1(&wrong_type).is_err());
}

#[test]
fn snapshot_roundtrips_nonempty_inventory_and_legacy_max_roots() {
    let mut input = fixture();
    let object = ObjectV1 {
        name: [0; 80],
        description: [0; 80],
        key: [[0; 20]; 3],
        use_output: [0; 80],
        value: 1,
        weight: 1,
        type_code: 1,
        adjustment: 0,
        shots_max: 1,
        shots_current: 1,
        ndice: 1,
        sdice: 1,
        pdice: 1,
        armor: 0,
        wear_flag: 0,
        magic_power: 0,
        magic_realm: 0,
        special: 0,
        flags: [0; 8],
        quest_num: 0,
    };
    input.inventory.nodes.push(ObjectGraphNodeV1 {
        object: object.clone(),
        parent_index: None,
        child_index: 0,
    });
    assert_eq!(
        decode_player_snapshot_v1(&encode_player_snapshot_v1(&input).unwrap()).unwrap(),
        input
    );
    input.inventory.nodes[0].object.name = [b'x'; 80];
    assert!(encode_player_snapshot_v1(&input).is_err());
    input.inventory.nodes[0].object.name = [0; 80];
    input.inventory.nodes = (0..4096)
        .map(|index| ObjectGraphNodeV1 {
            object: object.clone(),
            parent_index: None,
            child_index: index,
        })
        .collect();
    let wire = encode_player_snapshot_v1(&input).expect("4096 legacy roots must encode");
    assert_eq!(
        decode_player_snapshot_v1(&wire).expect("4096 legacy roots must decode"),
        input
    );
    input.inventory.nodes.push(ObjectGraphNodeV1 {
        object: object.clone(),
        parent_index: None,
        child_index: 4096,
    });
    assert!(encode_player_snapshot_v1(&input).is_err());

    let mut fields = decode(&encode_player_snapshot_v1(&fixture()).unwrap())
        .unwrap()
        .fields()
        .to_vec();
    fields[39] = Field::bytes(
        40,
        muhan_core_dto::encode_object_graph_v1(&input.inventory).unwrap(),
    );
    let malformed = encode(&Record::new(Kind::PlayerSnapshot, fields).unwrap()).unwrap();
    assert!(decode_player_snapshot_v1(&malformed).is_err());

    input.inventory.nodes = std::iter::once(ObjectGraphNodeV1 {
        object: object.clone(),
        parent_index: None,
        child_index: 0,
    })
    .chain((0..4096).map(|index| ObjectGraphNodeV1 {
        object: object.clone(),
        parent_index: Some(0),
        child_index: index,
    }))
    .collect();
    assert!(encode_player_snapshot_v1(&input).is_ok());
    input.inventory.nodes.push(ObjectGraphNodeV1 {
        object,
        parent_index: Some(0),
        child_index: 4096,
    });
    assert!(encode_player_snapshot_v1(&input).is_err());
}
