use muhan_core_dto::player_snapshot_v1::{
    verify_player_snapshot_replay_v1, MAX_LIST_ITEMS, REPLAY_VERIFICATION_V1_ALGORITHM,
    REPLAY_VERIFICATION_V1_VERSION,
};
use muhan_core_dto::{
    encode, Error, Field, Kind, Record, MAX_ENVELOPE_SIZE, OBJECT_GRAPH_V1_MAX_DEPTH,
    OBJECT_GRAPH_V1_MAX_NODES, OBJECT_GRAPH_V1_NODE_LENGTH,
};

fn fixture_hex(source: &str) -> Vec<u8> {
    let text = source.trim();
    assert_eq!(text.len() % 2, 0, "fixture has whole hex octets");
    (0..text.len())
        .step_by(2)
        .map(|index| u8::from_str_radix(&text[index..index + 2], 16).expect("fixture hex"))
        .collect()
}

fn fixtures() -> [(Vec<u8>, usize); 3] {
    [
        (
            fixture_hex(include_str!(
                "../../../tests/fixtures/player_snapshot_v1_canonical.hex"
            )),
            0,
        ),
        (
            fixture_hex(include_str!(
                "../../../tests/fixtures/player_snapshot_v1_one_inventory_item.hex"
            )),
            1,
        ),
        (
            fixture_hex(include_str!(
                "../../../tests/fixtures/player_snapshot_v1_tree_inventory.hex"
            )),
            5,
        ),
    ]
}

fn player_snapshot_fields(inventory: Vec<u8>) -> Vec<Field> {
    vec![
        Field::bytes(1, vec![0; 80]),
        Field::bytes(2, vec![0; 80]),
        Field::bytes(3, vec![0; 80]),
        Field::bytes(4, vec![0; 20]),
        Field::bytes(5, vec![0; 20]),
        Field::bytes(6, vec![0; 20]),
        Field::u8(7, 0),
        Field::i8(8, 0),
        Field::i8(9, 0),
        Field::i8(10, 0),
        Field::i8(11, 0),
        Field::i16(12, 0),
        Field::i8(13, 0),
        Field::i8(14, 0),
        Field::i8(15, 0),
        Field::i8(16, 0),
        Field::i8(17, 0),
        Field::i16(18, 0),
        Field::i16(19, 0),
        Field::i16(20, 0),
        Field::i16(21, 0),
        Field::i8(22, 0),
        Field::i8(23, 0),
        Field::i64(24, 0),
        Field::i64(25, 0),
        Field::i16(26, 0),
        Field::i16(27, 0),
        Field::i16(28, 0),
        Field::i16(29, 0),
        Field::bytes(30, vec![0; 40]),
        Field::bytes(31, vec![0; 32]),
        Field::bytes(32, vec![0; 16]),
        Field::bytes(33, vec![0; 8]),
        Field::bytes(34, vec![0; 16]),
        Field::i8(35, 0),
        Field::bytes(36, vec![0; 20]),
        Field::i16(37, 0),
        Field::bytes(38, vec![0; 100]),
        Field::bytes(39, vec![0; 810]),
        Field::bytes(40, inventory),
    ]
}

fn player_snapshot_envelope_with_inventory(inventory: Vec<u8>) -> Vec<u8> {
    encode(
        &Record::new(Kind::PlayerSnapshot, player_snapshot_fields(inventory))
            .expect("player snapshot field metadata is canonical"),
    )
    .expect("outer player snapshot envelope has a valid digest")
}

fn object_graph_envelope_with_headers<I>(headers: I) -> Vec<u8>
where
    I: IntoIterator<Item = (Option<u32>, u32)>,
{
    let headers: Vec<_> = headers.into_iter().collect();
    let mut fields = Vec::with_capacity(headers.len() + 1);
    fields.push(Field::u32(
        1,
        u32::try_from(headers.len()).expect("test graph count fits u32"),
    ));
    for (index, (parent, child_index)) in headers.into_iter().enumerate() {
        let mut node = vec![0; OBJECT_GRAPH_V1_NODE_LENGTH];
        node[0..4].copy_from_slice(
            &u32::try_from(index)
                .expect("test graph index fits u32")
                .to_be_bytes(),
        );
        node[4..8].copy_from_slice(&parent.unwrap_or(u32::MAX).to_be_bytes());
        node[8..12].copy_from_slice(&child_index.to_be_bytes());
        fields.push(Field::bytes(
            u16::try_from(index + 2).expect("test graph field id fits u16"),
            node,
        ));
    }
    encode(
        &Record::new(Kind::ObjectGraph, fields)
            .expect("graph envelope field metadata is canonical"),
    )
    .expect("graph envelope has a valid digest")
}

#[test]
fn fixed_player_snapshot_fixtures_produce_pinned_non_sensitive_reports() {
    for (wire, expected_nodes) in fixtures() {
        let report = verify_player_snapshot_replay_v1(&wire).expect("fixture verifies");
        assert_eq!(report.algorithm, REPLAY_VERIFICATION_V1_ALGORITHM);
        assert_eq!(report.version, REPLAY_VERIFICATION_V1_VERSION);
        assert_eq!(report.canonical_octets, wire.len());
        assert_eq!(report.inventory_node_count, expected_nodes);
        assert_eq!(report.input_digest, report.canonical_digest);
    }
}

#[test]
fn verification_is_deterministic_for_the_same_input() {
    let wire = fixtures()[2].0.clone();
    assert_eq!(
        verify_player_snapshot_replay_v1(&wire).expect("first verification"),
        verify_player_snapshot_replay_v1(&wire).expect("second verification")
    );
}

#[test]
fn verification_rejects_byte_mutation_malformed_and_oversized_input() {
    let mut digest_mutation = fixtures()[0].0.clone();
    let last = digest_mutation.len() - 1;
    digest_mutation[last] ^= 1;
    assert_eq!(
        verify_player_snapshot_replay_v1(&digest_mutation),
        Err(Error::DigestMismatch)
    );

    let malformed = vec![0; 1];
    assert!(matches!(
        verify_player_snapshot_replay_v1(&malformed),
        Err(Error::Truncated { .. })
    ));

    assert_eq!(
        verify_player_snapshot_replay_v1(&vec![0; MAX_ENVELOPE_SIZE + 1]),
        Err(Error::SizeLimitExceeded {
            limit: MAX_ENVELOPE_SIZE
        })
    );
}

#[test]
fn verification_rejects_digest_valid_invalid_inventory_graphs_and_bounds() {
    let invalid_node_graph = encode(
        &Record::new(
            Kind::ObjectGraph,
            vec![
                Field::u32(1, 1),
                Field::bytes(2, vec![0; OBJECT_GRAPH_V1_NODE_LENGTH]),
            ],
        )
        .expect("graph envelope metadata is canonical"),
    )
    .expect("invalid graph still has a valid envelope digest");
    let invalid_node_snapshot = player_snapshot_envelope_with_inventory(invalid_node_graph);
    assert_eq!(
        verify_player_snapshot_replay_v1(&invalid_node_snapshot),
        Err(Error::InvalidFieldLength { field_id: 2 })
    );

    let too_many_nodes_graph = encode(
        &Record::new(
            Kind::ObjectGraph,
            vec![Field::u32(1, OBJECT_GRAPH_V1_MAX_NODES as u32 + 1)],
        )
        .expect("bounded graph envelope metadata is canonical"),
    )
    .expect("out-of-bound graph still has a valid envelope digest");
    let too_many_nodes_snapshot = player_snapshot_envelope_with_inventory(too_many_nodes_graph);
    assert_eq!(
        verify_player_snapshot_replay_v1(&too_many_nodes_snapshot),
        Err(Error::InvalidFieldLength { field_id: 1 })
    );
}

#[test]
fn verification_rejects_digest_valid_inventory_topology_depth_and_list_boundaries() {
    let non_ancestor_parent_graph =
        object_graph_envelope_with_headers([(None, 0), (Some(0), 0), (None, 1), (Some(0), 1)]);
    assert_eq!(
        verify_player_snapshot_replay_v1(&player_snapshot_envelope_with_inventory(
            non_ancestor_parent_graph,
        )),
        Err(Error::InvalidFieldLength { field_id: 5 })
    );

    let depth_overflow_graph =
        object_graph_envelope_with_headers((0..=OBJECT_GRAPH_V1_MAX_DEPTH).map(|index| {
            (
                index
                    .checked_sub(1)
                    .map(|parent| u32::try_from(parent).expect("test parent fits u32")),
                0,
            )
        }));
    assert_eq!(
        verify_player_snapshot_replay_v1(&player_snapshot_envelope_with_inventory(
            depth_overflow_graph
        )),
        Err(Error::SizeLimitExceeded {
            limit: OBJECT_GRAPH_V1_MAX_DEPTH
        })
    );

    let root_list_overflow_graph =
        object_graph_envelope_with_headers((0..=MAX_LIST_ITEMS).map(|index| {
            (
                None,
                u32::try_from(index).expect("test root index fits u32"),
            )
        }));
    assert_eq!(
        verify_player_snapshot_replay_v1(&player_snapshot_envelope_with_inventory(
            root_list_overflow_graph,
        )),
        Err(Error::SizeLimitExceeded {
            limit: MAX_LIST_ITEMS
        })
    );
}

#[test]
fn verification_rejects_digest_valid_noncanonical_outer_field_type() {
    let empty_graph = object_graph_envelope_with_headers([]);
    let mut fields = player_snapshot_fields(empty_graph);
    fields[0] = Field::optional_bytes(1, vec![0; 80]);
    let noncanonical_outer = encode(
        &Record::new(Kind::PlayerSnapshot, fields)
            .expect("outer field remains a valid generic CDTO field"),
    )
    .expect("outer envelope has a valid digest");

    assert_eq!(
        verify_player_snapshot_replay_v1(&noncanonical_outer),
        Err(Error::InvalidFieldLength { field_id: 1 })
    );
}
