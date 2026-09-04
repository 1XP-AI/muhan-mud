use muhan_m3_wake_protocol::{decode, encode, FRAME_LENGTH};
use std::env;
use std::process::Command;

const CANONICAL_HEX: &str = "4d55484d33574b000001000100000000";

fn hex(bytes: &[u8]) -> String {
    bytes.iter().map(|byte| format!("{byte:02x}")).collect()
}

fn c_oracle(arguments: &[&str]) -> String {
    let path = env::var("M3_WAKE_V1_C_ORACLE")
        .expect("M3_WAKE_V1_C_ORACLE must point to the C oracle binary");
    let output = Command::new(path)
        .args(arguments)
        .output()
        .expect("C oracle starts");
    assert!(output.status.success(), "C oracle exits successfully");
    String::from_utf8(output.stdout)
        .expect("C oracle output is UTF-8")
        .trim()
        .to_owned()
}

#[test]
fn canonical_encode_matches_c_oracle() {
    assert_eq!(hex(&encode()), CANONICAL_HEX);
    let canonical_from_c = c_oracle(&["encode"]);
    assert_eq!(canonical_from_c, CANONICAL_HEX);
}

#[test]
fn deterministic_malformed_corpus_matches_c_oracle() {
    let canonical = encode();
    let mut corpus = vec![Vec::new(), canonical[..15].to_vec(), canonical.to_vec()];
    corpus.push([canonical.as_slice(), &[0]].concat());
    for offset in [0usize, 7, 8, 9, 10, 11, 12, 15] {
        let mut mutated = canonical;
        mutated[offset] ^= 1;
        corpus.push(mutated.to_vec());
    }

    for bytes in corpus {
        let rust_accepts = decode(&bytes).is_ok();
        let oracle_accepts = c_oracle(&["decode", &hex(&bytes)]) == "accept";
        assert_eq!(rust_accepts, oracle_accepts, "corpus item {}", hex(&bytes));
    }
}

#[test]
fn canonical_decoder_is_exactly_sixteen_bytes() {
    assert_eq!(encode().len(), FRAME_LENGTH);
    assert!(decode(&encode()).is_ok());
    assert!(decode(&[]).is_err());
    assert!(decode(&encode()[..FRAME_LENGTH - 1]).is_err());
}
