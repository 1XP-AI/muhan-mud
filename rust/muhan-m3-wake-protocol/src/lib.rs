//! Fixed framing for the lossy M3 helper wake optimization.
//!
//! This crate has no transport, persistence, identity, or runtime wiring.

/// Exact v1 wire-frame length in octets.
pub const FRAME_LENGTH: usize = 16;

const CANONICAL: [u8; FRAME_LENGTH] = [
    0x4d, 0x55, 0x48, 0x4d, 0x33, 0x57, 0x4b, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00,
];

/// A v1 frame was absent, the wrong size, or not byte-for-byte canonical.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct DecodeError;

/// Returns the only valid v1 frame: `MUHM3WK\0`, version 1, WAKE kind 1,
/// and a zero-length payload, all in big-endian framing.
pub fn encode() -> [u8; FRAME_LENGTH] {
    CANONICAL
}

/// Accepts only the exact canonical v1 frame.
pub fn decode(input: &[u8]) -> Result<(), DecodeError> {
    if input == CANONICAL {
        Ok(())
    } else {
        Err(DecodeError)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn encoder_emits_the_pinned_big_endian_frame() {
        assert_eq!(
            encode(),
            [
                0x4d, 0x55, 0x48, 0x4d, 0x33, 0x57, 0x4b, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00,
                0x00, 0x00,
            ]
        );
    }

    #[test]
    fn decoder_rejects_every_noncanonical_field_and_length() {
        let canonical = encode();
        assert!(decode(&canonical).is_ok());
        assert!(decode(&[]).is_err());
        assert!(decode(&canonical[..FRAME_LENGTH - 1]).is_err());
        assert!(decode(&[canonical.as_slice(), &[0]].concat()).is_err());
        for offset in 0..FRAME_LENGTH {
            let mut malformed = canonical;
            malformed[offset] ^= 1;
            assert!(decode(&malformed).is_err(), "offset {offset}");
        }
    }
}
