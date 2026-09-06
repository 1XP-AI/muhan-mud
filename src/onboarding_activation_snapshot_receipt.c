/* Local activation tuple receipt proof; deliberately outside runtime wiring. */
#include "onboarding_activation_snapshot_receipt.h"

#include <errno.h>
#include <fcntl.h>
#include <stdio.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

#ifndef O_BINARY
#define O_BINARY 0
#endif
#ifndef O_DIRECTORY
#define O_DIRECTORY 0
#endif
#ifndef O_NOFOLLOW
#define O_NOFOLLOW 0
#endif
#ifndef O_CLOEXEC
#define O_CLOEXEC 0
#endif

#define OASR_TEXT_MAX 256U
#define OASR_NAME_MAX 80U

static unsigned long oasr_bounded(value, limit)
const char *value;
unsigned long limit;
{
    unsigned long length;
    if(!value) return limit+1U;
    for(length=0;length<=limit;length++) if(!value[length]) return length;
    return limit+1U;
}

static int oasr_hex(value)
char value;
{ return (value>='0'&&value<='9')||(value>='a'&&value<='f'); }

static int oasr_uuid(value)
const char *value;
{
    unsigned long index;
    if(oasr_bounded(value,ONBOARDING_ADMISSION_UUID_LEN)!=
       ONBOARDING_ADMISSION_UUID_LEN) return 0;
    for(index=0;index<ONBOARDING_ADMISSION_UUID_LEN;index++)
        if(index==8U||index==13U||index==18U||index==23U) {
            if(value[index]!='-') return 0;
        } else if(!oasr_hex(value[index])) return 0;
    return 1;
}

static int oasr_copy(destination, capacity, source)
char *destination;
unsigned long capacity;
const char *source;
{
    unsigned long length;
    if(!destination||!capacity||!source) return -1;
    length=oasr_bounded(source,capacity-1U);
    if(length>=capacity) return -1;
    memcpy(destination,source,length); destination[length]=0;
    return 0;
}

static const char *oasr_mode_name(mode)
onboarding_activation_binding_mode mode;
{
    if(mode==ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION) return "provision";
    if(mode==ONBOARDING_ACTIVATION_BINDING_MODE_CLAIM) return "claim";
    return 0;
}

static onboarding_activation_binding_mode oasr_mode(value)
const char *value;
{
    if(value&&!strcmp(value,"provision"))
        return ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION;
    if(value&&!strcmp(value,"claim")) return ONBOARDING_ACTIVATION_BINDING_MODE_CLAIM;
    return ONBOARDING_ACTIVATION_BINDING_MODE_INVALID;
}

static int oasr_binding_valid(value)
const onboarding_activation_binding *value;
{
    return value&&oasr_uuid(value->actor_user_id)&&oasr_uuid(value->correlation_id)&&
        oasr_uuid(value->character_id)&&oasr_mode_name(value->mode)&&
        oasr_uuid(value->command_id);
}

static int oasr_reservation_valid(value)
const onboarding_snapshot_command_reservation *value;
{
    return value&&value->state==ONBOARDING_SNAPSHOT_COMMAND_RESERVATION_RESERVED&&
        oasr_binding_valid(&value->activation);
}

static int oasr_same(left,right)
const onboarding_activation_binding *left;
const onboarding_activation_binding *right;
{
    return oasr_binding_valid(left)&&oasr_binding_valid(right)&&
        !strcmp(left->actor_user_id,right->actor_user_id)&&
        !strcmp(left->correlation_id,right->correlation_id)&&
        !strcmp(left->character_id,right->character_id)&&left->mode==right->mode&&
        !strcmp(left->command_id,right->command_id);
}

static int oasr_name(command_id, output, output_size)
const char *command_id;
char *output;
unsigned long output_size;
{
    int length;
    if(!oasr_uuid(command_id)||!output||!output_size) return -1;
    length=snprintf(output,output_size,"%s.activation-receipt",command_id);
    return length==55 ? 0:-1;
}

static int oasr_temp_name(command_id, output, output_size)
const char *command_id;
char *output;
unsigned long output_size;
{
    int length;
    if(!oasr_uuid(command_id)||!output||!output_size) return -1;
    length=snprintf(output,output_size,".%s.activation-receipt.tmp",command_id);
    return length==60 ? 0:-1;
}

static int oasr_directory(directory_fd)
int directory_fd;
{
    struct stat status;
    return directory_fd>=0&&!fstat(directory_fd,&status)&&S_ISDIR(status.st_mode)&&
        (status.st_mode&0777)==0700;
}

static int oasr_write_all(fd, bytes, length)
int fd;
const char *bytes;
unsigned long length;
{
    int count;
    while(length) {
        count=write(fd,bytes,length);
        if(count<0&&errno==EINTR) continue;
        if(count<=0) return -1;
        bytes+=count; length-=(unsigned long)count;
    }
    return 0;
}

static int oasr_format(value, output, output_size)
const onboarding_activation_binding *value;
char *output;
unsigned long output_size;
{
    int length;
    if(!oasr_binding_valid(value)||!output||!output_size) return -1;
    length=snprintf(output,output_size,
        "actor_user_id=%s\ncorrelation_id=%s\ncharacter_id=%s\nmode=%s\ncommand_id=%s\n",
        value->actor_user_id,value->correlation_id,value->character_id,
        oasr_mode_name(value->mode),value->command_id);
    return length<0||(unsigned long)length>=output_size ? -1:length;
}

static int oasr_line(line, prefix, output, output_size)
char *line;
const char *prefix;
char *output;
unsigned long output_size;
{
    unsigned long length;
    if(!line||!prefix||!output) return -1;
    length=(unsigned long)strlen(prefix);
    if(strncmp(line,prefix,length)) return -1;
    return oasr_copy(output,output_size,line+length);
}

static int oasr_parse(text, reservation)
char *text;
onboarding_snapshot_command_reservation *reservation;
{
    static const char *prefixes[5]={ "actor_user_id=", "correlation_id=",
        "character_id=", "mode=", "command_id=" };
    char *line,*next,mode[16];
    int index;
    if(!text||!reservation) return -1;
    memset(reservation,0,sizeof(*reservation)); memset(mode,0,sizeof(mode));
    line=text;
    for(index=0;index<5;index++) {
        next=strchr(line,'\n'); if(!next) goto bad;
        *next=0;
        if(index==0&&oasr_line(line,prefixes[index],reservation->activation.actor_user_id,
           sizeof(reservation->activation.actor_user_id))) goto bad;
        if(index==1&&oasr_line(line,prefixes[index],reservation->activation.correlation_id,
           sizeof(reservation->activation.correlation_id))) goto bad;
        if(index==2&&oasr_line(line,prefixes[index],reservation->activation.character_id,
           sizeof(reservation->activation.character_id))) goto bad;
        if(index==3&&oasr_line(line,prefixes[index],mode,sizeof(mode))) goto bad;
        if(index==4&&oasr_line(line,prefixes[index],reservation->activation.command_id,
           sizeof(reservation->activation.command_id))) goto bad;
        line=next+1;
    }
    if(*line) goto bad;
    reservation->activation.mode=oasr_mode(mode);
    reservation->state=ONBOARDING_SNAPSHOT_COMMAND_RESERVATION_RESERVED;
    if(!oasr_reservation_valid(reservation)) goto bad;
    memset(mode,0,sizeof(mode)); return 0;
bad:
    memset(reservation,0,sizeof(*reservation)); memset(mode,0,sizeof(mode));
    return -1;
}

static onboarding_activation_snapshot_receipt_result oasr_read_at(directory,
    name, reservation)
int directory;
const char *name;
onboarding_snapshot_command_reservation *reservation;
{
    char text[OASR_TEXT_MAX];
    struct stat status;
    int fd,count,extra,result;
    fd=-1; result=ONBOARDING_ACTIVATION_SNAPSHOT_RECEIPT_IO_ERROR;
    memset(text,0,sizeof(text)); if(reservation)memset(reservation,0,sizeof(*reservation));
    if(directory<0||!name||!reservation) goto out;
    fd=openat(directory,name,O_RDONLY|O_BINARY|O_NOFOLLOW|O_CLOEXEC);
    if(fd<0) {
        result=errno==ENOENT ? ONBOARDING_ACTIVATION_SNAPSHOT_RECEIPT_NO_PROOF:
            ONBOARDING_ACTIVATION_SNAPSHOT_RECEIPT_IO_ERROR;
        goto out;
    }
    if(fstat(fd,&status)||!S_ISREG(status.st_mode)||(status.st_mode&0777)!=0600||
       status.st_nlink!=1) { result=ONBOARDING_ACTIVATION_SNAPSHOT_RECEIPT_CORRUPT; goto out; }
    count=read(fd,text,sizeof(text)-1U);
    if(count<0) goto out;
    text[count]=0; extra=read(fd,text+count,1);
    if(extra!=0) goto out;
    if(close(fd)) { fd=-1; goto out; }
    fd=-1;
    result=oasr_parse(text,reservation)==0 ? ONBOARDING_ACTIVATION_SNAPSHOT_RECEIPT_RECORDED:
        ONBOARDING_ACTIVATION_SNAPSHOT_RECEIPT_CORRUPT;
out:
    if(fd>=0) close(fd);
    memset(text,0,sizeof(text));
    return result;
}

static int oasr_boundary_matches(reservation,artifact,receipt)
const onboarding_snapshot_command_reservation *reservation;
const character_player_snapshot_v1_artifact_metadata *artifact;
const character_save_journal_v2_receipt *receipt;
{
    return oasr_reservation_valid(reservation)&&artifact&&receipt&&
        oasr_uuid(artifact->character_id)&&oasr_uuid(artifact->command_id)&&
        oasr_uuid(receipt->character_id)&&oasr_uuid(receipt->command_id)&&
        !strcmp(artifact->character_id,reservation->activation.character_id)&&
        !strcmp(artifact->command_id,reservation->activation.command_id)&&
        !strcmp(receipt->character_id,reservation->activation.character_id)&&
        !strcmp(receipt->command_id,reservation->activation.command_id);
}

onboarding_activation_snapshot_receipt_result
onboarding_activation_snapshot_receipt_read(reservation_directory_fd,command_id,
    reservation_out)
int reservation_directory_fd;
const char *command_id;
onboarding_snapshot_command_reservation *reservation_out;
{
    char name[OASR_NAME_MAX];
    if(reservation_out) memset(reservation_out,0,sizeof(*reservation_out));
    if(!reservation_out||!oasr_uuid(command_id)||!oasr_directory(reservation_directory_fd)||
       oasr_name(command_id,name,sizeof(name)))
        return ONBOARDING_ACTIVATION_SNAPSHOT_RECEIPT_INVALID;
    {
        onboarding_activation_snapshot_receipt_result result=
            oasr_read_at(reservation_directory_fd,name,reservation_out);
        if(result==ONBOARDING_ACTIVATION_SNAPSHOT_RECEIPT_RECORDED&&
           strcmp(command_id,reservation_out->activation.command_id)) {
            memset(reservation_out,0,sizeof(*reservation_out));
            return ONBOARDING_ACTIVATION_SNAPSHOT_RECEIPT_CORRUPT;
        }
        return result;
    }
}

onboarding_activation_snapshot_receipt_result
onboarding_activation_snapshot_receipt_record(reservation_directory_fd,reservation,
    artifact,receipt)
int reservation_directory_fd;
const onboarding_snapshot_command_reservation *reservation;
const character_player_snapshot_v1_artifact_metadata *artifact;
const character_save_journal_v2_receipt *receipt;
{
    onboarding_snapshot_command_reservation prior;
    onboarding_activation_snapshot_receipt_result read_result;
    char name[OASR_NAME_MAX],temporary[OASR_NAME_MAX],text[OASR_TEXT_MAX];
    int fd,text_length,existing;

    memset(&prior,0,sizeof(prior)); memset(temporary,0,sizeof(temporary));
    memset(text,0,sizeof(text)); fd=-1; existing=0;
    if(!oasr_directory(reservation_directory_fd)||!oasr_reservation_valid(reservation)||
       !artifact||!receipt||oasr_name(reservation->activation.command_id,name,sizeof(name))||
       oasr_temp_name(reservation->activation.command_id,temporary,sizeof(temporary))||
       (text_length=oasr_format(&reservation->activation,text,sizeof(text)))<0)
        goto invalid;
    if(!oasr_boundary_matches(reservation,artifact,receipt)) goto mismatch;
    read_result=oasr_read_at(reservation_directory_fd,name,&prior);
    if(read_result==ONBOARDING_ACTIVATION_SNAPSHOT_RECEIPT_RECORDED&&
       strcmp(prior.activation.command_id,reservation->activation.command_id))
        return ONBOARDING_ACTIVATION_SNAPSHOT_RECEIPT_CORRUPT;
    if(read_result==ONBOARDING_ACTIVATION_SNAPSHOT_RECEIPT_RECORDED)
        return oasr_same(&prior.activation,&reservation->activation) ?
            ONBOARDING_ACTIVATION_SNAPSHOT_RECEIPT_EXACT_RETRY:
            ONBOARDING_ACTIVATION_SNAPSHOT_RECEIPT_CONFLICT;
    if(read_result!=ONBOARDING_ACTIVATION_SNAPSHOT_RECEIPT_NO_PROOF) return read_result;
    fd=openat(reservation_directory_fd,temporary,O_WRONLY|O_CREAT|O_EXCL|O_BINARY|
        O_NOFOLLOW|O_CLOEXEC,0600);
    if(fd<0||fchmod(fd,0600)||oasr_write_all(fd,text,(unsigned long)text_length)||
       fsync(fd)) goto io;
    if(close(fd)) { fd=-1; goto io; }
    fd=-1;
    if(linkat(reservation_directory_fd,temporary,reservation_directory_fd,name,0)) {
        if(errno!=EEXIST) goto io;
        existing=1;
        read_result=oasr_read_at(reservation_directory_fd,name,&prior);
        if(read_result==ONBOARDING_ACTIVATION_SNAPSHOT_RECEIPT_RECORDED&&
           strcmp(prior.activation.command_id,reservation->activation.command_id)) {
            read_result=ONBOARDING_ACTIVATION_SNAPSHOT_RECEIPT_CORRUPT;
            goto return_read;
        }
        if(read_result!=ONBOARDING_ACTIVATION_SNAPSHOT_RECEIPT_RECORDED) goto return_read;
        if(!oasr_same(&prior.activation,&reservation->activation)) goto conflict;
    }
    if(unlinkat(reservation_directory_fd,temporary,0)&&errno!=ENOENT) goto io;
    temporary[0]=0;
    if(fsync(reservation_directory_fd)) goto io;
    memset(&prior,0,sizeof(prior)); memset(text,0,sizeof(text));
    return existing ? ONBOARDING_ACTIVATION_SNAPSHOT_RECEIPT_EXACT_RETRY:
        ONBOARDING_ACTIVATION_SNAPSHOT_RECEIPT_RECORDED;
return_read:
    if(fd>=0) close(fd);
    if(temporary[0]) unlinkat(reservation_directory_fd,temporary,0);
    memset(&prior,0,sizeof(prior)); memset(text,0,sizeof(text));
    return read_result;
conflict:
    if(temporary[0]) unlinkat(reservation_directory_fd,temporary,0);
    memset(&prior,0,sizeof(prior)); memset(text,0,sizeof(text));
    return ONBOARDING_ACTIVATION_SNAPSHOT_RECEIPT_CONFLICT;
mismatch:
    memset(&prior,0,sizeof(prior)); memset(text,0,sizeof(text));
    return ONBOARDING_ACTIVATION_SNAPSHOT_RECEIPT_BOUNDARY_MISMATCH;
invalid:
    memset(&prior,0,sizeof(prior)); memset(text,0,sizeof(text));
    return ONBOARDING_ACTIVATION_SNAPSHOT_RECEIPT_INVALID;
io:
    if(fd>=0) close(fd);
    if(temporary[0]) unlinkat(reservation_directory_fd,temporary,0);
    memset(&prior,0,sizeof(prior)); memset(text,0,sizeof(text));
    return ONBOARDING_ACTIVATION_SNAPSHOT_RECEIPT_IO_ERROR;
}
