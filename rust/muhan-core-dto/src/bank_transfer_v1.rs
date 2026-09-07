//! Detached money-transfer planning. A plan is not a committed transaction.
//! Caller must bind both revisions and identity, then atomically persist both
//! snapshots before publishing success or replacing live state.
use crate::player_snapshot_v1::{encode_player_snapshot_v1, PlayerSnapshotV1};
use crate::{encode_bank_snapshot_v1, BankSnapshotV1};

/// Calculate from independently digest-bound stored bytes, returning canonical
/// proposed bytes for a caller-owned compare-and-swap transaction.
pub fn plan_money_transfer_bytes(
    player: &[u8],
    bank: &[u8],
    player_digest: &[u8; 32],
    bank_digest: &[u8; 32],
    direction: Direction,
    amount: i64,
) -> Result<(Vec<u8>, Vec<u8>), TransferError> {
    if player.len() > 4 * 1024 * 1024
        || bank.len() > 4 * 1024 * 1024
        || crate::sha256(player) != *player_digest
        || crate::sha256(bank) != *bank_digest
    {
        return Err(TransferError::InvalidSnapshot);
    }
    let decoded_player = crate::player_snapshot_v1::decode_player_snapshot_v1(player)
        .map_err(|_| TransferError::InvalidSnapshot)?;
    if encode_player_snapshot_v1(&decoded_player).map_err(|_| TransferError::InvalidSnapshot)?
        != player
    {
        return Err(TransferError::InvalidSnapshot);
    }
    let decoded_bank = crate::verify_bank_snapshot_v1(bank, bank_digest)
        .map_err(|_| TransferError::InvalidSnapshot)?;
    let plan = plan_money_transfer(&decoded_player, &decoded_bank, direction, amount)?;
    Ok((
        encode_player_snapshot_v1(&plan.player).map_err(|_| TransferError::InvalidSnapshot)?,
        encode_bank_snapshot_v1(&plan.bank).map_err(|_| TransferError::InvalidSnapshot)?,
    ))
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum Direction {
    Deposit,
    Withdraw,
}
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum TransferError {
    InvalidAmount,
    InvalidSnapshot,
    InvalidBalance,
    InsufficientFunds,
    BankLimit,
    Overflow,
}
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct MoneyTransferPlan {
    pub player: PlayerSnapshotV1,
    pub bank: BankSnapshotV1,
}

/// Explicit positive amounts only. UI "all" resolves against the same revision
/// used to plan; zero/negative amounts are rejected, never silently applied.
pub fn plan_money_transfer(
    player: &PlayerSnapshotV1,
    bank: &BankSnapshotV1,
    direction: Direction,
    amount: i64,
) -> Result<MoneyTransferPlan, TransferError> {
    if amount <= 0 {
        return Err(TransferError::InvalidAmount);
    }
    encode_player_snapshot_v1(player).map_err(|_| TransferError::InvalidSnapshot)?;
    encode_bank_snapshot_v1(bank).map_err(|_| TransferError::InvalidSnapshot)?;
    let balance = bank.root.nodes[0].object.value;
    if player.gold < 0 || balance < 0 {
        return Err(TransferError::InvalidBalance);
    }
    let (gold, bank_value) = match direction {
        Direction::Deposit => {
            if player.gold < amount {
                return Err(TransferError::InsufficientFunds);
            }
            let next = balance.checked_add(amount).ok_or(TransferError::Overflow)?;
            if next > 300_000_000 {
                return Err(TransferError::BankLimit);
            }
            (player.gold - amount, next)
        }
        Direction::Withdraw => {
            if balance < amount {
                return Err(TransferError::InsufficientFunds);
            }
            (
                player
                    .gold
                    .checked_add(amount)
                    .ok_or(TransferError::Overflow)?,
                balance - amount,
            )
        }
    };
    let mut plan = MoneyTransferPlan {
        player: player.clone(),
        bank: bank.clone(),
    };
    plan.player.gold = gold;
    plan.bank.root.nodes[0].object.value = bank_value;
    Ok(plan)
}
