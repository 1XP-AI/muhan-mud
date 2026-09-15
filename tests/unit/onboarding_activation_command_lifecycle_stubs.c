/* Dormant legacy branches remain in command1's two production handlers.
 * Their lower-level symbols are link fakes only: the lifecycle fixture enters
 * the accepted ACTIVATED cases exclusively. */
char cmdlist;
int low;

int alias_cmd() { return 0; }
int check_item() { return 0; }
int find_who() { return -1; }
int free_crt() { return 0; }
int init_alias() { return 0; }
int init_ply() { return 0; }
int init_staged_ply() { return 0; }
int load_ply() { return -1; }
int log_dmcmd() { return 0; }
int log_fl() { return 0; }
int logn() { return 0; }
int lowercize() { return 0; }
int merror() { return 0; }
int onboarding_evidence_control_enabled() { return 0; }
int onboarding_evidence_emission_prepare() { return -1; }
int onboarding_receipt_mark_committed() { return -1; }
int onboarding_receipt_mark_saved() { return -1; }
int onboarding_receipt_write_pending() { return -1; }
int onboarding_session_claim_allow_live() { return 0; }
int onboarding_session_file_sha256() { return -1; }
int onboarding_session_is_protocol_line() { return 0; }
int player_path_from_name() { return -1; }
int player_recovery_login_blocked() { return 0; }
int rp_fopen() { return 0; }
int rp_stat() { return -1; }
int save_ply() { return -1; }
int special_cmd() { return 0; }
int up_level() { return 0; }
int utf8_codepoint_len() { return 0; }
int utf8_next_codepoint() { return 0; }
int utf8_validate() { return 0; }
int zero() { return 0; }
