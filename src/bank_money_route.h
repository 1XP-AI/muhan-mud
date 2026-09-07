#ifndef MUHAN_BANK_MONEY_ROUTE_H
#define MUHAN_BANK_MONEY_ROUTE_H
struct creature;
struct cmd;
enum bank_money_route_result { BANK_MONEY_LEGACY=0, BANK_MONEY_COMMITTED=1, BANK_MONEY_REJECTED=2 };
typedef struct bank_money_ack { long amount,player_gold,bank_gold; } bank_money_ack;
typedef struct bank_money_route_ops {
    /* 0 = legacy, 1 = DB authority selected, anything else = reject. */
    int (*select)(void *,const struct creature *);
    /* Only 1 means durably committed/verified exact retry. No source mutation.
     * The coordinator owns command identity, parsing, timeout/retry and reply
     * binding. It must not return success for a queued/unconfirmed write. */
    int (*transfer)(void *,const struct creature *,const struct cmd *,int,bank_money_ack *);
    void *context;
} bank_money_route_ops;
int bank_money_route_set(const bank_money_route_ops *);
void bank_money_route_reset(void);
/* Unported file-bank commands are allowed only with explicit legacy selection
 * (or no installed routing policy). Invalid/missing binding must select error,
 * not legacy. This check never invokes the money transfer callback. */
int bank_money_route_allows_legacy(const struct creature *);
int bank_money_route_dispatch(struct creature *,const struct cmd *,int,bank_money_ack *);
#endif
