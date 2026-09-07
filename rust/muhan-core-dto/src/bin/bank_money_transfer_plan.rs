//! Bounded binary planner bridge. No network, database, file or live-state writes.
use muhan_core_dto::bank_transfer_v1::{plan_money_transfer_bytes, Direction};
use std::io::{self, Read, Write};
use std::process::ExitCode;
const LIMIT: usize = 4 * 1024 * 1024;
fn digest(s: &str) -> Option<[u8; 32]> {
    if s.len() != 64
        || !s
            .bytes()
            .all(|b| b.is_ascii_digit() || (b'a'..=b'f').contains(&b))
    {
        return None;
    }
    let mut out = [0; 32];
    for (i, b) in out.iter_mut().enumerate() {
        *b = u8::from_str_radix(&s[i * 2..i * 2 + 2], 16).ok()?;
    }
    Some(out)
}
fn plan(args: &[String], input: &mut impl Read, output: &mut impl Write) -> Option<()> {
    if args.len() != 4 {
        return None;
    }
    let direction = match args[0].as_str() {
        "deposit" => Direction::Deposit,
        "withdraw" => Direction::Withdraw,
        _ => return None,
    };
    if args[1].is_empty()
        || args[1].starts_with('0')
        || !args[1].bytes().all(|b| b.is_ascii_digit())
    {
        return None;
    }
    let amount = args[1].parse::<i64>().ok()?;
    let pd = digest(&args[2])?;
    let bd = digest(&args[3])?;
    let mut bytes = Vec::new();
    input
        .take((LIMIT * 2 + 9) as u64)
        .read_to_end(&mut bytes)
        .ok()?;
    if bytes.len() < 8 || bytes.len() > LIMIT * 2 + 8 {
        return None;
    }
    let pl = u32::from_be_bytes(bytes[0..4].try_into().ok()?) as usize;
    let bl = u32::from_be_bytes(bytes[4..8].try_into().ok()?) as usize;
    if pl > LIMIT || bl > LIMIT || pl + bl + 8 != bytes.len() {
        return None;
    }
    let (p, b) = plan_money_transfer_bytes(
        &bytes[8..8 + pl],
        &bytes[8 + pl..],
        &pd,
        &bd,
        direction,
        amount,
    )
    .ok()?;
    // Build the complete result before writing; invalid inputs emit no payload.
    let mut result = Vec::with_capacity(p.len() + b.len() + 8);
    result.extend_from_slice(&(p.len() as u32).to_be_bytes());
    result.extend_from_slice(&(b.len() as u32).to_be_bytes());
    result.extend(p);
    result.extend(b);
    output.write_all(&result).ok()?;
    output.flush().ok()?;
    Some(())
}
fn main() -> ExitCode {
    if plan(
        &std::env::args().skip(1).collect::<Vec<_>>(),
        &mut io::stdin().lock(),
        &mut io::stdout().lock(),
    )
    .is_some()
    {
        ExitCode::SUCCESS
    } else {
        let _ = io::stderr().write_all(b"rejected: invalid bank transfer plan\n");
        ExitCode::from(1)
    }
}
#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn malformed_or_unbound_input_never_emits_a_plan() {
        for amount in ["0", "-1", "01", "+1", "9223372036854775808"] {
            let mut output = Vec::new();
            assert!(plan(
                &[
                    "deposit".into(),
                    amount.into(),
                    "00".repeat(32),
                    "00".repeat(32)
                ],
                &mut &[][..],
                &mut output
            )
            .is_none());
            assert!(output.is_empty());
        }
        let args = [
            "deposit".into(),
            "1".into(),
            "00".repeat(32),
            "00".repeat(32),
        ];
        for bytes in [vec![], vec![0; 8], vec![255; 8], vec![0; 9]] {
            let mut output = Vec::new();
            assert!(plan(&args, &mut &bytes[..], &mut output).is_none());
            assert!(output.is_empty());
        }
        assert!(digest(&"AA".repeat(32)).is_none());
    }
}
