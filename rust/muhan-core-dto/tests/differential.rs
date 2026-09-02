//! Process-level, clone-only C/Rust CDTO v1 differential corpus.
//!
//! `scripts/run-cdto-differential.sh` builds the C oracle under ASan/UBSan
//! and supplies its path.  Keeping the oracle opt-in lets ordinary workspace
//! tests stay hermetic while CI (or a developer) gets actual byte comparison.

use muhan_core_dto::player_snapshot_v1::{
    decode_player_snapshot_v1, encode_player_snapshot_v1, LastTimeV1, PlayerSnapshotV1,
};
use muhan_core_dto::{
    decode, decode_creature_v1, decode_object_graph_v1, decode_object_v1, encode,
    encode_creature_v1, encode_object_graph_v1, encode_object_v1, CreatureV1, DailyV1, Error,
    Field, Kind, ObjectGraphNodeV1, ObjectGraphV1, ObjectV1, Record, TYPE_BOOL,
};
use std::env;
use std::fs;
use std::path::{Path, PathBuf};
use std::process::Command;

const DEFAULT_SEED: u64 = 0x4d55_4843_4454_4f31;
const C_CREATURE_GOLDEN: &str = "4d55484344544f000001000100000017000109000000055465727261000203000000040000002abd42d992f5822c03c8cd5f3d1f5d59baf16d471022c81989109304a43cd57347";
const C_DUPLICATE_GOLDEN: &str = "4d55484344544f0000010004000000100001010000000101000101000000010256721d45f3407a216bb58edd8098250a5415e56ed4530401cb47c8709e7e3e13";

fn oracle() -> Option<PathBuf> {
    env::var_os("CDTO_V1_C_ORACLE").map(PathBuf::from)
}

fn run(oracle: &Path, args: &[String]) -> String {
    let output = Command::new(oracle)
        .args(args)
        .output()
        .expect("C DTO oracle must launch");
    assert!(
        output.status.success(),
        "C DTO oracle rejected its test command"
    );
    String::from_utf8(output.stdout)
        .expect("C DTO oracle output is ASCII")
        .trim()
        .to_owned()
}

fn hex(input: &[u8]) -> String {
    const DIGITS: &[u8; 16] = b"0123456789abcdef";
    let mut output = String::with_capacity(input.len() * 2);
    for &byte in input {
        output.push(DIGITS[(byte >> 4) as usize] as char);
        output.push(DIGITS[(byte & 15) as usize] as char);
    }
    output
}

fn decode_hex(input: &str) -> Vec<u8> {
    assert_eq!(input.len() % 2, 0);
    input
        .as_bytes()
        .as_chunks::<2>()
        .0
        .iter()
        .map(|pair| (digit(pair[0]) << 4) | digit(pair[1]))
        .collect()
}

fn digit(value: u8) -> u8 {
    match value {
        b'0'..=b'9' => value - b'0',
        b'a'..=b'f' => value - b'a' + 10,
        _ => panic!("fixture contains non-hex data"),
    }
}

fn next(seed: &mut u64) -> u64 {
    *seed ^= *seed << 7;
    *seed ^= *seed >> 9;
    *seed ^= *seed << 8;
    *seed
}

fn kind(value: u16) -> Kind {
    match value {
        1 => Kind::Creature,
        2 => Kind::Object,
        3 => Kind::Room,
        4 => Kind::Session,
        5 => Kind::AbiFingerprint,
        _ => unreachable!(),
    }
}

fn parse_seed(input: &str) -> Result<u64, &'static str> {
    if input.len() != 16 || !input.bytes().all(|value| value.is_ascii_hexdigit()) {
        return Err("must be exactly 16 hexadecimal digits");
    }
    u64::from_str_radix(input, 16).map_err(|_| "must fit in u64")
}

fn configured_seed() -> u64 {
    match env::var("CDTO_V1_DIFF_SEED") {
        Ok(value) => parse_seed(&value)
            .unwrap_or_else(|reason| panic!("invalid CDTO_V1_DIFF_SEED: {reason}")),
        Err(env::VarError::NotPresent) => DEFAULT_SEED,
        Err(env::VarError::NotUnicode(_)) => {
            panic!("invalid CDTO_V1_DIFF_SEED: must be ASCII hexadecimal")
        }
    }
}

fn write_manifest(path: &Path, seed: u64) {
    fs::create_dir_all(path).expect("differential artifact directory must be writable");
    fs::write(
        path.join("cdto-v1-differential-manifest.txt"),
        format!(
            "seed={seed:016x}\n\
             corpus=synthetic-canonical-bytes-only\n\
             cases=64\n\
             player-snapshot-cases=64\n\
             boundaries=max-fields,one-mib,one-mib-plus,count-overflow,player-roots-4096,player-list-4096,player-depth-64,player-nodes-8192\n\
             malformed=unknown-kind,truncated,duplicate,length-overflow,one-mib-plus,player-schema\n"
        ),
    )
    .expect("differential artifact manifest must be writable");
}

fn assert_wire_equal(artifact_dir: &Path, label: &str, c_wire: &[u8], rust_wire: &[u8]) {
    if c_wire != rust_wire {
        fs::write(artifact_dir.join(format!("{label}-c.hex")), hex(c_wire))
            .expect("C failure artifact must be writable");
        fs::write(
            artifact_dir.join(format!("{label}-rust.hex")),
            hex(rust_wire),
        )
        .expect("Rust failure artifact must be writable");
        panic!("C/Rust CDTO wire mismatch for {label}; synthetic artifacts were retained");
    }
}

fn status(oracle: &Path, wire: &[u8]) -> String {
    run(oracle, &["decode".into(), hex(wire)])
}

#[test]
fn c_and_rust_match_the_fixed_seed_differential_corpus() {
    let Some(oracle) = oracle() else {
        eprintln!(
            "skipping external CDTO differential oracle; run scripts/run-cdto-differential.sh"
        );
        return;
    };
    let seed = configured_seed();
    let artifact_dir = env::var_os("CDTO_V1_DIFF_ARTIFACT_DIR")
        .map(PathBuf::from)
        .unwrap_or_else(|| env::temp_dir().join("muhan-cdto-differential-artifacts"));
    write_manifest(&artifact_dir, seed);

    let golden = decode_hex(C_CREATURE_GOLDEN);
    let golden_record = decode(&golden).expect("C golden must decode in Rust");
    assert_eq!(
        encode(&golden_record).unwrap(),
        golden,
        "C golden must round-trip in Rust"
    );
    assert_eq!(status(&oracle, &golden), "0", "C must accept its golden");

    let abi = decode_hex(&run(&oracle, &["abi".into()]));
    let abi_record = decode(&abi).expect("C ABI fingerprint must decode in Rust");
    assert_eq!(
        abi_record.kind(),
        Kind::AbiFingerprint,
        "kind 5 is ABI fingerprint"
    );
    assert_eq!(
        encode(&abi_record).unwrap(),
        abi,
        "kind 5 must remain byte-stable"
    );

    let mixed = Record::new(
        Kind::Creature,
        vec![
            Field::bytes(1, Vec::new()),
            Field::raw(2, 4, 42u64.to_be_bytes().to_vec()),
            Field::raw(3, TYPE_BOOL, vec![1]),
            Field::optional_raw(4, 0x61, vec![0x80, 0x01]),
        ],
    )
    .unwrap();
    compare_encoded(&oracle, &artifact_dir, "mixed", &mixed);

    let mut generator_seed = seed;
    for case in 0..64 {
        let count = (next(&mut generator_seed) % 12 + 1) as u16;
        let mut fields = Vec::new();
        for id in 1..=count {
            let length = (next(&mut generator_seed) % 97) as usize;
            let value = (0..length)
                .map(|_| next(&mut generator_seed) as u8)
                .collect();
            fields.push(Field::bytes(id, value));
        }
        let record = Record::new(kind((case % 5 + 1) as u16), fields).unwrap();
        compare_encoded(
            &oracle,
            &artifact_dir,
            &format!("seed-{seed:016x}-{case:02}"),
            &record,
        );
    }

    assert_boundaries(&oracle, &artifact_dir);
    assert_malformed_rejections(&oracle);
    assert_object_v1(&oracle);
    assert_object_graph_v1(&oracle);
    assert_creature_v1(&oracle);
    assert_player_snapshot_v1(&oracle, &artifact_dir, seed);
}

#[test]
fn configured_seed_format_is_strict_and_reproducible() {
    assert_eq!(parse_seed("4d55484344544f31"), Ok(DEFAULT_SEED));
    assert_eq!(parse_seed("4D55484344544F31"), Ok(DEFAULT_SEED));
    for invalid in [
        "",
        "4d55484344544f3",
        "4d55484344544f311",
        "not-a-seed-00000",
        "4d55484344544f3g",
    ] {
        assert!(
            parse_seed(invalid).is_err(),
            "invalid seed accepted: {invalid}"
        );
    }
}

fn compare_encoded(oracle: &Path, artifact_dir: &Path, label: &str, record: &Record) {
    let rust_wire = encode(record).expect("synthetic canonical record must encode");
    let mut args = vec!["encode".into(), (record.kind() as u16).to_string()];
    for field in record.fields() {
        args.push(format!(
            "{}:{}:{}",
            field.id(),
            field.type_tag(),
            hex(field.value())
        ));
    }
    let c_wire = decode_hex(&run(oracle, &args));
    assert_wire_equal(artifact_dir, label, &c_wire, &rust_wire);
    assert_eq!(
        decode(&c_wire).unwrap(),
        record.clone(),
        "C wire must decode to the Rust record"
    );
    assert_eq!(
        status(oracle, &rust_wire),
        "0",
        "C must decode Rust canonical wire"
    );
}

fn assert_boundaries(oracle: &Path, artifact_dir: &Path) {
    let max_fields_path = artifact_dir.join("max-fields.cdto");
    assert_eq!(
        run(
            oracle,
            &[
                "boundary".into(),
                "max-fields".into(),
                max_fields_path.display().to_string(),
            ],
        ),
        "ok 458800"
    );
    let max_fields = Record::new(
        Kind::Session,
        (0..=u16::MAX)
            .map(|id| Field::bytes(id, Vec::new()))
            .collect(),
    )
    .unwrap();
    assert_wire_equal(
        artifact_dir,
        "max-fields",
        &fs::read(&max_fields_path).unwrap(),
        &encode(&max_fields).unwrap(),
    );

    let one_mib_path = artifact_dir.join("one-mib.cdto");
    assert_eq!(
        run(
            oracle,
            &[
                "boundary".into(),
                "one-mib".into(),
                one_mib_path.display().to_string(),
            ],
        ),
        "ok 1048624"
    );
    let value: Vec<u8> = (0..(Kind::Session.payload_limit() - 7))
        .map(|index| (index as u8).wrapping_mul(37).wrapping_add(11))
        .collect();
    let one_mib = Record::new(Kind::Session, vec![Field::bytes(1, value)]).unwrap();
    assert_wire_equal(
        artifact_dir,
        "one-mib",
        &fs::read(&one_mib_path).unwrap(),
        &encode(&one_mib).unwrap(),
    );

    assert_eq!(
        run(oracle, &["boundary".into(), "one-mib-plus".into()]),
        "err -6"
    );
    let too_large = Record::new(
        Kind::Session,
        vec![Field::bytes(1, vec![0; Kind::Session.payload_limit()])],
    )
    .unwrap();
    assert!(matches!(
        encode(&too_large),
        Err(Error::SizeLimitExceeded { .. })
    ));

    /* C calls a count above the u16-ID-space cap invalid caller input (-1),
     * while Rust calls that same pre-allocation rejection a size limit.  This
     * test locks the shared fail-closed behavior, not an error-number ABI. */
    assert_eq!(
        run(oracle, &["boundary".into(), "count-overflow".into()]),
        "err -1"
    );
    let mut too_many: Vec<_> = (0..=u16::MAX)
        .map(|id| Field::bytes(id, Vec::new()))
        .collect();
    too_many.push(Field::bytes(u16::MAX, Vec::new()));
    assert!(matches!(
        Record::new(Kind::Session, too_many),
        Err(Error::SizeLimitExceeded { .. })
    ));
}

fn assert_malformed_rejections(oracle: &Path) {
    let golden = decode_hex(C_CREATURE_GOLDEN);
    assert_eq!(status(oracle, &golden[..golden.len() - 1]), "-7");
    assert!(matches!(
        decode(&golden[..golden.len() - 1]),
        Err(Error::Truncated { .. })
    ));

    let unknown_kind = decode_hex("4d55484344544f000001ffff00000000");
    assert_eq!(status(oracle, &unknown_kind), "-5");
    assert!(matches!(
        decode(&unknown_kind),
        Err(Error::UnknownKind { kind: 65535 })
    ));

    let duplicate = decode_hex(C_DUPLICATE_GOLDEN);
    assert_eq!(status(oracle, &duplicate), "-10");
    assert!(matches!(
        decode(&duplicate),
        Err(Error::DuplicateField { field_id: 1 })
    ));

    let length_overflow = decode_hex("4d55484344544f0000010004ffffffff");
    assert_eq!(status(oracle, &length_overflow), "-6");
    assert!(matches!(
        decode(&length_overflow),
        Err(Error::SizeLimitExceeded { .. })
    ));

    let one_mib_plus = decode_hex("4d55484344544f000001000400100001");
    assert_eq!(status(oracle, &one_mib_plus), "-6");
    assert!(matches!(
        decode(&one_mib_plus),
        Err(Error::SizeLimitExceeded { .. })
    ));
}

fn assert_object_v1(oracle: &Path) {
    let wire = decode_hex(&run(oracle, &["object-fixture".into()]));
    let expected = ObjectV1 {
        name: fixed(b"bronze-key"),
        description: fixed(b"A weathered bronze key."),
        key: [fixed(b"key"), fixed(b"bronze"), fixed(b"quest")],
        use_output: fixed(b"The key turns.\n"),
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
    assert_eq!(
        decode_object_v1(&wire).unwrap(),
        expected,
        "Rust must decode C ObjectV1 semantics"
    );
    assert_eq!(
        encode_object_v1(&expected).unwrap(),
        wire,
        "C/Rust ObjectV1 bytes must be identical"
    );
    assert_eq!(
        run(oracle, &["object-roundtrip".into(), hex(&wire)]),
        hex(&wire),
        "C ObjectV1 decode/re-encode must retain the Rust-compatible fixture"
    );

    let mut fields = decode(&wire).unwrap().fields().to_vec();
    fields.push(Field::optional_raw(23, 0x61, vec![1]));
    let unknown = encode(&Record::new(Kind::Object, fields).unwrap()).unwrap();
    assert!(
        matches!(
            decode_object_v1(&unknown),
            Err(Error::InvalidFieldLength { .. })
        ),
        "ObjectV1 must reject added optional fields rather than silently drifting"
    );
}

fn object_graph_object(name: &[u8], value: i64) -> ObjectV1 {
    ObjectV1 {
        name: fixed(name),
        description: fixed(b"A weathered bronze key."),
        key: [fixed(b"key"), fixed(b"bronze"), fixed(b"quest")],
        use_output: fixed(b"The key turns.\n"),
        value,
        weight: value as i16,
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
    }
}

fn assert_object_graph_v1(oracle: &Path) {
    let expected = ObjectGraphV1 {
        nodes: vec![
            ObjectGraphNodeV1 {
                object: object_graph_object(b"synthetic-bag", 11),
                parent_index: None,
                child_index: 0,
            },
            ObjectGraphNodeV1 {
                object: object_graph_object(b"synthetic-coin", 12),
                parent_index: Some(0),
                child_index: 0,
            },
            ObjectGraphNodeV1 {
                object: object_graph_object(b"synthetic-gem", 14),
                parent_index: Some(1),
                child_index: 0,
            },
            ObjectGraphNodeV1 {
                object: object_graph_object(b"synthetic-key", 13),
                parent_index: Some(0),
                child_index: 1,
            },
        ],
    };
    let wire = decode_hex(&run(oracle, &["object-graph-fixture".into()]));
    assert_eq!(
        decode_object_graph_v1(&wire).unwrap(),
        expected,
        "Rust must decode the C preorder graph semantics"
    );
    assert_eq!(
        encode_object_graph_v1(&expected).unwrap(),
        wire,
        "C/Rust ObjectGraphV1 bytes must be identical"
    );
    assert_eq!(
        run(oracle, &["object-graph-roundtrip".into(), hex(&wire)]),
        hex(&wire),
        "C graph import/export must retain Rust-compatible canonical bytes"
    );
    let two_roots = ObjectGraphV1 {
        nodes: vec![
            ObjectGraphNodeV1 {
                object: object_graph_object(b"synthetic-root-a", 21),
                parent_index: None,
                child_index: 0,
            },
            ObjectGraphNodeV1 {
                object: object_graph_object(b"synthetic-child-a", 22),
                parent_index: Some(0),
                child_index: 0,
            },
            ObjectGraphNodeV1 {
                object: object_graph_object(b"synthetic-root-b", 23),
                parent_index: None,
                child_index: 1,
            },
        ],
    };
    let two_root_wire = decode_hex(&run(oracle, &["object-graph-two-root-fixture".into()]));
    assert_eq!(decode_object_graph_v1(&two_root_wire).unwrap(), two_roots);
    assert_eq!(encode_object_graph_v1(&two_roots).unwrap(), two_root_wire);

    let mut fields = decode(&wire).unwrap().fields().to_vec();
    fields.push(Field::optional_raw(6, 0x61, vec![1]));
    let unknown = encode(&Record::new(Kind::ObjectGraph, fields).unwrap()).unwrap();
    assert_eq!(
        run(oracle, &["object-graph-decode".into(), hex(&unknown)]),
        "-13",
        "C must classify an unknown graph field as a closed schema violation"
    );
    assert!(matches!(
        decode_object_graph_v1(&unknown),
        Err(Error::InvalidFieldLength { .. })
    ));

    let mut malformed = decode(&wire).unwrap().fields().to_vec();
    let mut first_node = malformed[1].value().to_vec();
    first_node[4..8].copy_from_slice(&0u32.to_be_bytes());
    malformed[1] = Field::bytes(2, first_node);
    let malformed = encode(&Record::new(Kind::ObjectGraph, malformed).unwrap()).unwrap();
    assert_eq!(
        run(oracle, &["object-graph-decode".into(), hex(&malformed)]),
        "-13",
        "C and Rust must reject a root carrying a non-root parent identity"
    );
    assert!(matches!(
        decode_object_graph_v1(&malformed),
        Err(Error::InvalidFieldLength { .. })
    ));

    let mut closed_root = decode(&wire).unwrap().fields().to_vec();
    let mut node = closed_root[3].value().to_vec();
    node[4..8].copy_from_slice(&u32::MAX.to_be_bytes());
    node[8..12].copy_from_slice(&1u32.to_be_bytes());
    closed_root[3] = Field::bytes(4, node);
    let closed_root = encode(&Record::new(Kind::ObjectGraph, closed_root).unwrap()).unwrap();
    assert_eq!(
        run(oracle, &["object-graph-decode".into(), hex(&closed_root)]),
        "-13",
        "C must reject a late child re-entering an already closed root subtree"
    );
    assert!(matches!(
        decode_object_graph_v1(&closed_root),
        Err(Error::InvalidFieldLength { .. })
    ));

    let mut closed_sibling = decode(&wire).unwrap().fields().to_vec();
    let mut node = closed_sibling[3].value().to_vec();
    node[4..8].copy_from_slice(&0u32.to_be_bytes());
    node[8..12].copy_from_slice(&1u32.to_be_bytes());
    closed_sibling[3] = Field::bytes(4, node);
    let mut node = closed_sibling[4].value().to_vec();
    node[4..8].copy_from_slice(&1u32.to_be_bytes());
    node[8..12].copy_from_slice(&0u32.to_be_bytes());
    closed_sibling[4] = Field::bytes(5, node);
    let closed_sibling = encode(&Record::new(Kind::ObjectGraph, closed_sibling).unwrap()).unwrap();
    assert_eq!(
        run(
            oracle,
            &["object-graph-decode".into(), hex(&closed_sibling)]
        ),
        "-13",
        "C must reject a late grandchild re-entering an already closed sibling subtree"
    );
    assert!(matches!(
        decode_object_graph_v1(&closed_sibling),
        Err(Error::InvalidFieldLength { .. })
    ));

    let canonical = decode(&wire).unwrap();
    let mut depth_fields = vec![Field::u32(1, 65)];
    for index in 0..65u32 {
        let mut node = canonical.fields()[if index == 0 { 1 } else { 2 }]
            .value()
            .to_vec();
        node[0..4].copy_from_slice(&index.to_be_bytes());
        node[4..8].copy_from_slice(&(if index == 0 { u32::MAX } else { index - 1 }).to_be_bytes());
        node[8..12].copy_from_slice(&0u32.to_be_bytes());
        depth_fields.push(Field::bytes((index + 2) as u16, node));
    }
    let depth_65 = encode(&Record::new(Kind::ObjectGraph, depth_fields).unwrap()).unwrap();
    assert_eq!(
        run(oracle, &["object-graph-decode".into(), hex(&depth_65)]),
        "-6",
        "C must classify wire depth 65 as a graph size-limit violation"
    );
    assert!(matches!(
        decode_object_graph_v1(&depth_65),
        Err(Error::SizeLimitExceeded { limit: 64 })
    ));
    assert_eq!(
        run(
            oracle,
            &["object-graph-decode".into(), hex(&wire[..wire.len() - 1]),],
        ),
        "-7"
    );
    assert!(matches!(
        decode_object_graph_v1(&wire[..wire.len() - 1]),
        Err(Error::Truncated { .. })
    ));
}

fn assert_creature_v1(oracle: &Path) {
    let wire = decode_hex(&run(oracle, &["creature-fixture".into()]));
    let expected = CreatureV1 {
        name: fixed(b"synthetic-ranger"),
        description: fixed(b"Synthetic clone fixture."),
        key: [fixed(b"ranger"), fixed(b"synthetic"), fixed(b"test")],
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
    };
    assert_eq!(
        decode_creature_v1(&wire).unwrap(),
        expected,
        "Rust must decode C CreatureV1 safe semantics"
    );
    assert_eq!(
        encode_creature_v1(&expected).unwrap(),
        wire,
        "C/Rust CreatureV1 bytes must be identical"
    );
    assert_eq!(
        run(oracle, &["creature-roundtrip".into(), hex(&wire)]),
        hex(&wire),
        "C CreatureV1 decode/re-encode must retain Rust-compatible bytes"
    );
    let mut added = decode(&wire).unwrap().fields().to_vec();
    added.push(Field::optional_raw(38, 0x61, vec![1]));
    let added = encode(&Record::new(Kind::Creature, added).unwrap()).unwrap();
    assert!(matches!(
        decode_creature_v1(&added),
        Err(Error::InvalidFieldLength { .. })
    ));
}

fn seeded_player_object(seed: &mut u64, case: usize, index: usize) -> ObjectV1 {
    let mut value = object_graph_object(
        format!("snapshot-{case:02}-{index:02}").as_bytes(),
        next(seed) as i64,
    );
    value.weight = next(seed) as i16;
    value.type_code = next(seed) as i8;
    value.adjustment = next(seed) as i8;
    value.shots_max = (next(seed) % (i16::MAX as u64 + 1)) as i16;
    value.shots_current = (next(seed) % (value.shots_max as u64 + 1)) as i16;
    value.ndice = next(seed) as i16;
    value.sdice = next(seed) as i16;
    value.pdice = next(seed) as i16;
    value.armor = next(seed) as i8;
    value.wear_flag = next(seed) as i8;
    value.magic_power = next(seed) as i8;
    value.magic_realm = next(seed) as i8;
    value.special = next(seed) as i16;
    value.flags = std::array::from_fn(|_| next(seed) as u8);
    value.quest_num = next(seed) as i8;
    value
}

fn seeded_player_snapshot(seed: &mut u64, case: usize) -> PlayerSnapshotV1 {
    let hp_max = (next(seed) % 30_000 + 1) as i16;
    let mp_max = (next(seed) % 30_000 + 1) as i16;
    let node_count = case % 6;
    let mut nodes = Vec::with_capacity(node_count);
    for index in 0..node_count {
        let (parent_index, child_index) = match index {
            0 => (None, 0),
            1 => (Some(0), 0),
            2 => (Some(1), 0),
            3 => (Some(0), 1),
            4 => (None, 1),
            _ => (Some(4), 0),
        };
        nodes.push(ObjectGraphNodeV1 {
            object: seeded_player_object(seed, case, index),
            parent_index,
            child_index,
        });
    }
    PlayerSnapshotV1 {
        name: fixed(format!("player-{case:02}").as_bytes()),
        description: fixed(format!("seeded player snapshot {case:02}").as_bytes()),
        talk: fixed(format!("talk-{case:02}").as_bytes()),
        key: [fixed(b"player"), fixed(b"seeded"), fixed(b"snapshot")],
        level: next(seed) as u8,
        type_code: 0,
        class: next(seed) as i8,
        race: next(seed) as i8,
        numwander: next(seed) as i8,
        alignment: next(seed) as i16,
        strength: next(seed) as i8,
        dexterity: next(seed) as i8,
        constitution: next(seed) as i8,
        intelligence: next(seed) as i8,
        piety: next(seed) as i8,
        hp_max,
        hp_current: (next(seed) % (hp_max as u64 + 1)) as i16,
        mp_max,
        mp_current: (next(seed) % (mp_max as u64 + 1)) as i16,
        armor: next(seed) as i8,
        thaco: next(seed) as i8,
        experience: next(seed) as i64,
        gold: next(seed) as i64,
        ndice: next(seed) as i16,
        sdice: next(seed) as i16,
        pdice: next(seed) as i16,
        special: next(seed) as i16,
        proficiency: std::array::from_fn(|_| next(seed) as i64),
        realm: std::array::from_fn(|_| next(seed) as i64),
        spells: std::array::from_fn(|_| next(seed) as u8),
        flags: std::array::from_fn(|_| next(seed) as u8),
        quests: std::array::from_fn(|_| next(seed) as u8),
        quest_num: next(seed) as i8,
        carry: std::array::from_fn(|_| next(seed) as i16),
        room_number: next(seed) as i16,
        daily: std::array::from_fn(|_| DailyV1 {
            max: next(seed) as u8,
            current: next(seed) as u8,
            last_used: next(seed) as i64,
        }),
        lasttime: std::array::from_fn(|_| LastTimeV1 {
            interval: next(seed) as i64,
            last_used: next(seed) as i64,
            misc: next(seed) as i16,
        }),
        inventory: ObjectGraphV1 { nodes },
    }
}

fn assert_player_roundtrip(
    oracle: &Path,
    artifact_dir: &Path,
    label: &str,
    snapshot: &PlayerSnapshotV1,
) {
    let rust_wire = encode_player_snapshot_v1(snapshot).expect("Rust player snapshot must encode");
    assert_eq!(
        encode_player_snapshot_v1(&decode_player_snapshot_v1(&rust_wire).unwrap()).unwrap(),
        rust_wire,
        "Rust player snapshot must be byte-stable for {label}"
    );
    let c_output = run(
        oracle,
        &["player-snapshot-roundtrip".into(), hex(&rust_wire)],
    );
    assert!(
        !c_output.starts_with("err "),
        "C player snapshot rejected {label}: {c_output}"
    );
    assert_wire_equal(artifact_dir, label, &decode_hex(&c_output), &rust_wire);
}

fn assert_player_roundtrip_file(
    oracle: &Path,
    artifact_dir: &Path,
    label: &str,
    snapshot: &PlayerSnapshotV1,
) {
    let rust_wire = encode_player_snapshot_v1(snapshot).expect("large snapshot must encode");
    assert_eq!(
        encode_player_snapshot_v1(&decode_player_snapshot_v1(&rust_wire).unwrap()).unwrap(),
        rust_wire,
        "Rust player snapshot must be byte-stable for {label}"
    );
    let input = artifact_dir.join(format!("{label}-rust.cdto"));
    let output = artifact_dir.join(format!("{label}-c.cdto"));
    fs::write(&input, &rust_wire).expect("large snapshot input artifact must be writable");
    assert_eq!(
        run(
            oracle,
            &[
                "player-snapshot-roundtrip-file".into(),
                input.display().to_string(),
                output.display().to_string(),
            ],
        ),
        format!("ok {}", rust_wire.len()),
        "C must round-trip the file-transport snapshot"
    );
    assert_wire_equal(
        artifact_dir,
        label,
        &fs::read(output).expect("C large snapshot output must be readable"),
        &rust_wire,
    );
}

fn assert_player_rejected(oracle: &Path, wire: &[u8], label: &str) {
    assert!(
        decode_player_snapshot_v1(wire).is_err(),
        "Rust accepted malformed player snapshot: {label}"
    );
    assert_ne!(
        run(oracle, &["player-snapshot-decode".into(), hex(wire)]),
        "0",
        "C accepted malformed player snapshot: {label}"
    );
}

fn assert_player_rejected_file(oracle: &Path, artifact_dir: &Path, wire: &[u8], label: &str) {
    assert!(
        decode_player_snapshot_v1(wire).is_err(),
        "Rust accepted malformed player snapshot: {label}"
    );
    let input = artifact_dir.join(format!("{label}-malformed.cdto"));
    fs::write(&input, wire).expect("malformed snapshot artifact must be writable");
    assert_ne!(
        run(
            oracle,
            &[
                "player-snapshot-decode-file".into(),
                input.display().to_string(),
            ],
        ),
        "0",
        "C accepted malformed player snapshot: {label}"
    );
}

fn assert_player_snapshot_v1(oracle: &Path, artifact_dir: &Path, seed: u64) {
    let c_wire = decode_hex(&run(oracle, &["player-snapshot-fixture".into()]));
    let c_snapshot = decode_player_snapshot_v1(&c_wire)
        .expect("Rust must decode the complete C PlayerSnapshotV1 fixture");
    assert_eq!(c_snapshot.type_code, 0);
    assert!(c_snapshot
        .daily
        .iter()
        .any(|value| value.current > value.max));
    assert!(c_snapshot.lasttime.iter().any(|value| value.interval < 0));
    assert!(!c_snapshot.inventory.nodes.is_empty());
    assert!(
        !c_wire
            .windows(b"PW-SENTINEL".len())
            .any(|window| window == b"PW-SENTINEL"),
        "legacy password sentinel must not enter PlayerSnapshotV1"
    );
    assert_wire_equal(
        artifact_dir,
        "player-snapshot-c-fixture",
        &c_wire,
        &encode_player_snapshot_v1(&c_snapshot).unwrap(),
    );
    assert_eq!(
        run(oracle, &["player-snapshot-roundtrip".into(), hex(&c_wire)]),
        hex(&c_wire),
        "C PlayerSnapshotV1 must decode and re-encode its canonical fixture"
    );

    let mut generator_seed = seed ^ 0x5053_5631_0000_0001;
    for case in 0..64 {
        let snapshot = seeded_player_snapshot(&mut generator_seed, case);
        assert_player_roundtrip(
            oracle,
            artifact_dir,
            &format!("player-snapshot-seed-{seed:016x}-{case:02}"),
            &snapshot,
        );
    }

    let mut boundary = seeded_player_snapshot(&mut generator_seed, 4096);
    let root = seeded_player_object(&mut generator_seed, 4096, 0);
    boundary.inventory.nodes = (0..4096)
        .map(|index| ObjectGraphNodeV1 {
            object: root.clone(),
            parent_index: None,
            child_index: index,
        })
        .collect();
    assert_player_roundtrip_file(
        oracle,
        artifact_dir,
        "player-snapshot-roots-4096",
        &boundary,
    );

    let mut depth = seeded_player_snapshot(&mut generator_seed, 64);
    depth.inventory.nodes = (0u32..64)
        .map(|index| ObjectGraphNodeV1 {
            object: root.clone(),
            parent_index: index.checked_sub(1),
            child_index: 0,
        })
        .collect();
    assert_player_roundtrip(oracle, artifact_dir, "player-snapshot-depth-64", &depth);

    let mut maximum = seeded_player_snapshot(&mut generator_seed, 8192);
    maximum.inventory.nodes = (0u32..8192)
        .map(|index| ObjectGraphNodeV1 {
            object: root.clone(),
            parent_index: match index {
                0 | 4097 => None,
                1..=4096 => Some(0),
                _ => Some(4097),
            },
            child_index: match index {
                0 => 0,
                1..=4096 => index - 1,
                4097 => 1,
                _ => index - 4098,
            },
        })
        .collect();
    assert_player_roundtrip_file(oracle, artifact_dir, "player-snapshot-nodes-8192", &maximum);

    let valid = encode_player_snapshot_v1(&seeded_player_snapshot(&mut generator_seed, 0))
        .expect("malformed base snapshot must encode");
    let base_fields = decode(&valid).unwrap().fields().to_vec();

    let mut fields = base_fields.clone();
    fields[7] = Field::i8(8, -1);
    assert_player_rejected(
        oracle,
        &encode(&Record::new(Kind::PlayerSnapshot, fields).unwrap()).unwrap(),
        "non-player type",
    );

    let mut fields = base_fields.clone();
    let mut name = fields[0].value().to_vec();
    name[1] = 0;
    name[2] = b'x';
    fields[0] = Field::bytes(1, name);
    assert_player_rejected(
        oracle,
        &encode(&Record::new(Kind::PlayerSnapshot, fields).unwrap()).unwrap(),
        "noncanonical fixed-string tail",
    );

    let mut fields = base_fields.clone();
    fields[0] = Field::bytes(1, vec![b'x'; 80]);
    assert_player_rejected(
        oracle,
        &encode(&Record::new(Kind::PlayerSnapshot, fields).unwrap()).unwrap(),
        "unterminated player fixed string",
    );

    let mut fields = base_fields.clone();
    let mut unterminated_object = root.clone();
    unterminated_object.name = [b'x'; 80];
    let unterminated_inventory = ObjectGraphV1 {
        nodes: vec![ObjectGraphNodeV1 {
            object: unterminated_object,
            parent_index: None,
            child_index: 0,
        }],
    };
    fields[39] = Field::bytes(40, encode_object_graph_v1(&unterminated_inventory).unwrap());
    assert_player_rejected(
        oracle,
        &encode(&Record::new(Kind::PlayerSnapshot, fields).unwrap()).unwrap(),
        "unterminated inventory fixed string",
    );

    let mut fields = base_fields.clone();
    fields[38] = Field::bytes(39, fields[38].value()[..809].to_vec());
    assert_player_rejected(
        oracle,
        &encode(&Record::new(Kind::PlayerSnapshot, fields).unwrap()).unwrap(),
        "lasttime width",
    );

    let mut fields = base_fields.clone();
    let wrong_nested = encode(&Record::new(Kind::Session, Vec::new()).unwrap()).unwrap();
    fields[39] = Field::bytes(40, wrong_nested);
    assert_player_rejected(
        oracle,
        &encode(&Record::new(Kind::PlayerSnapshot, fields).unwrap()).unwrap(),
        "nested graph kind",
    );

    let mut fields = base_fields.clone();
    let graph = fields[39].value().to_vec();
    fields[39] = Field::bytes(41, graph.clone());
    assert_player_rejected(
        oracle,
        &encode(&Record::new(Kind::PlayerSnapshot, fields).unwrap()).unwrap(),
        "inventory field id",
    );

    let mut fields = base_fields.clone();
    fields[39] = Field::optional_bytes(40, graph);
    assert_player_rejected(
        oracle,
        &encode(&Record::new(Kind::PlayerSnapshot, fields).unwrap()).unwrap(),
        "inventory field type",
    );

    let mut fields = base_fields.clone();
    fields.push(Field::optional_raw(41, 0x61, vec![1]));
    assert_player_rejected(
        oracle,
        &encode(&Record::new(Kind::PlayerSnapshot, fields).unwrap()).unwrap(),
        "closed outer schema",
    );

    let mut fields = base_fields.clone();
    let over_roots = ObjectGraphV1 {
        nodes: (0..4097)
            .map(|index| ObjectGraphNodeV1 {
                object: root.clone(),
                parent_index: None,
                child_index: index,
            })
            .collect(),
    };
    fields[39] = Field::bytes(40, encode_object_graph_v1(&over_roots).unwrap());
    let over_roots_wire = encode(&Record::new(Kind::PlayerSnapshot, fields).unwrap()).unwrap();
    assert_player_rejected_file(
        oracle,
        artifact_dir,
        &over_roots_wire,
        "4097-inventory-roots",
    );

    let mut fields = base_fields;
    let over_children = ObjectGraphV1 {
        nodes: std::iter::once(ObjectGraphNodeV1 {
            object: root.clone(),
            parent_index: None,
            child_index: 0,
        })
        .chain((0..4097).map(|index| ObjectGraphNodeV1 {
            object: root.clone(),
            parent_index: Some(0),
            child_index: index,
        }))
        .collect(),
    };
    fields[39] = Field::bytes(40, encode_object_graph_v1(&over_children).unwrap());
    let over_children_wire = encode(&Record::new(Kind::PlayerSnapshot, fields).unwrap()).unwrap();
    assert_player_rejected_file(
        oracle,
        artifact_dir,
        &over_children_wire,
        "4097-inventory-children",
    );
}

fn fixed<const N: usize>(prefix: &[u8]) -> [u8; N] {
    let mut value = [0; N];
    value[..prefix.len()].copy_from_slice(prefix);
    value
}
