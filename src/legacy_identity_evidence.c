#include "legacy_identity_evidence.h"

#include "player_store.h"

#include <string.h>

extern void lowercize(char *str, int flag);

static void evidence_reset(legacy_identity_evidence *out)
{
    memset(out, 0, sizeof(*out));
    out->version = LEGACY_IDENTITY_EVIDENCE_VERSION;
    out->result = LEGACY_IDENTITY_EVIDENCE_CORRUPT;
    out->canonicalization = LEGACY_IDENTITY_EVIDENCE_INVALID;
    strcpy(out->storage_format, LEGACY_IDENTITY_EVIDENCE_STORAGE_FORMAT);
}

legacy_identity_evidence_result legacy_identity_evidence_inspect(
    const char *legacy_name, legacy_identity_evidence *out)
{
    int result;

    if(!out) return LEGACY_IDENTITY_EVIDENCE_IO_ERROR;
    evidence_reset(out);
    if(!legacy_name || !player_name_is_valid((const unsigned char *)legacy_name,
        PLAYER_NAME_MIN_CODEPOINTS, PLAYER_NAME_MAX_CODEPOINTS)) {
        out->result = LEGACY_IDENTITY_EVIDENCE_INVALID_INPUT;
        return out->result;
    }

    strcpy(out->canonical_name, legacy_name);
    lowercize(out->canonical_name, 1);
    if(!player_name_is_valid((const unsigned char *)out->canonical_name,
        PLAYER_NAME_MIN_CODEPOINTS, PLAYER_NAME_MAX_CODEPOINTS)) {
        memset(out->canonical_name, 0, sizeof(out->canonical_name));
        out->result = LEGACY_IDENTITY_EVIDENCE_INVALID_INPUT;
        return out->result;
    }
    out->canonicalization = strcmp(legacy_name, out->canonical_name) == 0 ?
        LEGACY_IDENTITY_EVIDENCE_CANONICAL : LEGACY_IDENTITY_EVIDENCE_NORMALIZED;
    if(player_path_shard_from_name(out->canonical_name, out->legacy_shard) != 0) {
        out->result = LEGACY_IDENTITY_EVIDENCE_IO_ERROR;
        return out->result;
    }

    result = file_player_store_inspect(out->canonical_name,
        out->player_file_sha256);
    switch(result) {
    case PLAYER_STORE_OK:
        out->result = LEGACY_IDENTITY_EVIDENCE_OK;
        break;
    case PLAYER_STORE_NOT_FOUND:
        out->result = LEGACY_IDENTITY_EVIDENCE_NOT_FOUND;
        break;
    case PLAYER_STORE_CORRUPT:
        out->result = LEGACY_IDENTITY_EVIDENCE_CORRUPT;
        break;
    default:
        out->result = LEGACY_IDENTITY_EVIDENCE_IO_ERROR;
        break;
    }
    if(out->result != LEGACY_IDENTITY_EVIDENCE_OK)
        memset(out->player_file_sha256, 0, sizeof(out->player_file_sha256));
    return out->result;
}
