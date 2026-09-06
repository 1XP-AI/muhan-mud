//! A narrow, read-only normalized view of a canonical player snapshot artifact.
//!
//! This module is intentionally a projection boundary, not a persistence or
//! gameplay API. It starts with the closed `PlayerSnapshotV1` decoder and only
//! exposes a reviewed numeric allowlist plus the canonical inventory topology.
//! Text, keys, flags, opaque payloads, credentials, paths, sessions, and
//! gameplay-control fields are never copied into these values.

use super::player_snapshot_v1::{decode_player_snapshot_v1, LastTimeV1};
use super::{sha256, DailyV1, Error, ObjectV1, DIGEST_LENGTH};

/// Version of the normalized projection schema and its canonical digest bytes.
pub const PLAYER_SNAPSHOT_NORMALIZED_V1_VERSION: u16 = 1;
/// The fixed digest algorithm used by [`PlayerSnapshotNormalizedV1::canonical_digest`].
pub const PLAYER_SNAPSHOT_NORMALIZED_V1_ALGORITHM: &str = "sha-256";

/// Numeric daily-limit data retained from one snapshot slot.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct NormalizedDailyV1 {
    pub max: u8,
    pub current: u8,
    pub last_used: i64,
}

/// Numeric timer data retained from one snapshot timer slot.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct NormalizedTimerV1 {
    pub interval: i64,
    pub last_used: i64,
    pub misc: i16,
}

/// One inventory node's reviewed numeric fields and canonical graph position.
///
/// `parent_index` and `child_index` are copied from the canonical preorder
/// object graph. No object labels, keys, flag bytes, or quest-control field
/// crosses this boundary.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct NormalizedItemV1 {
    pub parent_index: Option<u32>,
    pub child_index: u32,
    pub value: i64,
    pub weight: i16,
    pub type_code: i8,
    pub adjustment: i8,
    pub shots_max: i16,
    pub shots_current: i16,
    pub ndice: i16,
    pub sdice: i16,
    pub pdice: i16,
    pub armor: i8,
    pub wear_flag: i8,
    pub magic_power: i8,
    pub magic_realm: i8,
    pub special: i16,
}

/// The safe normalized state emitted by this artifact decode boundary.
///
/// Its fields are an explicit numeric allowlist: current level, HP/MP,
/// experience and gold; ten daily slots; forty-five timer slots; and item
/// numeric properties/topology. It has no text, byte buffers, identity,
/// credentials, paths, sessions, flags, quests, or mutable gameplay handles.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PlayerSnapshotNormalizedV1 {
    pub level: u8,
    pub hp_max: i16,
    pub hp_current: i16,
    pub mp_max: i16,
    pub mp_current: i16,
    pub experience: i64,
    pub gold: i64,
    pub daily: [NormalizedDailyV1; 10],
    pub timers: [NormalizedTimerV1; 45],
    pub items: Vec<NormalizedItemV1>,
}

impl PlayerSnapshotNormalizedV1 {
    /// Computes SHA-256 over the version-pinned, big-endian safe projection.
    /// The encoded bytes contain only this type's scalar/timer/item numeric
    /// fields and topology; no source artifact bytes are hashed directly.
    pub fn canonical_digest(&self) -> [u8; DIGEST_LENGTH] {
        sha256(&self.canonical_bytes())
    }

    fn canonical_bytes(&self) -> Vec<u8> {
        let mut output = Vec::with_capacity(2 + 1 + 4 * 2 + 2 * 8 + 10 * 10 + 45 * 18);
        output.extend_from_slice(b"muhan/player-snapshot-normalized-v1\0");
        output.extend_from_slice(&PLAYER_SNAPSHOT_NORMALIZED_V1_VERSION.to_be_bytes());
        output.push(self.level);
        output.extend_from_slice(&self.hp_max.to_be_bytes());
        output.extend_from_slice(&self.hp_current.to_be_bytes());
        output.extend_from_slice(&self.mp_max.to_be_bytes());
        output.extend_from_slice(&self.mp_current.to_be_bytes());
        output.extend_from_slice(&self.experience.to_be_bytes());
        output.extend_from_slice(&self.gold.to_be_bytes());
        for daily in &self.daily {
            output.push(daily.max);
            output.push(daily.current);
            output.extend_from_slice(&daily.last_used.to_be_bytes());
        }
        for timer in &self.timers {
            output.extend_from_slice(&timer.interval.to_be_bytes());
            output.extend_from_slice(&timer.last_used.to_be_bytes());
            output.extend_from_slice(&timer.misc.to_be_bytes());
        }
        output.extend_from_slice(
            &u32::try_from(self.items.len())
                .expect("PlayerSnapshotV1 decoder limits the item count")
                .to_be_bytes(),
        );
        for item in &self.items {
            output.extend_from_slice(&item.parent_index.unwrap_or(u32::MAX).to_be_bytes());
            output.extend_from_slice(&item.child_index.to_be_bytes());
            output.extend_from_slice(&item.value.to_be_bytes());
            output.extend_from_slice(&item.weight.to_be_bytes());
            output.extend_from_slice(&item.type_code.to_be_bytes());
            output.extend_from_slice(&item.adjustment.to_be_bytes());
            output.extend_from_slice(&item.shots_max.to_be_bytes());
            output.extend_from_slice(&item.shots_current.to_be_bytes());
            output.extend_from_slice(&item.ndice.to_be_bytes());
            output.extend_from_slice(&item.sdice.to_be_bytes());
            output.extend_from_slice(&item.pdice.to_be_bytes());
            output.extend_from_slice(&item.armor.to_be_bytes());
            output.extend_from_slice(&item.wear_flag.to_be_bytes());
            output.extend_from_slice(&item.magic_power.to_be_bytes());
            output.extend_from_slice(&item.magic_realm.to_be_bytes());
            output.extend_from_slice(&item.special.to_be_bytes());
        }
        output
    }
}

fn normalized_daily(value: DailyV1) -> NormalizedDailyV1 {
    NormalizedDailyV1 {
        max: value.max,
        current: value.current,
        last_used: value.last_used,
    }
}

fn normalized_timer(value: LastTimeV1) -> NormalizedTimerV1 {
    NormalizedTimerV1 {
        interval: value.interval,
        last_used: value.last_used,
        misc: value.misc,
    }
}

fn normalized_item(
    parent_index: Option<u32>,
    child_index: u32,
    object: &ObjectV1,
) -> NormalizedItemV1 {
    NormalizedItemV1 {
        parent_index,
        child_index,
        value: object.value,
        weight: object.weight,
        type_code: object.type_code,
        adjustment: object.adjustment,
        shots_max: object.shots_max,
        shots_current: object.shots_current,
        ndice: object.ndice,
        sdice: object.sdice,
        pdice: object.pdice,
        armor: object.armor,
        wear_flag: object.wear_flag,
        magic_power: object.magic_power,
        magic_realm: object.magic_realm,
        special: object.special,
    }
}

/// Decodes one artifact and emits the strict normalized projection.
///
/// `decode_player_snapshot_v1` is deliberately the first operation, so all
/// malformed, tampered, non-canonical, or invalid-player artifacts fail before
/// any projection is returned. This function performs no I/O or mutation.
pub fn project_player_snapshot_v1_artifact(
    artifact_bytes: &[u8],
) -> Result<PlayerSnapshotNormalizedV1, Error> {
    let snapshot = decode_player_snapshot_v1(artifact_bytes)?;
    Ok(PlayerSnapshotNormalizedV1 {
        level: snapshot.level,
        hp_max: snapshot.hp_max,
        hp_current: snapshot.hp_current,
        mp_max: snapshot.mp_max,
        mp_current: snapshot.mp_current,
        experience: snapshot.experience,
        gold: snapshot.gold,
        daily: snapshot.daily.map(normalized_daily),
        timers: snapshot.lasttime.map(normalized_timer),
        items: snapshot
            .inventory
            .nodes
            .iter()
            .map(|node| normalized_item(node.parent_index, node.child_index, &node.object))
            .collect(),
    })
}
