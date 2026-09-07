 /*
 * COMMAND1.C:
 *
 *      Command handling/parsing routines.
 *
 *      Copyright (C) 1991, 1992, 1993 Brett J. Vickers
 *
 */

#include "kstbl.h"
#include "mstruct.h"
#include "mextern.h"
#include "player_path.h"
#include "player_store.h"
#include "player_recovery.h"
#include "trusted_admission.h"
#include "onboarding_admission.h"
#include "onboarding_activation_binding.h"
#include "onboarding_activation_save_capability.h"
#ifdef USE_M3_RUNTIME
#include "onboarding_activation_gate.h"
#endif
#include "onboarding_evidence_control.h"
#include "onboarding_evidence_emission.h"
#include "onboarding_receipt.h"
#include "onboarding_session.h"
#include "resource_path.h"
#include <ctype.h>

#ifndef WIN32
#include <unistd.h>
#endif

#include <time.h>
#include <sys/stat.h>
#include <string.h>
#include <stdlib.h>


char pass_num[PMAX];
long last_login[PMAX];

static int onboarding_fd_active(fd)
int fd;
{
	return fd >= 0 && fd < PMAX && Ply[fd].extr &&
		Ply[fd].extr->onboarding_mode != 0;
}

static int onboarding_fd_provisioning(fd)
int fd;
{
	return onboarding_fd_active(fd) &&
		Ply[fd].extr->onboarding_mode == ONBOARDING_ADMISSION_MODE_PROVISION;
}

static void onboarding_clear_claim_transient(fd)
int fd;
{
	if(fd < 0 || fd >= PMAX || !Ply[fd].extr) return;
	memset(Ply[fd].extr->tempstr[0], 0,
	       sizeof(Ply[fd].extr->tempstr[0]));
	memset(Ply[fd].extr->onboarding_claim_sha256, 0,
	       sizeof(Ply[fd].extr->onboarding_claim_sha256));
	Ply[fd].extr->onboarding_claim_challenged_at = 0;
}

/* MUD1O claim is the only path that holds a loaded legacy password solely to
 * compare it once.  handle_commands has already wiped the consumed ring line
 * before entering this callback; do not erase unread Gateway controls. */
static void onboarding_zero_claim_credentials(fd, transient)
int fd;
unsigned char *transient;
{
	if(fd < 0 || fd >= PMAX) return;
	if(Ply[fd].ply)
		onboarding_session_zeroize_claim_memory(
			Ply[fd].ply->password, sizeof(Ply[fd].ply->password), 0, 0);
	if(transient)
		onboarding_session_zeroize_claim_memory(
			0, 0, transient, strlen((char *)transient) + 1);
}

static void ticket_reject(fd, response, length)
int fd;
const char *response;
unsigned int length;
{
	scwrite(fd, response, length);
	disconnect(fd);
}

void onboarding_fail(fd)
int fd;
{
	/* Numeric lifecycle diagnostics only: never log the consumed control line. */
	if(fd >= 0 && fd < PMAX && Ply[fd].extr && Ply[fd].io)
		fprintf(stderr, "MUD onboarding failure: mode=%d state=%d phase=%d\n",
			(int)Ply[fd].extr->onboarding_mode,
			(int)Ply[fd].extr->onboarding_state,
			(int)Ply[fd].io->fnparam);
	/* Do not let disconnect() turn a failed onboarding wizard into a retrying
	 * save.  A successfully saved file has already been atomically published
	 * and is intentionally left alone. */
	if(fd >= 0 && fd < PMAX && Ply[fd].ply)
		Ply[fd].ply->fd = -1;
	if(fd >= 0 && fd < PMAX && Ply[fd].extr)
		Ply[fd].extr->onboarding_state = (char)ONBOARDING_STATE_FAILED;
	if(fd >= 0 && fd < PMAX && Ply[fd].extr)
		onboarding_activation_save_capability_clear(
			&Ply[fd].extr->onboarding_activation_save);
	if(fd >= 0 && fd < PMAX && Ply[fd].extr &&
	   Ply[fd].extr->onboarding_mode == ONBOARDING_ADMISSION_MODE_CLAIM)
	{
		onboarding_zero_claim_credentials(fd, 0);
		onboarding_clear_claim_transient(fd);
	}
	ticket_reject(fd, "MUD1O ERR\n", 10);
}

static int onboarding_canonical_name(str)
unsigned char *str;
{
	unsigned long bytes, codepoints;

	if(!str || !utf8_validate(str, (unsigned long)strlen((char *)str))) return -1;
	bytes = (unsigned long)strlen((char *)str);
	codepoints = utf8_codepoint_len(str);
	if(!bytes || bytes > PLAYER_NAME_MAX_BYTES ||
	   codepoints < PLAYER_NAME_MIN_CODEPOINTS ||
	   codepoints > PLAYER_NAME_MAX_CODEPOINTS ||
	   !player_name_is_valid(str, PLAYER_NAME_MIN_CODEPOINTS,
					 PLAYER_NAME_MAX_CODEPOINTS)) return -1;
	lowercize(str, 1);
	if(!strcmp((char *)str, DMNAME) || !strcmp((char *)str, DMNAME2) ||
	   !strcmp((char *)str, DMNAME3) || !strcmp((char *)str, DMNAME4) ||
	   !strcmp((char *)str, DMNAME5) || !strcmp((char *)str, DMNAME6) ||
	   !strcmp((char *)str, DMNAME7)) return -1;
	return 0;
}

static int onboarding_name_hex(name, out, out_size)
const char *name;
char *out;
unsigned long out_size;
{
	static const char hex[] = "0123456789abcdef";
	unsigned long i, length;

	if(!name || !out) return -1;
	length = (unsigned long)strlen(name);
	if(!length || length > PLAYER_NAME_MAX_BYTES ||
	   out_size < length * 2 + 1) return -1;
	for(i=0; i<length; i++) {
		unsigned char byte = (unsigned char)name[i];
		out[2*i] = hex[byte >> 4];
		out[2*i+1] = hex[byte & 15];
	}
	out[length * 2] = 0;
	return 0;
}

static int onboarding_apply_control(fd, control, gateway)
int fd;
onboarding_control *control;
int gateway;
{
	onboarding_state state;
	int result;

	if(!onboarding_fd_active(fd) || !control) return -1;
	state = (onboarding_state)Ply[fd].extr->onboarding_state;
	result = gateway ? onboarding_state_apply_gateway_control(&state, control) :
			onboarding_state_apply_c_control(&state, control);
	if(result != 0) return -1;
	Ply[fd].extr->onboarding_state = (char)state;
	return 0;
}

static int onboarding_send_control(fd, control)
int fd;
onboarding_control *control;
{
	char line[ONBOARDING_ADMISSION_MAX_LINE + 1];
	unsigned long length;
	int written;

	if(!control || onboarding_apply_control(fd, control, 0) != 0 ||
	   onboarding_format_c_control(line, sizeof(line), control) != 0) return -1;
	length = (unsigned long)strlen(line);
	written = scwrite(fd, line, (unsigned int)length);
	if(written < 0 || (unsigned long)written != length) return -1;
	return 0;
}

/* The command UUID is bound only after the protocol state machine accepted
 * ACTIVATED.  This durable non-secret record is deliberately not an M3
 * consumption call; M3 remains unmodified until its separate integration. */
static int onboarding_write_activation_binding(fd, command_id)
int fd;
const char *command_id;
{
	onboarding_activation_binding_mode mode;
	if(!onboarding_fd_active(fd) || !command_id) return -1;
	mode = Ply[fd].extr->onboarding_mode == ONBOARDING_ADMISSION_MODE_PROVISION ?
		ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION :
		Ply[fd].extr->onboarding_mode == ONBOARDING_ADMISSION_MODE_CLAIM ?
		ONBOARDING_ACTIVATION_BINDING_MODE_CLAIM :
		ONBOARDING_ACTIVATION_BINDING_MODE_INVALID;
	return onboarding_activation_binding_write(
		Ply[fd].extr->onboarding_actor_id,
		Ply[fd].extr->onboarding_correlation_id,
		Ply[fd].extr->onboarding_character_id, mode, command_id);
}

/* The later save integration must consume this descriptor-owned object by the
 * same command UUID; this call neither selects a saver nor invokes one. */
static int onboarding_capture_activation_save_capability(fd, command_id,
							 canonical_name)
int fd;
const char *command_id;
const char *canonical_name;
{
	onboarding_activation_binding_mode mode;
	onboarding_activation_save_capability_status result;
	if(!onboarding_fd_active(fd) || !command_id || !canonical_name) return -1;
	mode = Ply[fd].extr->onboarding_mode == ONBOARDING_ADMISSION_MODE_PROVISION ?
		ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION :
		Ply[fd].extr->onboarding_mode == ONBOARDING_ADMISSION_MODE_CLAIM ?
		ONBOARDING_ACTIVATION_BINDING_MODE_CLAIM :
		ONBOARDING_ACTIVATION_BINDING_MODE_INVALID;
	result = onboarding_activation_save_capability_capture(
		&Ply[fd].extr->onboarding_activation_save,
		Ply[fd].extr->onboarding_actor_id,
		Ply[fd].extr->onboarding_correlation_id,
		Ply[fd].extr->onboarding_character_id, mode, command_id, canonical_name);
	if(result != ONBOARDING_ACTIVATION_SAVE_CAPABILITY_OK &&
	   result != ONBOARDING_ACTIVATION_SAVE_CAPABILITY_DISABLED)
		fprintf(stderr, "MUD activation capture rejected: result=%d\n", (int)result);
	return result == ONBOARDING_ACTIVATION_SAVE_CAPABILITY_OK ||
		result == ONBOARDING_ACTIVATION_SAVE_CAPABILITY_DISABLED ? 0:-1;
}

static int onboarding_send_active(fd, command_id)
int fd;
const char *command_id;
{
	onboarding_control control;
	if(!command_id) return -1;
	memset(&control, 0, sizeof(control));
	control.kind = ONBOARDING_CONTROL_ACTIVE;
	strcpy(control.command_id, command_id);
	return onboarding_send_control(fd, &control);
}

static void onboarding_finish_activation(fd)
int fd;
{
	if(!onboarding_fd_active(fd)) return;
	strcpy(Ply[fd].extr->auth_user_id, Ply[fd].extr->onboarding_actor_id);
	strcpy(Ply[fd].extr->character_id, Ply[fd].extr->onboarding_character_id);
	memset(Ply[fd].extr->onboarding_actor_id, 0,
	       sizeof(Ply[fd].extr->onboarding_actor_id));
	memset(Ply[fd].extr->onboarding_correlation_id, 0,
	       sizeof(Ply[fd].extr->onboarding_correlation_id));
	memset(Ply[fd].extr->onboarding_character_id, 0,
	       sizeof(Ply[fd].extr->onboarding_character_id));
	Ply[fd].extr->onboarding_mode = 0;
	Ply[fd].extr->onboarding_state = (char)ONBOARDING_STATE_NEW;
	Ply[fd].extr->onboarding_activation_pending = 0;
	memset(Ply[fd].extr->onboarding_activation_command_id, 0,
	       sizeof(Ply[fd].extr->onboarding_activation_command_id));
}

#ifdef ONBOARDING_ACTIVATION_COMMAND_TESTING
static void onboarding_activation_command_test_return(fd, param, str)
int fd;
int param;
unsigned char *str;
{ (void)fd; (void)param; (void)str; }
#endif

static int onboarding_activation_complete(fd)
int fd;
{
	if(!onboarding_fd_active(fd) || !Ply[fd].ply) return -1;
	if(Ply[fd].extr->onboarding_mode == ONBOARDING_ADMISSION_MODE_PROVISION) {
		Ply[fd].ply->fd = fd;
		if(activate_staged_ply(Ply[fd].ply) < 0 || onboarding_send_active(fd,
			Ply[fd].extr->onboarding_activation_command_id) != 0) {
			return -1;
		}
		Ply[fd].extr->onboarding_world_staged = 0;
		onboarding_finish_activation(fd);
		print(fd, "[환영]이라고 치시면 초보자 분들에게 도움이 되는 많은 정보를 얻을수 있습니다.\n");
		print(fd, "레벨 5 가 되지 않으면 아이디가 삭제될 수도 있습니다.\n");
#ifdef ONBOARDING_ACTIVATION_COMMAND_TESTING
		Ply[fd].io->fn = onboarding_activation_command_test_return;
#else
		Ply[fd].io->fn = command;
#endif
		Ply[fd].io->fnparam = 1;
		return 0;
	}
	if(Ply[fd].extr->onboarding_mode == ONBOARDING_ADMISSION_MODE_CLAIM) {
		if(onboarding_send_active(fd,
			Ply[fd].extr->onboarding_activation_command_id) != 0) {
			return -1;
		}
		onboarding_zero_claim_credentials(fd, 0);
		onboarding_finish_activation(fd); disconnect(fd); return 0;
	}
	return -1;
}

#ifdef USE_M3_RUNTIME
static void onboarding_activation_wait(fd, param, str)
int fd;
int param;
unsigned char *str;
{ (void)fd; (void)param; (void)str; }

/* 0 completes, 1 retains PREPARED for host retry, -1 is terminal. */
static int onboarding_activation_gate_advance(fd, command_id)
int fd;
const char *command_id;
{
	onboarding_activation_binding_mode mode;
	onboarding_activation_gate_result result;
	if(!onboarding_fd_active(fd) || !Ply[fd].ply || !command_id) return -1;
	mode = Ply[fd].extr->onboarding_mode == ONBOARDING_ADMISSION_MODE_PROVISION ?
		ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION :
		Ply[fd].extr->onboarding_mode == ONBOARDING_ADMISSION_MODE_CLAIM ?
		ONBOARDING_ACTIVATION_BINDING_MODE_CLAIM :
		ONBOARDING_ACTIVATION_BINDING_MODE_INVALID;
	result = onboarding_activation_gate_attempt(
		&Ply[fd].extr->onboarding_activation_save, command_id,
		Ply[fd].extr->onboarding_actor_id,
		Ply[fd].extr->onboarding_correlation_id,
		Ply[fd].extr->onboarding_character_id, mode, Ply[fd].ply->name,
		Ply[fd].ply->name, Ply[fd].ply);
	if(result == ONBOARDING_ACTIVATION_GATE_BYPASS ||
	   result == ONBOARDING_ACTIVATION_GATE_CONSUMED) {
		memmove(Ply[fd].extr->onboarding_activation_command_id, command_id,
			strlen(command_id) + 1);
		return onboarding_activation_complete(fd);
	}
	if(result != ONBOARDING_ACTIVATION_GATE_RETAINED) return -1;
	memmove(Ply[fd].extr->onboarding_activation_command_id, command_id,
		strlen(command_id) + 1);
	Ply[fd].extr->onboarding_activation_pending = 1;
	Ply[fd].io->fn = onboarding_activation_wait; Ply[fd].io->fnparam = 0;
	return 1;
}
#endif

/* This is the single command lifecycle boundary: command handlers use it
 * after an accepted ACTIVATED, and the M3 idle boundary uses the same entry
 * to consume a retained PREPARED command.  The legacy build deliberately
 * bypasses the dormant M3 gate but retains its previous completion effects. */
static int onboarding_activation_lifecycle_advance(fd, command_id)
int fd;
const char *command_id;
{
#ifdef USE_M3_RUNTIME
	return onboarding_activation_gate_advance(fd, command_id);
#else
	if(!onboarding_fd_active(fd) || !Ply[fd].ply || !command_id) return -1;
	strcpy(Ply[fd].extr->onboarding_activation_command_id, command_id);
	return onboarding_activation_complete(fd);
#endif
}

#ifdef USE_M3_RUNTIME
/* Called only from the post-update serialized host idle boundary. */
void onboarding_activation_gate_idle_retry()
{
	int fd, result;
	for(fd = 0; fd < PMAX; fd++) {
		if(!Ply[fd].extr || !Ply[fd].extr->onboarding_activation_pending)
			continue;
		if(!Ply[fd].io || !Ply[fd].ply ||
		   !Ply[fd].extr->onboarding_activation_command_id[0]) {
			onboarding_fail(fd); continue;
		}
		result = onboarding_activation_lifecycle_advance(fd,
			Ply[fd].extr->onboarding_activation_command_id);
		if(result < 0) onboarding_fail(fd);
	}
}
#endif

#ifdef ONBOARDING_ACTIVATION_COMMAND_TESTING
/* The dynamic harness supplies one Gateway record through the descriptor's
 * real selected onboarding handler.  This exists only in test objects: it
 * neither bypasses parser/state checks nor creates a production input path. */
void onboarding_activation_command_test_deliver_activated(fd, command_id)
int fd;
const char *command_id;
{
	char line[ONBOARDING_ADMISSION_MAX_LINE + 1];
	int written;

	if(!onboarding_fd_active(fd) || !Ply[fd].io || !Ply[fd].io->fn ||
	   !command_id) return;
	written = snprintf(line, sizeof(line), "MUD1O ACTIVATED|%s", command_id);
	if(written < 0 || (unsigned long)written >= sizeof(line)) return;
	Ply[fd].io->fn(fd, Ply[fd].io->fnparam, (unsigned char *)line);
	memset(line, 0, sizeof(line));
}
#ifdef USE_M3_RUNTIME
void onboarding_activation_command_test_idle_retry()
{
	onboarding_activation_gate_idle_retry();
}
#endif
#endif

/* The EVIDENCE lane carries the separately canonical metadata envelope, not a
 * legacy onboarding_control.  It advances only after inspection, tuple match,
 * and formatting have all succeeded; all failures leave no SAVED/VERIFIED
 * fallback and are handled by the existing generic onboarding abort path. */
static int onboarding_send_evidence(fd, canonical_name, known_file_sha256)
int fd;
const char *canonical_name;
const char *known_file_sha256;
{
	char line[ONBOARDING_EVIDENCE_CONTROL_MAX_RECORD_LENGTH + 1];
	onboarding_state state;
	int result;

	memset(line, 0, sizeof(line));
	result = -1;
	if(!onboarding_fd_active(fd) ||
	   onboarding_evidence_emission_prepare(canonical_name, known_file_sha256,
					      line, sizeof(line)) !=
	       ONBOARDING_EVIDENCE_EMISSION_OK)
		goto out;
	state = (onboarding_state)Ply[fd].extr->onboarding_state;
	if(onboarding_state_apply_evidence(&state) != 0)
		goto out;
	if(scwrite(fd, line, (unsigned int)strlen(line)) < 0)
		goto out;
	Ply[fd].extr->onboarding_state = (char)state;
	result = 0;
out:
	memset(line, 0, sizeof(line));
	return result;
}

static int onboarding_parse_gateway_line(str, control)
unsigned char *str;
onboarding_control *control;
{
	char line[ONBOARDING_ADMISSION_MAX_LINE + 1];
	unsigned long length;

	if(!str || !control) return -1;
	length = (unsigned long)strlen((char *)str);
	if(!length || length >= ONBOARDING_ADMISSION_MAX_LINE) return -1;
	memcpy(line, str, length);
	line[length] = '\n';
	line[length + 1] = 0;
	return onboarding_parse_gateway_control(line, control);
}

/* While create_ply owns a human prompt, protocol controls must still never
 * become character input.  Only ABORT is meaningful at that point. */
int onboarding_control_during_wizard(fd, str)
int fd;
unsigned char *str;
{
	onboarding_control control;

	if(!onboarding_fd_active(fd) || !onboarding_session_is_protocol_line(str) ||
	   !Ply[fd].io) return 0;
	/* A retained M3 activation has no client-driven transition.  Drop every
	 * line without failing or clearing its exact descriptor capability. */
	if(Ply[fd].extr->onboarding_activation_pending) return 1;
	if((Ply[fd].io->fn == onboarding_provision &&
	    (Ply[fd].io->fnparam == 5 || Ply[fd].io->fnparam == 6 ||
	     Ply[fd].io->fnparam == 7)) ||
	   (Ply[fd].io->fn == onboarding_claim &&
	    (Ply[fd].io->fnparam == 3 || Ply[fd].io->fnparam == 5 ||
	     Ply[fd].io->fnparam == 6)))
		return 0;
	if(onboarding_parse_gateway_line(str, &control) == 0 &&
	   control.kind == ONBOARDING_CONTROL_ABORT)
		onboarding_apply_control(fd, &control, 1);
	onboarding_fail(fd);
	return 1;
}

/**********************************************************************/
/*                              login                                 */
/**********************************************************************/

/* This function is the first function that gets input from a player when */
/* he logs in.  It asks for the player's name and password, and performs  */
/* the according function calls.                                          */

void login(fd, param, str)
int     fd;
int     param;
unsigned char    *str;
{
	FILE *fp;
		int             i;
		int             load_result;
		extern int      Numplayers;
		unsigned char   tempstr[20], str2[50], str3[50], nastr[20];
		long            t;
		creature        *ply_ptr;
              struct stat f_stat;
              char            tmp[256];
char file[80];
              struct tm *login_tt;
              char *wday[7]={"일","월","화","수","목","금","토",};

		switch(param) {
	case -1:
				/* A disabled MUD1O ticket must not be reinterpreted as a
				 * legacy name after the welcome prompt. */
				if(onboarding_session_is_protocol_line(str)) {
					ticket_reject(fd, "MUD1 ERR\n", 9);
					return;
				}
				str[0]=0;
		case 0:
                            pass_num[fd]=0;
				if(strcmp(Ply[fd].extr->tempstr[0], str)) {
						/* disconnect(fd);
						return; */
						RETURN(fd, login, 1);
				}
				print(fd, "\n당신의 이름은 무엇입니까? ");
				RETURN(fd, login, 1);
		case 1:
				unsigned long name_len;
				unsigned long name_bytes;

				if(!utf8_validate((unsigned char *)str, (unsigned long)strlen(str))) {
					print(fd, "이름이 UTF-8 형식이 아닙니다.\n");
					print(fd, "\n당신의 이름은 무엇입니까? ");
					RETURN(fd, login, 1);
				}

				name_len = utf8_codepoint_len((unsigned char *)str);
				name_bytes = (unsigned long)strlen((char *)str);
				if(name_len == 0) {
						print(fd, "이름은 한 자 이상이어야 합니다.\n");
					print(fd, "\n당신의 이름은 무엇입니까? ");
					RETURN(fd, login, 1);
				}

				if(name_len > PLAYER_NAME_MAX_CODEPOINTS) {
						print(fd, "이름이 너무 깁니다.\n\n");
						print(fd, "당신의 이름은 무엇입니까? ");
						RETURN(fd, login, 1);
				}

				if(name_bytes > PLAYER_NAME_MAX_BYTES) {
					print(fd, "이름은 최대 %ld바이트까지 가능합니다.\n\n", (long)PLAYER_NAME_MAX_BYTES);
					print(fd, "당신의 이름은 무엇입니까? ");
					RETURN(fd, login, 1);
				}

			if(!player_name_is_valid((unsigned char *)str, PLAYER_NAME_MIN_CODEPOINTS, PLAYER_NAME_MAX_CODEPOINTS)) {
				print(fd, "이름에 사용할 수 없는 문자가 있습니다.\n\n");
				print(fd, "당신의 이름은 무엇입니까? ");
				RETURN(fd, login, 1);
			}

				lowercize(str, 1);
				if(player_recovery_login_blocked()) {
					print(fd, "서버가 이전 접속의 저장을 복구 중입니다. 잠시 후 다시 시도하십시오.\n");
					print(fd, "\n당신의 이름은 무엇입니까? ");
					RETURN(fd, login, 1);
				}
				last_login[fd]=0;
				if(player_path_from_name((char *)str, tmp, sizeof(tmp)) == 0) {
					if (!rp_stat(tmp,&f_stat)) last_login[fd]=f_stat.st_ctime;
				}

				load_result = load_ply((char *)str, &ply_ptr);
				if(load_result == PLAYER_STORE_NOT_FOUND) {
						strcpy(Ply[fd].extr->tempstr[0], str);
						print(fd, "\n%S%j 하시겠습니까(예/아니오)? ", str,"4");
						RETURN(fd, login, 2);
				}
				else if(load_result != PLAYER_STORE_OK) {
						scwrite(fd, "\n캐릭터 데이터를 읽을 수 없습니다. 관리자에게 문의해 주십시오.\n",
							(int)strlen("\n캐릭터 데이터를 읽을 수 없습니다. 관리자에게 문의해 주십시오.\n"));
						disconnect(fd);
						return;
				}

				else {
						ply_ptr->fd = -1;
						Ply[fd].ply = ply_ptr;
		  if(strcmp(str , Ply[fd].ply->name)) {

                        scwrite(fd, "\n데이타가 손상되었습니다.\n",25);
			if(str[0]==0 || pass_num[fd]>=3)
                                scwrite(fd,"접속을 끊습니다.",18);
				disconnect(fd);
				return;
			}
          if(F_ISSET(ply_ptr, SUICD)) {
/*
              uninit_ply(ply_ptr);
*/ 
                scwrite(fd, "\n자살 신청한 아이디입니다.\n", 18);
				disconnect(fd);
				return;
          } 
                                                if(checkdouble(ply_ptr->name)) {
                                                                scwrite(fd, "이상하군요.. -_-;.\n", 25);
								disconnect(fd);
								return;
						}
					  print(fd, "암호를 넣어 주십시요: ");
                                          print(fd,"%c%c%c", 255,251,1);
					  RETURN(fd, login, 3);
				}

		case 2:
				if(strcmp(str,"예") && str[0]!='y' && str[0]!='Y') {
						Ply[fd].extr->tempstr[0][0] = 0;
						print(fd, "당신의 이름은 무엇입니까? ");
						RETURN(fd, login, 1);
				}
				else {
						print(fd, "\n[엔터]를 누르십시요.");
						RETURN(fd, create_ply, 1);
				}

		case 3:
                check_item(Ply[fd].ply);      
				if(strcmp(str, Ply[fd].ply->password)) {
                                                scwrite(fd, "\n암호가 틀립니다. ", 18);
                                              pass_num[fd]++;
						if(str[0]==0 || pass_num[fd] >= 3) {
    							t = time(0);
							strcpy(str3, (char *)ctime(&t));
							str3[strlen(str3)-1] = 0;
							log_fl(str3, Ply[fd].ply->name);
                                                        scwrite(fd,"접속을 끊습니다.\n\n",18);
                                                        disconnect(fd);
                                                        return;
                                                }
                                                else {
                                                        print(fd,"다시 입력하십시요.\n암호를 다시 입력하세요: ");
                                                        RETURN(fd, login, 3);
                                                }
				}
				else {
						print(fd, "%c%c%c\n",255,252,1);
						strncpy((char *)tempstr, Ply[fd].ply->name, sizeof(tempstr)-1);
						tempstr[sizeof(tempstr)-1] = 0;
						for(i=0; i<Tablesize; i++)
								if(Ply[i].ply && i != fd)
										if(!strcmp(Ply[i].ply->name,
											   Ply[fd].ply->name))
												disconnect(i);
						if(player_recovery_login_blocked()) {
							scwrite(fd, "이전 접속의 저장을 복구하지 못했습니다. 접속을 끊습니다.\n",
								(int)strlen("이전 접속의 저장을 복구하지 못했습니다. 접속을 끊습니다.\n"));
							disconnect(fd);
							return;
						}
						free_crt(Ply[fd].ply);
				load_result = load_ply((char *)tempstr, &Ply[fd].ply);
				if(load_result != PLAYER_STORE_OK)
				{
                                                scwrite(fd, "Player nolonger exits!\n", 23);
						t = time(0);
						strcpy(str2, (char *)ctime(&t));
						str2[strlen(str2)-1] = 0;
						logn("player_load_error","%s: %s (%s) 재읽기 실패 (%d).\n",
								str2, tempstr, Ply[fd].io->address, load_result);
						disconnect(fd);
						return;
				}
						Ply[fd].ply->fd = fd;


	                                                if(init_ply(Ply[fd].ply) < 0) {
								scwrite(fd, "\n서버 초기화 중 오류가 발생했습니다.\n",
									(int)strlen("\n서버 초기화 중 오류가 발생했습니다.\n"));
								if(Ply[fd].ply) {
									free_crt(Ply[fd].ply);
									Ply[fd].ply = 0;
								}
								pass_num[fd] = 0;
								print(fd, "\n당신의 이름은 무엇입니까? ");
								RETURN(fd, login, 1);
							}
	                                                init_alias(Ply[fd].ply);


             if(last_login[fd]) {
                 login_tt=localtime(&last_login[fd]);
                 print(fd,"\n마지막 접속시간: %d월 %d일(%s) %d시 %d분입니다.\n",
                 login_tt->tm_mon+1, login_tt->tm_mday, wday[login_tt->tm_wday],
                 login_tt->tm_hour, login_tt->tm_min);
            }

						RETURN(fd, command, 1);
				}
		}
}

/* Ticket mode deliberately never falls through to login(): malformed,
 * expired, replayed, or unavailable tickets all receive the same small
 * protocol response and then lose the socket. */
void trusted_admission_login(fd, param, str)
int fd;
int param;
unsigned char *str;
{
	trusted_admission_ticket ticket;
	creature *ply_ptr;
	int i, load_result;

	(void)param;
	if(trusted_admission_validate((char *)str, time(0), &ticket) != 0 ||
	   player_recovery_login_blocked()) {
		ticket_reject(fd, "MUD1 ERR\n", 9);
		return;
	}

	/* Preflight proves that the requested canonical file is currently usable,
	 * but never becomes the session snapshot: another session can save newer
	 * state while it is being disconnected below. */
	load_result = load_ply(ticket.name, &ply_ptr);
	if(load_result != PLAYER_STORE_OK || !ply_ptr ||
	   strcmp(ply_ptr->name, ticket.name) != 0 || F_ISSET(ply_ptr, SUICD)) {
		if(ply_ptr) free_crt(ply_ptr);
		ticket_reject(fd, "MUD1 ERR\n", 9);
		return;
	}
	free_crt(ply_ptr);
	ply_ptr = 0;

	/* Disconnect the old owner before the authoritative re-load.  If its save
	 * enters recovery, no stale pre-disconnect snapshot may be accepted. */
	for(i=0; i<Tablesize; i++)
		if(Ply[i].ply && i != fd && !strcmp(Ply[i].ply->name, ticket.name))
			disconnect(i);
	if(player_recovery_login_blocked() || checkdouble(ticket.name)) {
		ticket_reject(fd, "MUD1 ERR\n", 9);
		return;
	}
	load_result = load_ply(ticket.name, &ply_ptr);
	if(load_result != PLAYER_STORE_OK || !ply_ptr ||
	   strcmp(ply_ptr->name, ticket.name) != 0 || F_ISSET(ply_ptr, SUICD)) {
		if(ply_ptr) free_crt(ply_ptr);
		ticket_reject(fd, "MUD1 ERR\n", 9);
		return;
	}

	Ply[fd].ply = ply_ptr;
	ply_ptr->fd = fd;
	check_item(ply_ptr);
	if(init_ply(ply_ptr) < 0) {
		free_crt(Ply[fd].ply);
		Ply[fd].ply = 0;
		ticket_reject(fd, "MUD1 ERR\n", 9);
		return;
	}
	init_alias(ply_ptr);
	strcpy(Ply[fd].extr->auth_user_id, ticket.user_id);
	strcpy(Ply[fd].extr->character_id, ticket.character_id);
	strcpy(Ply[fd].extr->admission_nonce, ticket.nonce);

	/* The acknowledgement intentionally precedes every legacy game byte. */
	scwrite(fd, "MUD1 OK\n", 8);
	RETURN(fd, command, 1);
}

/* MUD1O is a separate private lane.  MUD1 stays delegated to its unchanged
 * ticket-only implementation, while an unknown initial line gets no legacy
 * prompt and no protocol detail. */
void onboarding_admission_login(fd, param, str)
int fd;
int param;
unsigned char *str;
{
	onboarding_admission_ticket ticket;
	onboarding_state state;
	char line[ONBOARDING_ADMISSION_MAX_LINE + 1];
	const char *secret;
	unsigned long length;

	(void)param;
	if(str && !strncmp((char *)str, "MUD1|", 5)) {
		trusted_admission_login(fd, 1, str);
		return;
	}
	if(onboarding_session_mode() != 1 ||
	   !onboarding_session_is_protocol_line(str)) {
		scwrite(fd, "MUD1 ERR\n", 9);
		disconnect(fd);
		return;
	}
	length = (unsigned long)strlen((char *)str);
	if(!length || length >= ONBOARDING_ADMISSION_MAX_LINE) {
		onboarding_fail(fd);
		return;
	}
	memcpy(line, str, length);
	line[length] = '\n';
	line[length + 1] = 0;
	secret = getenv("MUD_ADMISSION_SECRET");
	if(player_recovery_login_blocked() ||
	   onboarding_session_validate_ticket(line, secret, time(0), &ticket) != 0) {
		onboarding_fail(fd);
		return;
	}
	state = ONBOARDING_STATE_NEW;
	if(onboarding_state_accept_ticket(&state, &ticket) != 0) {
		onboarding_fail(fd);
		return;
	}
	/* Only validated identity/correlation metadata live in extra.  The raw
	 * bearer ticket, HMAC, and admission secret never survive validation. */
	strcpy(Ply[fd].extr->onboarding_actor_id, ticket.user_id);
	strcpy(Ply[fd].extr->onboarding_correlation_id, ticket.correlation_id);
	strcpy(Ply[fd].extr->admission_nonce, ticket.nonce);
	Ply[fd].extr->onboarding_mode = ticket.mode;
	Ply[fd].extr->onboarding_state = (char)state;
	scwrite(fd, "MUD1O OK\n", 9);
	if(ticket.mode == ONBOARDING_ADMISSION_MODE_PROVISION)
		onboarding_provision(fd, 1, 0);
	else
		onboarding_claim(fd, 1, 0);
}

/* Provisioning preserves login()'s name -> confirmation -> [enter] sequence
 * before it reserves the canonical file name and starts create_ply. */
void onboarding_provision(fd, param, str)
int fd;
int param;
unsigned char *str;
{
	creature *ply_ptr;
	onboarding_control control;
	char onboarding_digest[ONBOARDING_ADMISSION_SHA256_HEX_LEN + 1];
	int load_result;

	switch(param) {
	case 1:
		print(fd, "\n당신의 이름은 무엇입니까? ");
		RETURN(fd, onboarding_provision, 2);
	case 2:
		if(onboarding_canonical_name(str) != 0 ||
		   player_recovery_login_blocked()) {
			onboarding_fail(fd);
			return;
		}
		ply_ptr = 0;
		load_result = load_ply((char *)str, &ply_ptr);
		if(ply_ptr) free_crt(ply_ptr);
		if(load_result != PLAYER_STORE_NOT_FOUND) {
			onboarding_fail(fd);
			return;
		}
		strcpy(Ply[fd].extr->tempstr[0], (char *)str);
		print(fd, "\n%S%j 하시겠습니까(예/아니오)? ", str, "4");
		RETURN(fd, onboarding_provision, 3);
	case 3:
		if(strcmp((char *)str,"예") && str[0]!='y' && str[0]!='Y') {
			Ply[fd].extr->tempstr[0][0] = 0;
			print(fd, "당신의 이름은 무엇입니까? ");
			RETURN(fd, onboarding_provision, 2);
		}
		print(fd, "\n[엔터]를 누르십시요.");
		RETURN(fd, onboarding_provision, 4);
	case 4:
		memset(&control, 0, sizeof(control));
		control.kind = ONBOARDING_CONTROL_RESERVE;
		if(onboarding_name_hex(Ply[fd].extr->tempstr[0], control.name_hex,
				       sizeof(control.name_hex)) != 0 ||
		   onboarding_send_control(fd, &control) != 0) {
			onboarding_fail(fd);
			return;
		}
		RETURN(fd, onboarding_provision, 5);
	case 5:
		if(onboarding_parse_gateway_line(str, &control) != 0 ||
		   control.kind != ONBOARDING_CONTROL_RESERVED ||
		   Ply[fd].extr->onboarding_character_id[0] ||
		   onboarding_apply_control(fd, &control, 1) != 0) {
			onboarding_fail(fd);
			return;
		}
		strcpy(Ply[fd].extr->onboarding_character_id, control.character_id);
		/* The reservation is now externally durable.  Before the wizard can
		 * publish a player file, persist only the reconciliation allowlist.
		 * The receipt API never receives a ticket, HMAC, password, or input. */
		if(onboarding_receipt_write_pending(
				Ply[fd].extr->onboarding_actor_id,
				Ply[fd].extr->onboarding_correlation_id,
				Ply[fd].extr->onboarding_character_id,
				Ply[fd].extr->tempstr[0], "player-v1") != 0) {
			onboarding_fail(fd);
			return;
		}
		create_ply(fd, 1, 0);
		return;
	case 6:
		if(onboarding_parse_gateway_line(str, &control) != 0 ||
		   control.kind != ONBOARDING_CONTROL_COMMIT ||
		   onboarding_apply_control(fd, &control, 1) != 0) {
			onboarding_fail(fd);
			return;
		}
		if(!Ply[fd].ply || !Ply[fd].extr->onboarding_world_staged ||
		   Ply[fd].ply->parent_rom) {
			onboarding_fail(fd);
			return;
		}
		memset(onboarding_digest, 0, sizeof(onboarding_digest));
		/* A committed receipt is retained for out-of-band DB/file
		 * reconciliation; it is never deleted on the socket fast path. */
		if(onboarding_session_file_sha256(Ply[fd].ply->name,
					 onboarding_digest) != 0 ||
		   onboarding_receipt_mark_committed(
				   Ply[fd].extr->onboarding_actor_id,
				   Ply[fd].extr->onboarding_correlation_id,
				   Ply[fd].extr->onboarding_character_id,
				   Ply[fd].extr->tempstr[0], "player-v1",
				   onboarding_digest) != 0) {
			onboarding_fail(fd);
			return;
		}
		/* COMMIT is a completion prerequisite, not the publication edge. */
		RETURN(fd, onboarding_provision, 7);
	case 7:
		if(onboarding_parse_gateway_line(str, &control) != 0 ||
		   control.kind != ONBOARDING_CONTROL_ACTIVATED ||
		   onboarding_apply_control(fd, &control, 1) != 0 ||
		   !Ply[fd].ply || !Ply[fd].extr->onboarding_world_staged ||
		   Ply[fd].ply->parent_rom ||
		   onboarding_write_activation_binding(fd, control.command_id) != 0 ||
		   onboarding_capture_activation_save_capability(fd, control.command_id,
			   Ply[fd].ply->name) != 0) {
			onboarding_fail(fd);
			return;
		}
		if(onboarding_activation_lifecycle_advance(fd, control.command_id) < 0)
			onboarding_fail(fd);
		return;
	default:
		onboarding_fail(fd);
		return;
	}
}

/* Claim waits for the Gateway's database-backed ALLOW before it enables
 * password echo-off. There is exactly one password comparison and no retry
 * branch; all failure reasons look identical to the private peer. */
void onboarding_claim(fd, param, str)
int fd;
int param;
unsigned char *str;
{
	creature *ply_ptr;
	onboarding_control control;
	char verified_digest[ONBOARDING_ADMISSION_SHA256_HEX_LEN + 1];
	int load_result, password_ok;

	switch(param) {
	case 1:
		print(fd, "\n당신의 이름은 무엇입니까? ");
		RETURN(fd, onboarding_claim, 2);
	case 2:
		if(onboarding_canonical_name(str) != 0 ||
		   player_recovery_login_blocked()) {
			onboarding_fail(fd);
			return;
		}
		ply_ptr = 0;
		load_result = load_ply((char *)str, &ply_ptr);
		if(load_result != PLAYER_STORE_OK || !ply_ptr ||
		   strcmp((char *)str, ply_ptr->name) != 0 || F_ISSET(ply_ptr, SUICD)) {
			if(ply_ptr) {
				onboarding_session_zeroize_claim_memory(
					ply_ptr->password, sizeof(ply_ptr->password), 0, 0);
				free_crt(ply_ptr);
			}
			onboarding_fail(fd);
			return;
		}
		strcpy(Ply[fd].extr->tempstr[0], (char *)str);
		Ply[fd].ply = ply_ptr;
		/* A claim read is not a gameplay session and must never re-save on
		 * failure, ABORT, or normal roster-refresh close. */
		Ply[fd].ply->fd = -1;
		memset(&control, 0, sizeof(control));
		control.kind = ONBOARDING_CONTROL_CHALLENGE;
		if(onboarding_name_hex(Ply[fd].extr->tempstr[0], control.name_hex,
			       sizeof(control.name_hex)) != 0 ||
		   onboarding_session_file_sha256(Ply[fd].extr->tempstr[0],
			   Ply[fd].extr->onboarding_claim_sha256) != 0) {
			memset(&control, 0, sizeof(control));
			onboarding_fail(fd);
			return;
		}
		strcpy(control.file_sha256, Ply[fd].extr->onboarding_claim_sha256);
		if(onboarding_send_control(fd, &control) != 0) {
			memset(&control, 0, sizeof(control));
			onboarding_fail(fd);
			return;
		}
		memset(&control, 0, sizeof(control));
		Ply[fd].extr->onboarding_claim_challenged_at = time(0);
		RETURN(fd, onboarding_claim, 3);
	case 3:
		if(!Ply[fd].extr->onboarding_claim_sha256[0] ||
		   !onboarding_session_claim_allow_live(
			Ply[fd].extr->onboarding_claim_challenged_at, time(0)) ||
		   onboarding_parse_gateway_line(str, &control) != 0 ||
		   control.kind != ONBOARDING_CONTROL_ALLOW ||
		   onboarding_apply_control(fd, &control, 1) != 0) {
			onboarding_fail(fd);
			return;
		}
		print(fd, "암호를 넣어 주십시요: ");
		print(fd, "%c%c%c", 255, 251, 1);
		RETURN(fd, onboarding_claim, 4);
	case 4:
		if(!onboarding_session_claim_allow_live(
			Ply[fd].extr->onboarding_claim_challenged_at, time(0))) {
			onboarding_zero_claim_credentials(fd, str);
			onboarding_fail(fd);
			return;
		}
		password_ok = Ply[fd].ply && strcmp((char *)str, Ply[fd].ply->password) == 0;
		/* The comparison is complete: erase the password field and every byte
		 * in the socket input buffer before taking any success/failure branch. */
		onboarding_zero_claim_credentials(fd, str);
		if(!password_ok) {
			onboarding_fail(fd);
			return;
		}
		memset(verified_digest, 0, sizeof(verified_digest));
		if(onboarding_session_file_sha256(Ply[fd].extr->tempstr[0],
					 verified_digest) != 0 ||
		   strcmp(verified_digest, Ply[fd].extr->onboarding_claim_sha256) != 0) {
			memset(verified_digest, 0, sizeof(verified_digest));
			onboarding_fail(fd);
			return;
		}
		memset(&control, 0, sizeof(control));
		if(onboarding_evidence_control_enabled()) {
			if(onboarding_send_evidence(fd, Ply[fd].extr->tempstr[0],
						   verified_digest) != 0) {
				memset(verified_digest, 0, sizeof(verified_digest));
				memset(&control, 0, sizeof(control));
				onboarding_fail(fd);
				return;
			}
		}
		else {
			control.kind = ONBOARDING_CONTROL_VERIFIED;
			if(onboarding_name_hex(Ply[fd].extr->tempstr[0], control.name_hex,
				       sizeof(control.name_hex)) != 0) {
				memset(verified_digest, 0, sizeof(verified_digest));
				memset(&control, 0, sizeof(control));
				onboarding_fail(fd);
				return;
			}
			strcpy(control.file_sha256, verified_digest);
			if(onboarding_send_control(fd, &control) != 0) {
				memset(verified_digest, 0, sizeof(verified_digest));
				memset(&control, 0, sizeof(control));
				onboarding_fail(fd);
				return;
			}
		}
		memset(verified_digest, 0, sizeof(verified_digest));
		memset(&control, 0, sizeof(control));
		onboarding_clear_claim_transient(fd);
		RETURN(fd, onboarding_claim, 5);
	case 5:
		if(onboarding_parse_gateway_line(str, &control) != 0 ||
		   control.kind != ONBOARDING_CONTROL_CLAIMED ||
		   Ply[fd].extr->onboarding_character_id[0] ||
		   onboarding_apply_control(fd, &control, 1) != 0) {
			onboarding_fail(fd);
			return;
		}
		strcpy(Ply[fd].extr->onboarding_character_id, control.character_id);
		onboarding_zero_claim_credentials(fd, 0);
		RETURN(fd, onboarding_claim, 6);
	case 6:
		if(onboarding_parse_gateway_line(str, &control) != 0 ||
		   control.kind != ONBOARDING_CONTROL_ACTIVATED ||
		   onboarding_apply_control(fd, &control, 1) != 0 ||
		   onboarding_write_activation_binding(fd, control.command_id) != 0 ||
		   !Ply[fd].ply ||
		   onboarding_capture_activation_save_capability(fd, control.command_id,
			   Ply[fd].ply->name) != 0) {
			onboarding_fail(fd);
			return;
		}
		if(onboarding_activation_lifecycle_advance(fd, control.command_id) < 0)
			onboarding_fail(fd);
		return;
	default:
		onboarding_fail(fd);
		return;
	}
}

/**********************************************************************/
/*                              create_ply                            */
/**********************************************************************/

/* This function allows a new player to create his or her character. */

void create_ply(fd, param, str)
int     fd;
int     param;
char    *str;
{
		int     i, k, l, n, sum;
		int		save_result;
		int     num[5];
		onboarding_control onboarding_control_value;
		char onboarding_digest[ONBOARDING_ADMISSION_SHA256_HEX_LEN + 1];

		switch(param) {
		case 1:
				print(fd,"\n\n");
				Ply[fd].ply = (creature *)malloc(sizeof(creature));
				if(!Ply[fd].ply)
						merror("create_ply", FATAL);
				zero(Ply[fd].ply, sizeof(creature));
				Ply[fd].ply->fd = -1;
				Ply[fd].ply->rom_num = 1;
				print(fd, "당신은 남자입니까, 여자입니까(남자/여자)? ");
				RETURN(fd, create_ply, 2);
		case 2:
				if(strncmp(str,"남", 2) && strncmp(str,"여",2)) {
						print(fd, "입력이 잘못되었습니다.\n\n당신은 남자입니까, 여자입니까(남자/여자)? ");
						RETURN(fd, create_ply, 2);
				}
				if(!strncmp(str,"남",2))
						F_SET(Ply[fd].ply, PMALES);
				print(fd, "\n다음과 같은 직업이 있습니다.\n");
				print(fd, "1.자  객  2.권법가  3.불제자  4.검  사\n");
				print(fd, "5.도술사  6.무  사  7.포  졸  8.도  둑\n");
				print(fd, "직업을 고르세요: ");
				RETURN(fd, create_ply, 3);
		case 3:
				switch(low(str[0])) {
						case '1': Ply[fd].ply->class = ASSASSIN; break;
						case '2': Ply[fd].ply->class = BARBARIAN; break;
						case '3': Ply[fd].ply->class = CLERIC; break;
						case '4': Ply[fd].ply->class = FIGHTER; break;
						case '5': Ply[fd].ply->class = MAGE; break;
						case '6': Ply[fd].ply->class = PALADIN; break;
						case '7': Ply[fd].ply->class = RANGER; break;
						case '8': Ply[fd].ply->class = THIEF; break;
						default: print(fd, "직업을 고르세요: ");
								 RETURN(fd, create_ply, 3);
				}
				print(fd, "\n당신은 54점으로 다음 5가지 능력치를 구성할수 있습니다.\n\
3이상 18이하의 수치로 ## ## ## ## ##의 형식으로 5가지 능력치를 적어주십시요.\n\
능력: 힘 민첩 맷집 지식 신앙심\n예: 12 10 12 10 10\n\n");
/* Strength, Dexterity, Constitution, Intelligence, Piety. */

				print(fd, ": ");
				RETURN(fd, create_ply, 4);
		case 4:
				n = strlen(str); l = 0; k = 0;
				for(i=0; i<=n; i++) {
						if(str[i]==' ' || str[i]==0) {
								str[i] = 0;
								num[k++] = atoi(&str[l]);
								l = i+1;
						}
						if(k>4) break;
				}
				if(k<5) {
						print(fd, "5가지 능력치 모두를 위의 형식대로 적어 주십시요.\n");
						print(fd, ": ");
						RETURN(fd, create_ply, 4);
				}
				sum = 0;
				for(i=0; i<5; i++) {
						if(num[i] < 3 || num[i] > 18) {
								print(fd, "각 능력치는 3이상 18이하로 설정해야 합니다.\n");
								print(fd, ": ");
								RETURN(fd, create_ply, 4);
						}
						sum += num[i];
				}
				if(sum > 54) {
						print(fd, "각 능력치의 합이 54점을 초과할수 없습니다.\n");
						print(fd, ": ");
						RETURN(fd, create_ply, 4);
				}
				Ply[fd].ply->strength = num[0];
				Ply[fd].ply->dexterity = num[1];
				Ply[fd].ply->constitution = num[2];
				Ply[fd].ply->intelligence = num[3];
				Ply[fd].ply->piety = num[4];
				print(fd, "\n당신에게 익숙한 무기를 고르십시요.\n");
				print(fd, "1.도   2.검   3.봉   4.창   5.궁.\n");
				print(fd, ": ");
				RETURN(fd, create_ply, 5);
		case 5:
				switch(low(str[0])) {
						case '1': Ply[fd].ply->proficiency[0]=1024; break;
						case '2': Ply[fd].ply->proficiency[1]=1024; break;
						case '3': Ply[fd].ply->proficiency[2]=1024; break;
						case '4': Ply[fd].ply->proficiency[3]=1024; break;
						case '5': Ply[fd].ply->proficiency[4]=1024; break;
						default: print(fd, "다시 고르세요.\n: ");
								 RETURN(fd, create_ply, 5);
				}
				print(fd, "\n선한 구성원은 다른사람을 공격하지 못하고 공격 받을수도 없으며");
				print(fd, "\n그 구성원에게서 물건을 훔칠수도 없습니다.");
				print(fd, "\n그러나 악한 구성원은 공격할수도 있고 물건을 훔칠수도 있으며");
				print(fd, "\n다른 악한 구성원들에게 공격을 받을 수도 있습니다.\n");
				print(fd, "\n성향을 고르십시요(선함/악함): ");
				RETURN(fd, create_ply, 6);
		case 6:
				if(!strncmp(str,"악",2))
						F_SET(Ply[fd].ply, PCHAOS);
				else if(!strncmp(str,"선",2))
						F_CLR(Ply[fd].ply, PCHAOS);
				else {
						print(fd, "성향을 고르십시요(선함/악함): ");
						RETURN(fd, create_ply, 6);
				}
				print(fd, "\n다음과 같은 종족들이 있습니다.");
				print(fd, "\n1.난장이족  2.용 신 족  3.땅귀신족 4.요 괴 족");
				print(fd, "\n5.거 인 족  6.토 신 족  7.인 간 족 8.도깨비족");
				print(fd, "\n종족을 고르십시요: ");
				RETURN(fd, create_ply, 7);
		case 7:
				switch(low(str[0])) {
				case '1': Ply[fd].ply->race = DWARF; break;
				case '2': Ply[fd].ply->race = ELF; break;
				case '3': Ply[fd].ply->race = GNOME; break;
				case '8': Ply[fd].ply->race = ORC; break;
				case '4': Ply[fd].ply->race = HALFELF; break;
				case '5': Ply[fd].ply->race = HALFGIANT; break;
				case '6': Ply[fd].ply->race = HOBBIT; break;
				case '7': Ply[fd].ply->race = HUMAN; break;
				}
				if(!Ply[fd].ply->race) {
						print(fd, "\n종족을 고르십시요: ");
						RETURN(fd, create_ply, 7);
				}

				switch(Ply[fd].ply->race) {
				case DWARF:
						Ply[fd].ply->strength++;
						Ply[fd].ply->piety--;
						break;
				case ELF:
						Ply[fd].ply->intelligence+=2;
						Ply[fd].ply->constitution--;
						Ply[fd].ply->strength--;
						break;
				case GNOME:
						Ply[fd].ply->piety++;
						Ply[fd].ply->strength--;
						break;
				case HALFELF:
						Ply[fd].ply->intelligence++;
						Ply[fd].ply->constitution--;
						break;
				case HOBBIT:
						Ply[fd].ply->dexterity++;
						Ply[fd].ply->strength--;
						break;
				case HUMAN:
						Ply[fd].ply->constitution++;
						break;
				case ORC:
						Ply[fd].ply->strength++;
						Ply[fd].ply->constitution++;
						Ply[fd].ply->dexterity--;
						Ply[fd].ply->intelligence--;
						break;
				case HALFGIANT:
						Ply[fd].ply->strength+=2;
						Ply[fd].ply->intelligence--;
						Ply[fd].ply->piety--;
						break;
				}

				print(fd, "\n새 암호를 넣으십시요(3자이상 14자이하): ");
                                print(fd,"%c%c%c",255,251,1);
				RETURN(fd, create_ply, 8);
		case 8:
				if(strlen(str) > 14) {
						print(fd, "입력된 암호가 너무 깁니다.\n암호를 다시 넣으십시요(3자이상 14자이하): ");
						RETURN(fd, create_ply, 8);
				}
				if(strlen(str) < 3) {
						print(fd, "입력된 암호가 너무 짧습니다.\n암호를 다시 넣으십시요(3자이상 14자이하): ");
						RETURN(fd, create_ply, 8);
				}
				strncpy(Ply[fd].ply->password, str, sizeof(Ply[fd].ply->password) - 1);
				Ply[fd].ply->password[sizeof(Ply[fd].ply->password) - 1] = 0;
				strcpy(Ply[fd].ply->name, Ply[fd].extr->tempstr[0]);
				up_level(Ply[fd].ply);
				Ply[fd].ply->fd = fd;
				if((onboarding_fd_provisioning(fd) ?
				    init_staged_ply(Ply[fd].ply) :
				    init_ply(Ply[fd].ply)) < 0) {
					if(onboarding_fd_provisioning(fd)) {
						onboarding_fail(fd);
						return;
					}
					scwrite(fd, "\n서버 초기화 중 오류가 발생했습니다.\n",
						(int)strlen("\n서버 초기화 중 오류가 발생했습니다.\n"));
					if(Ply[fd].ply) {
						free_crt(Ply[fd].ply);
						Ply[fd].ply = 0;
					}
					pass_num[fd] = 0;
					print(fd, "\n당신의 이름은 무엇입니까? ");
					RETURN(fd, login, 1);
				}
				if(onboarding_fd_provisioning(fd))
					Ply[fd].extr->onboarding_world_staged = 1;
	                               init_alias(Ply[fd].ply);
		F_SET(Ply[fd].ply,PLECHO);
		F_SET(Ply[fd].ply,PPROMP);
                Ply[fd].ply->gold = 500;
				print(fd, "%c%c%c\n",255,252,1);
				save_result = save_ply(Ply[fd].ply->name, Ply[fd].ply);
				if(save_result != PLAYER_STORE_OK) {
					if(onboarding_fd_provisioning(fd)) {
						onboarding_fail(fd);
						return;
					}
					merror("create_ply", NONFATAL);
					print(fd, "새 캐릭터를 저장하지 못했습니다. 현재 접속을 유지하는 동안 저장 명령으로 다시 시도하십시오.\n");
					RETURN(fd, command, 1);
				}
				if(onboarding_fd_provisioning(fd)) {
					memset(&onboarding_control_value, 0,
						   sizeof(onboarding_control_value));
					memset(onboarding_digest, 0, sizeof(onboarding_digest));
					onboarding_control_value.kind = ONBOARDING_CONTROL_SAVED;
					strcpy(onboarding_control_value.character_id,
					       Ply[fd].extr->onboarding_character_id);
					strcpy(onboarding_control_value.storage_format, "player-v1");
					if(!onboarding_control_value.character_id[0] ||
					   onboarding_session_file_sha256(Ply[fd].ply->name,
								   onboarding_digest) != 0) {
						onboarding_fail(fd);
						return;
					}
					/* The player file and its exact hash are durable before this
					 * replacement.  Thus a crash before SAVED leaves a `saved`
					 * receipt for the reconciler, not an unprovable orphan file. */
					if(onboarding_receipt_mark_saved(
							Ply[fd].extr->onboarding_actor_id,
							Ply[fd].extr->onboarding_correlation_id,
							Ply[fd].extr->onboarding_character_id,
							Ply[fd].extr->tempstr[0], "player-v1",
							onboarding_digest) != 0) {
						onboarding_fail(fd);
						return;
					}
					if(onboarding_evidence_control_enabled()) {
						if(onboarding_send_evidence(fd,
								Ply[fd].extr->tempstr[0],
								onboarding_digest) != 0) {
							onboarding_fail(fd);
							return;
						}
					}
					else {
						strcpy(onboarding_control_value.file_sha256, onboarding_digest);
						if(onboarding_send_control(fd, &onboarding_control_value) != 0) {
							onboarding_fail(fd);
							return;
						}
					}
					/* Before COMMIT this remains a saved file, not a live gameplay
					 * owner.  disconnect() therefore frees it without a second save. */
					Ply[fd].ply->fd = -1;
					RETURN(fd, onboarding_provision, 6);
				}

				print(fd, "[환영]이라고 치시면 초보자 분들에게 도움이 되는 많은 정보를 얻을수 있습니다.\n");
				print(fd, "레벨 5 가 되지 않으면 아이디가 삭제될 수도 있습니다.\n");

				RETURN(fd, command, 1);
		}
}

/**********************************************************************/
/*                              command                               */
/**********************************************************************/

/* This function handles the main prompt commands, and calls the        */
/* appropriate function, depending on what service is requested by the  */
/* player.                                                              */

void command(fd, param, str)
int     fd;
int     param;
char    *str;
{
		cmd     cmnd;
		int     n;
		unsigned char ch;
		int     i;
		char buf[256];

#ifdef RECORD_ALL
/*
this logn commands wil print out all the commands entered by players.
It should be used in extreme case hen trying to isolate a players
input which causes a crash.
*/

logn("all_cmd","\n%s-%d (%d): %s\n",Ply[fd].ply->name,fd,Ply[fd].ply->rom_num,str);
#endif /* RECORD_ALL */

		switch(param) {
		case 1:

				if(F_ISSET(Ply[fd].ply, PHEXLN)) {
						for(n=0;n<strlen(str);n++) {
								ch = str[n];
								print(fd, "%02X", ch);
						}
						print(fd, "\n");
				}

				if(!strcmp(str, "!"))
						strncpy(str, Ply[fd].extr->lastcommand, 79);
				else if(str[0]=='!') {
						strncpy(buf, Ply[fd].extr->lastcommand, 79);
						strncat(buf,&str[1],79);
						strncpy(str,buf,79);
				}

				if(str[0]) {
						for(n=0; str[n] && str[n] == ' '; n++) ;
						strncpy(Ply[fd].extr->lastcommand, &str[n], 79);
				}

				strncpy(cmnd.fullstr, str, 255);
				lowercize(str, 0);
				parse(str, &cmnd); n = 0;

				/* 파서 분석 시험용..  */
/*      print(fd,"파서 분석....\n");
		for(i=0;i<cmnd.num;i++) print(fd,"%d: %s %d\n",i+1,cmnd.str[i],cmnd.val[i]);
*/
				if(cmnd.num){
					if((n=alias_cmd(Ply[fd].ply,&cmnd))==4000) n = process_cmd(fd, &cmnd);
				} else
						n = PROMPT;

				if(n == DISCONNECT) {
                                                scwrite(fd, "안녕히 가십시요!\n\n\n", 19);
						disconnect(fd);
						return;
				}
				else if(n == PROMPT) {
						if(F_ISSET(Ply[fd].ply, PPROMP))
								sprintf(str, "(%d 체력 %d 도력): ",
										Ply[fd].ply->hpcur, Ply[fd].ply->mpcur);
						else
								strcpy(str, ": ");
                                                scwrite(fd, str, strlen(str));
                                                if(Spy[fd] > -1) scwrite(Spy[fd], str, strlen(str));
				}

				if(n != DOPROMPT) {
						RETURN(fd, command, 1);
				}
				else
						return;
		}
}

/**********************************************************************/
/*                              parse                                 */
/**********************************************************************/

/* This function takes the string in the first parameter and breaks it */
/* up into its component words, stripping out useless words.  The      */
/* resulting words are stored in a command structure pointed to by the */
/* second argument.                                                    */

void parse(str, cmnd)
char    *str;
cmd     *cmnd;
{
		int     i, j, l, m, n, o, art;
		unsigned char   tempstr[25];

		l = m = n = 0;
		j = strlen(str);

		/* include for processing hangul command */
		if(j!=0)
		if(str[j-1]==' ' || str[j-1]=='.' || str[j-1]=='!' || str[j-1]=='?') {
				strncpy(cmnd->str[n++],"말",20);
				cmnd->val[m] = 1L;
		}
		else {
				i=j;
				while(i>=0 && str[i]!=' ') i--;
				if(i>=0) str[i]=0;
				j=i; i++;
				strncpy(cmnd->str[n++], &str[i], 20);
				cmnd->val[m] = 1L;
		}
		/* --- end --- */

		for(i=0; i<=j; i++) {
				if(str[i] == ' ' || str[i] == '#' || str[i] == 0) {
						str[i] = 0;     /* tokenize */

						/* Strip extra white-space */
						while((str[i+1] == ' ' || str[i] == '#') && i < j+1)
								str[++i] = 0;

						strncpy(tempstr, &str[l], 24); tempstr[24] = 0;
						l = i+1;
						if(!strlen(tempstr)) continue;

						/* Ignore article/useless words */
						/*
						o = art = 0;
						while(article[o][0] != '@') {
								if(!strcmp(article[o++], tempstr)) {
										art = 1;
										break;
								}
						}
						if(art) continue;
						*/

						/* Copy into command structure */
						if(n == m) {
								strncpy(cmnd->str[n++], tempstr, 20);
								cmnd->val[m] = 1L;
						}
						else if(is_number(tempstr) || (tempstr[0] == '-' &&
								is_number(tempstr+1))) {
								cmnd->val[m++] = atol(tempstr);
						}
						else {
								strncpy(cmnd->str[n++], tempstr, 20);
								cmnd->val[m++] = 1L;
						}

				}
				if(m >= COMMANDMAX) {
						n = 5;
						break;
				}
		}

		if(n > m)
				cmnd->val[m++] = 1L;
		cmnd->num = n;

}

/**********************************************************************/
/*                              process_cmd                           */
/**********************************************************************/

/* This function takes the command structure of the person at the socket */
/* in the first parameter and interprets the person's command.           */

int process_cmd(fd, cmnd)
int     fd;
cmd     *cmnd;
{
		int     match=0, cmdno=0, c=0, n;
		int current_deep,match_deep=0;

		do {
				if(!strcmp(cmnd->str[0], cmdlist[c].cmdstr)) {
						match = 1;
						cmdno = c;
						break;
				}
				else if((current_deep=str_compare(cmnd->str[0], cmdlist[c].cmdstr))!=0) {
						match = 1;
						if(match_deep==0 || match_deep>current_deep) {
								cmdno = c;
								match_deep = current_deep;
						}
				}
				c++;
		} while(cmdlist[c].cmdno);

		if(match == 0 || (cmnd->str[0][0]=='*' &&
                  (Ply[fd].ply->class<CARETAKER &&
                   Ply[fd].ply->class!=ZONEMAKER))) {
				print(fd, "\"%s\": 이런 명령어는 없네요.",
					  cmnd->str[0]);
				Ply[fd].io->fn = command; Ply[fd].io->fnparam = 1; return(0);
		}
/* 가장 비슷한 명령어를 쓰기 위해 수정.. 여러 낱말이 나올경우 에러처리=>사용
		else if(match > 1) {
				print(fd, "명령을 잘 모르겠네요.");
				RETURN(fd, command, 1);
		}
*/
		if(cmdlist[cmdno].cmdno < 0)
				return(special_cmd(Ply[fd].ply, 0-cmdlist[cmdno].cmdno, cmnd));

		/* DM command log */
		if(cmnd->str[0][0]=='*') {
		    log_dmcmd("%s : %s :\n",Ply[fd].ply->name,cmnd->fullstr);
		}

		return((*cmdlist[cmdno].cmdfn)(Ply[fd].ply, cmnd));

}

#ifdef CHECKDOUBLE

int checkdouble(name)
char *name;
{
		char    path[128], tempname[80];
		FILE    *fp;
		int     rtn=0;

		sprintf(path, "%s/simul/%s", PLAYERPATH, name);
		fp = rp_fopen(path, "r");
		if(!fp)
				return(0);

		while(!feof(fp)) {
				fgets(tempname, 80, fp);
				tempname[strlen(tempname)-1] = 0;
				if(!strcmp(tempname, name))
						continue;
				if(find_who(tempname)) {
						rtn = 1;
						break;
				}
		}

		fclose(fp);
		return(rtn);
}

#endif

char cut_paste_chr;

int cut_command(str)
char *str;
{
	int i;
	
	for(i=strlen(str)-1;i>=0;i--) {
		if(str[i]==' ') {
			break;
		}
	}
	if(i<0) i=0;
	cut_paste_chr=str[i];
	str[i]=0;

	return i;
}

void paste_command(str,index)
char *str; int index;
{
	str[index]=cut_paste_chr;
}

int ishan(str)
unsigned char *str;
{
	unsigned long i, n, c, cp;

	if(!str)
		return 0;

	n = (unsigned long)strlen((char *)str);
	if(n == 0)
		return 0;

	i = 0;
	while(i < n) {
		if(!utf8_next_codepoint(str + i, n - i, &c, &cp))
			return 0;
		if(cp < 0xAC00UL || cp > 0xD7A3UL)
			return 0;
		i += c;
	}

	return 1;
}

int is_hangul(str)
unsigned char *str;    /* one character */
{
	unsigned long c, cp, n;

	if(!str)
		return 0;

	n = (unsigned long)strlen((char *)str);
	if(!utf8_next_codepoint(str, n, &c, &cp))
		return 0;
	if(cp < 0xAC00UL || cp > 0xD7A3UL)
		return 0;

	return 1;
}

int under_han(str)
unsigned char *str;
{
	unsigned char tmp[512];
	unsigned long len;
	char *p;

	if(!str)
		return 0;

	len = (unsigned long)strlen((char *)str);
	if(len == 0)
		return 0;
	if(len >= sizeof(tmp))
		len = sizeof(tmp) - 1;

	memcpy(tmp, str, len);
	tmp[len] = 0;

	if(len > 0 && tmp[len - 1] == ')') {
		p = strrchr((char *)tmp, '(');
		if(p)
			*p = 0;
	}

	return utf8_has_jongseong_last(tmp);
}


char *first_han(str)
unsigned char *str;
{
    unsigned char high,low;
    int len,i;
    char *p = "temp";
    static unsigned char *exam[]={
        "가", "가", "나", "다", "다",
        "라", "마", "바", "바", "사",
        "사", "아", "자", "자", "차", 
        "카", "타", "파", "하", "" };
    static unsigned char *johab_exam[]={
        "늏", "똞", "륾", "봞", "쁝",
        "쏿", "쟞", "쨅", "쮉", "촡",
        "캻", "큑", "퇫", "펈", "픞",
        "훍", "?a", "?a", "?a", "" };

    len=strlen(str);
    if(len<2) return p;

    high=str[0];
    low=str[1];

    if(!is_hangul(&str[0])) return p;
    high=(KStbl[(high-0xb0)*94+low-0xa1] >> 8) & 0x7c;
    for(i=0;johab_exam[i][0];i++) {
        low= (johab_exam[i][0] & 0x7f);
        if(low==high) return exam[i];
    }
    return p;
}

int is_number(str)
unsigned char *str;
{
	int i;
	for(i=0;i<strlen(str);i++) {
		if(!isdigit(str[i])) return 0;
	}
	return 1;
}

int return_square(ply_ptr,cmnd)
creature    *ply_ptr;
cmd     *cmnd;
{
	room    *rom_ply;
	int fd;
        ctag    *cp;

	rom_ply=ply_ptr->parent_rom;
	fd=ply_ptr->fd;

	if(ply_is_attacking(ply_ptr,cmnd)) {
		print(fd,"당신은 싸우고 있는 중입니다!!");
		return 0;
	}

	if(rom_ply->rom_num==1001) {
		print(fd,"당신은 이미 광장에 와 있습니다!");
		return 0;
	}

        if(ply_ptr->following) {
            cp = ply_ptr->following->first_fol;
        }
        else {
            cp = ply_ptr->first_fol;
        }
        if(cp){
            print(fd,"먼저 그룹에서 나오세요.");
            return(0);
        }

	if(ply_ptr->level>20 && ply_ptr->class<INVINCIBLE) {
		print(fd, "당신이 귀환하려하자 흑암의 세력이 당신의 도력을 뺏습니다.\n");
		ply_ptr->mpcur = 0;
		/*
		ply_ptr->experience -= (ply_ptr->experience)/100000;
		*/
	}


	print(fd, "당신이 \"귀환!\"이라고 외치자 이상한 힘에 의해 어딘가로 빨려들어갑니다.");
	if(!F_ISSET(ply_ptr,PDMINV)) broadcast_rom(fd,ply_ptr->rom_num,"\n%m님이 갑자기 사라집니다!",ply_ptr);

	del_ply_rom(ply_ptr,rom_ply);
	if(!F_ISSET(ply_ptr,PFRTUN))
		load_rom(1001,&rom_ply);
	else	
		load_rom(3300 + ply_ptr->daily[DL_EXPND].max, &rom_ply);
	add_ply_rom(ply_ptr,rom_ply);
	if(!F_ISSET(ply_ptr,PDMINV)) broadcast_rom(fd,ply_ptr->rom_num, "\n%m님이 갑자기 자욱한 연기와 함께 나타났습니다!",ply_ptr);
	return 0;
}

int ply_is_attacking(ply_ptr,cmnd)
creature        *ply_ptr;
cmd             *cmnd;
{
    room            *rom_ptr;
    ctag            *cp;
    creature        *crt_ptr;

    rom_ptr=ply_ptr->parent_rom;

    cp = rom_ptr->first_mon;
    while(cp) {
        if(find_enm_crt(ply_ptr->name,cp->crt)>-1) return 1;
        cp = cp->next_tag;
    }
    return 0;
}

int str_compare(str1,str2)
unsigned char *str1,*str2;
{
	unsigned long i, j, n1, n2, c1, c2, cp1, cp2;

	n1 = (unsigned long)strlen((char *)str1);
	n2 = (unsigned long)strlen((char *)str2);
	if(!utf8_validate(str1, n1) || !utf8_validate(str2, n2)) {
		i = 0;
		while(str1[i] != 0 && str2[i] != 0 && str1[i] == str2[i])
			i++;
		if(str1[i] != 0)
			return 0;
		return (int)i;
	}

	i = 0;
	j = 0;
	while(i < n1 && j < n2) {
		if(!utf8_next_codepoint(str1 + i, n1 - i, &c1, &cp1))
			break;
		if(!utf8_next_codepoint(str2 + j, n2 - j, &c2, &cp2))
			break;
		if(cp1 != cp2)
			break;
		i += c1;
		j += c2;
	}

	if(i != n1)
		return 0;
	return (int)i;
}

char *cut_space(str)
char *str;
{
	static char buf[512];
	int i;

	strcpy(buf,str);
	for(i=strlen(buf)-1;i>=0;i--) {
		if(buf[i]!=' ') break;
	}
	buf[i+1]=0;

	return buf;
}
