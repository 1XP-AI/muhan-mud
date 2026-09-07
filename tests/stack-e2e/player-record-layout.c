/* Native ABI metadata only. Never reads a player file or prints its fields. */
#include <stddef.h>
#include <stdio.h>
#include "mstruct.h"

int main(void)
{
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
    printf("{\"name\":\"record-tail\",\"offset\":%lu,\"length\":0}]}\n",
        (unsigned long)sizeof(creature));
    return 0;
}
