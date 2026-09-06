# 수남이 에라 메세지 -Wall
# 또 다른 컴파일 옵션 -DRECORD_ALL 용량많아 지웠음..debug넣음
# 정균이 -DDEBUG없앰.
# 오토셧다운 없앰. -DAUTOSHUTDOWN
# 라토크 savegame_nomsg 함수를 넣고 싶은 경우 즉, 복사가 많이 되고 있을때에는
#       -DNODUPLE 옵션을 넣어주기를 바람.
# 크로키 레벨 제한을 두고 싶지 않은 경우, -DOBJLEVEL 을 삭제해 주면 됨.

LEGACY_CHARSET_FLAGS ?=
CFLAGS = -g -O2 -std=gnu89 -fcommon -DFINGERACCT -DCHECKDOUBLE -DSUICIDE -DOBJLEVEL -DDEBUG $(LEGACY_CHARSET_FLAGS)

ifeq ($(shell uname -s),Darwin)
CFLAGS += -Wno-error=implicit-function-declaration -Wno-error=return-mismatch -Wno-error=incompatible-function-pointer-types
UNIT_GC_FLAGS = -Wl,-dead_strip
	UNIT_LEGACY_WARN_FLAGS = -Wno-error=implicit-function-declaration -Wno-error=return-mismatch
	CDTO_V1_TEST_WARN_FLAGS = -Wno-deprecated-non-prototype
	DECODER_ASAN_OPTIONS = detect_leaks=0:halt_on_error=1
	V2_LINK_MAP_OPTION = -Wl,-map,
else
	UNIT_GC_FLAGS = -Wl,--gc-sections
	UNIT_LEGACY_WARN_FLAGS =
	CDTO_V1_TEST_WARN_FLAGS =
	DECODER_ASAN_OPTIONS = detect_leaks=1:halt_on_error=1
	V2_LINK_MAP_OPTION = -Wl,-Map,
endif

LIBS = -lm
CC = cc
OUTFILE = frp.new
EXTRA_LIBS =
V2_TEST_CFLAGS = -std=gnu89 -fcommon -Wall -Wextra -Werror $(CDTO_V1_TEST_WARN_FLAGS)
PG_CONFIG ?= pg_config

ifeq ($(USE_M3_RUNTIME),1)
ifeq ($(shell command -v $(PG_CONFIG) 2>/dev/null),)
$(error USE_M3_RUNTIME=1 requires PG_CONFIG to name an installed PostgreSQL client toolchain)
endif
M3_RUNTIME_PG_INCLUDEDIR := $(shell $(PG_CONFIG) --includedir)
M3_RUNTIME_PG_LIBDIR := $(shell $(PG_CONFIG) --libdir)
CFLAGS += -DUSE_M3_RUNTIME -I$(M3_RUNTIME_PG_INCLUDEDIR)
M3_RUNTIME_LIBS = -L$(M3_RUNTIME_PG_LIBDIR) -lpq
endif

ifeq ($(USE_RUST_RESOLVER),1)
CFLAGS += -DUSE_RUST_RESOLVER
RUST_FFI_LIB ?= ../rust/target/release/libmuhan_resource_ffi.a
EXTRA_LIBS += $(RUST_FFI_LIB) -ldl -lpthread
endif


OBJECTS = global.o main.o files1.o files2.o files3.o bank.o bank_store.o command1.o command2.o \
	  command3.o command4.o command5.o command6.o command7.o \
	  command8.o command9.o command10.o command11.o command12.o magic1.o magic2.o \
	 magic3.o magic4.o  magic5.o magic6.o magic7.o magic8.o \
	 dm1.o dm2.o dm3.o dm4.o dm5.o dm6.o io.o \
	  player.o creature.o room.o object.o update.o post.o action.o misc.o \
	  special1.o compress.o alias.o board.o resource_path.o utf8_text.o \
	  player_path.o player_store.o file_player_store.o player_recovery.o trusted_admission.o onboarding_admission.o onboarding_session.o onboarding_receipt.o onboarding_activation_binding.o onboarding_activation_save_capability.o onboarding_recovery.o \
	  legacy_identity_evidence.o legacy_identity_evidence_wire.o onboarding_evidence_control.o onboarding_evidence_emission.o

M3_RUNTIME_OBJECTS = character_save_journal_v2_runtime.o character_save_journal_v2_runtime_native.o \
	character_save_journal_v2_process_owner.o character_save_journal_v2_player_store.o \
	character_save_journal_v2_live_ops.o character_save_journal_v2_rpc_transport.o \
	character_save_journal_v2_bootstrap.o \
	character_save_journal_v2_rpc_transport_native.o character_save_journal_v2_deadline.o \
	character_save_journal_v2_deadline_native.o character_save_journal_v2_uuid.o \
	character_save_journal_v2_uuid_native.o character_save_journal_v2_protocol.o \
	character_save_journal_v2_recovery.o character_save_journal_v2_ack.o \
	character_save_journal_v2_publish.o character_save_journal_v2_route.o \
	character_save_journal_v2_writer.o character_save_journal_v2.o \
	player_record_serializer.o character_player_snapshot_v1_handoff.o \
	character_player_snapshot_v1_capture.o \
	character_player_snapshot_v1_capture_native.o \
	character_player_snapshot_v1_artifact.o character_player_snapshot_v1_receipt_pair.o \
	character_player_snapshot_v1_read_rehearsal.o \
	character_snapshot_shadow_outbox.o player_snapshot_v1.o \
	object_graph_v1.o cdto_v1.o

ifeq ($(USE_M3_RUNTIME),1)
ifneq ($(shell uname -s),Linux)
$(error USE_M3_RUNTIME=1 requires Linux deadline and UUID native adapters)
endif
OBJECTS += $(M3_RUNTIME_OBJECTS)
OBJECTS += onboarding_snapshot_command_consumer.o onboarding_activation_reservation_adapter.o \
	onboarding_activation_reservation_owner.o onboarding_activation_save_bridge.o \
	onboarding_activation_save_runtime_helper.o onboarding_activation_gate.o
endif

$(OUTFILE): $(OBJECTS)
	$(CC) $(CFLAGS) $(OBJECTS) -o $(OUTFILE) $(LIBS) $(EXTRA_LIBS) $(M3_RUNTIME_LIBS)
	chmod 777 $(OUTFILE)
#	strip $(OUTFILE)

# Need to add options to make utils here too
auth: auth.o
	$(CC) auth.o -o auth
	mv auth ../bin

clean: 
	rm -f $(OBJECTS) $(M3_RUNTIME_OBJECTS) $(OUTFILE)

# Pure, detached trusted-fact boundary; never links into the legacy binary.
trusted-alias-identity-context-v1-static-test:
	mkdir -p /tmp/muhan-unit
	$(CC) -std=gnu89 -fcommon -Wall -Wextra -Werror $(CDTO_V1_TEST_WARN_FLAGS) -I. -c trusted_alias_identity_context_v1.c -o /tmp/muhan-unit/trusted_alias_identity_context_v1_production.o
	python3 ../tests/unit/trusted_alias_identity_context_v1_static_test.py --object /tmp/muhan-unit/trusted_alias_identity_context_v1_production.o --source trusted_alias_identity_context_v1.c --header trusted_alias_identity_context_v1.h --makefile Makefile

trusted-alias-identity-context-v1-test: trusted-alias-identity-context-v1-static-test
	mkdir -p /tmp/muhan-unit
	$(CC) -std=gnu89 -fcommon -Wall -Wextra -Werror $(CDTO_V1_TEST_WARN_FLAGS) -I. ../tests/unit/trusted_alias_identity_context_v1_test.c trusted_alias_identity_context_v1.c alias_title_snapshot_manifest_v1.c alias_title_snapshot_v1.c cdto_v1.c -o /tmp/muhan-unit/trusted_alias_identity_context_v1_test
	/tmp/muhan-unit/trusted_alias_identity_context_v1_test

trusted-alias-identity-context-v1-sanitizer-test: trusted-alias-identity-context-v1-static-test
	mkdir -p /tmp/muhan-unit
	$(CC) -std=gnu89 -fcommon -Wall -Wextra -Werror $(CDTO_V1_TEST_WARN_FLAGS) -O1 -fno-omit-frame-pointer -fsanitize=address,undefined -I. ../tests/unit/trusted_alias_identity_context_v1_test.c trusted_alias_identity_context_v1.c alias_title_snapshot_manifest_v1.c alias_title_snapshot_v1.c cdto_v1.c -o /tmp/muhan-unit/trusted_alias_identity_context_v1_sanitizer_test
	ASAN_OPTIONS=$(DECODER_ASAN_OPTIONS) UBSAN_OPTIONS=halt_on_error=1 /tmp/muhan-unit/trusted_alias_identity_context_v1_sanitizer_test

unit-test:
	mkdir -p /tmp/muhan-unit
	python3 ../tests/unit/resource_tree_manifest_test.py
	$(MAKE) bank-store-test
	$(MAKE) bank-evidence-test
	$(CC) -std=gnu89 -fcommon -I. ../tests/unit/player_store_test.c player_store.c -o /tmp/muhan-unit/player_store_test
	/tmp/muhan-unit/player_store_test
	$(CC) -std=gnu89 -fcommon -I. ../tests/unit/player_store_file_test.c file_player_store.c player_path.c resource_path.c -o /tmp/muhan-unit/player_store_file_test
	/tmp/muhan-unit/player_store_file_test
	$(CC) -std=gnu89 -fcommon -I. ../tests/unit/runtime_path_test.c resource_path.c -o /tmp/muhan-unit/runtime_path_test
	/tmp/muhan-unit/runtime_path_test
	$(CC) -std=gnu89 -fcommon -I. -Dwrite=serializer_test_write ../tests/unit/files1_serializer_test.c files1.c -o /tmp/muhan-unit/files1_serializer_test
	/tmp/muhan-unit/files1_serializer_test
	$(MAKE) player-record-serializer-test
	$(MAKE) files1-decoder-test
	$(CC) -std=gnu89 -fcommon -I. -ffunction-sections -fdata-sections ../tests/unit/files2_load_obj_test.c files2.c resource_path.c $(UNIT_GC_FLAGS) -o /tmp/muhan-unit/files2_load_obj_test
	/tmp/muhan-unit/files2_load_obj_test
	$(CC) -std=gnu89 -fcommon -I. -ffunction-sections -fdata-sections -DMUHAN_UPDATE_SCHEDULE_TEST $(UNIT_LEGACY_WARN_FLAGS) ../tests/unit/update_schedule_test.c update.c $(UNIT_GC_FLAGS) -o /tmp/muhan-unit/update_schedule_test
	/tmp/muhan-unit/update_schedule_test
	$(CC) -std=gnu89 -fcommon -I. ../tests/unit/player_recovery_test.c player_recovery.c player_store.c -o /tmp/muhan-unit/player_recovery_test
	/tmp/muhan-unit/player_recovery_test
	$(CC) -std=gnu89 -fcommon -I. ../tests/unit/trusted_admission_test.c trusted_admission.c player_path.c utf8_text.c resource_path.c -o /tmp/muhan-unit/trusted_admission_test
	/tmp/muhan-unit/trusted_admission_test
	python3 ../tests/unit/trusted_admission_identity_binding_static_test.py
	$(MAKE) onboarding-admission-test
	python3 ../tests/unit/onboarding_credential_lifecycle_test.py
	$(CC) -std=gnu89 -fcommon -I. ../tests/unit/onboarding_receipt_test.c onboarding_receipt.c resource_path.c -o /tmp/muhan-unit/onboarding_receipt_test
	/tmp/muhan-unit/onboarding_receipt_test
	$(MAKE) onboarding-activation-binding-test
	$(CC) -std=gnu89 -fcommon -I. ../tests/unit/onboarding_recovery_test.c onboarding_recovery.c resource_path.c utf8_text.c -o /tmp/muhan-unit/onboarding_recovery_test
	/tmp/muhan-unit/onboarding_recovery_test
	$(MAKE) onboarding-evidence-emission-test
	$(MAKE) cdto-v1-test
	$(MAKE) alias-title-snapshot-manifest-v1-test
	$(MAKE) alias-title-snapshot-v1-outbox-test
	$(MAKE) object-v1-test
	$(MAKE) object-graph-v1-test
	$(MAKE) creature-v1-test
	$(MAKE) player-snapshot-v1-test
	$(MAKE) character-save-journal-test
	$(MAKE) character-save-journal-v2-test
	$(MAKE) character-save-journal-v2-writer-test
	$(MAKE) character-save-journal-v2-writer-bootstrap-test
	$(MAKE) character-save-journal-v2-uuid-test
	$(MAKE) character-save-journal-v2-route-test
	$(MAKE) character-save-journal-v2-publish-test
	$(MAKE) character-save-journal-v2-ack-test
	$(MAKE) character-save-journal-v2-recovery-test
	$(MAKE) character-save-journal-v2-receipt-transport-test
	$(MAKE) character-save-journal-v2-rpc-transport-test
	$(MAKE) character-save-journal-v2-live-ops-test
	$(MAKE) character-save-journal-v2-bootstrap-test
	$(MAKE) character-save-journal-v2-player-store-test
	$(MAKE) character-save-journal-v2-player-store-live-observer-test
	$(MAKE) character-save-journal-v2-deadline-test
	$(MAKE) character-save-journal-v2-process-owner-test
	$(MAKE) onboarding-activation-reservation-adapter-test
	$(MAKE) onboarding-activation-reservation-owner-test
	$(MAKE) onboarding-activation-owner-runtime-test
	$(MAKE) character-save-journal-v2-runtime-test
	$(MAKE) m3-feature-off-legacy-authority-test
	$(MAKE) onboarding-activation-snapshot-receipt-test
	$(MAKE) m3-live-save-all-shadow-binding-test
	$(MAKE) m3-save-all-ply-receipt-path-test
	$(MAKE) m3-runtime-idle-hook-static-test
	$(MAKE) m3-wake-v1-test
	$(MAKE) m3-wake-supervisor-test
	$(MAKE) m3-wake-transport-test
	$(MAKE) character-save-journal-v2-protocol-test
	$(MAKE) character-save-journal-v2-crash-test
	$(MAKE) character-snapshot-shadow-outbox-test
	$(MAKE) character-player-snapshot-v1-artifact-test
	$(MAKE) character-player-snapshot-v1-receipt-pair-test
	$(MAKE) character-player-snapshot-v1-capture-test
	$(MAKE) character-player-snapshot-v1-capture-native-test
	$(MAKE) character-player-snapshot-v1-handoff-test

bank-store-test:
	mkdir -p /tmp/muhan-unit
	$(CC) -std=gnu89 -fcommon -I. ../tests/unit/bank_store_test.c bank_store.c -o /tmp/muhan-unit/bank_store_test
	/tmp/muhan-unit/bank_store_test
	$(CC) -std=gnu89 -fcommon -I. -ffunction-sections -fdata-sections $(UNIT_LEGACY_WARN_FLAGS) ../tests/unit/bank_legacy_abi_test.c bank.c bank_store.c resource_path.c $(UNIT_GC_FLAGS) -o /tmp/muhan-unit/bank_legacy_abi_test
	/tmp/muhan-unit/bank_legacy_abi_test

# Read-only legacy bank metadata; absent from OBJECTS so it cannot become a
# gameplay persistence path without an explicit later composition.
bank-evidence-test:
	mkdir -p /tmp/muhan-unit
	$(CC) -std=gnu89 -fcommon -Wall -Wextra $(UNIT_LEGACY_WARN_FLAGS) -DBANK_EVIDENCE_TESTING -I. -ffunction-sections -fdata-sections ../tests/unit/bank_evidence_test.c bank_evidence.c bank_file_locator.c bank_snapshot_v1.c object_graph_v1.c cdto_v1.c bank_store.c player_path.c utf8_text.c resource_path.c $(UNIT_GC_FLAGS) -o /tmp/muhan-unit/bank_evidence_test
	/tmp/muhan-unit/bank_evidence_test

bank-evidence-sanitizer-test:
	mkdir -p /tmp/muhan-unit
	$(CC) -std=gnu89 -fcommon -Wno-deprecated-non-prototype -DBANK_EVIDENCE_TESTING -I. -O1 -fno-omit-frame-pointer -fsanitize=address,undefined -ffunction-sections -fdata-sections ../tests/unit/bank_evidence_test.c bank_evidence.c bank_file_locator.c bank_snapshot_v1.c object_graph_v1.c cdto_v1.c bank_store.c player_path.c utf8_text.c resource_path.c $(UNIT_GC_FLAGS) -o /tmp/muhan-unit/bank_evidence_sanitizer_test
	ASAN_OPTIONS=$(DECODER_ASAN_OPTIONS) UBSAN_OPTIONS=halt_on_error=1 /tmp/muhan-unit/bank_evidence_sanitizer_test

# Read-only metadata seam; it stays outside default OBJECTS so login and save
# behavior remain entirely legacy-owned until a separate composition opts in.
legacy-identity-evidence-test:
	mkdir -p /tmp/muhan-unit
	$(CC) -std=gnu89 -fcommon -DFILE_PLAYER_STORE_TESTING -I. -ffunction-sections -fdata-sections $(UNIT_LEGACY_WARN_FLAGS) ../tests/unit/legacy_identity_evidence_test.c legacy_identity_evidence.c legacy_identity_evidence_wire.c file_player_store.c player_path.c resource_path.c $(UNIT_GC_FLAGS) -o /tmp/muhan-unit/legacy_identity_evidence_test
	/tmp/muhan-unit/legacy_identity_evidence_test

legacy-identity-evidence-sanitizer-test:
	mkdir -p /tmp/muhan-unit
	$(CC) -std=gnu89 -fcommon -DFILE_PLAYER_STORE_TESTING -I. -ffunction-sections -fdata-sections $(UNIT_LEGACY_WARN_FLAGS) -O1 -fno-omit-frame-pointer -fsanitize=address,undefined ../tests/unit/legacy_identity_evidence_test.c legacy_identity_evidence.c legacy_identity_evidence_wire.c file_player_store.c player_path.c resource_path.c $(UNIT_GC_FLAGS) -o /tmp/muhan-unit/legacy_identity_evidence_sanitizer_test
	ASAN_OPTIONS=$(DECODER_ASAN_OPTIONS) UBSAN_OPTIONS=halt_on_error=1 /tmp/muhan-unit/legacy_identity_evidence_sanitizer_test

admission-identity-conformance-test:
	../scripts/run-admission-identity-conformance.sh

# Closed metadata-only C/Rust boundary.  This source is intentionally absent
# from OBJECTS, so no live MUD path links or invokes it by default.
legacy-identity-evidence-wire-test:
	mkdir -p /tmp/muhan-unit
	$(CC) -std=gnu89 -fcommon -I. -ffunction-sections -fdata-sections $(UNIT_LEGACY_WARN_FLAGS) ../tests/unit/legacy_identity_evidence_wire_test.c legacy_identity_evidence_wire.c $(UNIT_GC_FLAGS) -o /tmp/muhan-unit/legacy_identity_evidence_wire_test
	/tmp/muhan-unit/legacy_identity_evidence_wire_test

legacy-identity-evidence-wire-sanitizer-test:
	mkdir -p /tmp/muhan-unit
	$(CC) -std=gnu89 -fcommon -I. -O1 -fno-omit-frame-pointer -fsanitize=address,undefined ../tests/unit/legacy_identity_evidence_wire_test.c legacy_identity_evidence_wire.c -o /tmp/muhan-unit/legacy_identity_evidence_wire_sanitizer_test
	ASAN_OPTIONS=$(DECODER_ASAN_OPTIONS) UBSAN_OPTIONS=halt_on_error=1 /tmp/muhan-unit/legacy_identity_evidence_wire_sanitizer_test

# Feature-off metadata envelope.  This stays outside OBJECTS and is not wired
# into the live session, game relay, or onboarding state machine.
onboarding-evidence-control-test:
	mkdir -p /tmp/muhan-unit
	$(CC) -std=gnu89 -fcommon -I. -ffunction-sections -fdata-sections ../tests/unit/onboarding_evidence_control_test.c onboarding_evidence_control.c legacy_identity_evidence_wire.c onboarding_admission.c player_path.c utf8_text.c $(UNIT_GC_FLAGS) -o /tmp/muhan-unit/onboarding_evidence_control_test
	/tmp/muhan-unit/onboarding_evidence_control_test
	python3 ../tests/unit/onboarding_evidence_control_no_live_link_test.py

onboarding-evidence-control-sanitizer-test:
	mkdir -p /tmp/muhan-unit
	$(CC) -std=gnu89 -fcommon -I. -ffunction-sections -fdata-sections -O1 -fno-omit-frame-pointer -fsanitize=address,undefined ../tests/unit/onboarding_evidence_control_test.c onboarding_evidence_control.c legacy_identity_evidence_wire.c onboarding_admission.c player_path.c utf8_text.c $(UNIT_GC_FLAGS) -o /tmp/muhan-unit/onboarding_evidence_control_sanitizer_test
	ASAN_OPTIONS=$(DECODER_ASAN_OPTIONS) UBSAN_OPTIONS=halt_on_error=1 /tmp/muhan-unit/onboarding_evidence_control_sanitizer_test
	python3 ../tests/unit/onboarding_evidence_control_no_live_link_test.py

onboarding-evidence-emission-test:
	mkdir -p /tmp/muhan-unit
	$(CC) -std=gnu89 -fcommon -DFILE_PLAYER_STORE_TESTING -I. -ffunction-sections -fdata-sections $(UNIT_LEGACY_WARN_FLAGS) ../tests/unit/onboarding_evidence_emission_test.c onboarding_evidence_emission.c onboarding_evidence_control.c legacy_identity_evidence.c legacy_identity_evidence_wire.c file_player_store.c player_path.c resource_path.c onboarding_admission.c $(UNIT_GC_FLAGS) -o /tmp/muhan-unit/onboarding_evidence_emission_test
	/tmp/muhan-unit/onboarding_evidence_emission_test

onboarding-evidence-emission-sanitizer-test:
	mkdir -p /tmp/muhan-unit
	$(CC) -std=gnu89 -fcommon -DFILE_PLAYER_STORE_TESTING -I. -ffunction-sections -fdata-sections $(UNIT_LEGACY_WARN_FLAGS) -O1 -fno-omit-frame-pointer -fsanitize=address,undefined ../tests/unit/onboarding_evidence_emission_test.c onboarding_evidence_emission.c onboarding_evidence_control.c legacy_identity_evidence.c legacy_identity_evidence_wire.c file_player_store.c player_path.c resource_path.c onboarding_admission.c $(UNIT_GC_FLAGS) -o /tmp/muhan-unit/onboarding_evidence_emission_sanitizer_test
	ASAN_OPTIONS=$(DECODER_ASAN_OPTIONS) UBSAN_OPTIONS=halt_on_error=1 /tmp/muhan-unit/onboarding_evidence_emission_sanitizer_test

onboarding-activation-binding-test:
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. ../tests/unit/onboarding_activation_binding_test.c onboarding_activation_binding.c resource_path.c -o /tmp/muhan-unit/onboarding_activation_binding_test
	/tmp/muhan-unit/onboarding_activation_binding_test

onboarding-activation-binding-sanitizer-test:
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. -O1 -fno-omit-frame-pointer -fsanitize=address,undefined ../tests/unit/onboarding_activation_binding_test.c onboarding_activation_binding.c resource_path.c -o /tmp/muhan-unit/onboarding_activation_binding_sanitizer_test
	ASAN_OPTIONS=$(DECODER_ASAN_OPTIONS) UBSAN_OPTIONS=halt_on_error=1 /tmp/muhan-unit/onboarding_activation_binding_sanitizer_test

onboarding-activation-save-capability-test:
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. ../tests/unit/onboarding_activation_save_capability_test.c onboarding_activation_save_capability.c -o /tmp/muhan-unit/onboarding_activation_save_capability_test
	/tmp/muhan-unit/onboarding_activation_save_capability_test
	python3 ../tests/unit/onboarding_activation_save_capability_static_test.py

onboarding-activation-save-capability-sanitizer-test:
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. -O1 -fno-omit-frame-pointer -fsanitize=address,undefined ../tests/unit/onboarding_activation_save_capability_test.c onboarding_activation_save_capability.c -o /tmp/muhan-unit/onboarding_activation_save_capability_sanitizer_test
	ASAN_OPTIONS=$(DECODER_ASAN_OPTIONS) UBSAN_OPTIONS=halt_on_error=1 /tmp/muhan-unit/onboarding_activation_save_capability_sanitizer_test
	python3 ../tests/unit/onboarding_activation_save_capability_static_test.py

# This bridge remains a caller-owned composition seam: it does not join
# OBJECTS or install a resolver for default, claim, recovery, or legacy saves.
onboarding-activation-save-bridge-test:
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. ../tests/unit/onboarding_activation_save_bridge_test.c onboarding_activation_save_bridge.c onboarding_activation_save_capability.c -o /tmp/muhan-unit/onboarding_activation_save_bridge_test
	/tmp/muhan-unit/onboarding_activation_save_bridge_test
	python3 ../tests/unit/onboarding_activation_save_bridge_static_test.py

onboarding-activation-save-bridge-sanitizer-test:
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. -O1 -fno-omit-frame-pointer -fsanitize=address,undefined ../tests/unit/onboarding_activation_save_bridge_test.c onboarding_activation_save_bridge.c onboarding_activation_save_capability.c -o /tmp/muhan-unit/onboarding_activation_save_bridge_sanitizer_test
	ASAN_OPTIONS=$(DECODER_ASAN_OPTIONS) UBSAN_OPTIONS=halt_on_error=1 /tmp/muhan-unit/onboarding_activation_save_bridge_sanitizer_test
	python3 ../tests/unit/onboarding_activation_save_bridge_static_test.py

# Caller-owned runtime helper seam: fake weak protocol hooks deliberately keep
# this focused on one active owner/store dispatch, not production wiring.
onboarding-activation-save-runtime-helper-test:
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. ../tests/unit/onboarding_activation_save_v4_composition_test.c character_save_journal_v2_player_store.c onboarding_activation_save_runtime_helper.c onboarding_activation_save_bridge.c onboarding_activation_save_capability.c onboarding_activation_gate.c -o /tmp/muhan-unit/onboarding_activation_save_runtime_helper_test
	/tmp/muhan-unit/onboarding_activation_save_runtime_helper_test
	python3 ../tests/unit/onboarding_activation_gate_static_test.py

onboarding-activation-save-runtime-helper-sanitizer-test:
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. -O1 -fno-omit-frame-pointer -fsanitize=address,undefined ../tests/unit/onboarding_activation_save_v4_composition_test.c character_save_journal_v2_player_store.c onboarding_activation_save_runtime_helper.c onboarding_activation_save_bridge.c onboarding_activation_save_capability.c onboarding_activation_gate.c -o /tmp/muhan-unit/onboarding_activation_save_runtime_helper_sanitizer_test
	ASAN_OPTIONS=$(DECODER_ASAN_OPTIONS) UBSAN_OPTIONS=halt_on_error=1 /tmp/muhan-unit/onboarding_activation_save_runtime_helper_sanitizer_test
	python3 ../tests/unit/onboarding_activation_gate_static_test.py

onboarding-activation-owner-runtime-test:
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. ../tests/unit/onboarding_activation_owner_runtime_test.c onboarding_activation_gate.c -o /tmp/muhan-unit/onboarding_activation_owner_runtime_test
	/tmp/muhan-unit/onboarding_activation_owner_runtime_test

onboarding-activation-command-lifecycle-test:
	mkdir -p /tmp/muhan-unit
	$(CC) $(CFLAGS) -DUSE_M3_RUNTIME -DONBOARDING_ACTIVATION_COMMAND_TESTING -I. -ffunction-sections -fdata-sections -c command1.c -o /tmp/muhan-unit/command1_activation_runtime.o
	$(CC) $(V2_TEST_CFLAGS) -DUSE_M3_RUNTIME -I. -ffunction-sections -fdata-sections ../tests/unit/onboarding_activation_command_lifecycle_test.c ../tests/unit/onboarding_activation_command_lifecycle_globals.c ../tests/unit/onboarding_activation_command_lifecycle_stubs.c onboarding_admission.c onboarding_activation_save_capability.c onboarding_activation_gate.c /tmp/muhan-unit/command1_activation_runtime.o $(UNIT_GC_FLAGS) -o /tmp/muhan-unit/onboarding_activation_command_runtime_test
	/tmp/muhan-unit/onboarding_activation_command_runtime_test
	$(CC) $(CFLAGS) -DONBOARDING_ACTIVATION_COMMAND_TESTING -I. -ffunction-sections -fdata-sections -c command1.c -o /tmp/muhan-unit/command1_activation_legacy.o
	$(CC) $(V2_TEST_CFLAGS) -I. -ffunction-sections -fdata-sections ../tests/unit/onboarding_activation_command_lifecycle_test.c ../tests/unit/onboarding_activation_command_lifecycle_globals.c ../tests/unit/onboarding_activation_command_lifecycle_stubs.c onboarding_admission.c onboarding_activation_save_capability.c /tmp/muhan-unit/command1_activation_legacy.o $(UNIT_GC_FLAGS) -o /tmp/muhan-unit/onboarding_activation_command_legacy_test
	/tmp/muhan-unit/onboarding_activation_command_legacy_test

onboarding-activation-command-lifecycle-sanitizer-test:
	mkdir -p /tmp/muhan-unit
	$(CC) $(CFLAGS) -DUSE_M3_RUNTIME -DONBOARDING_ACTIVATION_COMMAND_TESTING -I. -ffunction-sections -fdata-sections -O1 -fno-omit-frame-pointer -fsanitize=address,undefined -c command1.c -o /tmp/muhan-unit/command1_activation_runtime_sanitizer.o
	$(CC) $(V2_TEST_CFLAGS) -DUSE_M3_RUNTIME -I. -ffunction-sections -fdata-sections -O1 -fno-omit-frame-pointer -fsanitize=address,undefined ../tests/unit/onboarding_activation_command_lifecycle_test.c ../tests/unit/onboarding_activation_command_lifecycle_globals.c ../tests/unit/onboarding_activation_command_lifecycle_stubs.c onboarding_admission.c onboarding_activation_save_capability.c onboarding_activation_gate.c /tmp/muhan-unit/command1_activation_runtime_sanitizer.o $(UNIT_GC_FLAGS) -o /tmp/muhan-unit/onboarding_activation_command_runtime_sanitizer_test
	ASAN_OPTIONS=$(DECODER_ASAN_OPTIONS) UBSAN_OPTIONS=halt_on_error=1 /tmp/muhan-unit/onboarding_activation_command_runtime_sanitizer_test
	$(CC) $(CFLAGS) -DONBOARDING_ACTIVATION_COMMAND_TESTING -I. -ffunction-sections -fdata-sections -O1 -fno-omit-frame-pointer -fsanitize=address,undefined -c command1.c -o /tmp/muhan-unit/command1_activation_legacy_sanitizer.o
	$(CC) $(V2_TEST_CFLAGS) -I. -ffunction-sections -fdata-sections -O1 -fno-omit-frame-pointer -fsanitize=address,undefined ../tests/unit/onboarding_activation_command_lifecycle_test.c ../tests/unit/onboarding_activation_command_lifecycle_globals.c ../tests/unit/onboarding_activation_command_lifecycle_stubs.c onboarding_admission.c onboarding_activation_save_capability.c /tmp/muhan-unit/command1_activation_legacy_sanitizer.o $(UNIT_GC_FLAGS) -o /tmp/muhan-unit/onboarding_activation_command_legacy_sanitizer_test
	ASAN_OPTIONS=$(DECODER_ASAN_OPTIONS) UBSAN_OPTIONS=halt_on_error=1 /tmp/muhan-unit/onboarding_activation_command_legacy_sanitizer_test

onboarding-activation-command-lifecycle-static-test:
	mkdir -p /tmp/muhan-unit
	$(CC) $(CFLAGS) -I. -ffunction-sections -fdata-sections -c command1.c -o /tmp/muhan-unit/command1_activation_production_audit.o
	@if nm /tmp/muhan-unit/command1_activation_production_audit.o | grep -q onboarding_activation_command_test; then echo "test-only activation command seam leaked into production object" >&2; exit 1; fi
	python3 ../tests/unit/onboarding_activation_gate_static_test.py

# Focused production-V4 composition with local journal fixtures and fake
# lower-level route/serializer/receipt callbacks only; no live service linkage.
onboarding-activation-save-v4-composition-test:
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -DCHARACTER_SAVE_JOURNAL_V2_TESTING -DCHARACTER_SAVE_JOURNAL_V2_WRITER_TESTING -DCHARACTER_SAVE_JOURNAL_V2_PUBLISH_TESTING -DCHARACTER_SAVE_JOURNAL_V2_ACK_TESTING -I. ../tests/unit/onboarding_activation_save_v4_protocol_composition_test.c onboarding_activation_save_bridge.c onboarding_activation_save_capability.c character_save_journal_v2_protocol.c character_save_journal_v2_recovery.c character_save_journal_v2_ack.c character_save_journal_v2_publish.c character_save_journal_v2_route.c character_save_journal_v2_writer.c character_save_journal_v2.c utf8_text.c -o /tmp/muhan-unit/onboarding_activation_save_v4_composition_test
	/tmp/muhan-unit/onboarding_activation_save_v4_composition_test

onboarding-activation-save-v4-composition-sanitizer-test:
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -DCHARACTER_SAVE_JOURNAL_V2_TESTING -DCHARACTER_SAVE_JOURNAL_V2_WRITER_TESTING -DCHARACTER_SAVE_JOURNAL_V2_PUBLISH_TESTING -DCHARACTER_SAVE_JOURNAL_V2_ACK_TESTING -I. -O1 -fno-omit-frame-pointer -fsanitize=address,undefined ../tests/unit/onboarding_activation_save_v4_protocol_composition_test.c onboarding_activation_save_bridge.c onboarding_activation_save_capability.c character_save_journal_v2_protocol.c character_save_journal_v2_recovery.c character_save_journal_v2_ack.c character_save_journal_v2_publish.c character_save_journal_v2_route.c character_save_journal_v2_writer.c character_save_journal_v2.c utf8_text.c -o /tmp/muhan-unit/onboarding_activation_save_v4_composition_sanitizer_test
	ASAN_OPTIONS=$(DECODER_ASAN_OPTIONS) UBSAN_OPTIONS=halt_on_error=1 /tmp/muhan-unit/onboarding_activation_save_v4_composition_sanitizer_test

# Local-only command reservation seam.  This deliberately stays out of both
# OBJECTS and M3_RUNTIME_OBJECTS until a separate runtime wiring slice exists.
onboarding-snapshot-command-consumer-test:
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. ../tests/unit/onboarding_snapshot_command_consumer_test.c onboarding_snapshot_command_consumer.c onboarding_activation_binding.c resource_path.c -o /tmp/muhan-unit/onboarding_snapshot_command_consumer_test
	/tmp/muhan-unit/onboarding_snapshot_command_consumer_test

onboarding-snapshot-command-consumer-sanitizer-test:
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. -O1 -fno-omit-frame-pointer -fsanitize=address,undefined ../tests/unit/onboarding_snapshot_command_consumer_test.c onboarding_snapshot_command_consumer.c onboarding_activation_binding.c resource_path.c -o /tmp/muhan-unit/onboarding_snapshot_command_consumer_sanitizer_test
	ASAN_OPTIONS=$(DECODER_ASAN_OPTIONS) UBSAN_OPTIONS=halt_on_error=1 /tmp/muhan-unit/onboarding_snapshot_command_consumer_sanitizer_test

# Evidence harness for the next ownership slice.  It joins the local
# descriptor-rooted reservation proof to the existing V4 candidate bridge in
# a test binary only; no live owner currently owns such a descriptor.
onboarding-activated-command-snapshot-harness-static-test:
	python3 ../tests/unit/onboarding_snapshot_command_runtime_gap_test.py

onboarding-activated-command-snapshot-harness-test: onboarding-activated-command-snapshot-harness-static-test
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. ../tests/unit/onboarding_activated_command_snapshot_harness_test.c onboarding_snapshot_command_consumer.c onboarding_activation_binding.c onboarding_activation_save_capability.c onboarding_activation_save_bridge.c resource_path.c -o /tmp/muhan-unit/onboarding_activated_command_snapshot_harness_test
	/tmp/muhan-unit/onboarding_activated_command_snapshot_harness_test

onboarding-activated-command-snapshot-harness-sanitizer-test: onboarding-activated-command-snapshot-harness-static-test
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. -O1 -fno-omit-frame-pointer -fsanitize=address,undefined ../tests/unit/onboarding_activated_command_snapshot_harness_test.c onboarding_snapshot_command_consumer.c onboarding_activation_binding.c onboarding_activation_save_capability.c onboarding_activation_save_bridge.c resource_path.c -o /tmp/muhan-unit/onboarding_activated_command_snapshot_harness_sanitizer_test
	ASAN_OPTIONS=$(DECODER_ASAN_OPTIONS) UBSAN_OPTIONS=halt_on_error=1 /tmp/muhan-unit/onboarding_activated_command_snapshot_harness_sanitizer_test

# Feature-off receipt proof at the existing artifact/journal boundary.  It is
# intentionally not linked by either the default or M3 live object graph.
onboarding-activation-snapshot-receipt-static-test:
	python3 ../tests/unit/onboarding_activation_snapshot_receipt_static_test.py

onboarding-activation-snapshot-receipt-test: m3-feature-off-legacy-authority-test onboarding-activation-snapshot-receipt-static-test
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -DONBOARDING_ACTIVATION_SNAPSHOT_RECEIPT_TESTING -I. ../tests/unit/onboarding_activation_snapshot_receipt_test.c onboarding_activation_snapshot_receipt.c -o /tmp/muhan-unit/onboarding_activation_snapshot_receipt_test
	/tmp/muhan-unit/onboarding_activation_snapshot_receipt_test

onboarding-activation-snapshot-receipt-sanitizer-test: m3-feature-off-legacy-authority-sanitizer-test onboarding-activation-snapshot-receipt-static-test
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -DONBOARDING_ACTIVATION_SNAPSHOT_RECEIPT_TESTING -I. -O1 -fno-omit-frame-pointer -fsanitize=address,undefined ../tests/unit/onboarding_activation_snapshot_receipt_test.c onboarding_activation_snapshot_receipt.c -o /tmp/muhan-unit/onboarding_activation_snapshot_receipt_sanitizer_test
	ASAN_OPTIONS=$(DECODER_ASAN_OPTIONS) UBSAN_OPTIONS=halt_on_error=1 /tmp/muhan-unit/onboarding_activation_snapshot_receipt_sanitizer_test

# Caller-owned composition only: this bridge installer remains outside both
# default and generic M3 runtime object graphs.
onboarding-activation-reservation-adapter-static-test:
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. -c onboarding_activation_reservation_adapter.c -o /tmp/muhan-unit/onboarding_activation_reservation_adapter_prod.o
	python3 ../tests/unit/onboarding_activation_reservation_adapter_static_test.py

onboarding-activation-reservation-adapter-test: onboarding-activation-reservation-adapter-static-test
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. ../tests/unit/onboarding_activation_reservation_adapter_test.c onboarding_activation_reservation_adapter.c -o /tmp/muhan-unit/onboarding_activation_reservation_adapter_test
	/tmp/muhan-unit/onboarding_activation_reservation_adapter_test

onboarding-activation-reservation-adapter-sanitizer-test: onboarding-activation-reservation-adapter-static-test
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. -O1 -fno-omit-frame-pointer -fsanitize=address,undefined ../tests/unit/onboarding_activation_reservation_adapter_test.c onboarding_activation_reservation_adapter.c -o /tmp/muhan-unit/onboarding_activation_reservation_adapter_sanitizer_test
	ASAN_OPTIONS=$(DECODER_ASAN_OPTIONS) UBSAN_OPTIONS=halt_on_error=1 /tmp/muhan-unit/onboarding_activation_reservation_adapter_sanitizer_test

onboarding-activation-reservation-owner-test: onboarding-activation-reservation-adapter-static-test
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. ../tests/unit/onboarding_activation_reservation_owner_test.c onboarding_activation_reservation_owner.c -o /tmp/muhan-unit/onboarding_activation_reservation_owner_test
	/tmp/muhan-unit/onboarding_activation_reservation_owner_test

onboarding-activation-reservation-owner-sanitizer-test: onboarding-activation-reservation-adapter-static-test
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. -O1 -fno-omit-frame-pointer -fsanitize=address,undefined ../tests/unit/onboarding_activation_reservation_owner_test.c onboarding_activation_reservation_owner.c -o /tmp/muhan-unit/onboarding_activation_reservation_owner_sanitizer_test
	ASAN_OPTIONS=$(DECODER_ASAN_OPTIONS) UBSAN_OPTIONS=halt_on_error=1 /tmp/muhan-unit/onboarding_activation_reservation_owner_sanitizer_test

onboarding-admission-test:
	mkdir -p /tmp/muhan-unit
	$(CC) -std=gnu89 -fcommon -I. ../tests/unit/onboarding_admission_test.c onboarding_admission.c onboarding_session.c onboarding_receipt.c trusted_admission.c player_path.c utf8_text.c resource_path.c -o /tmp/muhan-unit/onboarding_admission_test
	/tmp/muhan-unit/onboarding_admission_test

onboarding-admission-sanitizer-test:
	mkdir -p /tmp/muhan-unit
	$(CC) -std=gnu89 -fcommon -I. -O1 -fno-omit-frame-pointer -fsanitize=address,undefined ../tests/unit/onboarding_admission_test.c onboarding_admission.c onboarding_session.c onboarding_receipt.c trusted_admission.c player_path.c utf8_text.c resource_path.c -o /tmp/muhan-unit/onboarding_admission_sanitizer_test
	ASAN_OPTIONS=$(DECODER_ASAN_OPTIONS) UBSAN_OPTIONS=halt_on_error=1 /tmp/muhan-unit/onboarding_admission_sanitizer_test

m3-feature-off-legacy-authority-test:
	mkdir -p /tmp/muhan-unit
	$(CC) -std=gnu89 -fcommon -I. ../tests/unit/m3_feature_off_legacy_authority_test.c character_save_journal_v2_runtime.c player_store.c file_player_store.c player_path.c resource_path.c -o /tmp/muhan-unit/m3_feature_off_legacy_authority_test
	/tmp/muhan-unit/m3_feature_off_legacy_authority_test

m3-feature-off-legacy-authority-sanitizer-test:
	mkdir -p /tmp/muhan-unit
	$(CC) -std=gnu89 -fcommon -I. -O1 -fno-omit-frame-pointer -fsanitize=address,undefined ../tests/unit/m3_feature_off_legacy_authority_test.c character_save_journal_v2_runtime.c player_store.c file_player_store.c player_path.c resource_path.c -o /tmp/muhan-unit/m3_feature_off_legacy_authority_sanitizer_test
	ASAN_OPTIONS=$(DECODER_ASAN_OPTIONS) UBSAN_OPTIONS=halt_on_error=1 /tmp/muhan-unit/m3_feature_off_legacy_authority_sanitizer_test

# This links the real bulk-save caller and savegame adapter against a
# deterministic shadow-binding seam; it never opens a database or network.
m3-live-save-all-shadow-binding-test:
	mkdir -p /tmp/muhan-unit
	$(CC) -std=gnu89 -fcommon -DUSE_M3_RUNTIME -I. -ffunction-sections -fdata-sections $(UNIT_LEGACY_WARN_FLAGS) ../tests/unit/m3_live_save_all_shadow_binding_test.c files2.c command8.c global.c character_save_journal_v2_runtime.c player_store.c $(UNIT_GC_FLAGS) -o /tmp/muhan-unit/m3_live_save_all_shadow_binding_test
	/tmp/muhan-unit/m3_live_save_all_shadow_binding_test

m3-live-save-all-shadow-binding-sanitizer-test:
	mkdir -p /tmp/muhan-unit
	$(CC) -std=gnu89 -fcommon -DUSE_M3_RUNTIME -I. -ffunction-sections -fdata-sections $(UNIT_LEGACY_WARN_FLAGS) -O1 -fno-omit-frame-pointer -fsanitize=address,undefined ../tests/unit/m3_live_save_all_shadow_binding_test.c files2.c command8.c global.c character_save_journal_v2_runtime.c player_store.c $(UNIT_GC_FLAGS) -o /tmp/muhan-unit/m3_live_save_all_shadow_binding_sanitizer_test
	ASAN_OPTIONS=$(DECODER_ASAN_OPTIONS) UBSAN_OPTIONS=halt_on_error=1 /tmp/muhan-unit/m3_live_save_all_shadow_binding_sanitizer_test

# This is a hermetic integration slice for the actual legacy bulk-save caller.
# It binds the production journal PlayerStore via the runtime's shadow seam and
# uses only an in-process RPC fixture plus an mkdtemp journal root.
m3-save-all-ply-receipt-path-test:
	mkdir -p /tmp/muhan-unit
	$(CC) -std=gnu89 -fcommon -DUSE_M3_RUNTIME -DCHARACTER_SAVE_JOURNAL_V2_TESTING -DCHARACTER_SAVE_JOURNAL_V2_WRITER_TESTING -DCHARACTER_SAVE_JOURNAL_V2_PUBLISH_TESTING -DCHARACTER_SAVE_JOURNAL_V2_ACK_TESTING -I. -ffunction-sections -fdata-sections $(UNIT_LEGACY_WARN_FLAGS) ../tests/unit/m3_save_all_ply_receipt_path_test.c files2.c command8.c global.c character_save_journal_v2_runtime.c player_store.c character_save_journal_v2_player_store.c character_save_journal_v2_bootstrap.c character_save_journal_v2_live_ops.c character_save_journal_v2_rpc_transport.c character_save_journal_v2_protocol.c character_save_journal_v2_recovery.c character_save_journal_v2_ack.c character_save_journal_v2_publish.c character_save_journal_v2_route.c character_save_journal_v2_writer.c character_save_journal_v2.c utf8_text.c $(UNIT_GC_FLAGS) -o /tmp/muhan-unit/m3_save_all_ply_receipt_path_test
	/tmp/muhan-unit/m3_save_all_ply_receipt_path_test

character-snapshot-shadow-outbox-test:
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. -c character_snapshot_shadow_outbox.c -o /tmp/muhan-unit/character_snapshot_shadow_outbox_prod.o
	$(CC) $(V2_TEST_CFLAGS) -DCHARACTER_SNAPSHOT_SHADOW_OUTBOX_TESTING -I. ../tests/unit/character_snapshot_shadow_outbox_test.c character_snapshot_shadow_outbox.c -o /tmp/muhan-unit/character_snapshot_shadow_outbox_test
	/tmp/muhan-unit/character_snapshot_shadow_outbox_test

character-snapshot-shadow-outbox-sanitizer-test:
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -DCHARACTER_SNAPSHOT_SHADOW_OUTBOX_TESTING -I. -O1 -fno-omit-frame-pointer -fsanitize=address,undefined ../tests/unit/character_snapshot_shadow_outbox_test.c character_snapshot_shadow_outbox.c -o /tmp/muhan-unit/character_snapshot_shadow_outbox_sanitizer_test
	ASAN_OPTIONS=$(DECODER_ASAN_OPTIONS) UBSAN_OPTIONS=halt_on_error=1 /tmp/muhan-unit/character_snapshot_shadow_outbox_sanitizer_test

# This immutable capture boundary never enters default OBJECTS: the legacy
# executable remains unaware of artifacts.  The Linux M3 opt-in graph above
# is the sole production composition that links it.
character-player-snapshot-v1-artifact-test:
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. -c character_player_snapshot_v1_artifact.c -o /tmp/muhan-unit/character_player_snapshot_v1_artifact_prod.o
	$(CC) $(V2_TEST_CFLAGS) -DCHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_TESTING -I. ../tests/unit/character_player_snapshot_v1_artifact_test.c character_player_snapshot_v1_artifact.c player_snapshot_v1.c object_graph_v1.c cdto_v1.c -o /tmp/muhan-unit/character_player_snapshot_v1_artifact_test
	/tmp/muhan-unit/character_player_snapshot_v1_artifact_test

character-player-snapshot-v1-artifact-sanitizer-test:
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -DCHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_TESTING -I. -O1 -fno-omit-frame-pointer -fsanitize=address,undefined ../tests/unit/character_player_snapshot_v1_artifact_test.c character_player_snapshot_v1_artifact.c player_snapshot_v1.c object_graph_v1.c cdto_v1.c -o /tmp/muhan-unit/character_player_snapshot_v1_artifact_sanitizer_test
	ASAN_OPTIONS=$(DECODER_ASAN_OPTIONS) UBSAN_OPTIONS=halt_on_error=1 /tmp/muhan-unit/character_player_snapshot_v1_artifact_sanitizer_test

# This receipt-pair belongs only to the opt-in M3 composition; default
# OBJECTS remain free of its artifact and relay dependencies.
character-player-snapshot-v1-receipt-pair-static-test:
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. -c character_player_snapshot_v1_receipt_pair.c -o /tmp/muhan-unit/character_player_snapshot_v1_receipt_pair_prod.o

character-player-snapshot-v1-receipt-pair-test: character-player-snapshot-v1-receipt-pair-static-test
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -DCHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_TESTING -DCHARACTER_SNAPSHOT_SHADOW_OUTBOX_TESTING -DCHARACTER_SAVE_JOURNAL_V2_TESTING -DCHARACTER_SAVE_JOURNAL_V2_WRITER_TESTING -DCHARACTER_SAVE_JOURNAL_V2_PUBLISH_TESTING -DCHARACTER_SAVE_JOURNAL_V2_ACK_TESTING -I. ../tests/unit/character_player_snapshot_v1_receipt_pair_test.c character_player_snapshot_v1_receipt_pair.c character_player_snapshot_v1_artifact.c character_snapshot_shadow_outbox.c player_snapshot_v1.c object_graph_v1.c cdto_v1.c character_save_journal_v2_ack.c character_save_journal_v2_publish.c character_save_journal_v2_route.c character_save_journal_v2_writer.c character_save_journal_v2.c utf8_text.c -o /tmp/muhan-unit/character_player_snapshot_v1_receipt_pair_test
	/tmp/muhan-unit/character_player_snapshot_v1_receipt_pair_test

character-player-snapshot-v1-receipt-pair-sanitizer-test: character-player-snapshot-v1-receipt-pair-static-test
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -DCHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_TESTING -DCHARACTER_SNAPSHOT_SHADOW_OUTBOX_TESTING -DCHARACTER_SAVE_JOURNAL_V2_TESTING -DCHARACTER_SAVE_JOURNAL_V2_WRITER_TESTING -DCHARACTER_SAVE_JOURNAL_V2_PUBLISH_TESTING -DCHARACTER_SAVE_JOURNAL_V2_ACK_TESTING -I. -O1 -fno-omit-frame-pointer -fsanitize=address,undefined ../tests/unit/character_player_snapshot_v1_receipt_pair_test.c character_player_snapshot_v1_receipt_pair.c character_player_snapshot_v1_artifact.c character_snapshot_shadow_outbox.c player_snapshot_v1.c object_graph_v1.c cdto_v1.c character_save_journal_v2_ack.c character_save_journal_v2_publish.c character_save_journal_v2_route.c character_save_journal_v2_writer.c character_save_journal_v2.c utf8_text.c -o /tmp/muhan-unit/character_player_snapshot_v1_receipt_pair_sanitizer_test
	ASAN_OPTIONS=$(DECODER_ASAN_OPTIONS) UBSAN_OPTIONS=halt_on_error=1 /tmp/muhan-unit/character_player_snapshot_v1_receipt_pair_sanitizer_test

character-player-snapshot-v1-read-rehearsal-test:
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. ../tests/unit/character_player_snapshot_v1_read_rehearsal_test.c character_player_snapshot_v1_read_rehearsal.c player_store.c -o /tmp/muhan-unit/character_player_snapshot_v1_read_rehearsal_test
	/tmp/muhan-unit/character_player_snapshot_v1_read_rehearsal_test

character-player-snapshot-v1-read-rehearsal-sanitizer-test:
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. -O1 -fno-omit-frame-pointer -fsanitize=address,undefined ../tests/unit/character_player_snapshot_v1_read_rehearsal_test.c character_player_snapshot_v1_read_rehearsal.c player_store.c -o /tmp/muhan-unit/character_player_snapshot_v1_read_rehearsal_sanitizer_test
	ASAN_OPTIONS=$(DECODER_ASAN_OPTIONS) UBSAN_OPTIONS=halt_on_error=1 /tmp/muhan-unit/character_player_snapshot_v1_read_rehearsal_sanitizer_test

character-player-snapshot-v1-capture-test:
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. -c character_player_snapshot_v1_capture.c -o /tmp/muhan-unit/character_player_snapshot_v1_capture_prod.o
	$(CC) $(V2_TEST_CFLAGS) -DCHARACTER_SAVE_JOURNAL_V2_TESTING -DCHARACTER_SAVE_JOURNAL_V2_WRITER_TESTING -I. ../tests/unit/character_player_snapshot_v1_capture_test.c character_player_snapshot_v1_capture.c character_player_snapshot_v1_artifact.c player_snapshot_v1.c object_graph_v1.c cdto_v1.c character_save_journal_v2_writer.c character_save_journal_v2.c utf8_text.c -o /tmp/muhan-unit/character_player_snapshot_v1_capture_test
	/tmp/muhan-unit/character_player_snapshot_v1_capture_test

character-player-snapshot-v1-capture-sanitizer-test:
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -DCHARACTER_SAVE_JOURNAL_V2_TESTING -DCHARACTER_SAVE_JOURNAL_V2_WRITER_TESTING -I. -O1 -fno-omit-frame-pointer -fsanitize=address,undefined ../tests/unit/character_player_snapshot_v1_capture_test.c character_player_snapshot_v1_capture.c character_player_snapshot_v1_artifact.c player_snapshot_v1.c object_graph_v1.c cdto_v1.c character_save_journal_v2_writer.c character_save_journal_v2.c utf8_text.c -o /tmp/muhan-unit/character_player_snapshot_v1_capture_sanitizer_test
	ASAN_OPTIONS=$(DECODER_ASAN_OPTIONS) UBSAN_OPTIONS=halt_on_error=1 /tmp/muhan-unit/character_player_snapshot_v1_capture_sanitizer_test

character-player-snapshot-v1-capture-native-test:
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. -c character_player_snapshot_v1_capture_native.c -o /tmp/muhan-unit/character_player_snapshot_v1_capture_native_prod.o
	$(CC) -std=gnu89 -fcommon -I. -ffunction-sections -fdata-sections $(UNIT_LEGACY_WARN_FLAGS) -c files1.c -o /tmp/muhan-unit/character_player_snapshot_v1_capture_files1.o
	$(CC) $(V2_TEST_CFLAGS) -I. -ffunction-sections -fdata-sections ../tests/unit/character_player_snapshot_v1_capture_native_test.c character_player_snapshot_v1_capture_native.c character_player_snapshot_v1_capture.c character_player_snapshot_v1_artifact.c player_record_serializer.c player_snapshot_v1.c object_graph_v1.c cdto_v1.c character_save_journal_v2_writer.c character_save_journal_v2.c utf8_text.c /tmp/muhan-unit/character_player_snapshot_v1_capture_files1.o $(UNIT_GC_FLAGS) -o /tmp/muhan-unit/character_player_snapshot_v1_capture_native_test
	/tmp/muhan-unit/character_player_snapshot_v1_capture_native_test

character-player-snapshot-v1-capture-native-sanitizer-test:
	mkdir -p /tmp/muhan-unit
	$(CC) -std=gnu89 -fcommon -I. -ffunction-sections -fdata-sections $(UNIT_LEGACY_WARN_FLAGS) -O1 -fno-omit-frame-pointer -fsanitize=address,undefined -c files1.c -o /tmp/muhan-unit/character_player_snapshot_v1_capture_files1_sanitizer.o
	$(CC) $(V2_TEST_CFLAGS) -I. -ffunction-sections -fdata-sections -O1 -fno-omit-frame-pointer -fsanitize=address,undefined ../tests/unit/character_player_snapshot_v1_capture_native_test.c character_player_snapshot_v1_capture_native.c character_player_snapshot_v1_capture.c character_player_snapshot_v1_artifact.c player_record_serializer.c player_snapshot_v1.c object_graph_v1.c cdto_v1.c character_save_journal_v2_writer.c character_save_journal_v2.c utf8_text.c /tmp/muhan-unit/character_player_snapshot_v1_capture_files1_sanitizer.o $(UNIT_GC_FLAGS) -o /tmp/muhan-unit/character_player_snapshot_v1_capture_native_sanitizer_test
	ASAN_OPTIONS=$(DECODER_ASAN_OPTIONS) UBSAN_OPTIONS=halt_on_error=1 /tmp/muhan-unit/character_player_snapshot_v1_capture_native_sanitizer_test

character-player-snapshot-v1-handoff-static-test:
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. -c character_player_snapshot_v1_handoff.c -o /tmp/muhan-unit/character_player_snapshot_v1_handoff_prod.o

character-player-snapshot-v1-handoff-test: character-player-snapshot-v1-handoff-static-test
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -DCHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_TESTING -DCHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_TESTING -DCHARACTER_SNAPSHOT_SHADOW_OUTBOX_TESTING -DCHARACTER_SAVE_JOURNAL_V2_TESTING -DCHARACTER_SAVE_JOURNAL_V2_WRITER_TESTING -DCHARACTER_SAVE_JOURNAL_V2_PUBLISH_TESTING -DCHARACTER_SAVE_JOURNAL_V2_ACK_TESTING -I. ../tests/unit/character_player_snapshot_v1_handoff_test.c character_player_snapshot_v1_handoff.c character_player_snapshot_v1_receipt_pair.c character_player_snapshot_v1_capture.c character_player_snapshot_v1_artifact.c character_snapshot_shadow_outbox.c player_snapshot_v1.c object_graph_v1.c cdto_v1.c character_save_journal_v2_ack.c character_save_journal_v2_publish.c character_save_journal_v2_route.c character_save_journal_v2_writer.c character_save_journal_v2.c utf8_text.c -o /tmp/muhan-unit/character_player_snapshot_v1_handoff_test
	/tmp/muhan-unit/character_player_snapshot_v1_handoff_test

character-player-snapshot-v1-handoff-sanitizer-test: character-player-snapshot-v1-handoff-static-test
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -DCHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_TESTING -DCHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_TESTING -DCHARACTER_SNAPSHOT_SHADOW_OUTBOX_TESTING -DCHARACTER_SAVE_JOURNAL_V2_TESTING -DCHARACTER_SAVE_JOURNAL_V2_WRITER_TESTING -DCHARACTER_SAVE_JOURNAL_V2_PUBLISH_TESTING -DCHARACTER_SAVE_JOURNAL_V2_ACK_TESTING -I. -O1 -fno-omit-frame-pointer -fsanitize=address,undefined ../tests/unit/character_player_snapshot_v1_handoff_test.c character_player_snapshot_v1_handoff.c character_player_snapshot_v1_receipt_pair.c character_player_snapshot_v1_capture.c character_player_snapshot_v1_artifact.c character_snapshot_shadow_outbox.c player_snapshot_v1.c object_graph_v1.c cdto_v1.c character_save_journal_v2_ack.c character_save_journal_v2_publish.c character_save_journal_v2_route.c character_save_journal_v2_writer.c character_save_journal_v2.c utf8_text.c -o /tmp/muhan-unit/character_player_snapshot_v1_handoff_sanitizer_test
	ASAN_OPTIONS=$(DECODER_ASAN_OPTIONS) UBSAN_OPTIONS=halt_on_error=1 /tmp/muhan-unit/character_player_snapshot_v1_handoff_sanitizer_test

character-save-journal-v2-receipt-transport-static-test:
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. -c character_save_journal_v2_receipt_transport.c -o /tmp/muhan-unit/character_save_journal_v2_receipt_transport_prod.o
	python3 ../tests/unit/character_save_journal_v2_receipt_transport_no_live_link_test.py --adapter-object /tmp/muhan-unit/character_save_journal_v2_receipt_transport_prod.o --makefile Makefile --adapter-source character_save_journal_v2_receipt_transport.c --adapter-header character_save_journal_v2_receipt_transport.h --native-source character_save_journal_v2_receipt_transport_native.c --native-header character_save_journal_v2_receipt_transport_native.h

character-save-journal-v2-receipt-transport-test: character-save-journal-v2-receipt-transport-static-test
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. ../tests/unit/character_save_journal_v2_receipt_transport_test.c character_save_journal_v2_receipt_transport.c -o /tmp/muhan-unit/character_save_journal_v2_receipt_transport_test
	/tmp/muhan-unit/character_save_journal_v2_receipt_transport_test

character-save-journal-v2-receipt-transport-sanitizer-test: character-save-journal-v2-receipt-transport-static-test
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. -O1 -fno-omit-frame-pointer -fsanitize=address,undefined ../tests/unit/character_save_journal_v2_receipt_transport_test.c character_save_journal_v2_receipt_transport.c -o /tmp/muhan-unit/character_save_journal_v2_receipt_transport_sanitizer_test
	ASAN_OPTIONS=$(DECODER_ASAN_OPTIONS) UBSAN_OPTIONS=halt_on_error=1 /tmp/muhan-unit/character_save_journal_v2_receipt_transport_sanitizer_test

# This owner is deliberately opt-in/test-only: default OBJECTS and frp.new
# must never gain it or a libpq symbol.
character-save-journal-v2-rpc-transport-default-link-inputs:
	@printf '%s\n' 'OBJECTS=$(OBJECTS)' 'LIBS=$(LIBS)' 'EXTRA_LIBS=$(EXTRA_LIBS)' 'M3_RUNTIME_LIBS=$(M3_RUNTIME_LIBS)'

character-save-journal-v2-rpc-transport-static-test:
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. -c character_save_journal_v2_rpc_transport.c -o /tmp/muhan-unit/character_save_journal_v2_rpc_transport_prod.o
	python3 ../tests/unit/character_save_journal_v2_rpc_transport_no_live_link_test.py --object /tmp/muhan-unit/character_save_journal_v2_rpc_transport_prod.o --makefile Makefile --source character_save_journal_v2_rpc_transport.c --header character_save_journal_v2_rpc_transport.h --native-source character_save_journal_v2_rpc_transport_native.c --native-header character_save_journal_v2_rpc_transport_native.h

character-save-journal-v2-rpc-transport-test: character-save-journal-v2-rpc-transport-static-test
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. ../tests/unit/character_save_journal_v2_rpc_transport_test.c character_save_journal_v2_rpc_transport.c -o /tmp/muhan-unit/character_save_journal_v2_rpc_transport_test
	/tmp/muhan-unit/character_save_journal_v2_rpc_transport_test

character-save-journal-v2-rpc-transport-sanitizer-test: character-save-journal-v2-rpc-transport-static-test
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. -O1 -fno-omit-frame-pointer -fsanitize=address,undefined ../tests/unit/character_save_journal_v2_rpc_transport_test.c character_save_journal_v2_rpc_transport.c -o /tmp/muhan-unit/character_save_journal_v2_rpc_transport_sanitizer_test
	ASAN_OPTIONS=$(DECODER_ASAN_OPTIONS) UBSAN_OPTIONS=halt_on_error=1 /tmp/muhan-unit/character_save_journal_v2_rpc_transport_sanitizer_test

character-save-journal-v2-live-ops-test:
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. ../tests/unit/character_save_journal_v2_live_ops_test.c character_save_journal_v2_live_ops.c character_save_journal_v2_rpc_transport.c -o /tmp/muhan-unit/character_save_journal_v2_live_ops_test
	/tmp/muhan-unit/character_save_journal_v2_live_ops_test

character-save-journal-v2-live-ops-sanitizer-test:
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. -O1 -fno-omit-frame-pointer -fsanitize=address,undefined ../tests/unit/character_save_journal_v2_live_ops_test.c character_save_journal_v2_live_ops.c character_save_journal_v2_rpc_transport.c -o /tmp/muhan-unit/character_save_journal_v2_live_ops_sanitizer_test
	ASAN_OPTIONS=$(DECODER_ASAN_OPTIONS) UBSAN_OPTIONS=halt_on_error=1 /tmp/muhan-unit/character_save_journal_v2_live_ops_sanitizer_test

character-save-journal-v2-bootstrap-test:
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -DCHARACTER_SAVE_JOURNAL_V2_TESTING -I. ../tests/unit/character_save_journal_v2_bootstrap_test.c character_save_journal_v2_bootstrap.c character_save_journal_v2.c utf8_text.c -o /tmp/muhan-unit/character_save_journal_v2_bootstrap_test
	/tmp/muhan-unit/character_save_journal_v2_bootstrap_test

character-save-journal-v2-bootstrap-sanitizer-test:
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -DCHARACTER_SAVE_JOURNAL_V2_TESTING -I. -O1 -fno-omit-frame-pointer -fsanitize=address,undefined ../tests/unit/character_save_journal_v2_bootstrap_test.c character_save_journal_v2_bootstrap.c character_save_journal_v2.c utf8_text.c -o /tmp/muhan-unit/character_save_journal_v2_bootstrap_sanitizer_test
	ASAN_OPTIONS=$(DECODER_ASAN_OPTIONS) UBSAN_OPTIONS=halt_on_error=1 /tmp/muhan-unit/character_save_journal_v2_bootstrap_sanitizer_test

character-save-journal-v2-player-store-static-test:
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. -c character_save_journal_v2_player_store.c -o /tmp/muhan-unit/character_save_journal_v2_player_store_prod.o
	$(CC) $(V2_TEST_CFLAGS) -I. -c character_save_journal_v2_bootstrap.c -o /tmp/muhan-unit/character_save_journal_v2_bootstrap_prod.o
	$(CC) $(V2_TEST_CFLAGS) -I. -c character_save_journal_v2_live_ops.c -o /tmp/muhan-unit/character_save_journal_v2_live_ops_prod.o
	$(CC) $(V2_TEST_CFLAGS) -I. -c character_save_journal_v2_rpc_transport.c -o /tmp/muhan-unit/character_save_journal_v2_rpc_transport_prod.o

character-save-journal-v2-player-store-test: character-save-journal-v2-player-store-static-test
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. ../tests/unit/character_save_journal_v2_player_store_test.c character_save_journal_v2_player_store.c player_recovery.c player_store.c -o /tmp/muhan-unit/character_save_journal_v2_player_store_test
	/tmp/muhan-unit/character_save_journal_v2_player_store_test

character-save-journal-v2-player-store-sanitizer-test: character-save-journal-v2-player-store-static-test
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. -O1 -fno-omit-frame-pointer -fsanitize=address,undefined ../tests/unit/character_save_journal_v2_player_store_test.c character_save_journal_v2_player_store.c player_recovery.c player_store.c -o /tmp/muhan-unit/character_save_journal_v2_player_store_sanitizer_test
	ASAN_OPTIONS=$(DECODER_ASAN_OPTIONS) UBSAN_OPTIONS=halt_on_error=1 /tmp/muhan-unit/character_save_journal_v2_player_store_sanitizer_test

character-save-journal-v2-player-store-live-observer-test: character-save-journal-v2-player-store-static-test
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -DCHARACTER_SAVE_JOURNAL_V2_TESTING -DCHARACTER_SAVE_JOURNAL_V2_WRITER_TESTING -DCHARACTER_SAVE_JOURNAL_V2_PUBLISH_TESTING -DCHARACTER_SAVE_JOURNAL_V2_ACK_TESTING -I. ../tests/unit/character_save_journal_v2_player_store_live_observer_test.c character_save_journal_v2_player_store.c character_save_journal_v2_bootstrap.c character_save_journal_v2_live_ops.c character_save_journal_v2_rpc_transport.c character_save_journal_v2_protocol.c character_save_journal_v2_recovery.c character_save_journal_v2_ack.c character_save_journal_v2_publish.c character_save_journal_v2_route.c character_save_journal_v2_writer.c character_save_journal_v2.c utf8_text.c -o /tmp/muhan-unit/character_save_journal_v2_player_store_live_observer_test
	/tmp/muhan-unit/character_save_journal_v2_player_store_live_observer_test

character-save-journal-v2-player-store-live-observer-sanitizer-test: character-save-journal-v2-player-store-static-test
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -DCHARACTER_SAVE_JOURNAL_V2_TESTING -DCHARACTER_SAVE_JOURNAL_V2_WRITER_TESTING -DCHARACTER_SAVE_JOURNAL_V2_PUBLISH_TESTING -DCHARACTER_SAVE_JOURNAL_V2_ACK_TESTING -I. -O1 -fno-omit-frame-pointer -fsanitize=address,undefined ../tests/unit/character_save_journal_v2_player_store_live_observer_test.c character_save_journal_v2_player_store.c character_save_journal_v2_bootstrap.c character_save_journal_v2_live_ops.c character_save_journal_v2_rpc_transport.c character_save_journal_v2_protocol.c character_save_journal_v2_recovery.c character_save_journal_v2_ack.c character_save_journal_v2_publish.c character_save_journal_v2_route.c character_save_journal_v2_writer.c character_save_journal_v2.c utf8_text.c -o /tmp/muhan-unit/character_save_journal_v2_player_store_live_observer_sanitizer_test
	ASAN_OPTIONS=$(DECODER_ASAN_OPTIONS) UBSAN_OPTIONS=halt_on_error=1 /tmp/muhan-unit/character_save_journal_v2_player_store_live_observer_sanitizer_test

character-save-journal-v2-deadline-static-test:
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. -c character_save_journal_v2_deadline.c -o /tmp/muhan-unit/character_save_journal_v2_deadline_prod.o

character-save-journal-v2-deadline-test: character-save-journal-v2-deadline-static-test
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. ../tests/unit/character_save_journal_v2_deadline_test.c character_save_journal_v2_deadline.c -o /tmp/muhan-unit/character_save_journal_v2_deadline_test
	/tmp/muhan-unit/character_save_journal_v2_deadline_test

character-save-journal-v2-deadline-sanitizer-test: character-save-journal-v2-deadline-static-test
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. -O1 -fno-omit-frame-pointer -fsanitize=address,undefined ../tests/unit/character_save_journal_v2_deadline_test.c character_save_journal_v2_deadline.c -o /tmp/muhan-unit/character_save_journal_v2_deadline_sanitizer_test
	ASAN_OPTIONS=$(DECODER_ASAN_OPTIONS) UBSAN_OPTIONS=halt_on_error=1 /tmp/muhan-unit/character_save_journal_v2_deadline_sanitizer_test

ifeq ($(shell uname -s),Linux)
character-save-journal-v2-deadline-native-test: character-save-journal-v2-deadline-static-test
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. -c character_save_journal_v2_deadline_native.c -o /tmp/muhan-unit/character_save_journal_v2_deadline_native.o
	printf '%s\n' '#include "character_save_journal_v2_deadline_native.h"' 'int main(void) { character_save_journal_v2_deadline_native native; char output[CHARACTER_SAVE_JOURNAL_V2_DEADLINE_OUTPUT_SIZE]; character_save_journal_v2_deadline_native_init(&native, 120); return character_save_journal_v2_deadline_native_callback(&native, output) != 0 || output[19] != '\''Z'\'' || output[20] != 0; }' | $(CC) $(V2_TEST_CFLAGS) -I. character_save_journal_v2_deadline.c /tmp/muhan-unit/character_save_journal_v2_deadline_native.o -x c - -o /tmp/muhan-unit/character_save_journal_v2_deadline_native_test
	/tmp/muhan-unit/character_save_journal_v2_deadline_native_test
else
character-save-journal-v2-deadline-native-test:
	@echo "character-save-journal-v2-deadline-native-test skipped (Linux CLOCK_REALTIME target)"
endif

character-save-journal-v2-process-owner-static-test:
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. -c character_save_journal_v2_process_owner.c -o /tmp/muhan-unit/character_save_journal_v2_process_owner_prod.o

character-save-journal-v2-process-owner-test: character-save-journal-v2-process-owner-static-test
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -DCHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_TEST_CLOSE=character_save_journal_v2_process_owner_test_close -I. ../tests/unit/character_save_journal_v2_process_owner_test.c character_save_journal_v2_process_owner.c -o /tmp/muhan-unit/character_save_journal_v2_process_owner_test
	/tmp/muhan-unit/character_save_journal_v2_process_owner_test

character-save-journal-v2-process-owner-sanitizer-test: character-save-journal-v2-process-owner-static-test
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -DCHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_TEST_CLOSE=character_save_journal_v2_process_owner_test_close -I. -O1 -fno-omit-frame-pointer -fsanitize=address,undefined ../tests/unit/character_save_journal_v2_process_owner_test.c character_save_journal_v2_process_owner.c -o /tmp/muhan-unit/character_save_journal_v2_process_owner_sanitizer_test
	ASAN_OPTIONS=$(DECODER_ASAN_OPTIONS) UBSAN_OPTIONS=halt_on_error=1 /tmp/muhan-unit/character_save_journal_v2_process_owner_sanitizer_test

character-save-journal-v2-rpc-transport-native-compile: character-save-journal-v2-rpc-transport-static-test
	mkdir -p /tmp/muhan-unit
	@command -v "$(PG_CONFIG)" >/dev/null 2>&1 || { echo "character-save-journal-v2-rpc-transport-native-compile: PG_CONFIG unavailable" >&2; exit 2; }
	$(CC) $(V2_TEST_CFLAGS) -I. -I"$$($(PG_CONFIG) --includedir)" -c character_save_journal_v2_rpc_transport_native.c -o /tmp/muhan-unit/character_save_journal_v2_rpc_transport_native.o
	python3 ../tests/unit/character_save_journal_v2_rpc_transport_no_live_link_test.py --object /tmp/muhan-unit/character_save_journal_v2_rpc_transport_prod.o --native /tmp/muhan-unit/character_save_journal_v2_rpc_transport_native.o --makefile Makefile --source character_save_journal_v2_rpc_transport.c --header character_save_journal_v2_rpc_transport.h --native-source character_save_journal_v2_rpc_transport_native.c --native-header character_save_journal_v2_rpc_transport_native.h

# Deliberately opt-in: CI supplies its just-built default executable.  This
# never reads a developer's local frp.new during unit/static testing.
M3_RPC_TRANSPORT_LIVE_BINARY ?=
character-save-journal-v2-rpc-transport-live-mud-audit:
	@if test -z "$(M3_RPC_TRANSPORT_LIVE_BINARY)"; then echo "m3 RPC live MUD audit skipped (set M3_RPC_TRANSPORT_LIVE_BINARY to a CI artifact)"; else case "$$(uname -s)" in Linux) python3 ../tests/unit/character_save_journal_v2_rpc_transport_live_mud_audit.py --binary "$(M3_RPC_TRANSPORT_LIVE_BINARY)" ;; *) echo "m3 RPC live MUD audit requires Linux" >&2; exit 2 ;; esac; fi

# The production default is deliberately free of libpq.  This separate gate
# proves that the complete M3 owner/runtime graph links only in the supported
# Linux + pg_config environment, and always writes outside src/frp.new.
M3_RUNTIME_LINK_OUTFILE ?= /tmp/muhan-unit/muhan-m3-runtime
m3-runtime-linux-link: character-save-journal-v2-runtime-native-test
	@case "$$(uname -s)" in Linux) ;; *) echo "m3 runtime full link requires Linux" >&2; exit 2 ;; esac
	@command -v "$(PG_CONFIG)" >/dev/null 2>&1 || { echo "m3 runtime full link requires PG_CONFIG to name an installed PostgreSQL client toolchain" >&2; exit 2; }
	mkdir -p "$$(dirname "$(M3_RUNTIME_LINK_OUTFILE)")"
	$(MAKE) --no-print-directory USE_M3_RUNTIME=1 PG_CONFIG="$(PG_CONFIG)" CC="$(CC)" OUTFILE="$(M3_RUNTIME_LINK_OUTFILE)" "$(M3_RUNTIME_LINK_OUTFILE)"

# Both PG17 harnesses are intentionally disposable and Linux-only: each
# starts a private Docker PostgreSQL 17 instance and never reuses a developer
# database.  Keep the scripts opt-in locally, but make CI opt in explicitly.
M3_PROCESS_OWNER_PG17_SCRIPT ?= ../supabase/tests/m3_process_owner_pg17_integration.sh
M3_RUNTIME_SHADOW_PG17_SCRIPT ?= ../supabase/tests/m3_runtime_shadow_pg17_integration.sh
M4_FILE_SNAPSHOT_MANIFEST_PG17_SCRIPT ?= ../supabase/tests/m4_file_snapshot_manifest_pg17_integration.sh

ifeq ($(M4_FILE_SNAPSHOT_MANIFEST_ALLOW_DISPOSABLE),1)
character-snapshot-shadow-outbox-pg17-integration:
	@case "$$(uname -s)" in Linux) ;; *) echo "m4 file snapshot manifest PG17 integration requires Linux" >&2; exit 2 ;; esac
	@command -v docker >/dev/null 2>&1 || { echo "m4 file snapshot manifest PG17 integration requires docker" >&2; exit 2; }
	@test -f "$(M4_FILE_SNAPSHOT_MANIFEST_PG17_SCRIPT)" || { echo "m4 file snapshot manifest PG17 integration script is missing: $(M4_FILE_SNAPSHOT_MANIFEST_PG17_SCRIPT)" >&2; exit 2; }
	M4_FILE_SNAPSHOT_MANIFEST_ALLOW_DISPOSABLE=1 bash "$(M4_FILE_SNAPSHOT_MANIFEST_PG17_SCRIPT)"
else
character-snapshot-shadow-outbox-pg17-integration:
	@echo "m4 file snapshot manifest PG17 integration skipped (set M4_FILE_SNAPSHOT_MANIFEST_ALLOW_DISPOSABLE=1)"
endif

ifeq ($(M3_PROCESS_OWNER_PG17_ALLOW_DISPOSABLE),1)
character-save-journal-v2-process-owner-pg17-integration:
	@case "$$(uname -s)" in Linux) ;; *) echo "m3 process-owner PG17 integration requires Linux" >&2; exit 2 ;; esac
	@command -v "$(PG_CONFIG)" >/dev/null 2>&1 || { echo "m3 process-owner PG17 integration requires PG_CONFIG" >&2; exit 2; }
	@command -v docker >/dev/null 2>&1 || { echo "m3 process-owner PG17 integration requires docker" >&2; exit 2; }
	@test -f "$(M3_PROCESS_OWNER_PG17_SCRIPT)" || { echo "m3 process-owner PG17 integration script is missing: $(M3_PROCESS_OWNER_PG17_SCRIPT)" >&2; exit 2; }
	M3_PROCESS_OWNER_PG17_ALLOW_DISPOSABLE="$(M3_PROCESS_OWNER_PG17_ALLOW_DISPOSABLE)" PG_CONFIG="$(PG_CONFIG)" CC="$(CC)" bash "$(M3_PROCESS_OWNER_PG17_SCRIPT)"
else
character-save-journal-v2-process-owner-pg17-integration:
	@echo "m3 process-owner PG17 integration skipped (set M3_PROCESS_OWNER_PG17_ALLOW_DISPOSABLE=1)"
endif

ifeq ($(M3_RUNTIME_SHADOW_PG17_ALLOW_DISPOSABLE),1)
character-save-journal-v2-runtime-shadow-pg17-integration:
	@case "$$(uname -s)" in Linux) ;; *) echo "m3 runtime shadow PG17 integration requires Linux" >&2; exit 2 ;; esac
	@command -v "$(PG_CONFIG)" >/dev/null 2>&1 || { echo "m3 runtime shadow PG17 integration requires PG_CONFIG" >&2; exit 2; }
	@command -v docker >/dev/null 2>&1 || { echo "m3 runtime shadow PG17 integration requires docker" >&2; exit 2; }
	@test -f "$(M3_RUNTIME_SHADOW_PG17_SCRIPT)" || { echo "m3 runtime shadow PG17 integration script is missing: $(M3_RUNTIME_SHADOW_PG17_SCRIPT)" >&2; exit 2; }
	M3_RUNTIME_SHADOW_PG17_ALLOW_DISPOSABLE="$(M3_RUNTIME_SHADOW_PG17_ALLOW_DISPOSABLE)" PG_CONFIG="$(PG_CONFIG)" CC="$(CC)" bash "$(M3_RUNTIME_SHADOW_PG17_SCRIPT)"
else
character-save-journal-v2-runtime-shadow-pg17-integration:
	@echo "m3 runtime shadow PG17 integration skipped (set M3_RUNTIME_SHADOW_PG17_ALLOW_DISPOSABLE=1)"
endif

ifeq ($(M3_RPC_TRANSPORT_ALLOW_DISPOSABLE),1)
character-save-journal-v2-rpc-transport-integration: character-save-journal-v2-rpc-transport-native-compile
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. -I"$$($(PG_CONFIG) --includedir)" ../tests/integration/character_save_journal_v2_rpc_transport_integration.c character_save_journal_v2_rpc_transport.c character_save_journal_v2_rpc_transport_native.c -L"$$($(PG_CONFIG) --libdir)" -lpq -o /tmp/muhan-unit/character_save_journal_v2_rpc_transport_integration
	M3_RPC_TRANSPORT_ALLOW_DISPOSABLE=1 bash ../supabase/tests/m3_rpc_transport_integration.sh /tmp/muhan-unit/character_save_journal_v2_rpc_transport_integration
else
character-save-journal-v2-rpc-transport-integration:
	@echo "m3 RPC transport integration skipped (set M3_RPC_TRANSPORT_ALLOW_DISPOSABLE=1)"
endif

character-save-journal-v2-runtime-static-test:
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. -c character_save_journal_v2_runtime.c -o /tmp/muhan-unit/character_save_journal_v2_runtime_prod.o
	python3 ../tests/unit/character_save_journal_v2_runtime_no_live_link_test.py --runtime-object /tmp/muhan-unit/character_save_journal_v2_runtime_prod.o --makefile Makefile --runtime-source character_save_journal_v2_runtime.c --runtime-header character_save_journal_v2_runtime.h --native-source character_save_journal_v2_runtime_native.c --native-header character_save_journal_v2_runtime_native.h --main-source main.c

character-save-journal-v2-runtime-test: character-save-journal-v2-runtime-static-test
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. ../tests/unit/character_save_journal_v2_runtime_test.c character_save_journal_v2_runtime.c -o /tmp/muhan-unit/character_save_journal_v2_runtime_test
	/tmp/muhan-unit/character_save_journal_v2_runtime_test

character-save-journal-v2-runtime-sanitizer-test: character-save-journal-v2-runtime-static-test
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. -O1 -fno-omit-frame-pointer -fsanitize=address,undefined ../tests/unit/character_save_journal_v2_runtime_test.c character_save_journal_v2_runtime.c -o /tmp/muhan-unit/character_save_journal_v2_runtime_sanitizer_test
	ASAN_OPTIONS=$(DECODER_ASAN_OPTIONS) UBSAN_OPTIONS=halt_on_error=1 /tmp/muhan-unit/character_save_journal_v2_runtime_sanitizer_test

character-save-journal-v2-runtime-native-compile: character-save-journal-v2-runtime-static-test
	mkdir -p /tmp/muhan-unit
	@command -v "$(PG_CONFIG)" >/dev/null 2>&1 || { echo "character-save-journal-v2-runtime-native-compile: PG_CONFIG unavailable" >&2; exit 2; }
	$(CC) $(V2_TEST_CFLAGS) -I. -I"$$($(PG_CONFIG) --includedir)" -c character_save_journal_v2_runtime_native.c -o /tmp/muhan-unit/character_save_journal_v2_runtime_native.o
	python3 ../tests/unit/character_save_journal_v2_runtime_no_live_link_test.py --native-object /tmp/muhan-unit/character_save_journal_v2_runtime_native.o --makefile Makefile --runtime-source character_save_journal_v2_runtime.c --runtime-header character_save_journal_v2_runtime.h --native-source character_save_journal_v2_runtime_native.c --native-header character_save_journal_v2_runtime_native.h --main-source main.c

ifeq ($(shell uname -s),Linux)
character-save-journal-v2-runtime-native-test: character-save-journal-v2-runtime-native-compile
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. -I"$$($(PG_CONFIG) --includedir)" ../tests/unit/character_save_journal_v2_runtime_native_test.c -o /tmp/muhan-unit/character_save_journal_v2_runtime_native_test
	/tmp/muhan-unit/character_save_journal_v2_runtime_native_test
	$(CC) $(V2_TEST_CFLAGS) -I. -I"$$($(PG_CONFIG) --includedir)" -O1 -fno-omit-frame-pointer -fsanitize=address,undefined ../tests/unit/character_save_journal_v2_runtime_native_test.c -o /tmp/muhan-unit/character_save_journal_v2_runtime_native_sanitizer_test
	ASAN_OPTIONS=$(DECODER_ASAN_OPTIONS) UBSAN_OPTIONS=halt_on_error=1 /tmp/muhan-unit/character_save_journal_v2_runtime_native_sanitizer_test
else
character-save-journal-v2-runtime-native-test:
	@echo "character-save-journal-v2-runtime-native-test skipped (Linux native lifecycle target)"
endif

m3-runtime-idle-hook-static-test:
	mkdir -p /tmp/muhan-unit
	$(CC) $(CFLAGS) -I. -c io.c -o /tmp/muhan-unit/io_default_p1f.o
	python3 ../tests/unit/m3_runtime_idle_hook_static_test.py --main-source main.c --io-source io.c --native-source character_save_journal_v2_runtime_native.c --default-io-object /tmp/muhan-unit/io_default_p1f.o

m3-wake-v1-test:
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. ../tests/unit/m3_wake_v1_test.c m3_wake_v1.c -o /tmp/muhan-unit/m3_wake_v1_test
	/tmp/muhan-unit/m3_wake_v1_test

m3-wake-v1-sanitizer-test:
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. -O1 -fno-omit-frame-pointer -fsanitize=address,undefined ../tests/unit/m3_wake_v1_test.c m3_wake_v1.c -o /tmp/muhan-unit/m3_wake_v1_sanitizer_test
	ASAN_OPTIONS=$(DECODER_ASAN_OPTIONS) UBSAN_OPTIONS=halt_on_error=1 /tmp/muhan-unit/m3_wake_v1_sanitizer_test

# This contract-only module remains outside default OBJECTS; callers supply
# every effect through its operations table.
m3-wake-supervisor-default-link-inputs:
	@printf '%s\n' 'OBJECTS=$(OBJECTS)' 'LIBS=$(LIBS)' 'EXTRA_LIBS=$(EXTRA_LIBS)' 'M3_RUNTIME_LIBS=$(M3_RUNTIME_LIBS)' 'M3_RUNTIME_OBJECTS=$(M3_RUNTIME_OBJECTS)'

m3-wake-supervisor-static-default-link-test:
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. -c m3_wake_supervisor.c -o /tmp/muhan-unit/m3_wake_supervisor_prod.o
	python3 ../tests/unit/m3_wake_supervisor_default_link_test.py --object /tmp/muhan-unit/m3_wake_supervisor_prod.o --makefile Makefile --source m3_wake_supervisor.c --header m3_wake_supervisor.h

m3-wake-supervisor-test: m3-wake-supervisor-static-default-link-test
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. ../tests/unit/m3_wake_supervisor_test.c m3_wake_supervisor.c m3_wake_v1.c -o /tmp/muhan-unit/m3_wake_supervisor_test
	/tmp/muhan-unit/m3_wake_supervisor_test

m3-wake-supervisor-sanitizer-test: m3-wake-supervisor-static-default-link-test
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. -O1 -fno-omit-frame-pointer -fsanitize=address,undefined ../tests/unit/m3_wake_supervisor_test.c m3_wake_supervisor.c m3_wake_v1.c -o /tmp/muhan-unit/m3_wake_supervisor_sanitizer_test
	ASAN_OPTIONS=$(DECODER_ASAN_OPTIONS) UBSAN_OPTIONS=halt_on_error=1 /tmp/muhan-unit/m3_wake_supervisor_sanitizer_test

# This bounded local queue is contract-only: no descriptor, process, socket,
# database, or default runtime linkage is introduced.
m3-wake-transport-default-link-inputs:
	@printf '%s\n' 'OBJECTS=$(OBJECTS)' 'LIBS=$(LIBS)' 'EXTRA_LIBS=$(EXTRA_LIBS)' 'M3_RUNTIME_LIBS=$(M3_RUNTIME_LIBS)' 'M3_RUNTIME_OBJECTS=$(M3_RUNTIME_OBJECTS)'

m3-wake-transport-static-default-link-test:
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. -c m3_wake_transport.c -o /tmp/muhan-unit/m3_wake_transport_prod.o
	python3 ../tests/unit/m3_wake_transport_default_link_test.py --object /tmp/muhan-unit/m3_wake_transport_prod.o --makefile Makefile --source m3_wake_transport.c --header m3_wake_transport.h

m3-wake-transport-test: m3-wake-transport-static-default-link-test
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. ../tests/unit/m3_wake_transport_test.c m3_wake_transport.c m3_wake_v1.c -o /tmp/muhan-unit/m3_wake_transport_test
	/tmp/muhan-unit/m3_wake_transport_test

m3-wake-transport-sanitizer-test:
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. -O1 -fno-omit-frame-pointer -fsanitize=address,undefined ../tests/unit/m3_wake_transport_test.c m3_wake_transport.c m3_wake_v1.c -o /tmp/muhan-unit/m3_wake_transport_sanitizer_test
	ASAN_OPTIONS=$(DECODER_ASAN_OPTIONS) UBSAN_OPTIONS=halt_on_error=1 /tmp/muhan-unit/m3_wake_transport_sanitizer_test

p1f-runtime-idle-test:
	mkdir -p /tmp/muhan-unit
	$(CC) $(CFLAGS) -DUSE_M3_RUNTIME -DM3_RUNTIME_IDLE_HOOK_TESTING -I. -ffunction-sections -fdata-sections -c io.c -o /tmp/muhan-unit/io_p1f_idle_test.o
	$(CC) $(CFLAGS) -I. -ffunction-sections -fdata-sections ../tests/unit/m3_runtime_idle_hook_test.c /tmp/muhan-unit/io_p1f_idle_test.o $(UNIT_GC_FLAGS) -o /tmp/muhan-unit/m3_runtime_idle_hook_test
	/tmp/muhan-unit/m3_runtime_idle_hook_test
	$(CC) $(CFLAGS) -DUSE_M3_RUNTIME -DM3_RUNTIME_IDLE_HOOK_TESTING -I. -ffunction-sections -fdata-sections -O1 -fno-omit-frame-pointer -fsanitize=address,undefined -c io.c -o /tmp/muhan-unit/io_p1f_idle_sanitizer.o
	$(CC) $(CFLAGS) -I. -ffunction-sections -fdata-sections -O1 -fno-omit-frame-pointer -fsanitize=address,undefined ../tests/unit/m3_runtime_idle_hook_test.c /tmp/muhan-unit/io_p1f_idle_sanitizer.o $(UNIT_GC_FLAGS) -o /tmp/muhan-unit/m3_runtime_idle_hook_sanitizer_test
	ASAN_OPTIONS=$(DECODER_ASAN_OPTIONS) UBSAN_OPTIONS=halt_on_error=1 /tmp/muhan-unit/m3_runtime_idle_hook_sanitizer_test

ifeq ($(shell uname -s),Linux)
p1f-runtime-native-test:
	@command -v "$(PG_CONFIG)" >/dev/null 2>&1 || { echo "p1f native lifecycle test requires PG_CONFIG" >&2; exit 2; }
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. -I"$$($(PG_CONFIG) --includedir)" ../tests/unit/character_save_journal_v2_runtime_native_test.c -o /tmp/muhan-unit/character_save_journal_v2_runtime_native_p1f_test
	/tmp/muhan-unit/character_save_journal_v2_runtime_native_p1f_test
	$(CC) $(V2_TEST_CFLAGS) -I. -I"$$($(PG_CONFIG) --includedir)" -O1 -fno-omit-frame-pointer -fsanitize=address,undefined ../tests/unit/character_save_journal_v2_runtime_native_test.c -o /tmp/muhan-unit/character_save_journal_v2_runtime_native_p1f_sanitizer_test
	ASAN_OPTIONS=$(DECODER_ASAN_OPTIONS) UBSAN_OPTIONS=halt_on_error=1 /tmp/muhan-unit/character_save_journal_v2_runtime_native_p1f_sanitizer_test
else
p1f-runtime-native-test:
	@echo "p1f native lifecycle test skipped (Linux M3 runtime target)"
endif

ifeq ($(M3_RUNTIME_ALLOW_DISPOSABLE),1)
character-save-journal-v2-runtime-integration: character-save-journal-v2-runtime-native-compile
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -DCHARACTER_SAVE_JOURNAL_V2_RUNTIME_PROBE_ONLY -I. -I"$$($(PG_CONFIG) --includedir)" ../tests/integration/character_save_journal_v2_runtime_integration.c character_save_journal_v2_runtime.c character_save_journal_v2_runtime_native.c -L"$$($(PG_CONFIG) --libdir)" -lpq -o /tmp/muhan-unit/character_save_journal_v2_runtime_integration
	M3_WRITER_SESSION_ALLOW_DISPOSABLE=1 bash ../supabase/tests/m3_writer_session_actual_login_integration.sh /tmp/muhan-unit/character_save_journal_v2_runtime_integration
else
character-save-journal-v2-runtime-integration:
	@echo "m3 runtime integration skipped (set M3_RUNTIME_ALLOW_DISPOSABLE=1)"
endif

character-save-journal-v2-protocol-prod-link-test:
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. -c character_save_journal_v2_protocol.c -o /tmp/muhan-unit/character_save_journal_v2_protocol_prod.o
	$(CC) $(V2_TEST_CFLAGS) -I. ../tests/unit/character_save_journal_v2_protocol_prod_link_test.c character_save_journal_v2_protocol.c character_save_journal_v2_recovery.c character_save_journal_v2_ack.c character_save_journal_v2_publish.c character_save_journal_v2_route.c character_save_journal_v2_writer.c character_save_journal_v2.c utf8_text.c $(V2_LINK_MAP_OPTION)/tmp/muhan-unit/character_save_journal_v2_protocol_prod.map -o /tmp/muhan-unit/character_save_journal_v2_protocol_prod_link_test
	/tmp/muhan-unit/character_save_journal_v2_protocol_prod_link_test

character-save-journal-v2-protocol-static-test: character-save-journal-v2-protocol-prod-link-test
	python3 ../tests/unit/character_save_journal_v2_protocol_no_live_link_test.py --binary /tmp/muhan-unit/character_save_journal_v2_protocol_prod_link_test --object /tmp/muhan-unit/character_save_journal_v2_protocol_prod.o --map /tmp/muhan-unit/character_save_journal_v2_protocol_prod.map --source character_save_journal_v2_protocol.c --header character_save_journal_v2_protocol.h --makefile Makefile

character-save-journal-v2-protocol-test: character-save-journal-v2-protocol-static-test
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -DCHARACTER_SAVE_JOURNAL_V2_TESTING -DCHARACTER_SAVE_JOURNAL_V2_WRITER_TESTING -DCHARACTER_SAVE_JOURNAL_V2_PUBLISH_TESTING -DCHARACTER_SAVE_JOURNAL_V2_ACK_TESTING -DCHARACTER_SAVE_JOURNAL_V2_RECOVERY_TESTING -I. ../tests/unit/character_save_journal_v2_protocol_test.c character_save_journal_v2_protocol.c character_save_journal_v2_recovery.c character_save_journal_v2_ack.c character_save_journal_v2_publish.c character_save_journal_v2_route.c character_save_journal_v2_writer.c character_save_journal_v2.c utf8_text.c -o /tmp/muhan-unit/character_save_journal_v2_protocol_test
	/tmp/muhan-unit/character_save_journal_v2_protocol_test

character-save-journal-v2-crash-test: character-save-journal-v2-protocol-static-test
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -DCHARACTER_SAVE_JOURNAL_V2_TESTING -DCHARACTER_SAVE_JOURNAL_V2_WRITER_TESTING -DCHARACTER_SAVE_JOURNAL_V2_PUBLISH_TESTING -DCHARACTER_SAVE_JOURNAL_V2_ACK_TESTING -DCHARACTER_SAVE_JOURNAL_V2_RECOVERY_TESTING -I. ../tests/unit/character_save_journal_v2_crash_test.c character_save_journal_v2_protocol.c character_save_journal_v2_recovery.c character_save_journal_v2_ack.c character_save_journal_v2_publish.c character_save_journal_v2_route.c character_save_journal_v2_writer.c character_save_journal_v2.c utf8_text.c -o /tmp/muhan-unit/character_save_journal_v2_crash_test
	/tmp/muhan-unit/character_save_journal_v2_crash_test

character-save-journal-v2-crash-sanitizer-test: character-save-journal-v2-protocol-static-test
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -DCHARACTER_SAVE_JOURNAL_V2_TESTING -DCHARACTER_SAVE_JOURNAL_V2_WRITER_TESTING -DCHARACTER_SAVE_JOURNAL_V2_PUBLISH_TESTING -DCHARACTER_SAVE_JOURNAL_V2_ACK_TESTING -DCHARACTER_SAVE_JOURNAL_V2_RECOVERY_TESTING -I. -O1 -fno-omit-frame-pointer -fsanitize=address,undefined ../tests/unit/character_save_journal_v2_crash_test.c character_save_journal_v2_protocol.c character_save_journal_v2_recovery.c character_save_journal_v2_ack.c character_save_journal_v2_publish.c character_save_journal_v2_route.c character_save_journal_v2_writer.c character_save_journal_v2.c utf8_text.c -o /tmp/muhan-unit/character_save_journal_v2_crash_sanitizer_test
	ASAN_OPTIONS=$(DECODER_ASAN_OPTIONS) UBSAN_OPTIONS=halt_on_error=1 /tmp/muhan-unit/character_save_journal_v2_crash_sanitizer_test

character-save-journal-v2-protocol-sanitizer-test: character-save-journal-v2-protocol-static-test
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -DCHARACTER_SAVE_JOURNAL_V2_TESTING -DCHARACTER_SAVE_JOURNAL_V2_WRITER_TESTING -DCHARACTER_SAVE_JOURNAL_V2_PUBLISH_TESTING -DCHARACTER_SAVE_JOURNAL_V2_ACK_TESTING -DCHARACTER_SAVE_JOURNAL_V2_RECOVERY_TESTING -I. -O1 -fno-omit-frame-pointer -fsanitize=address,undefined ../tests/unit/character_save_journal_v2_protocol_test.c character_save_journal_v2_protocol.c character_save_journal_v2_recovery.c character_save_journal_v2_ack.c character_save_journal_v2_publish.c character_save_journal_v2_route.c character_save_journal_v2_writer.c character_save_journal_v2.c utf8_text.c -o /tmp/muhan-unit/character_save_journal_v2_protocol_sanitizer_test
	ASAN_OPTIONS=$(DECODER_ASAN_OPTIONS) UBSAN_OPTIONS=halt_on_error=1 /tmp/muhan-unit/character_save_journal_v2_protocol_sanitizer_test

character-save-journal-v2-receipt-transport-native-compile:
	mkdir -p /tmp/muhan-unit
	@command -v "$(PG_CONFIG)" >/dev/null 2>&1 || { echo "character-save-journal-v2-receipt-transport-native-compile: PG_CONFIG unavailable" >&2; exit 2; }
	$(CC) $(V2_TEST_CFLAGS) -I. -I"$$($(PG_CONFIG) --includedir)" -c character_save_journal_v2_receipt_transport_native.c -o /tmp/muhan-unit/character_save_journal_v2_receipt_transport_native.o
	python3 ../tests/unit/character_save_journal_v2_receipt_transport_no_live_link_test.py --native-object /tmp/muhan-unit/character_save_journal_v2_receipt_transport_native.o --makefile Makefile --adapter-source character_save_journal_v2_receipt_transport.c --adapter-header character_save_journal_v2_receipt_transport.h --native-source character_save_journal_v2_receipt_transport_native.c --native-header character_save_journal_v2_receipt_transport_native.h

ifeq ($(M3_RECEIPT_TRANSPORT_ALLOW_DISPOSABLE),1)
character-save-journal-v2-receipt-transport-integration:
	mkdir -p /tmp/muhan-unit
	@command -v "$(PG_CONFIG)" >/dev/null 2>&1 || { echo "character-save-journal-v2-receipt-transport-integration: PG_CONFIG unavailable" >&2; exit 2; }
	$(CC) $(V2_TEST_CFLAGS) -I. -I"$$($(PG_CONFIG) --includedir)" ../tests/integration/character_save_journal_v2_receipt_transport_integration.c character_save_journal_v2_receipt_transport.c character_save_journal_v2_receipt_transport_native.c -L"$$($(PG_CONFIG) --libdir)" -lpq -o /tmp/muhan-unit/character_save_journal_v2_receipt_transport_integration
	bash ../supabase/tests/m3_receipt_transport_integration.sh /tmp/muhan-unit/character_save_journal_v2_receipt_transport_integration

character-save-journal-v2-receipt-transport-restart-integration: character-save-journal-v2-protocol-static-test character-save-journal-v2-receipt-transport-static-test
	mkdir -p /tmp/muhan-unit
	@command -v "$(PG_CONFIG)" >/dev/null 2>&1 || { echo "character-save-journal-v2-receipt-transport-restart-integration: PG_CONFIG unavailable" >&2; exit 2; }
	$(CC) $(V2_TEST_CFLAGS) -DCHARACTER_SAVE_JOURNAL_V2_TESTING -DCHARACTER_SAVE_JOURNAL_V2_WRITER_TESTING -DCHARACTER_SAVE_JOURNAL_V2_PUBLISH_TESTING -DCHARACTER_SAVE_JOURNAL_V2_ACK_TESTING -DCHARACTER_SAVE_JOURNAL_V2_RECOVERY_TESTING -I. -I"$$($(PG_CONFIG) --includedir)" ../tests/integration/character_save_journal_v2_rpc_restart_integration.c character_save_journal_v2_receipt_transport.c character_save_journal_v2_receipt_transport_native.c character_save_journal_v2_protocol.c character_save_journal_v2_recovery.c character_save_journal_v2_ack.c character_save_journal_v2_publish.c character_save_journal_v2_route.c character_save_journal_v2_writer.c character_save_journal_v2.c utf8_text.c -L"$$($(PG_CONFIG) --libdir)" -lpq -o /tmp/muhan-unit/character_save_journal_v2_rpc_restart_integration
	bash ../supabase/tests/m3_receipt_transport_restart_integration.sh /tmp/muhan-unit/character_save_journal_v2_rpc_restart_integration
else
character-save-journal-v2-receipt-transport-integration:
	@echo "m3 receipt transport integration skipped (set M3_RECEIPT_TRANSPORT_ALLOW_DISPOSABLE=1)"

character-save-journal-v2-receipt-transport-restart-integration:
	@echo "m3 receipt transport restart integration skipped (set M3_RECEIPT_TRANSPORT_ALLOW_DISPOSABLE=1)"
endif

character-save-journal-v2-recovery-static-test:
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. -c character_save_journal_v2_recovery.c -o /tmp/muhan-unit/character_save_journal_v2_recovery_prod.o
	python3 ../tests/unit/character_save_journal_v2_recovery_no_live_link_test.py --object /tmp/muhan-unit/character_save_journal_v2_recovery_prod.o --makefile Makefile

character-save-journal-v2-recovery-test: character-save-journal-v2-recovery-static-test
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -DCHARACTER_SAVE_JOURNAL_V2_TESTING -DCHARACTER_SAVE_JOURNAL_V2_WRITER_TESTING -DCHARACTER_SAVE_JOURNAL_V2_PUBLISH_TESTING -DCHARACTER_SAVE_JOURNAL_V2_ACK_TESTING -DCHARACTER_SAVE_JOURNAL_V2_RECOVERY_TESTING -I. ../tests/unit/character_save_journal_v2_recovery_test.c character_save_journal_v2_recovery.c character_save_journal_v2_ack.c character_save_journal_v2_publish.c character_save_journal_v2_route.c character_save_journal_v2_writer.c character_save_journal_v2.c utf8_text.c -o /tmp/muhan-unit/character_save_journal_v2_recovery_test
	/tmp/muhan-unit/character_save_journal_v2_recovery_test

character-save-journal-v2-recovery-sanitizer-test: character-save-journal-v2-recovery-static-test
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -DCHARACTER_SAVE_JOURNAL_V2_TESTING -DCHARACTER_SAVE_JOURNAL_V2_WRITER_TESTING -DCHARACTER_SAVE_JOURNAL_V2_PUBLISH_TESTING -DCHARACTER_SAVE_JOURNAL_V2_ACK_TESTING -DCHARACTER_SAVE_JOURNAL_V2_RECOVERY_TESTING -I. -O1 -fno-omit-frame-pointer -fsanitize=address,undefined ../tests/unit/character_save_journal_v2_recovery_test.c character_save_journal_v2_recovery.c character_save_journal_v2_ack.c character_save_journal_v2_publish.c character_save_journal_v2_route.c character_save_journal_v2_writer.c character_save_journal_v2.c utf8_text.c -o /tmp/muhan-unit/character_save_journal_v2_recovery_sanitizer_test
	ASAN_OPTIONS=$(DECODER_ASAN_OPTIONS) UBSAN_OPTIONS=halt_on_error=1 /tmp/muhan-unit/character_save_journal_v2_recovery_sanitizer_test

character-save-journal-v2-ack-test: character-save-journal-v2-ack-static-test
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -DCHARACTER_SAVE_JOURNAL_V2_TESTING -DCHARACTER_SAVE_JOURNAL_V2_WRITER_TESTING -DCHARACTER_SAVE_JOURNAL_V2_PUBLISH_TESTING -DCHARACTER_SAVE_JOURNAL_V2_ACK_TESTING -I. ../tests/unit/character_save_journal_v2_ack_test.c character_save_journal_v2_ack.c character_save_journal_v2_publish.c character_save_journal_v2_route.c character_save_journal_v2_writer.c character_save_journal_v2.c utf8_text.c -o /tmp/muhan-unit/character_save_journal_v2_ack_test
	/tmp/muhan-unit/character_save_journal_v2_ack_test

character-save-journal-v2-ack-static-test:
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. -c character_save_journal_v2_ack.c -o /tmp/muhan-unit/character_save_journal_v2_ack_prod.o
	python3 ../tests/unit/character_save_journal_v2_ack_no_live_link_test.py --object /tmp/muhan-unit/character_save_journal_v2_ack_prod.o --makefile Makefile

character-save-journal-v2-ack-sanitizer-test: character-save-journal-v2-ack-static-test
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -DCHARACTER_SAVE_JOURNAL_V2_TESTING -DCHARACTER_SAVE_JOURNAL_V2_WRITER_TESTING -DCHARACTER_SAVE_JOURNAL_V2_PUBLISH_TESTING -DCHARACTER_SAVE_JOURNAL_V2_ACK_TESTING -I. -O1 -fno-omit-frame-pointer -fsanitize=address,undefined ../tests/unit/character_save_journal_v2_ack_test.c character_save_journal_v2_ack.c character_save_journal_v2_publish.c character_save_journal_v2_route.c character_save_journal_v2_writer.c character_save_journal_v2.c utf8_text.c -o /tmp/muhan-unit/character_save_journal_v2_ack_sanitizer_test
	ASAN_OPTIONS=$(DECODER_ASAN_OPTIONS) UBSAN_OPTIONS=halt_on_error=1 /tmp/muhan-unit/character_save_journal_v2_ack_sanitizer_test

character-save-journal-test:
	mkdir -p /tmp/muhan-unit
	$(CC) -std=gnu89 -fcommon -DCHARACTER_SAVE_JOURNAL_TESTING -I. ../tests/unit/character_save_journal_test.c character_save_journal.c player_path.c resource_path.c utf8_text.c -o /tmp/muhan-unit/character_save_journal_test
	/tmp/muhan-unit/character_save_journal_test

character-save-journal-sanitizer-test:
	mkdir -p /tmp/muhan-unit
	$(CC) -std=gnu89 -fcommon -DCHARACTER_SAVE_JOURNAL_TESTING -I. -O1 -fno-omit-frame-pointer -fsanitize=address,undefined ../tests/unit/character_save_journal_test.c character_save_journal.c player_path.c resource_path.c utf8_text.c -o /tmp/muhan-unit/character_save_journal_sanitizer_test
	ASAN_OPTIONS=$(DECODER_ASAN_OPTIONS) UBSAN_OPTIONS=halt_on_error=1 /tmp/muhan-unit/character_save_journal_sanitizer_test

character-save-journal-v2-static-test:
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. -c character_save_journal_v2.c -o /tmp/muhan-unit/character_save_journal_v2_prod.o
	$(CC) $(V2_TEST_CFLAGS) -I. -c utf8_text.c -o /tmp/muhan-unit/character_save_journal_v2_utf8_prod.o
	$(CC) $(V2_TEST_CFLAGS) -I. -c ../tests/unit/character_save_journal_v2_prod_link_test.c -o /tmp/muhan-unit/character_save_journal_v2_prod_link_test.o
	$(CC) /tmp/muhan-unit/character_save_journal_v2_prod_link_test.o /tmp/muhan-unit/character_save_journal_v2_prod.o /tmp/muhan-unit/character_save_journal_v2_utf8_prod.o $(V2_LINK_MAP_OPTION)/tmp/muhan-unit/character_save_journal_v2_prod.map -o /tmp/muhan-unit/character_save_journal_v2_prod_link_test
	/tmp/muhan-unit/character_save_journal_v2_prod_link_test
	python3 ../tests/unit/character_save_journal_v2_no_live_link_test.py --binary /tmp/muhan-unit/character_save_journal_v2_prod_link_test --object /tmp/muhan-unit/character_save_journal_v2_prod.o --map /tmp/muhan-unit/character_save_journal_v2_prod.map --makefile Makefile

character-save-journal-v2-test: character-save-journal-v2-static-test
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -DCHARACTER_SAVE_JOURNAL_V2_TESTING -I. ../tests/unit/character_save_journal_v2_test.c character_save_journal_v2.c utf8_text.c -o /tmp/muhan-unit/character_save_journal_v2_test
	/tmp/muhan-unit/character_save_journal_v2_test

character-save-journal-v2-sanitizer-test: character-save-journal-v2-static-test
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -DCHARACTER_SAVE_JOURNAL_V2_TESTING -I. -O1 -fno-omit-frame-pointer -fsanitize=address,undefined ../tests/unit/character_save_journal_v2_test.c character_save_journal_v2.c utf8_text.c -o /tmp/muhan-unit/character_save_journal_v2_sanitizer_test
	ASAN_OPTIONS=$(DECODER_ASAN_OPTIONS) UBSAN_OPTIONS=halt_on_error=1 /tmp/muhan-unit/character_save_journal_v2_sanitizer_test

character-save-journal-v2-uuid-test:
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. ../tests/unit/character_save_journal_v2_uuid_test.c character_save_journal_v2_uuid.c -o /tmp/muhan-unit/character_save_journal_v2_uuid_test
	/tmp/muhan-unit/character_save_journal_v2_uuid_test

character-save-journal-v2-uuid-sanitizer-test:
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. -O1 -fno-omit-frame-pointer -fsanitize=address,undefined ../tests/unit/character_save_journal_v2_uuid_test.c character_save_journal_v2_uuid.c -o /tmp/muhan-unit/character_save_journal_v2_uuid_sanitizer_test
	ASAN_OPTIONS=$(DECODER_ASAN_OPTIONS) UBSAN_OPTIONS=halt_on_error=1 /tmp/muhan-unit/character_save_journal_v2_uuid_sanitizer_test

ifeq ($(shell uname -s),Linux)
character-save-journal-v2-uuid-native-test:
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -D_GNU_SOURCE -I. -c character_save_journal_v2_uuid_native.c -o /tmp/muhan-unit/character_save_journal_v2_uuid_native.o
	printf '%s\n' '#include "character_save_journal_v2_uuid_native.h"' 'int main(void) { char output[CHARACTER_SAVE_JOURNAL_V2_UUID_TEXT_LENGTH + 1]; return character_save_journal_v2_uuid_generate_native(output) != CHARACTER_SAVE_JOURNAL_V2_UUID_OK; }' | $(CC) $(V2_TEST_CFLAGS) -D_GNU_SOURCE -I. character_save_journal_v2_uuid.c /tmp/muhan-unit/character_save_journal_v2_uuid_native.o -x c - -o /tmp/muhan-unit/character_save_journal_v2_uuid_native_test
	/tmp/muhan-unit/character_save_journal_v2_uuid_native_test
else
character-save-journal-v2-uuid-native-test:
	@echo "character-save-journal-v2-uuid-native-test skipped (Linux getrandom target)"
endif

character-save-journal-v2-writer-static-test:
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. -c character_save_journal_v2_writer.c -o /tmp/muhan-unit/character_save_journal_v2_writer_prod.o
	python3 ../tests/unit/character_save_journal_v2_writer_no_live_link_test.py --object /tmp/muhan-unit/character_save_journal_v2_writer_prod.o --makefile Makefile

character-save-journal-v2-writer-test: character-save-journal-v2-writer-static-test
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -DCHARACTER_SAVE_JOURNAL_V2_WRITER_TESTING -I. ../tests/unit/character_save_journal_v2_writer_test.c character_save_journal_v2_writer.c -o /tmp/muhan-unit/character_save_journal_v2_writer_test
	/tmp/muhan-unit/character_save_journal_v2_writer_test

character-save-journal-v2-writer-sanitizer-test: character-save-journal-v2-writer-static-test
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -DCHARACTER_SAVE_JOURNAL_V2_WRITER_TESTING -I. -O1 -fno-omit-frame-pointer -fsanitize=address,undefined ../tests/unit/character_save_journal_v2_writer_test.c character_save_journal_v2_writer.c -o /tmp/muhan-unit/character_save_journal_v2_writer_sanitizer_test
	ASAN_OPTIONS=$(DECODER_ASAN_OPTIONS) UBSAN_OPTIONS=halt_on_error=1 /tmp/muhan-unit/character_save_journal_v2_writer_sanitizer_test

character-save-journal-v2-writer-bootstrap-test:
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -DCHARACTER_SAVE_JOURNAL_V2_WRITER_TESTING -I. ../tests/unit/character_save_journal_v2_writer_bootstrap_test.c character_save_journal_v2_writer.c -o /tmp/muhan-unit/character_save_journal_v2_writer_bootstrap_test
	/tmp/muhan-unit/character_save_journal_v2_writer_bootstrap_test

character-save-journal-v2-writer-bootstrap-sanitizer-test:
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -DCHARACTER_SAVE_JOURNAL_V2_WRITER_TESTING -I. -O1 -fno-omit-frame-pointer -fsanitize=address,undefined ../tests/unit/character_save_journal_v2_writer_bootstrap_test.c character_save_journal_v2_writer.c -o /tmp/muhan-unit/character_save_journal_v2_writer_bootstrap_sanitizer_test
	ASAN_OPTIONS=$(DECODER_ASAN_OPTIONS) UBSAN_OPTIONS=halt_on_error=1 /tmp/muhan-unit/character_save_journal_v2_writer_bootstrap_sanitizer_test

character-save-journal-v2-route-static-test:
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. -c character_save_journal_v2_route.c -o /tmp/muhan-unit/character_save_journal_v2_route_prod.o
	python3 ../tests/unit/character_save_journal_v2_route_no_live_link_test.py --object /tmp/muhan-unit/character_save_journal_v2_route_prod.o --makefile Makefile --writer-header character_save_journal_v2_writer.h

character-save-journal-v2-route-test: character-save-journal-v2-route-static-test
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -DCHARACTER_SAVE_JOURNAL_V2_WRITER_TESTING -I. ../tests/unit/character_save_journal_v2_route_test.c character_save_journal_v2_route.c character_save_journal_v2_writer.c -o /tmp/muhan-unit/character_save_journal_v2_route_test
	/tmp/muhan-unit/character_save_journal_v2_route_test

character-save-journal-v2-route-sanitizer-test: character-save-journal-v2-route-static-test
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -DCHARACTER_SAVE_JOURNAL_V2_WRITER_TESTING -I. -O1 -fno-omit-frame-pointer -fsanitize=address,undefined ../tests/unit/character_save_journal_v2_route_test.c character_save_journal_v2_route.c character_save_journal_v2_writer.c -o /tmp/muhan-unit/character_save_journal_v2_route_sanitizer_test
	ASAN_OPTIONS=$(DECODER_ASAN_OPTIONS) UBSAN_OPTIONS=halt_on_error=1 /tmp/muhan-unit/character_save_journal_v2_route_sanitizer_test

character-save-journal-v2-publish-static-test:
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. -c character_save_journal_v2_publish.c -o /tmp/muhan-unit/character_save_journal_v2_publish_prod.o
	python3 ../tests/unit/character_save_journal_v2_publish_no_live_link_test.py --object /tmp/muhan-unit/character_save_journal_v2_publish_prod.o --makefile Makefile

character-save-journal-v2-publish-test: character-save-journal-v2-publish-static-test
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -DCHARACTER_SAVE_JOURNAL_V2_TESTING -DCHARACTER_SAVE_JOURNAL_V2_WRITER_TESTING -DCHARACTER_SAVE_JOURNAL_V2_PUBLISH_TESTING -I. ../tests/unit/character_save_journal_v2_publish_test.c character_save_journal_v2_publish.c character_save_journal_v2_route.c character_save_journal_v2_writer.c character_save_journal_v2.c utf8_text.c -o /tmp/muhan-unit/character_save_journal_v2_publish_test
	/tmp/muhan-unit/character_save_journal_v2_publish_test

character-save-journal-v2-publish-sanitizer-test: character-save-journal-v2-publish-static-test
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -DCHARACTER_SAVE_JOURNAL_V2_TESTING -DCHARACTER_SAVE_JOURNAL_V2_WRITER_TESTING -DCHARACTER_SAVE_JOURNAL_V2_PUBLISH_TESTING -I. -O1 -fno-omit-frame-pointer -fsanitize=address,undefined ../tests/unit/character_save_journal_v2_publish_test.c character_save_journal_v2_publish.c character_save_journal_v2_route.c character_save_journal_v2_writer.c character_save_journal_v2.c utf8_text.c -o /tmp/muhan-unit/character_save_journal_v2_publish_sanitizer_test
	ASAN_OPTIONS=$(DECODER_ASAN_OPTIONS) UBSAN_OPTIONS=halt_on_error=1 /tmp/muhan-unit/character_save_journal_v2_publish_sanitizer_test

files1-decoder-test:
	mkdir -p /tmp/muhan-unit
	$(CC) -std=gnu89 -fcommon -I. -ffunction-sections -fdata-sections -DPLAYER_DECODER_TEST_HOOK -Dread=decoder_test_read -fsanitize=address,undefined -fno-omit-frame-pointer -c files1.c -o /tmp/muhan-unit/files1_decoder_files1.o
	$(CC) -std=gnu89 -fcommon -I. -fsanitize=address,undefined -fno-omit-frame-pointer -c ../tests/unit/files1_decoder_test.c -o /tmp/muhan-unit/files1_decoder_test.o
	$(CC) -fsanitize=address,undefined /tmp/muhan-unit/files1_decoder_files1.o /tmp/muhan-unit/files1_decoder_test.o $(UNIT_GC_FLAGS) -o /tmp/muhan-unit/files1_decoder_test
	ASAN_OPTIONS=$(DECODER_ASAN_OPTIONS) UBSAN_OPTIONS=halt_on_error=1 /tmp/muhan-unit/files1_decoder_test

# Test-only S1 seam: the oracle stays under tests/ and never joins OBJECTS or
# M3_RUNTIME_OBJECTS, so legacy save/load authority remains unchanged.
legacy-player-snapshot-v1-abi-static-test:
	python3 ../tests/unit/legacy_player_snapshot_v1_abi_static_test.py

legacy-player-snapshot-v1-oracle-static-test: legacy-player-snapshot-v1-abi-static-test
	@if printf '%s\n' '$(OBJECTS) $(M3_RUNTIME_OBJECTS)' | grep -q 'legacy_player_snapshot_v1_oracle'; then echo "test oracle leaked into production objects" >&2; exit 1; fi
	@for fixture in ../tests/fixtures/player_snapshot_v1_legacy_decoder_canonical.hex ../tests/fixtures/player_snapshot_v1_legacy_decoder_minimal.hex ../tests/fixtures/player_snapshot_v1_legacy_decoder_persisted_graph.hex; do test -s "$$fixture" || { echo "missing S1 legacy snapshot fixture: $$fixture" >&2; exit 1; }; done

legacy-player-snapshot-v1-oracle-test: legacy-player-snapshot-v1-oracle-static-test
	../scripts/run-legacy-player-snapshot-v1-differential.sh

legacy-player-snapshot-v1-oracle-sanitizer-test: legacy-player-snapshot-v1-oracle-static-test
	LEGACY_PLAYER_SNAPSHOT_V1_SANITIZE=1 DECODER_ASAN_OPTIONS=$(DECODER_ASAN_OPTIONS) ../scripts/run-legacy-player-snapshot-v1-differential.sh

player-record-serializer-test:
	mkdir -p /tmp/muhan-unit
	$(CC) -std=gnu89 -fcommon -I. -ffunction-sections -fdata-sections ../tests/unit/player_record_serializer_test.c files1.c player_record_serializer.c $(UNIT_GC_FLAGS) -o /tmp/muhan-unit/player_record_serializer_test
	/tmp/muhan-unit/player_record_serializer_test

player-record-serializer-sanitizer-test:
	mkdir -p /tmp/muhan-unit
	$(CC) -std=gnu89 -fcommon -I. -ffunction-sections -fdata-sections -O1 -fno-omit-frame-pointer -fsanitize=address,undefined ../tests/unit/player_record_serializer_test.c files1.c player_record_serializer.c $(UNIT_GC_FLAGS) -o /tmp/muhan-unit/player_record_serializer_sanitizer_test
	ASAN_OPTIONS=$(DECODER_ASAN_OPTIONS) UBSAN_OPTIONS=halt_on_error=1 /tmp/muhan-unit/player_record_serializer_sanitizer_test

cdto-v1-test:
	mkdir -p /tmp/muhan-unit
	$(CC) -std=gnu89 -fcommon -I. $(CDTO_V1_TEST_WARN_FLAGS) ../tests/unit/cdto_v1_test.c cdto_v1.c -o /tmp/muhan-unit/cdto_v1_test
	/tmp/muhan-unit/cdto_v1_test

cdto-v1-sanitizer-test:
	mkdir -p /tmp/muhan-unit
	$(CC) -std=gnu89 -fcommon -I. $(CDTO_V1_TEST_WARN_FLAGS) -O1 -fno-omit-frame-pointer -fsanitize=address,undefined ../tests/unit/cdto_v1_test.c cdto_v1.c -o /tmp/muhan-unit/cdto_v1_sanitizer_test
	/tmp/muhan-unit/cdto_v1_sanitizer_test

# Closed metadata codec: intentionally absent from OBJECTS and all live/M3
# composition lists. The fixture is shared verbatim with the Rust decoder.
alias-title-snapshot-manifest-v1-test:
	mkdir -p /tmp/muhan-unit
	$(CC) -std=gnu89 -fcommon -Wall -Wextra -Werror $(CDTO_V1_TEST_WARN_FLAGS) -I. ../tests/unit/alias_title_snapshot_manifest_v1_test.c alias_title_snapshot_manifest_v1.c alias_title_snapshot_v1.c cdto_v1.c -o /tmp/muhan-unit/alias_title_snapshot_manifest_v1_test
	/tmp/muhan-unit/alias_title_snapshot_manifest_v1_test

alias-title-snapshot-manifest-v1-sanitizer-test:
	mkdir -p /tmp/muhan-unit
	$(CC) -std=gnu89 -fcommon -Wall -Wextra -Werror $(CDTO_V1_TEST_WARN_FLAGS) -O1 -fno-omit-frame-pointer -fsanitize=address,undefined -I. ../tests/unit/alias_title_snapshot_manifest_v1_test.c alias_title_snapshot_manifest_v1.c alias_title_snapshot_v1.c cdto_v1.c -o /tmp/muhan-unit/alias_title_snapshot_manifest_v1_sanitizer_test
	ASAN_OPTIONS=$(DECODER_ASAN_OPTIONS) UBSAN_OPTIONS=halt_on_error=1 /tmp/muhan-unit/alias_title_snapshot_manifest_v1_sanitizer_test

alias-title-snapshot-v1-observer-test:
	mkdir -p /tmp/muhan-unit
	$(MAKE) alias-title-snapshot-v1-static-test
	$(CC) -std=gnu89 -fcommon -DALIAS_TITLE_SNAPSHOT_V1_TEST_SEAM -I. -ffunction-sections -fdata-sections ../tests/unit/alias_title_snapshot_v1_observer_test.c alias.c alias_title_snapshot_v1.c cdto_v1.c $(UNIT_GC_FLAGS) -o /tmp/muhan-unit/alias_title_snapshot_v1_observer_test
	/tmp/muhan-unit/alias_title_snapshot_v1_observer_test

alias-title-snapshot-v1-observer-sanitizer-test:
	mkdir -p /tmp/muhan-unit
	$(MAKE) alias-title-snapshot-v1-static-test
	$(CC) -std=gnu89 -fcommon -DALIAS_TITLE_SNAPSHOT_V1_TEST_SEAM -I. -ffunction-sections -fdata-sections -O1 -fno-omit-frame-pointer -fsanitize=address,undefined ../tests/unit/alias_title_snapshot_v1_observer_test.c alias.c alias_title_snapshot_v1.c cdto_v1.c $(UNIT_GC_FLAGS) -o /tmp/muhan-unit/alias_title_snapshot_v1_observer_sanitizer_test
	ASAN_OPTIONS=$(DECODER_ASAN_OPTIONS) UBSAN_OPTIONS=halt_on_error=1 /tmp/muhan-unit/alias_title_snapshot_v1_observer_sanitizer_test

alias-title-snapshot-v1-static-test:
	mkdir -p /tmp/muhan-unit
	$(CC) -std=gnu89 -fcommon -I. -c alias.c -o /tmp/muhan-unit/alias_title_snapshot_v1_production.o
	python3 ../tests/unit/alias_title_snapshot_v1_no_live_link_test.py --object /tmp/muhan-unit/alias_title_snapshot_v1_production.o --source alias.c --makefile Makefile

alias-title-snapshot-v1-outbox-static-test:
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. -c alias_title_snapshot_v1_outbox.c -o /tmp/muhan-unit/alias_title_snapshot_v1_outbox_production.o
	python3 ../tests/unit/alias_title_snapshot_v1_outbox_no_live_link_test.py --object /tmp/muhan-unit/alias_title_snapshot_v1_outbox_production.o --source alias_title_snapshot_v1_outbox.c --header alias_title_snapshot_v1_outbox.h --makefile Makefile

alias-title-snapshot-v1-outbox-test: alias-title-snapshot-v1-outbox-static-test
	mkdir -p /tmp/muhan-unit
	$(CC) -std=gnu89 -fcommon -Wall -Wextra -Werror -Wno-deprecated-non-prototype -DALIAS_TITLE_SNAPSHOT_V1_OUTBOX_TESTING -I. ../tests/unit/alias_title_snapshot_v1_outbox_test.c alias_title_snapshot_v1_outbox.c alias_title_snapshot_v1.c cdto_v1.c -o /tmp/muhan-unit/alias_title_snapshot_v1_outbox_test
	/tmp/muhan-unit/alias_title_snapshot_v1_outbox_test

alias-title-snapshot-v1-outbox-sanitizer-test: alias-title-snapshot-v1-outbox-static-test
	mkdir -p /tmp/muhan-unit
	$(CC) -std=gnu89 -fcommon -Wall -Wextra -Werror -Wno-deprecated-non-prototype -DALIAS_TITLE_SNAPSHOT_V1_OUTBOX_TESTING -I. -O1 -fno-omit-frame-pointer -fsanitize=address,undefined ../tests/unit/alias_title_snapshot_v1_outbox_test.c alias_title_snapshot_v1_outbox.c alias_title_snapshot_v1.c cdto_v1.c -o /tmp/muhan-unit/alias_title_snapshot_v1_outbox_sanitizer_test
	ASAN_OPTIONS=$(DECODER_ASAN_OPTIONS) UBSAN_OPTIONS=halt_on_error=1 /tmp/muhan-unit/alias_title_snapshot_v1_outbox_sanitizer_test

object-v1-test:
	mkdir -p /tmp/muhan-unit
	$(CC) -std=gnu89 -fcommon -I. $(CDTO_V1_TEST_WARN_FLAGS) ../tests/unit/object_v1_test.c object_v1.c cdto_v1.c -o /tmp/muhan-unit/object_v1_test
	/tmp/muhan-unit/object_v1_test

object-v1-sanitizer-test:
	mkdir -p /tmp/muhan-unit
	$(CC) -std=gnu89 -fcommon -I. $(CDTO_V1_TEST_WARN_FLAGS) -O1 -fno-omit-frame-pointer -fsanitize=address,undefined ../tests/unit/object_v1_test.c object_v1.c cdto_v1.c -o /tmp/muhan-unit/object_v1_sanitizer_test
	/tmp/muhan-unit/object_v1_sanitizer_test

object-graph-v1-test:
	mkdir -p /tmp/muhan-unit
	$(CC) -std=gnu89 -fcommon -I. -DOBJECT_GRAPH_V1_TESTING $(CDTO_V1_TEST_WARN_FLAGS) ../tests/unit/object_graph_v1_test.c object_graph_v1.c cdto_v1.c -o /tmp/muhan-unit/object_graph_v1_test
	/tmp/muhan-unit/object_graph_v1_test

object-graph-v1-sanitizer-test:
	mkdir -p /tmp/muhan-unit
	$(CC) -std=gnu89 -fcommon -I. -DOBJECT_GRAPH_V1_TESTING $(CDTO_V1_TEST_WARN_FLAGS) -O1 -fno-omit-frame-pointer -fsanitize=address,undefined ../tests/unit/object_graph_v1_test.c object_graph_v1.c cdto_v1.c -o /tmp/muhan-unit/object_graph_v1_sanitizer_test
	ASAN_OPTIONS=$(DECODER_ASAN_OPTIONS) UBSAN_OPTIONS=halt_on_error=1 /tmp/muhan-unit/object_graph_v1_sanitizer_test

bank-snapshot-v1-test:
	mkdir -p /tmp/muhan-unit
	$(CC) -std=gnu89 -fcommon -I. -DOBJECT_GRAPH_V1_TESTING $(CDTO_V1_TEST_WARN_FLAGS) ../tests/unit/bank_snapshot_v1_test.c bank_snapshot_v1.c object_graph_v1.c cdto_v1.c -o /tmp/muhan-unit/bank_snapshot_v1_test
	/tmp/muhan-unit/bank_snapshot_v1_test

bank-snapshot-v1-sanitizer-test:
	mkdir -p /tmp/muhan-unit
	$(CC) -std=gnu89 -fcommon -I. -DOBJECT_GRAPH_V1_TESTING $(CDTO_V1_TEST_WARN_FLAGS) -O1 -fno-omit-frame-pointer -fsanitize=address,undefined ../tests/unit/bank_snapshot_v1_test.c bank_snapshot_v1.c object_graph_v1.c cdto_v1.c -o /tmp/muhan-unit/bank_snapshot_v1_sanitizer_test
	ASAN_OPTIONS=$(DECODER_ASAN_OPTIONS) UBSAN_OPTIONS=halt_on_error=1 /tmp/muhan-unit/bank_snapshot_v1_sanitizer_test

bank-snapshot-v1-artifact-static-test:
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. -c bank_snapshot_v1_artifact.c -o /tmp/muhan-unit/bank_snapshot_v1_artifact_prod.o

bank-snapshot-v1-artifact-test: bank-snapshot-v1-artifact-static-test
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -DBANK_SNAPSHOT_V1_ARTIFACT_TESTING -I. ../tests/unit/bank_snapshot_v1_artifact_test.c bank_snapshot_v1_artifact.c bank_snapshot_v1.c object_graph_v1.c cdto_v1.c -o /tmp/muhan-unit/bank_snapshot_v1_artifact_test
	/tmp/muhan-unit/bank_snapshot_v1_artifact_test

bank-snapshot-v1-artifact-sanitizer-test: bank-snapshot-v1-artifact-static-test
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -DBANK_SNAPSHOT_V1_ARTIFACT_TESTING -I. -O1 -fno-omit-frame-pointer -fsanitize=address,undefined ../tests/unit/bank_snapshot_v1_artifact_test.c bank_snapshot_v1_artifact.c bank_snapshot_v1.c object_graph_v1.c cdto_v1.c -o /tmp/muhan-unit/bank_snapshot_v1_artifact_sanitizer_test
	ASAN_OPTIONS=$(DECODER_ASAN_OPTIONS) UBSAN_OPTIONS=halt_on_error=1 /tmp/muhan-unit/bank_snapshot_v1_artifact_sanitizer_test

creature-v1-test:
	mkdir -p /tmp/muhan-unit
	$(CC) -std=gnu89 -fcommon -I. $(CDTO_V1_TEST_WARN_FLAGS) ../tests/unit/creature_v1_test.c creature_v1.c cdto_v1.c -o /tmp/muhan-unit/creature_v1_test
	/tmp/muhan-unit/creature_v1_test

creature-v1-sanitizer-test:
	mkdir -p /tmp/muhan-unit
	$(CC) -std=gnu89 -fcommon -I. $(CDTO_V1_TEST_WARN_FLAGS) -O1 -fno-omit-frame-pointer -fsanitize=address,undefined ../tests/unit/creature_v1_test.c creature_v1.c cdto_v1.c -o /tmp/muhan-unit/creature_v1_sanitizer_test
	/tmp/muhan-unit/creature_v1_sanitizer_test

player-snapshot-v1-test:
	mkdir -p /tmp/muhan-unit
	$(CC) -std=gnu89 -fcommon -I. -DPLAYER_SNAPSHOT_V1_TESTING -DOBJECT_GRAPH_V1_TESTING $(CDTO_V1_TEST_WARN_FLAGS) ../tests/unit/player_snapshot_v1_test.c player_snapshot_v1.c object_graph_v1.c cdto_v1.c -o /tmp/muhan-unit/player_snapshot_v1_test
	/tmp/muhan-unit/player_snapshot_v1_test

player-snapshot-v1-sanitizer-test:
	mkdir -p /tmp/muhan-unit
	$(CC) -std=gnu89 -fcommon -I. -DPLAYER_SNAPSHOT_V1_TESTING -DOBJECT_GRAPH_V1_TESTING $(CDTO_V1_TEST_WARN_FLAGS) -O1 -fno-omit-frame-pointer -fsanitize=address,undefined ../tests/unit/player_snapshot_v1_test.c player_snapshot_v1.c object_graph_v1.c cdto_v1.c -o /tmp/muhan-unit/player_snapshot_v1_sanitizer_test
	ASAN_OPTIONS=$(DECODER_ASAN_OPTIONS) UBSAN_OPTIONS=halt_on_error=1 /tmp/muhan-unit/player_snapshot_v1_sanitizer_test

port:
	rm -f io.o main.o;
	make

fing: finger.o
	$(CC) finger.o -o fing
	mv fing ../bin
