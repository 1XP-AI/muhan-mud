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
else
UNIT_GC_FLAGS = -Wl,--gc-sections
UNIT_LEGACY_WARN_FLAGS =
endif

LIBS = -lm
CC = cc
OUTFILE = frp.new
EXTRA_LIBS =

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
	  player_path.o player_store.o file_player_store.o player_recovery.o trusted_admission.o onboarding_admission.o

$(OUTFILE): $(OBJECTS)
	$(CC) $(CFLAGS) $(OBJECTS) -o $(OUTFILE) $(LIBS) $(EXTRA_LIBS)
	chmod 777 $(OUTFILE)
#	strip $(OUTFILE)

# Need to add options to make utils here too
auth: auth.o
	$(CC) auth.o -o auth
	mv auth ../bin

clean: 
	rm -f $(OBJECTS) $(OUTFILE)

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
	$(CC) -std=gnu89 -fcommon -I. -ffunction-sections -fdata-sections ../tests/unit/files2_load_obj_test.c files2.c resource_path.c $(UNIT_GC_FLAGS) -o /tmp/muhan-unit/files2_load_obj_test
	/tmp/muhan-unit/files2_load_obj_test
	$(CC) -std=gnu89 -fcommon -I. -ffunction-sections -fdata-sections -DMUHAN_UPDATE_SCHEDULE_TEST $(UNIT_LEGACY_WARN_FLAGS) ../tests/unit/update_schedule_test.c update.c $(UNIT_GC_FLAGS) -o /tmp/muhan-unit/update_schedule_test
	/tmp/muhan-unit/update_schedule_test
	$(CC) -std=gnu89 -fcommon -I. ../tests/unit/player_recovery_test.c player_recovery.c player_store.c -o /tmp/muhan-unit/player_recovery_test
	/tmp/muhan-unit/player_recovery_test
	$(CC) -std=gnu89 -fcommon -I. ../tests/unit/trusted_admission_test.c trusted_admission.c player_path.c utf8_text.c resource_path.c -o /tmp/muhan-unit/trusted_admission_test
	/tmp/muhan-unit/trusted_admission_test
	$(CC) -std=gnu89 -fcommon -I. ../tests/unit/onboarding_admission_test.c onboarding_admission.c trusted_admission.c player_path.c utf8_text.c resource_path.c -o /tmp/muhan-unit/onboarding_admission_test
	/tmp/muhan-unit/onboarding_admission_test

port:
	rm -f io.o main.o;
	make

fing: finger.o
	$(CC) finger.o -o fing
	mv fing ../bin
