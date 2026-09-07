use muhan_core_dto::bank_transfer_v1::{plan_money_transfer, Direction, TransferError};
use muhan_core_dto::player_snapshot_v1::{decode_player_snapshot_v1, PlayerSnapshotV1};
use muhan_core_dto::{BankSnapshotV1, ObjectGraphV1};

fn fixtures() -> (PlayerSnapshotV1, BankSnapshotV1) {
    let hex =
        include_str!("../../../tests/fixtures/player_snapshot_v1_one_inventory_item.hex").trim();
    let wire: Vec<u8> = (0..hex.len())
        .step_by(2)
        .map(|i| u8::from_str_radix(&hex[i..i + 2], 16).unwrap())
        .collect();
    let mut player = decode_player_snapshot_v1(&wire).unwrap();
    player.gold = 100;
    let mut root = player.inventory.nodes[0].clone();
    root.parent_index = None;
    root.child_index = 0;
    root.object.value = 50;
    let mut child = root.clone();
    child.parent_index = Some(0);
    child.object.value = 99;
    (
        player,
        BankSnapshotV1 {
            root: ObjectGraphV1 {
                nodes: vec![root, child],
            },
        },
    )
}

#[test]
#[ignore = "requires compiled actual C bank command oracle; local Linux runner supplies it"]
fn actual_c_money_commands_match_rust_plans() {
    let oracle = std::env::var("MUHAN_BANK_COMMAND_ORACLE").expect("explicit C oracle required");
    let (mut player, mut bank) = fixtures();
    let mut compared = 0;
    for direction in [Direction::Deposit, Direction::Withdraw] {
        for gold in [0, 1, 50, 100, 299_999_999, 300_000_000, 300_000_001] {
            for balance in [0, 1, 50, 100, 299_999_999, 300_000_000, 300_000_001] {
                for amount in [0, 1, 25, 50, 100, 300_000_000, 300_000_001] {
                    player.gold = gold;
                    bank.root.nodes[0].object.value = balance;
                    let expected = match plan_money_transfer(&player, &bank, direction, amount) {
                        Ok(plan) => format!(
                            "OK {} {}",
                            plan.player.gold, plan.bank.root.nodes[0].object.value
                        ),
                        Err(_) => format!("REJECT {gold} {balance}"),
                    };
                    let output = std::process::Command::new(&oracle)
                        .args([
                            if direction == Direction::Deposit {
                                "deposit"
                            } else {
                                "withdraw"
                            }
                            .to_owned(),
                            gold.to_string(),
                            balance.to_string(),
                            amount.to_string(),
                        ])
                        .output()
                        .expect("launch actual C command");
                    assert!(output.status.success());
                    assert_eq!(
                        String::from_utf8(output.stdout).unwrap().trim(),
                        expected,
                        "direction={direction:?}, player={gold}, bank={balance}, amount={amount}"
                    );
                    compared += 1;
                }
            }
        }
    }
    assert_eq!(compared, 686);
}

#[test]
fn transfers_change_only_two_balances_and_do_not_mutate_inputs() {
    let (player, bank) = fixtures();
    let original = (player.clone(), bank.clone());
    for direction in [Direction::Deposit, Direction::Withdraw] {
        let result = plan_money_transfer(&player, &bank, direction, 25).unwrap();
        assert_eq!(
            result.player.gold + result.bank.root.nodes[0].object.value,
            150
        );
        assert_eq!(
            result.player.gold,
            if direction == Direction::Deposit {
                75
            } else {
                125
            }
        );
        let mut expected_player = player.clone();
        expected_player.gold = result.player.gold;
        let mut expected_bank = bank.clone();
        expected_bank.root.nodes[0].object.value = result.bank.root.nodes[0].object.value;
        assert_eq!(result.player, expected_player);
        assert_eq!(result.bank, expected_bank);
        assert_eq!((&player, &bank), (&original.0, &original.1));
    }
}

#[test]
fn rejects_invalid_amount_funds_cap_overflow_and_malformed_state() {
    let (mut player, mut bank) = fixtures();
    for amount in [0, -1, i64::MIN] {
        assert_eq!(
            plan_money_transfer(&player, &bank, Direction::Deposit, amount),
            Err(TransferError::InvalidAmount)
        );
    }
    assert_eq!(
        plan_money_transfer(&player, &bank, Direction::Deposit, 101),
        Err(TransferError::InsufficientFunds)
    );
    assert_eq!(
        plan_money_transfer(&player, &bank, Direction::Withdraw, 51),
        Err(TransferError::InsufficientFunds)
    );
    bank.root.nodes[0].object.value = 300_000_000;
    assert_eq!(
        plan_money_transfer(&player, &bank, Direction::Deposit, 1),
        Err(TransferError::BankLimit)
    );
    bank.root.nodes[0].object.value = 50;
    player.gold = i64::MAX;
    assert_eq!(
        plan_money_transfer(&player, &bank, Direction::Withdraw, 1),
        Err(TransferError::Overflow)
    );
    player.gold = -1;
    assert_eq!(
        plan_money_transfer(&player, &bank, Direction::Withdraw, 1),
        Err(TransferError::InvalidBalance)
    );
    player.gold = 100;
    bank.root.nodes.clear();
    assert_eq!(
        plan_money_transfer(&player, &bank, Direction::Deposit, 1),
        Err(TransferError::InvalidSnapshot)
    );
}

#[test]
fn deterministic_balances_conserve_value_across_roundtrips() {
    let (mut player, mut bank) = fixtures();
    for gold in [1, 99, 1000, 300_000_000] {
        for balance in [0, 1, 500, 299_999_999] {
            player.gold = gold;
            bank.root.nodes[0].object.value = balance;
            for amount in [1, gold / 2, gold] {
                if amount <= 0 || balance + amount > 300_000_000 {
                    continue;
                }
                let deposit =
                    plan_money_transfer(&player, &bank, Direction::Deposit, amount).unwrap();
                let withdraw = plan_money_transfer(
                    &deposit.player,
                    &deposit.bank,
                    Direction::Withdraw,
                    amount,
                )
                .unwrap();
                assert_eq!(withdraw.player, player);
                assert_eq!(withdraw.bank, bank);
            }
        }
    }
}
