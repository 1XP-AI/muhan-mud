#include <stdio.h>
#include <string.h>

#include "trusted_admission.h"

static int expect(condition, message)
int condition;
const char *message;
{
    if(condition) return 0;
    fprintf(stderr, "trusted_admission_test: %s\n", message);
    return 1;
}

static void signed_ticket(out, signed_part, nonce, name_hex, expires_at)
char *out;
char *signed_part;
const char *nonce;
const char *name_hex;
long expires_at;
{
    char mac[65];
    sprintf(signed_part, "MUD1|%ld|%s|123e4567-e89b-12d3-a456-426614174000|"
            "123e4567-e89b-12d3-a456-426614174001|%s", expires_at, nonce, name_hex);
    trusted_admission_hmac_hex("0123456789abcdef0123456789abcdef", signed_part, mac);
    sprintf(out, "%s|%s", signed_part, mac);
}

static void finish_ticket(out, signed_part)
char *out;
char *signed_part;
{
    char mac[65];
    trusted_admission_hmac_hex("0123456789abcdef0123456789abcdef", signed_part, mac);
    sprintf(out, "%s|%s", signed_part, mac);
}

int main(void)
{
    char mac[65], line[258], signed_part[190], long_key[132], nonce[33];
    trusted_admission_ticket ticket;
    int i, failed = 0;

    failed += expect(trusted_admission_hmac_hex("Jefe", "what do ya want for nothing?", mac) == 0 &&
                     strcmp(mac, "5bdcc146bf60754e6a042426089575c75a003f089d2739839dec58b964ec3843") == 0,
                     "RFC 4231 HMAC-SHA256 vector must match");
    memset(long_key, 0xaa, 131);
    long_key[131] = 0;
    failed += expect(trusted_admission_hmac_hex(long_key,
                     "Test Using Larger Than Block-Size Key - Hash Key First", mac) == 0 &&
                     strcmp(mac, "60e431591ee0b67f0d8a26aacbf5b77f8e0bc6213728c5140546040f0ee37f54") == 0,
                     "RFC 4231 long-key HMAC vector must match");
    failed += expect(trusted_admission_hmac_hex(long_key,
                     "This is a test using a larger than block-size key and a larger than block-size data. "
                     "The key needs to be hashed before being used by the HMAC algorithm.", mac) == 0 &&
                     strcmp(mac, "9b09ffa71b942fcb27635fbcd5b0e944bfdc63644f0713938a7f51535c3a35e2") == 0,
                     "RFC 4231 multi-block HMAC vector must match");
    failed += expect(trusted_admission_set_secret_for_test("short") < 0,
                     "short secret must not enable ticket mode");
	failed += expect(trusted_admission_set_secret_for_test("") < 0,
				 "present empty secret must fail closed");
    failed += expect(trusted_admission_set_secret_for_test("0123456789abcdef0123456789abcdef") == 0,
                     "32-byte ASCII secret must enable ticket mode");

    signed_ticket(line, signed_part,
                  "00112233445566778899aabbccddeeff", "416c696365", 1000);
    failed += expect(trusted_admission_validate(line, 1000, &ticket) == 0 &&
                     strcmp(ticket.name, "Alice") == 0 &&
                     strcmp(ticket.user_id, "123e4567-e89b-12d3-a456-426614174000") == 0,
                     "valid ticket must parse and consume identity");
    failed += expect(trusted_admission_validate(line, 1000, &ticket) < 0,
                     "nonce must be single-use after a valid ticket");

    signed_ticket(line, signed_part,
                  "10112233445566778899aabbccddeeff", "416c696365", 999);
    failed += expect(trusted_admission_validate(line, 1000, &ticket) < 0,
                     "expired ticket must fail");
    signed_ticket(line, signed_part,
                  "20112233445566778899aabbccddeeff", "416c696365", 1031);
    failed += expect(trusted_admission_validate(line, 1000, &ticket) < 0,
                     "future ticket beyond 30 seconds must fail");
    signed_ticket(line, signed_part,
                  "30112233445566778899aabbccddeeff", "416c696365", 1000);
    line[strlen(line)-1] = line[strlen(line)-1] == '0' ? '1' : '0';
    failed += expect(trusted_admission_validate(line, 1000, &ticket) < 0,
                     "bad signature must fail");
    signed_ticket(line, signed_part,
                  "40112233445566778899aabbccddeeff", "616c696365", 1000);
    failed += expect(trusted_admission_validate(line, 1000, &ticket) < 0,
                     "non-canonical ASCII name must fail");
    signed_ticket(line, signed_part,
                  "50112233445566778899aabbccddeeff", "416c696365", 1000);
    strcpy(line + 5, "X");
    failed += expect(trusted_admission_validate(line, 1000, &ticket) < 0,
                     "malformed ticket must fail");

    sprintf(signed_part, "MUD1|1000|60112233445566778899aabbccddeeff|"
            "123E4567-e89b-12d3-a456-426614174000|"
            "123e4567-e89b-12d3-a456-426614174001|416c696365");
    trusted_admission_hmac_hex("0123456789abcdef0123456789abcdef", signed_part, mac);
    sprintf(line, "%s|%s", signed_part, mac);
    failed += expect(trusted_admission_validate(line, 1000, &ticket) < 0,
                     "uppercase UUID must fail even with a valid signature");

    signed_ticket(line, signed_part,
                  "70112233445566778899aabbccddeeff", "416c696365", 1000);
    strcat(line, "\n");
    failed += expect(trusted_admission_validate(line, 1000, &ticket) == 0,
                     "wire-format terminal LF must be accepted");

    signed_ticket(line, signed_part,
                  "80112233445566778899aabbccddeeff", "416c696365", 1000);
    strcat(line, "\n\n");
    failed += expect(trusted_admission_validate(line, 1000, &ticket) < 0,
                     "extra terminal LF must fail");
    sprintf(signed_part, "MUD1|1000|90112233445566778899aabbccddeeff|"
            "123e4567-e89b-12d3-a456-426614174000|"
            "123e4567-e89b-12d3-a456-426614174001|c0af");
    finish_ticket(line, signed_part);
    failed += expect(trusted_admission_validate(line, 1000, &ticket) < 0,
                     "non-canonical UTF-8 name must fail");
    sprintf(signed_part, "MUD1|1000|91112233445566778899aabbccddeeff|"
            "123e4567-e89b-12d3-a456-426614174000|"
            "123e4567-e89b-12d3-a456-426614174001|416c00696365");
    finish_ticket(line, signed_part);
    failed += expect(trusted_admission_validate(line, 1000, &ticket) < 0,
                     "embedded NUL in decoded name must fail without truncation");
    signed_ticket(line, signed_part,
                  "a0112233445566778899aabbccddeeff", "416c696365", 1000);
    signed_part[2] = 1;
    finish_ticket(line, signed_part);
    failed += expect(trusted_admission_validate(line, 1000, &ticket) < 0,
                     "embedded control byte must fail before signature use");
    memset(line, 'a', TRUSTED_ADMISSION_MAX_LINE);
    line[TRUSTED_ADMISSION_MAX_LINE] = 0;
    failed += expect(trusted_admission_validate(line, 1000, &ticket) < 0,
                     "exact parser boundary must be bounded and fail malformed input");
    line[TRUSTED_ADMISSION_MAX_LINE] = 'a';
    line[TRUSTED_ADMISSION_MAX_LINE + 1] = 0;
    failed += expect(trusted_admission_validate(line, 1000, &ticket) < 0,
                     "oversize parser input must fail");
    sprintf(signed_part, "MUD1|999999999999999999999999|b0112233445566778899aabbccddeeff|"
            "123e4567-e89b-12d3-a456-426614174000|"
            "123e4567-e89b-12d3-a456-426614174001|416c696365");
    finish_ticket(line, signed_part);
    failed += expect(trusted_admission_validate(line, 1000, &ticket) < 0,
                     "timestamp overflow must fail");

    trusted_admission_set_secret_for_test("0123456789abcdef0123456789abcdef");
    for(i=0; i<TRUSTED_ADMISSION_REPLAY_LIMIT; i++) {
        sprintf(nonce, "%032x", i);
        signed_ticket(line, signed_part, nonce, "416c696365", 1000);
        failed += expect(trusted_admission_validate(line, 1000, &ticket) == 0,
                         "replay cache must accept its documented capacity");
    }
    sprintf(nonce, "%032x", TRUSTED_ADMISSION_REPLAY_LIMIT);
    signed_ticket(line, signed_part, nonce, "416c696365", 1000);
    failed += expect(trusted_admission_validate(line, 1000, &ticket) < 0,
                     "full replay cache must fail closed");
    signed_ticket(line, signed_part, nonce, "416c696365", 1001);
    failed += expect(trusted_admission_validate(line, 1001, &ticket) == 0,
                     "expired replay entries must be evicted before capacity check");

    trusted_admission_reset_for_test();
    if(failed) return 1;
    puts("trusted_admission_test: ok");
    return 0;
}
