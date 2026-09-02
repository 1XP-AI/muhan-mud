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


OBJECTS = global.o main.o files1.o files2.o files3.o bank.o command1.o command2.o \
	  command3.o command4.o command5.o command6.o command7.o \
	  command8.o command9.o command10.o command11.o command12.o magic1.o magic2.o \
	 magic3.o magic4.o  magic5.o magic6.o magic7.o magic8.o \
	 dm1.o dm2.o dm3.o dm4.o dm5.o dm6.o io.o \
	  player.o creature.o room.o object.o update.o post.o action.o misc.o \
	  special1.o compress.o alias.o board.o resource_path.o utf8_text.o \
	  player_path.o player_store.o file_player_store.o player_recovery.o trusted_admission.o onboarding_admission.o onboarding_session.o onboarding_receipt.o onboarding_recovery.o

M3_RUNTIME_OBJECTS = character_save_journal_v2_runtime.o character_save_journal_v2_runtime_native.o

ifeq ($(USE_M3_RUNTIME),1)
OBJECTS += $(M3_RUNTIME_OBJECTS)
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

unit-test:
	mkdir -p /tmp/muhan-unit
	$(CC) -std=gnu89 -fcommon -I. ../tests/unit/player_store_test.c player_store.c -o /tmp/muhan-unit/player_store_test
	/tmp/muhan-unit/player_store_test
	$(CC) -std=gnu89 -fcommon -I. ../tests/unit/player_store_file_test.c file_player_store.c player_path.c resource_path.c -o /tmp/muhan-unit/player_store_file_test
	/tmp/muhan-unit/player_store_file_test
	$(CC) -std=gnu89 -fcommon -I. ../tests/unit/runtime_path_test.c resource_path.c -o /tmp/muhan-unit/runtime_path_test
	/tmp/muhan-unit/runtime_path_test
	$(CC) -std=gnu89 -fcommon -I. -Dwrite=serializer_test_write ../tests/unit/files1_serializer_test.c files1.c -o /tmp/muhan-unit/files1_serializer_test
	/tmp/muhan-unit/files1_serializer_test
	$(MAKE) files1-decoder-test
	$(CC) -std=gnu89 -fcommon -I. -ffunction-sections -fdata-sections ../tests/unit/files2_load_obj_test.c files2.c resource_path.c $(UNIT_GC_FLAGS) -o /tmp/muhan-unit/files2_load_obj_test
	/tmp/muhan-unit/files2_load_obj_test
	$(CC) -std=gnu89 -fcommon -I. -ffunction-sections -fdata-sections -DMUHAN_UPDATE_SCHEDULE_TEST $(UNIT_LEGACY_WARN_FLAGS) ../tests/unit/update_schedule_test.c update.c $(UNIT_GC_FLAGS) -o /tmp/muhan-unit/update_schedule_test
	/tmp/muhan-unit/update_schedule_test
	$(CC) -std=gnu89 -fcommon -I. ../tests/unit/player_recovery_test.c player_recovery.c player_store.c -o /tmp/muhan-unit/player_recovery_test
	/tmp/muhan-unit/player_recovery_test
	$(CC) -std=gnu89 -fcommon -I. ../tests/unit/trusted_admission_test.c trusted_admission.c player_path.c utf8_text.c resource_path.c -o /tmp/muhan-unit/trusted_admission_test
	/tmp/muhan-unit/trusted_admission_test
	$(CC) -std=gnu89 -fcommon -I. ../tests/unit/onboarding_admission_test.c onboarding_admission.c onboarding_session.c onboarding_receipt.c trusted_admission.c player_path.c utf8_text.c resource_path.c -o /tmp/muhan-unit/onboarding_admission_test
	/tmp/muhan-unit/onboarding_admission_test
	python3 ../tests/unit/onboarding_credential_lifecycle_test.py
	$(CC) -std=gnu89 -fcommon -I. ../tests/unit/onboarding_receipt_test.c onboarding_receipt.c resource_path.c -o /tmp/muhan-unit/onboarding_receipt_test
	/tmp/muhan-unit/onboarding_receipt_test
	$(CC) -std=gnu89 -fcommon -I. ../tests/unit/onboarding_recovery_test.c onboarding_recovery.c resource_path.c utf8_text.c -o /tmp/muhan-unit/onboarding_recovery_test
	/tmp/muhan-unit/onboarding_recovery_test
	$(MAKE) cdto-v1-test
	$(MAKE) object-v1-test
	$(MAKE) object-graph-v1-test
	$(MAKE) creature-v1-test
	$(MAKE) character-save-journal-test
	$(MAKE) character-save-journal-v2-test
	$(MAKE) character-save-journal-v2-writer-test
	$(MAKE) character-save-journal-v2-route-test
	$(MAKE) character-save-journal-v2-publish-test
	$(MAKE) character-save-journal-v2-ack-test
	$(MAKE) character-save-journal-v2-recovery-test
	$(MAKE) character-save-journal-v2-receipt-transport-test
	$(MAKE) character-save-journal-v2-runtime-test
	$(MAKE) character-save-journal-v2-protocol-test
	$(MAKE) character-save-journal-v2-crash-test

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

ifeq ($(M3_RUNTIME_ALLOW_DISPOSABLE),1)
character-save-journal-v2-runtime-integration: character-save-journal-v2-runtime-native-compile
	mkdir -p /tmp/muhan-unit
	$(CC) $(V2_TEST_CFLAGS) -I. -I"$$($(PG_CONFIG) --includedir)" ../tests/integration/character_save_journal_v2_runtime_integration.c character_save_journal_v2_runtime.c character_save_journal_v2_runtime_native.c -L"$$($(PG_CONFIG) --libdir)" -lpq -o /tmp/muhan-unit/character_save_journal_v2_runtime_integration
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

cdto-v1-test:
	mkdir -p /tmp/muhan-unit
	$(CC) -std=gnu89 -fcommon -I. $(CDTO_V1_TEST_WARN_FLAGS) ../tests/unit/cdto_v1_test.c cdto_v1.c -o /tmp/muhan-unit/cdto_v1_test
	/tmp/muhan-unit/cdto_v1_test

cdto-v1-sanitizer-test:
	mkdir -p /tmp/muhan-unit
	$(CC) -std=gnu89 -fcommon -I. $(CDTO_V1_TEST_WARN_FLAGS) -O1 -fno-omit-frame-pointer -fsanitize=address,undefined ../tests/unit/cdto_v1_test.c cdto_v1.c -o /tmp/muhan-unit/cdto_v1_sanitizer_test
	/tmp/muhan-unit/cdto_v1_sanitizer_test

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

creature-v1-test:
	mkdir -p /tmp/muhan-unit
	$(CC) -std=gnu89 -fcommon -I. $(CDTO_V1_TEST_WARN_FLAGS) ../tests/unit/creature_v1_test.c creature_v1.c cdto_v1.c -o /tmp/muhan-unit/creature_v1_test
	/tmp/muhan-unit/creature_v1_test

creature-v1-sanitizer-test:
	mkdir -p /tmp/muhan-unit
	$(CC) -std=gnu89 -fcommon -I. $(CDTO_V1_TEST_WARN_FLAGS) -O1 -fno-omit-frame-pointer -fsanitize=address,undefined ../tests/unit/creature_v1_test.c creature_v1.c cdto_v1.c -o /tmp/muhan-unit/creature_v1_sanitizer_test
	/tmp/muhan-unit/creature_v1_sanitizer_test

port:
	rm -f io.o main.o;
	make

fing: finger.o
	$(CC) finger.o -o fing
	mv fing ../bin
