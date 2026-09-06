-- AliasTitleSnapshotManifestV1 shadow intake contract.
--
-- This is intentionally NOT a migration.  It is detached and default-off:
-- no application path, deployment configuration, or database bootstrap loads it.
-- Apply it only to an explicitly created disposable database when exercising
-- the contract described in shadow_intake/README.md.

BEGIN;

CREATE SCHEMA IF NOT EXISTS shadow_intake;

CREATE TABLE shadow_intake.alias_title_snapshot_manifest_v1 (
    event_id uuid PRIMARY KEY,
    world_id text NOT NULL CHECK (world_id = btrim(world_id) AND world_id <> ''),
    canonical_legacy_name_key text NOT NULL
        CHECK (canonical_legacy_name_key = btrim(canonical_legacy_name_key)
               AND canonical_legacy_name_key <> ''),
    character_id uuid NOT NULL,
    writer_instance_id uuid NOT NULL,
    writer_epoch bigint NOT NULL CHECK (writer_epoch >= 0),
    writer_revision bigint NOT NULL CHECK (writer_revision >= 0),
    command_id uuid NOT NULL,
    correlation_id uuid NOT NULL,
    manifest_format text NOT NULL CHECK (manifest_format = 'CDTO'),
    manifest_kind smallint NOT NULL CHECK (manifest_kind = 9),
    manifest_schema text NOT NULL
        CHECK (manifest_schema = 'AliasTitleSnapshotManifestV1'),
    recorded_at timestamptz NOT NULL
);

-- The wire image is deliberately a separate relation.  The intake does not
-- decode, normalize, reserialize, or otherwise replace this kind-9 CDTO body.
CREATE TABLE shadow_intake.alias_title_snapshot_manifest_v1_wire (
    event_id uuid PRIMARY KEY
        REFERENCES shadow_intake.alias_title_snapshot_manifest_v1 (event_id),
    canonical_cdto_wire bytea NOT NULL,
    canonical_cdto_digest_sha256 text NOT NULL
        CHECK (canonical_cdto_digest_sha256 ~ '^[0-9a-f]{64}$'),
    canonical_cdto_length integer NOT NULL
        CHECK (canonical_cdto_length >= 0
               AND canonical_cdto_length = octet_length(canonical_cdto_wire))
);

CREATE OR REPLACE FUNCTION shadow_intake.reject_mutation()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, shadow_intake
AS $$
BEGIN
    RAISE EXCEPTION 'shadow intake is append-only';
END;
$$;

CREATE TRIGGER alias_title_snapshot_manifest_v1_no_mutation
BEFORE UPDATE OR DELETE ON shadow_intake.alias_title_snapshot_manifest_v1
FOR EACH ROW EXECUTE FUNCTION shadow_intake.reject_mutation();

CREATE TRIGGER alias_title_snapshot_manifest_v1_wire_no_mutation
BEFORE UPDATE OR DELETE ON shadow_intake.alias_title_snapshot_manifest_v1_wire
FOR EACH ROW EXECUTE FUNCTION shadow_intake.reject_mutation();

-- The sole intended writer entry point.  Its outcome is one of:
--   first_recorded             the event UUID was not yet present
--   exact_retry_idempotent     every immutable field, including wire bytes,
--                               digest, length, and recorded timestamp matched
-- Any difference for an existing event UUID raises SQLSTATE 23505.
CREATE OR REPLACE FUNCTION shadow_intake.record_alias_title_snapshot_manifest_v1(
    p_event_id uuid,
    p_world_id text,
    p_canonical_legacy_name_key text,
    p_character_id uuid,
    p_writer_instance_id uuid,
    p_writer_epoch bigint,
    p_writer_revision bigint,
    p_command_id uuid,
    p_correlation_id uuid,
    p_manifest_format text,
    p_manifest_kind smallint,
    p_manifest_schema text,
    p_recorded_at timestamptz,
    p_canonical_cdto_wire bytea,
    p_canonical_cdto_digest_sha256 text,
    p_canonical_cdto_length integer
)
RETURNS text
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, shadow_intake
AS $$
DECLARE
    v_existing record;
BEGIN
    INSERT INTO shadow_intake.alias_title_snapshot_manifest_v1 (
        event_id, world_id, canonical_legacy_name_key, character_id,
        writer_instance_id, writer_epoch, writer_revision, command_id,
        correlation_id, manifest_format, manifest_kind, manifest_schema,
        recorded_at
    ) VALUES (
        p_event_id, p_world_id, p_canonical_legacy_name_key, p_character_id,
        p_writer_instance_id, p_writer_epoch, p_writer_revision, p_command_id,
        p_correlation_id, p_manifest_format, p_manifest_kind, p_manifest_schema,
        p_recorded_at
    ) ON CONFLICT (event_id) DO NOTHING;

    IF FOUND THEN
        INSERT INTO shadow_intake.alias_title_snapshot_manifest_v1_wire (
            event_id, canonical_cdto_wire, canonical_cdto_digest_sha256,
            canonical_cdto_length
        ) VALUES (
            p_event_id, p_canonical_cdto_wire, p_canonical_cdto_digest_sha256,
            p_canonical_cdto_length
        );
        RETURN 'first_recorded';
    END IF;

    SELECT m.*, w.canonical_cdto_wire, w.canonical_cdto_digest_sha256,
           w.canonical_cdto_length
      INTO v_existing
      FROM shadow_intake.alias_title_snapshot_manifest_v1 AS m
      JOIN shadow_intake.alias_title_snapshot_manifest_v1_wire AS w
        ON w.event_id = m.event_id
     WHERE m.event_id = p_event_id;

    IF v_existing.world_id IS NOT DISTINCT FROM p_world_id
       AND v_existing.canonical_legacy_name_key IS NOT DISTINCT FROM p_canonical_legacy_name_key
       AND v_existing.character_id IS NOT DISTINCT FROM p_character_id
       AND v_existing.writer_instance_id IS NOT DISTINCT FROM p_writer_instance_id
       AND v_existing.writer_epoch IS NOT DISTINCT FROM p_writer_epoch
       AND v_existing.writer_revision IS NOT DISTINCT FROM p_writer_revision
       AND v_existing.command_id IS NOT DISTINCT FROM p_command_id
       AND v_existing.correlation_id IS NOT DISTINCT FROM p_correlation_id
       AND v_existing.manifest_format IS NOT DISTINCT FROM p_manifest_format
       AND v_existing.manifest_kind IS NOT DISTINCT FROM p_manifest_kind
       AND v_existing.manifest_schema IS NOT DISTINCT FROM p_manifest_schema
       AND v_existing.recorded_at IS NOT DISTINCT FROM p_recorded_at
       AND v_existing.canonical_cdto_wire IS NOT DISTINCT FROM p_canonical_cdto_wire
       AND v_existing.canonical_cdto_digest_sha256 IS NOT DISTINCT FROM p_canonical_cdto_digest_sha256
       AND v_existing.canonical_cdto_length IS NOT DISTINCT FROM p_canonical_cdto_length THEN
        RETURN 'exact_retry_idempotent';
    END IF;

    RAISE EXCEPTION USING
        ERRCODE = '23505',
        MESSAGE = format('shadow intake immutable conflict for event %s', p_event_id);
END;
$$;

-- Default deny.  A future disposable contract harness may create a dedicated
-- NOLOGIN role, grant only schema USAGE and this function's EXECUTE to it, and
-- invoke the RPC through that role.  There are deliberately no PUBLIC grants.
REVOKE ALL ON SCHEMA shadow_intake FROM PUBLIC;
REVOKE ALL ON TABLE shadow_intake.alias_title_snapshot_manifest_v1 FROM PUBLIC;
REVOKE ALL ON TABLE shadow_intake.alias_title_snapshot_manifest_v1_wire FROM PUBLIC;
REVOKE ALL ON FUNCTION shadow_intake.reject_mutation() FROM PUBLIC;
REVOKE ALL ON FUNCTION shadow_intake.record_alias_title_snapshot_manifest_v1(
    uuid, text, text, uuid, uuid, bigint, bigint, uuid, uuid, text, smallint,
    text, timestamptz, bytea, text, integer
) FROM PUBLIC;

COMMIT;
