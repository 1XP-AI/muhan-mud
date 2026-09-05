#include "onboarding_admission.h"
#include "trusted_admission.h"

#include <limits.h>
#include <stdio.h>
#include <string.h>

static unsigned long oa_bounded_strlen(value, limit)
const char *value;
unsigned long limit;
{
    unsigned long length;
    if(!value) return limit + 1;
    for(length=0; length<=limit; length++)
        if(value[length] == 0) return length;
    return limit + 1;
}

static int oa_copy_value(destination, destination_size, value)
char *destination;
unsigned long destination_size;
const char *value;
{
    unsigned long length;
    if(!destination || !destination_size || !value) return -1;
    length = oa_bounded_strlen(value, destination_size - 1);
    if(length >= destination_size) return -1;
    memcpy(destination, value, length);
    destination[length] = 0;
    return 0;
}

static int oa_lower_hex(c)
char c;
{
    return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f');
}

static int oa_lower_hex_value(value, expected_length)
const char *value;
unsigned long expected_length;
{
    unsigned long i;
    if(!value || oa_bounded_strlen(value, expected_length) != expected_length)
        return 0;
    for(i=0; i<expected_length; i++) if(!oa_lower_hex(value[i])) return 0;
    return 1;
}

static int oa_uuid(value)
const char *value;
{
    int i;
    if(!value || oa_bounded_strlen(value, ONBOARDING_ADMISSION_UUID_LEN) !=
       ONBOARDING_ADMISSION_UUID_LEN) return 0;
    for(i=0; i<ONBOARDING_ADMISSION_UUID_LEN; i++) {
        if(i == 8 || i == 13 || i == 18 || i == 23) {
            if(value[i] != '-') return 0;
        }
        else if(!oa_lower_hex(value[i])) return 0;
    }
    return 1;
}

static int oa_time_value(value, parsed)
const char *value;
long *parsed;
{
    unsigned long i, length;
    long result;
    if(!value || !parsed) return 0;
    length = oa_bounded_strlen(value, 20);
    if(!length || length > 19) return 0;
    result = 0;
    for(i=0; i<length; i++) {
        long digit;
        if(value[i] < '0' || value[i] > '9') return 0;
        digit = value[i] - '0';
        if(result > (LONG_MAX - digit) / 10) return 0;
        result = result * 10 + digit;
    }
    *parsed = result;
    return 1;
}

static int oa_secret_valid(secret)
const char *secret;
{
    unsigned long i, length;
    if(!secret) return 0;
    length = oa_bounded_strlen(secret, 512);
    if(length < 32 || length > 512) return 0;
    for(i=0; i<length; i++)
        if((unsigned char)secret[i] < 0x20 || (unsigned char)secret[i] > 0x7e)
            return 0;
    return 1;
}

static int oa_constant_time_equal(left, right, length)
const char *left;
const char *right;
unsigned long length;
{
    unsigned char different;
    unsigned long i;
    different = 0;
    for(i=0; i<length; i++) different |= (unsigned char)(left[i] ^ right[i]);
    return different == 0;
}

static int oa_ticket_window(expires_at, now)
long expires_at;
long now;
{
    if(expires_at < now) return 0;
    if(now > LONG_MAX - 30) return expires_at == now;
    return expires_at <= now + 30;
}

int onboarding_admission_validate_ticket(line, secret, now, replay, replay_context, ticket)
const char *line;
const char *secret;
long now;
onboarding_admission_replay_fn replay;
void *replay_context;
onboarding_admission_ticket *ticket;
{
    char copy[ONBOARDING_ADMISSION_MAX_LINE + 1];
    char signed_part[ONBOARDING_ADMISSION_MAX_LINE + 1];
    char expected[ONBOARDING_ADMISSION_HMAC_HEX_LEN + 1];
    char *part[7];
    unsigned long length, payload_length, signed_length;
    long expires_at;
    int bars, i, result;

    result = -1;
    memset(copy, 0, sizeof(copy));
    memset(signed_part, 0, sizeof(signed_part));
    memset(expected, 0, sizeof(expected));
    if(ticket) memset(ticket, 0, sizeof(*ticket));
    if(!line || !ticket || !replay || !oa_secret_valid(secret)) goto out;

    length = oa_bounded_strlen(line, ONBOARDING_ADMISSION_MAX_LINE);
    if(!length || length > ONBOARDING_ADMISSION_MAX_LINE || line[length-1] != '\n')
        goto out;
    payload_length = length - 1;
    if(!payload_length) goto out;
    for(i=0; i<(int)payload_length; i++)
        if((unsigned char)line[i] < 0x20 || (unsigned char)line[i] > 0x7e)
            goto out;

    memcpy(copy, line, payload_length);
    copy[payload_length] = 0;
    part[0] = copy;
    bars = 0;
    for(i=0; i<(int)payload_length; i++) {
        if(copy[i] == '|') {
            if(++bars > 6) goto out;
            copy[i] = 0;
            part[bars] = copy + i + 1;
        }
    }
    if(bars != 6 || strcmp(part[0], "MUD1O") ||
       oa_bounded_strlen(part[1], 1) != 1 ||
       (part[1][0] != ONBOARDING_ADMISSION_MODE_PROVISION &&
        part[1][0] != ONBOARDING_ADMISSION_MODE_CLAIM) ||
       !oa_time_value(part[2], &expires_at) ||
       !oa_lower_hex_value(part[3], ONBOARDING_ADMISSION_NONCE_LEN) ||
       !oa_uuid(part[4]) || !oa_uuid(part[5]) ||
       !oa_lower_hex_value(part[6], ONBOARDING_ADMISSION_HMAC_HEX_LEN) ||
       !oa_ticket_window(expires_at, now)) goto out;

    signed_length = payload_length - ONBOARDING_ADMISSION_HMAC_HEX_LEN - 1;
    memcpy(signed_part, line, signed_length);
    signed_part[signed_length] = 0;
    if(trusted_admission_hmac_hex(secret, signed_part, expected) != 0 ||
       !oa_constant_time_equal(expected, part[6], ONBOARDING_ADMISSION_HMAC_HEX_LEN))
        goto out;
    if(replay(replay_context, part[3], expires_at) != 0) goto out;

    ticket->mode = part[1][0];
    strcpy(ticket->nonce, part[3]);
    strcpy(ticket->user_id, part[4]);
    strcpy(ticket->correlation_id, part[5]);
    ticket->expires_at = expires_at;
    result = 0;

out:
    memset(copy, 0, sizeof(copy));
    memset(signed_part, 0, sizeof(signed_part));
    memset(expected, 0, sizeof(expected));
    return result;
}

static int oa_hex_value(value)
const char *value;
{
    char decoded[PLAYER_NAME_MAX_BYTES + 1];
    char canonical[PLAYER_NAME_MAX_BYTES + 1];
    unsigned long i, length;
    unsigned int high, low, byte;
    int valid;

    valid = 0;
    memset(decoded, 0, sizeof(decoded));
    memset(canonical, 0, sizeof(canonical));
    if(!value) goto out;
    length = oa_bounded_strlen(value, ONBOARDING_CONTROL_NAME_HEX_MAX);
    if(length < 2 || length > ONBOARDING_CONTROL_NAME_HEX_MAX || (length & 1))
        goto out;
    for(i=0; i<length; i++) if(!oa_lower_hex(value[i])) goto out;
    for(i=0; i<length; i+=2) {
        high = value[i] <= '9' ? value[i] - '0' : value[i] - 'a' + 10;
        low = value[i+1] <= '9' ? value[i+1] - '0' : value[i+1] - 'a' + 10;
        byte = (high << 4) | low;
        if(!byte) goto out;
        decoded[i/2] = (char)byte;
    }
    decoded[length/2] = 0;
    if(!player_name_is_valid((const unsigned char *)decoded,
                             PLAYER_NAME_MIN_CODEPOINTS,
                             PLAYER_NAME_MAX_CODEPOINTS)) goto out;
    strcpy(canonical, decoded);
    for(i=0; canonical[i]; i++)
        if(canonical[i] >= 'A' && canonical[i] <= 'Z') canonical[i] += 'a' - 'A';
    if(canonical[0] >= 'a' && canonical[0] <= 'z') canonical[0] -= 'a' - 'A';
    valid = strcmp(canonical, decoded) == 0;
out:
    memset(decoded, 0, sizeof(decoded));
    memset(canonical, 0, sizeof(canonical));
    return valid;
}

static int oa_storage_format(value)
const char *value;
{
    unsigned long i, length;
    if(!value) return 0;
    length = oa_bounded_strlen(value, ONBOARDING_CONTROL_STORAGE_FORMAT_MAX);
    if(!length || length > ONBOARDING_CONTROL_STORAGE_FORMAT_MAX) return 0;
    if(value[0] < 'a' || value[0] > 'z') return 0;
    for(i=1; i<length; i++)
        if(!((value[i] >= 'a' && value[i] <= 'z') ||
             (value[i] >= '0' && value[i] <= '9') || value[i] == '-' ||
             value[i] == '_' || value[i] == '.')) return 0;
    return 1;
}

static int oa_empty(value, size)
const char *value;
unsigned long size;
{
    return value && oa_bounded_strlen(value, size - 1) == 0;
}

static int oa_c_control_valid(control)
const onboarding_control *control;
{
    if(!control) return 0;
    switch(control->kind) {
    case ONBOARDING_CONTROL_OK:
    case ONBOARDING_CONTROL_ERR:
        return oa_empty(control->name_hex, sizeof(control->name_hex)) &&
               oa_empty(control->character_id, sizeof(control->character_id)) &&
               oa_empty(control->file_sha256, sizeof(control->file_sha256)) &&
               oa_empty(control->storage_format, sizeof(control->storage_format));
    case ONBOARDING_CONTROL_RESERVE:
        return oa_hex_value(control->name_hex) &&
               oa_empty(control->character_id, sizeof(control->character_id)) &&
               oa_empty(control->file_sha256, sizeof(control->file_sha256)) &&
               oa_empty(control->storage_format, sizeof(control->storage_format));
    case ONBOARDING_CONTROL_VERIFIED:
    case ONBOARDING_CONTROL_CHALLENGE:
        return oa_hex_value(control->name_hex) &&
               oa_empty(control->character_id, sizeof(control->character_id)) &&
               oa_lower_hex_value(control->file_sha256,
                                  ONBOARDING_ADMISSION_SHA256_HEX_LEN) &&
               oa_empty(control->storage_format, sizeof(control->storage_format));
    case ONBOARDING_CONTROL_SAVED:
        return oa_empty(control->name_hex, sizeof(control->name_hex)) &&
               oa_uuid(control->character_id) &&
               oa_lower_hex_value(control->file_sha256,
                                  ONBOARDING_ADMISSION_SHA256_HEX_LEN) &&
               oa_storage_format(control->storage_format);
    default:
        return 0;
    }
}

static int oa_gateway_control_valid(control)
const onboarding_control *control;
{
    if(!control) return 0;
    switch(control->kind) {
    case ONBOARDING_CONTROL_RESERVED:
    case ONBOARDING_CONTROL_CLAIMED:
        return oa_empty(control->name_hex, sizeof(control->name_hex)) &&
               oa_uuid(control->character_id) &&
               oa_empty(control->file_sha256, sizeof(control->file_sha256)) &&
               oa_empty(control->storage_format, sizeof(control->storage_format));
    case ONBOARDING_CONTROL_COMMIT:
    case ONBOARDING_CONTROL_ABORT:
    case ONBOARDING_CONTROL_ALLOW:
        return oa_empty(control->name_hex, sizeof(control->name_hex)) &&
               oa_empty(control->character_id, sizeof(control->character_id)) &&
               oa_empty(control->file_sha256, sizeof(control->file_sha256)) &&
               oa_empty(control->storage_format, sizeof(control->storage_format));
    default:
        return 0;
    }
}

static int oa_prepare_control_line(line, copy, parts, expected_bars)
const char *line;
char copy[ONBOARDING_ADMISSION_MAX_LINE + 1];
char **parts;
int expected_bars;
{
    unsigned long length, payload_length;
    int i, bars;
    if(!line || !copy || !parts) return -1;
    length = oa_bounded_strlen(line, ONBOARDING_ADMISSION_MAX_LINE);
    if(!length || length > ONBOARDING_ADMISSION_MAX_LINE || line[length-1] != '\n')
        return -1;
    payload_length = length - 1;
    if(!payload_length) return -1;
    for(i=0; i<(int)payload_length; i++)
        if((unsigned char)line[i] < 0x20 || (unsigned char)line[i] > 0x7e)
            return -1;
    memcpy(copy, line, payload_length);
    copy[payload_length] = 0;
    parts[0] = copy;
    bars = 0;
    for(i=0; i<(int)payload_length; i++) {
        if(copy[i] == '|') {
            if(++bars > expected_bars) return -1;
            copy[i] = 0;
            parts[bars] = copy + i + 1;
        }
    }
    return bars == expected_bars ? 0 : -1;
}

int onboarding_parse_c_control(line, control)
const char *line;
onboarding_control *control;
{
    char copy[ONBOARDING_ADMISSION_MAX_LINE + 1];
    char *part[4];
    int result;
    result = -1;
    memset(copy, 0, sizeof(copy));
    if(control) memset(control, 0, sizeof(*control));
    if(!control) goto out;
    if(oa_prepare_control_line(line, copy, part, 0) == 0) {
        if(!strcmp(part[0], "MUD1O OK")) control->kind = ONBOARDING_CONTROL_OK;
        else if(!strcmp(part[0], "MUD1O ERR")) control->kind = ONBOARDING_CONTROL_ERR;
    }
    else if(oa_prepare_control_line(line, copy, part, 1) == 0 &&
            !strcmp(part[0], "MUD1O RESERVE")) {
        control->kind = ONBOARDING_CONTROL_RESERVE;
        if(oa_copy_value(control->name_hex, sizeof(control->name_hex),
                         part[1]) != 0) control->kind = ONBOARDING_CONTROL_INVALID;
    }
    else if(oa_prepare_control_line(line, copy, part, 2) == 0 &&
            !strcmp(part[0], "MUD1O VERIFIED")) {
        control->kind = ONBOARDING_CONTROL_VERIFIED;
        if(oa_copy_value(control->name_hex, sizeof(control->name_hex), part[1]) != 0 ||
           oa_copy_value(control->file_sha256, sizeof(control->file_sha256),
                         part[2]) != 0)
            control->kind = ONBOARDING_CONTROL_INVALID;
    }
    else if(oa_prepare_control_line(line, copy, part, 2) == 0 &&
            !strcmp(part[0], "MUD1O CHALLENGE")) {
        control->kind = ONBOARDING_CONTROL_CHALLENGE;
        if(oa_copy_value(control->name_hex, sizeof(control->name_hex), part[1]) != 0 ||
           oa_copy_value(control->file_sha256, sizeof(control->file_sha256),
                         part[2]) != 0)
            control->kind = ONBOARDING_CONTROL_INVALID;
    }
    else if(oa_prepare_control_line(line, copy, part, 3) == 0 &&
            !strcmp(part[0], "MUD1O SAVED")) {
        control->kind = ONBOARDING_CONTROL_SAVED;
        if(oa_copy_value(control->character_id, sizeof(control->character_id),
                         part[1]) != 0 ||
           oa_copy_value(control->file_sha256, sizeof(control->file_sha256),
                         part[2]) != 0 ||
           oa_copy_value(control->storage_format, sizeof(control->storage_format),
                         part[3]) != 0)
            control->kind = ONBOARDING_CONTROL_INVALID;
    }
    if(oa_c_control_valid(control)) result = 0;
out:
    if(result != 0 && control) memset(control, 0, sizeof(*control));
    memset(copy, 0, sizeof(copy));
    return result;
}

int onboarding_parse_gateway_control(line, control)
const char *line;
onboarding_control *control;
{
    char copy[ONBOARDING_ADMISSION_MAX_LINE + 1];
    char *part[2];
    int result;
    result = -1;
    memset(copy, 0, sizeof(copy));
    if(control) memset(control, 0, sizeof(*control));
    if(!control) goto out;
    if(oa_prepare_control_line(line, copy, part, 0) == 0) {
        if(!strcmp(part[0], "MUD1O COMMIT")) control->kind = ONBOARDING_CONTROL_COMMIT;
        else if(!strcmp(part[0], "MUD1O ABORT")) control->kind = ONBOARDING_CONTROL_ABORT;
        else if(!strcmp(part[0], "MUD1O ALLOW")) control->kind = ONBOARDING_CONTROL_ALLOW;
    }
    else if(oa_prepare_control_line(line, copy, part, 1) == 0) {
        if(!strcmp(part[0], "MUD1O RESERVED")) control->kind = ONBOARDING_CONTROL_RESERVED;
        else if(!strcmp(part[0], "MUD1O CLAIMED")) control->kind = ONBOARDING_CONTROL_CLAIMED;
        if(control->kind &&
           oa_copy_value(control->character_id, sizeof(control->character_id),
                         part[1]) != 0)
            control->kind = ONBOARDING_CONTROL_INVALID;
    }
    if(oa_gateway_control_valid(control)) result = 0;
out:
    if(result != 0 && control) memset(control, 0, sizeof(*control));
    memset(copy, 0, sizeof(copy));
    return result;
}

static int oa_write_control(out, out_size, control, gateway)
char *out;
unsigned long out_size;
const onboarding_control *control;
int gateway;
{
    int written;
    if(!out || !out_size || (gateway ? !oa_gateway_control_valid(control) :
                                      !oa_c_control_valid(control))) return -1;
    switch(control->kind) {
    case ONBOARDING_CONTROL_OK:
        written = snprintf(out, out_size, "MUD1O OK\n");
        break;
    case ONBOARDING_CONTROL_RESERVE:
        written = snprintf(out, out_size, "MUD1O RESERVE|%s\n", control->name_hex);
        break;
    case ONBOARDING_CONTROL_VERIFIED:
        written = snprintf(out, out_size, "MUD1O VERIFIED|%s|%s\n",
                           control->name_hex, control->file_sha256);
        break;
    case ONBOARDING_CONTROL_CHALLENGE:
        written = snprintf(out, out_size, "MUD1O CHALLENGE|%s|%s\n",
                           control->name_hex, control->file_sha256);
        break;
    case ONBOARDING_CONTROL_SAVED:
        written = snprintf(out, out_size, "MUD1O SAVED|%s|%s|%s\n",
                           control->character_id, control->file_sha256,
                           control->storage_format);
        break;
    case ONBOARDING_CONTROL_ERR:
        written = snprintf(out, out_size, "MUD1O ERR\n");
        break;
    case ONBOARDING_CONTROL_RESERVED:
        written = snprintf(out, out_size, "MUD1O RESERVED|%s\n",
                           control->character_id);
        break;
    case ONBOARDING_CONTROL_CLAIMED:
        written = snprintf(out, out_size, "MUD1O CLAIMED|%s\n",
                           control->character_id);
        break;
    case ONBOARDING_CONTROL_COMMIT:
        written = snprintf(out, out_size, "MUD1O COMMIT\n");
        break;
    case ONBOARDING_CONTROL_ABORT:
        written = snprintf(out, out_size, "MUD1O ABORT\n");
        break;
    case ONBOARDING_CONTROL_ALLOW:
        written = snprintf(out, out_size, "MUD1O ALLOW\n");
        break;
    default:
        return -1;
    }
    if(written < 0 || (unsigned long)written >= out_size ||
       (unsigned long)written > ONBOARDING_ADMISSION_MAX_LINE) {
        if(out_size) out[0] = 0;
        return -1;
    }
    return 0;
}

int onboarding_format_c_control(out, out_size, control)
char *out;
unsigned long out_size;
const onboarding_control *control;
{
    return oa_write_control(out, out_size, control, 0);
}

int onboarding_format_gateway_control(out, out_size, control)
char *out;
unsigned long out_size;
const onboarding_control *control;
{
    return oa_write_control(out, out_size, control, 1);
}

static int oa_live_state(state)
onboarding_state state;
{
    return state == ONBOARDING_STATE_PROVISION_READY ||
           state == ONBOARDING_STATE_PROVISION_AWAIT_RESERVED ||
           state == ONBOARDING_STATE_PROVISION_RESERVED ||
           state == ONBOARDING_STATE_PROVISION_AWAIT_COMMIT ||
           state == ONBOARDING_STATE_CLAIM_READY ||
           state == ONBOARDING_STATE_CLAIM_AWAIT_ALLOW ||
           state == ONBOARDING_STATE_CLAIM_PASSWORD_READY ||
           state == ONBOARDING_STATE_CLAIM_AWAIT_CLAIMED;
}

int onboarding_state_accept_ticket(state, ticket)
onboarding_state *state;
const onboarding_admission_ticket *ticket;
{
    if(!state || !ticket || *state != ONBOARDING_STATE_NEW) return -1;
    if(ticket->mode == ONBOARDING_ADMISSION_MODE_PROVISION) {
        *state = ONBOARDING_STATE_PROVISION_READY;
        return 0;
    }
    if(ticket->mode == ONBOARDING_ADMISSION_MODE_CLAIM) {
        *state = ONBOARDING_STATE_CLAIM_READY;
        return 0;
    }
    return -1;
}

int onboarding_state_apply_evidence(state)
onboarding_state *state;
{
    if(!state) return -1;
    if(*state == ONBOARDING_STATE_PROVISION_RESERVED) {
        *state = ONBOARDING_STATE_PROVISION_AWAIT_COMMIT;
        return 0;
    }
    if(*state == ONBOARDING_STATE_CLAIM_PASSWORD_READY) {
        *state = ONBOARDING_STATE_CLAIM_AWAIT_CLAIMED;
        return 0;
    }
    return -1;
}

int onboarding_state_apply_c_control(state, control)
onboarding_state *state;
const onboarding_control *control;
{
    if(!state || !oa_c_control_valid(control)) return -1;
    if(control->kind == ONBOARDING_CONTROL_ERR && oa_live_state(*state)) {
        *state = ONBOARDING_STATE_FAILED;
        return 0;
    }
    if(control->kind == ONBOARDING_CONTROL_OK &&
       (*state == ONBOARDING_STATE_PROVISION_READY ||
        *state == ONBOARDING_STATE_CLAIM_READY)) return 0;
    if(control->kind == ONBOARDING_CONTROL_RESERVE &&
       *state == ONBOARDING_STATE_PROVISION_READY) {
        *state = ONBOARDING_STATE_PROVISION_AWAIT_RESERVED;
        return 0;
    }
    if(control->kind == ONBOARDING_CONTROL_SAVED &&
       *state == ONBOARDING_STATE_PROVISION_RESERVED) {
        *state = ONBOARDING_STATE_PROVISION_AWAIT_COMMIT;
        return 0;
    }
    if(control->kind == ONBOARDING_CONTROL_CHALLENGE &&
       *state == ONBOARDING_STATE_CLAIM_READY) {
        *state = ONBOARDING_STATE_CLAIM_AWAIT_ALLOW;
        return 0;
    }
    if(control->kind == ONBOARDING_CONTROL_VERIFIED &&
       *state == ONBOARDING_STATE_CLAIM_PASSWORD_READY) {
        *state = ONBOARDING_STATE_CLAIM_AWAIT_CLAIMED;
        return 0;
    }
    return -1;
}

int onboarding_state_apply_gateway_control(state, control)
onboarding_state *state;
const onboarding_control *control;
{
    if(!state || !oa_gateway_control_valid(control)) return -1;
    if(control->kind == ONBOARDING_CONTROL_ABORT && oa_live_state(*state)) {
        *state = ONBOARDING_STATE_FAILED;
        return 0;
    }
    if(control->kind == ONBOARDING_CONTROL_RESERVED &&
       *state == ONBOARDING_STATE_PROVISION_AWAIT_RESERVED) {
        *state = ONBOARDING_STATE_PROVISION_RESERVED;
        return 0;
    }
    if(control->kind == ONBOARDING_CONTROL_COMMIT &&
       *state == ONBOARDING_STATE_PROVISION_AWAIT_COMMIT) {
        *state = ONBOARDING_STATE_READY;
        return 0;
    }
    if(control->kind == ONBOARDING_CONTROL_ALLOW &&
       *state == ONBOARDING_STATE_CLAIM_AWAIT_ALLOW) {
        *state = ONBOARDING_STATE_CLAIM_PASSWORD_READY;
        return 0;
    }
    if(control->kind == ONBOARDING_CONTROL_CLAIMED &&
       *state == ONBOARDING_STATE_CLAIM_AWAIT_CLAIMED) {
        *state = ONBOARDING_STATE_READY;
        return 0;
    }
    return -1;
}
