/* Native ABI metadata only. Never reads a player file or prints its fields. */
#include <stddef.h>
#include <stdio.h>
#include "mstruct.h"

int main(void)
{
    const unsigned int endian_probe = 1;
    printf("{\"passwordOffset\":%lu,\"passwordLength\":%lu,\"fields\":[",
        (unsigned long)offsetof(creature, password),
        (unsigned long)sizeof(((creature *)0)->password));
#define FIELD(member) printf("{\"name\":\"" #member "\",\"offset\":%lu,\"length\":%lu},", \
    (unsigned long)offsetof(creature, member), \
    (unsigned long)sizeof(((creature *)0)->member))
    FIELD(name); FIELD(description); FIELD(talk); FIELD(password); FIELD(key);
    FIELD(fd); FIELD(level); FIELD(type); FIELD(class); FIELD(race);
    FIELD(numwander); FIELD(alignment); FIELD(strength); FIELD(dexterity);
    FIELD(constitution); FIELD(intelligence); FIELD(piety); FIELD(hpmax);
    FIELD(hpcur); FIELD(mpmax); FIELD(mpcur); FIELD(armor); FIELD(thaco);
    FIELD(experience); FIELD(gold); FIELD(ndice); FIELD(sdice); FIELD(pdice);
    FIELD(special); FIELD(proficiency); FIELD(realm); FIELD(spells); FIELD(flags);
    FIELD(quests); FIELD(questnum); FIELD(carry); FIELD(rom_num); FIELD(ready);
    FIELD(daily); FIELD(lasttime); FIELD(following); FIELD(first_fol);
    FIELD(first_obj); FIELD(first_enm); FIELD(first_tlk); FIELD(parent_rom);
#undef FIELD
    printf("{\"name\":\"record-tail\",\"offset\":%lu,\"length\":0}],",
        (unsigned long)sizeof(creature));
    printf("\"recordSize\":%lu,\"longSize\":%lu,\"littleEndian\":%s,",
        (unsigned long)sizeof(creature), (unsigned long)sizeof(long),
        *(const unsigned char *)&endian_probe == 1 ? "true" : "false");
    printf("\"fd\":{\"offset\":%lu,\"length\":%lu},",
        (unsigned long)offsetof(creature, fd), (unsigned long)sizeof(((creature *)0)->fd));
    printf("\"parentRoom\":{\"offset\":%lu,\"length\":%lu},",
        (unsigned long)offsetof(creature, parent_rom), (unsigned long)sizeof(((creature *)0)->parent_rom));
    printf("\"timers\":{\"offset\":%lu,\"stride\":%lu,\"count\":45,\"ltimeOffset\":%lu,\"intervalOffset\":%lu,\"miscOffset\":%lu},",
        (unsigned long)offsetof(creature, lasttime), (unsigned long)sizeof(lasttime),
        (unsigned long)offsetof(lasttime, ltime), (unsigned long)offsetof(lasttime, interval),
        (unsigned long)offsetof(lasttime, misc));
    printf("\"saveIndex\":%d,\"hoursIndex\":%d,\"healIndex\":%d,\"saveInterval\":%d}\n",
        LT_PSAVE, LT_HOURS, LT_HEALS, SAVEINTERVAL);
    return 0;
}
