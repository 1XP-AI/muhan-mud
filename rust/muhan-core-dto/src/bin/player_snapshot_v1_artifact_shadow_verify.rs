//! Default-unwired, read-only observer for a complete C snapshot artifact.
//!
//! It reads one artifact from stdin and emits a deterministic metadata-only
//! result.  No game process invokes this binary; it cannot mutate state.

use muhan_core_dto::player_snapshot_v1_artifact::{
    format_player_snapshot_v1_artifact_shadow_verification,
    verify_player_snapshot_v1_artifact_shadow, PLAYER_SNAPSHOT_V1_ARTIFACT_FILE_MAX,
};
use std::io::{self, Read, Write};
use std::process::ExitCode;

const REJECTION: &[u8] = b"rejected: invalid player snapshot artifact\n";

fn reject(stderr: &mut impl Write) -> ExitCode {
    let _ = stderr.write_all(REJECTION);
    let _ = stderr.flush();
    ExitCode::from(1)
}

fn run(input: &mut impl Read, stdout: &mut impl Write, stderr: &mut impl Write) -> ExitCode {
    let mut artifact = Vec::new();
    let max_input =
        u64::try_from(PLAYER_SNAPSHOT_V1_ARTIFACT_FILE_MAX).expect("artifact bound fits u64") + 1;
    if input.take(max_input).read_to_end(&mut artifact).is_err()
        || artifact.len() > PLAYER_SNAPSHOT_V1_ARTIFACT_FILE_MAX
    {
        return reject(stderr);
    }
    let report = match verify_player_snapshot_v1_artifact_shadow(&artifact) {
        Ok(report) => report,
        Err(_) => return reject(stderr),
    };
    let rendered = format_player_snapshot_v1_artifact_shadow_verification(&report);
    if stdout.write_all(rendered.as_bytes()).is_err() || stdout.flush().is_err() {
        return reject(stderr);
    }
    ExitCode::SUCCESS
}

fn main() -> ExitCode {
    let mut input = io::stdin().lock();
    let mut stdout = io::stdout().lock();
    let mut stderr = io::stderr().lock();
    if std::env::args_os().len() != 1 {
        return reject(&mut stderr);
    }
    run(&mut input, &mut stdout, &mut stderr)
}

#[cfg(test)]
mod tests {
    use super::{run, REJECTION};
    use muhan_core_dto::player_snapshot_v1_artifact::PLAYER_SNAPSHOT_V1_ARTIFACT_FILE_MAX;
    use std::io::{self, Cursor, Read};
    use std::process::ExitCode;

    struct OversizedReader(usize);

    impl Read for OversizedReader {
        fn read(&mut self, buffer: &mut [u8]) -> io::Result<usize> {
            let remaining = (PLAYER_SNAPSHOT_V1_ARTIFACT_FILE_MAX + 2).saturating_sub(self.0);
            let count = remaining.min(buffer.len());
            buffer[..count].fill(0);
            self.0 += count;
            Ok(count)
        }
    }

    #[test]
    fn oversized_or_malformed_input_has_one_fixed_rejection() {
        let mut stderr = Vec::new();
        assert_eq!(
            run(
                &mut Cursor::new(b"not an artifact"),
                &mut Vec::new(),
                &mut stderr
            ),
            ExitCode::from(1)
        );
        assert_eq!(stderr, REJECTION);

        let mut stderr = Vec::new();
        let mut input = OversizedReader(0);
        assert_eq!(
            run(&mut input, &mut Vec::new(), &mut stderr),
            ExitCode::from(1)
        );
        assert_eq!(stderr, REJECTION);
        assert_eq!(input.0, PLAYER_SNAPSHOT_V1_ARTIFACT_FILE_MAX + 1);
    }
}
