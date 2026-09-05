//! Version-pinned, Rust-only verifier for one PlayerSnapshotV1 CDTO envelope.

use muhan_core_dto::player_snapshot_v1::{
    format_replay_verification_v1_report, verify_player_snapshot_replay_v1,
};
use muhan_core_dto::MAX_ENVELOPE_SIZE;
use std::io::{self, Read, Write};
use std::process::ExitCode;

const REJECTION: &[u8] = b"rejected: invalid player snapshot CDTO\n";

fn reject(stderr: &mut impl Write) -> ExitCode {
    let _ = stderr.write_all(REJECTION);
    let _ = stderr.flush();
    ExitCode::from(1)
}

fn run_replay_verifier(
    input: &mut impl Read,
    stdout: &mut impl Write,
    stderr: &mut impl Write,
) -> ExitCode {
    let mut wire = Vec::new();
    let max_input_octets =
        u64::try_from(MAX_ENVELOPE_SIZE).expect("CDTO envelope limit fits u64") + 1;
    if input.take(max_input_octets).read_to_end(&mut wire).is_err()
        || wire.len() > MAX_ENVELOPE_SIZE
    {
        return reject(stderr);
    }

    let report = match verify_player_snapshot_replay_v1(&wire) {
        Ok(report) => report,
        Err(_) => return reject(stderr),
    };
    let rendered = format_replay_verification_v1_report(&report);
    if stdout.write_all(rendered.as_bytes()).is_err() || stdout.flush().is_err() {
        return reject(stderr);
    }
    ExitCode::SUCCESS
}

fn main() -> ExitCode {
    let mut input = io::stdin().lock();
    let mut stdout = io::stdout().lock();
    let mut stderr = io::stderr().lock();
    run_replay_verifier(&mut input, &mut stdout, &mut stderr)
}

#[cfg(test)]
mod tests {
    use super::{run_replay_verifier, REJECTION};
    use muhan_core_dto::MAX_ENVELOPE_SIZE;
    use std::io::{self, Cursor, Read, Write};
    use std::process::ExitCode;

    struct FailingReader;

    impl Read for FailingReader {
        fn read(&mut self, _: &mut [u8]) -> io::Result<usize> {
            Err(io::Error::other("untrusted input detail"))
        }
    }

    struct FailingWriter;

    impl Write for FailingWriter {
        fn write(&mut self, _: &[u8]) -> io::Result<usize> {
            Err(io::Error::other("untrusted output detail"))
        }

        fn flush(&mut self) -> io::Result<()> {
            Ok(())
        }
    }

    struct FlushFailingWriter(Vec<u8>);

    impl Write for FlushFailingWriter {
        fn write(&mut self, bytes: &[u8]) -> io::Result<usize> {
            self.0.extend_from_slice(bytes);
            Ok(bytes.len())
        }

        fn flush(&mut self) -> io::Result<()> {
            Err(io::Error::other("untrusted flush detail"))
        }
    }

    struct OversizedReader {
        bytes_read: usize,
    }

    impl Read for OversizedReader {
        fn read(&mut self, buffer: &mut [u8]) -> io::Result<usize> {
            let total = MAX_ENVELOPE_SIZE + 2;
            let remaining = total.saturating_sub(self.bytes_read);
            let count = remaining.min(buffer.len());
            buffer[..count].fill(0);
            self.bytes_read += count;
            Ok(count)
        }
    }

    fn assert_rejected(status: ExitCode, stderr: &[u8]) {
        assert_eq!(status, ExitCode::from(1));
        assert_eq!(stderr, REJECTION);
    }

    fn canonical_fixture() -> Vec<u8> {
        let fixture =
            include_str!("../../../../tests/fixtures/player_snapshot_v1_canonical.hex").trim();
        (0..fixture.len())
            .step_by(2)
            .map(|index| u8::from_str_radix(&fixture[index..index + 2], 16).expect("fixture hex"))
            .collect()
    }

    #[test]
    fn runner_sanitizes_stdin_read_failure() {
        let mut input = FailingReader;
        let mut stdout = Vec::new();
        let mut stderr = Vec::new();

        assert_rejected(
            run_replay_verifier(&mut input, &mut stdout, &mut stderr),
            &stderr,
        );
        assert!(stdout.is_empty());
    }

    #[test]
    fn runner_sanitizes_stdout_write_failure() {
        let mut input = Cursor::new(canonical_fixture());
        let mut stdout = FailingWriter;
        let mut stderr = Vec::new();

        assert_rejected(
            run_replay_verifier(&mut input, &mut stdout, &mut stderr),
            &stderr,
        );
    }

    #[test]
    fn runner_sanitizes_stdout_flush_failure() {
        let mut input = Cursor::new(canonical_fixture());
        let mut stdout = FlushFailingWriter(Vec::new());
        let mut stderr = Vec::new();

        assert_rejected(
            run_replay_verifier(&mut input, &mut stdout, &mut stderr),
            &stderr,
        );
    }

    #[test]
    fn runner_stops_reading_once_the_envelope_bound_is_exceeded() {
        let mut input = OversizedReader { bytes_read: 0 };
        let mut stdout = Vec::new();
        let mut stderr = Vec::new();

        assert_rejected(
            run_replay_verifier(&mut input, &mut stdout, &mut stderr),
            &stderr,
        );
        assert!(stdout.is_empty());
        assert_eq!(input.bytes_read, MAX_ENVELOPE_SIZE + 1);
    }
}
