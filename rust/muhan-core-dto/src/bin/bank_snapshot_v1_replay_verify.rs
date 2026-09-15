//! Read-only, bounded bank restore verifier. No database or live runtime access.
use muhan_core_dto::{verify_bank_snapshot_v1, DIGEST_LENGTH};
use std::io::{self, Read, Write};
use std::process::ExitCode;

fn digest(args: &[String]) -> Option<[u8; DIGEST_LENGTH]> {
    if args.len() != 2 || args[0] != "--snapshot-sha256" { return None; }
    let hex = args[1].as_bytes();
    if hex.len() != 64 || !hex.iter().all(|b| b.is_ascii_digit() || (b'a'..=b'f').contains(b)) { return None; }
    let mut output = [0; DIGEST_LENGTH];
    for (i, pair) in hex.chunks_exact(2).enumerate() {
        output[i] = u8::from_str_radix(std::str::from_utf8(pair).ok()?,16).ok()?;
    }
    Some(output)
}
fn run() -> Option<()> {
    let expected = digest(&std::env::args().skip(1).collect::<Vec<_>>())?;
    let mut wire = Vec::new();
    io::stdin().lock().take(4 * 1024 * 1024 + 1).read_to_end(&mut wire).ok()?;
    let bank = verify_bank_snapshot_v1(&wire,&expected).ok()?;
    let mut output = io::stdout().lock();
    writeln!(output,"bank_node_count={}",bank.root.nodes.len()).ok()?;
    output.flush().ok()?;
    Some(())
}
fn main() -> ExitCode {
    if run().is_some() { ExitCode::SUCCESS } else {
        let _ = io::stderr().write_all(b"rejected: invalid bank snapshot\n");
        ExitCode::from(1)
    }
}
#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn requires_one_canonical_digest_argument() {
        assert_eq!(digest(&["--snapshot-sha256".into(),"ab".repeat(32)]),Some([0xab;32]));
        assert!(digest(&[]).is_none());
        for value in ["AB".repeat(32),"x".repeat(64),"é".repeat(32),"a".repeat(63)] {
            assert!(digest(&["--snapshot-sha256".into(),value]).is_none());
        }
        assert!(digest(&["--snapshot-sha256".into(),"ab".repeat(32),"extra".into()]).is_none());
    }
}
