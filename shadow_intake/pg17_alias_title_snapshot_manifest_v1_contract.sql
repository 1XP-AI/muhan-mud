-- Explicit disposable PostgreSQL 17 assertions for the detached shadow intake.
-- Invoked only by run_pg17_alias_title_snapshot_manifest_v1_contract.sh after
-- its environment opt-in and caller-provided database URL checks have passed.
-- This file never creates or drops a database.  Its temporary role, grants,
-- and fixture data are all rolled back at the end of this session.

BEGIN;

DO $$
BEGIN
    IF current_setting('server_version_num')::integer < 170000
       OR current_setting('server_version_num')::integer >= 180000 THEN
        RAISE EXCEPTION 'PG17 contract harness requires PostgreSQL 17, found %',
            current_setting('server_version');
    END IF;
END;
$$;

-- A NOLOGIN role lets this harness prove the intended narrow writer surface.
-- The outer transaction makes this role and its membership non-persistent.
CREATE ROLE shadow_intake_contract_writer NOLOGIN;
GRANT shadow_intake_contract_writer TO CURRENT_USER;
GRANT USAGE ON SCHEMA shadow_intake TO shadow_intake_contract_writer;
GRANT EXECUTE ON FUNCTION shadow_intake.record_alias_title_snapshot_manifest_v1(
    uuid, text, text, uuid, uuid, bigint, bigint, uuid, uuid, text, timestamptz,
    bytea, text, integer
) TO shadow_intake_contract_writer;

SET LOCAL ROLE shadow_intake_contract_writer;

DO $$
BEGIN
    IF NOT has_schema_privilege(current_user, 'shadow_intake', 'USAGE') THEN
        RAISE EXCEPTION 'writer role lacks required schema USAGE';
    END IF;
    IF NOT has_function_privilege(
        current_user,
        'shadow_intake.record_alias_title_snapshot_manifest_v1(uuid,text,text,uuid,uuid,bigint,bigint,uuid,uuid,text,timestamptz,bytea,text,integer)'::regprocedure,
        'EXECUTE'
    ) THEN
        RAISE EXCEPTION 'writer role lacks RPC EXECUTE';
    END IF;
    IF has_function_privilege(current_user, 'shadow_intake.reject_mutation()'::regprocedure, 'EXECUTE') THEN
        RAISE EXCEPTION 'writer role has unexpected helper-function EXECUTE';
    END IF;
    IF has_table_privilege(current_user, 'shadow_intake.alias_title_snapshot_manifest_v1', 'SELECT, INSERT, UPDATE, DELETE')
       OR has_table_privilege(current_user, 'shadow_intake.alias_title_snapshot_manifest_v1_snapshot_wire', 'SELECT, INSERT, UPDATE, DELETE') THEN
        RAISE EXCEPTION 'writer role has unexpected direct table privilege';
    END IF;

    BEGIN
        PERFORM 1 FROM shadow_intake.alias_title_snapshot_manifest_v1 LIMIT 1;
        RAISE EXCEPTION 'writer direct metadata SELECT unexpectedly succeeded';
    EXCEPTION WHEN insufficient_privilege THEN
        NULL;
    END;

    BEGIN
        PERFORM 1 FROM shadow_intake.alias_title_snapshot_manifest_v1_snapshot_wire LIMIT 1;
        RAISE EXCEPTION 'writer direct wire SELECT unexpectedly succeeded';
    EXCEPTION WHEN insufficient_privilege THEN
        NULL;
    END;

    BEGIN
        INSERT INTO shadow_intake.alias_title_snapshot_manifest_v1 (event_id)
        VALUES ('018f12de-5f1e-7bce-9f02-5a6d4f0b01f1');
        RAISE EXCEPTION 'writer direct metadata INSERT unexpectedly succeeded';
    EXCEPTION WHEN insufficient_privilege THEN
        NULL;
    END;
END;
$$;

DO $$
DECLARE
    v_event_id uuid := '018f12de-5f1e-7bce-9f02-5a6d4f0b0101';
    v_world_id text := 'muhan-prod';
    v_legacy_key text := 'aabbccddeeff';
    v_character_id uuid := '018f12de-5f1e-7bce-9f02-5a6d4f0b0102';
    v_writer_instance_id uuid := '018f12de-5f1e-7bce-9f02-5a6d4f0b0103';
    v_writer_epoch bigint := 42;
    v_writer_revision bigint := 9;
    v_command_id uuid := '018f12de-5f1e-7bce-9f02-5a6d4f0b0104';
    v_correlation_id uuid := '018f12de-5f1e-7bce-9f02-5a6d4f0b0105';
    v_manifest_schema text := 'AliasTitleSnapshotManifestV1';
    v_recorded_at timestamptz := '2026-09-06T00:00:00Z';
    v_wire bytea := decode(
        '4d55484344544f00000100090000003e0001020000000200010002090000001a016e00056e6f7274680161000d61747461636b2074617267657400030b00000001010004090000000552756c6572d0c08f3ceb36620b0c7c47890070c4b0f5111f97007b26832b56916b42191715',
        'hex'
    );
    v_digest text := 'd0c08f3ceb36620b0c7c47890070c4b0f5111f97007b26832b56916b42191715';
    v_length integer := 110;
    v_outcome text;
    v_field text;
BEGIN
    v_outcome := shadow_intake.record_alias_title_snapshot_manifest_v1(
        v_event_id, v_world_id, v_legacy_key, v_character_id,
        v_writer_instance_id, v_writer_epoch, v_writer_revision, v_command_id,
        v_correlation_id, v_manifest_schema, v_recorded_at, v_wire, v_digest, v_length
    );
    IF v_outcome <> 'first_recorded' THEN
        RAISE EXCEPTION 'first record returned %, expected first_recorded', v_outcome;
    END IF;

    v_outcome := shadow_intake.record_alias_title_snapshot_manifest_v1(
        v_event_id, v_world_id, v_legacy_key, v_character_id,
        v_writer_instance_id, v_writer_epoch, v_writer_revision, v_command_id,
        v_correlation_id, v_manifest_schema, v_recorded_at, v_wire, v_digest, v_length
    );
    IF v_outcome <> 'exact_retry_idempotent' THEN
        RAISE EXCEPTION 'exact retry returned %, expected exact_retry_idempotent', v_outcome;
    END IF;

    FOREACH v_field IN ARRAY ARRAY[
        'world_id', 'canonical_legacy_name_key', 'character_id',
        'writer_instance_id', 'writer_epoch', 'writer_revision', 'command_id',
        'correlation_id', 'companion_manifest_schema', 'recorded_at', 'wire',
        'digest', 'length'
    ] LOOP
        BEGIN
            PERFORM shadow_intake.record_alias_title_snapshot_manifest_v1(
                v_event_id,
                CASE WHEN v_field = 'world_id' THEN 'muhan-stage' ELSE v_world_id END,
                CASE WHEN v_field = 'canonical_legacy_name_key' THEN '001122334455' ELSE v_legacy_key END,
                CASE WHEN v_field = 'character_id' THEN '018f12de-5f1e-7bce-9f02-5a6d4f0b0198'::uuid ELSE v_character_id END,
                CASE WHEN v_field = 'writer_instance_id' THEN '018f12de-5f1e-7bce-9f02-5a6d4f0b0197'::uuid ELSE v_writer_instance_id END,
                CASE WHEN v_field = 'writer_epoch' THEN 43 ELSE v_writer_epoch END,
                CASE WHEN v_field = 'writer_revision' THEN 10 ELSE v_writer_revision END,
                CASE WHEN v_field = 'command_id' THEN '018f12de-5f1e-7bce-9f02-5a6d4f0b0196'::uuid ELSE v_command_id END,
                CASE WHEN v_field = 'correlation_id' THEN '018f12de-5f1e-7bce-9f02-5a6d4f0b0195'::uuid ELSE v_correlation_id END,
                CASE WHEN v_field = 'companion_manifest_schema' THEN 'OtherManifestV1' ELSE v_manifest_schema END,
                CASE WHEN v_field = 'recorded_at' THEN '2026-09-06T00:00:01Z'::timestamptz ELSE v_recorded_at END,
                CASE WHEN v_field = 'wire' THEN set_byte(v_wire, octet_length(v_wire) - 1, 20) ELSE v_wire END,
                CASE WHEN v_field = 'digest' THEN 'e0c08f3ceb36620b0c7c47890070c4b0f5111f97007b26832b56916b42191715' ELSE v_digest END,
                CASE WHEN v_field = 'length' THEN 109 ELSE v_length END
            );
            RAISE EXCEPTION 'immutable conflict for % unexpectedly succeeded', v_field;
        EXCEPTION WHEN unique_violation THEN
            NULL;
        END;
    END LOOP;
END;
$$;

RESET ROLE;

DO $$
DECLARE
    v_table text;
    v_event_id uuid := '018f12de-5f1e-7bce-9f02-5a6d4f0b0101';
BEGIN
    FOR v_table IN SELECT * FROM unnest(ARRAY[
        'shadow_intake.alias_title_snapshot_manifest_v1',
        'shadow_intake.alias_title_snapshot_manifest_v1_snapshot_wire'
    ]) LOOP
        BEGIN
            EXECUTE format('UPDATE %s SET event_id = event_id WHERE event_id = $1', v_table)
                USING v_event_id;
            RAISE EXCEPTION 'append-only UPDATE on % unexpectedly succeeded', v_table;
        EXCEPTION WHEN raise_exception THEN
            IF SQLERRM <> 'shadow intake is append-only' THEN
                RAISE;
            END IF;
        END;
        BEGIN
            EXECUTE format('DELETE FROM %s WHERE event_id = $1', v_table)
                USING v_event_id;
            RAISE EXCEPTION 'append-only DELETE on % unexpectedly succeeded', v_table;
        EXCEPTION WHEN raise_exception THEN
            IF SQLERRM <> 'shadow intake is append-only' THEN
                RAISE;
            END IF;
        END;
    END LOOP;
END;
$$;

ROLLBACK;
