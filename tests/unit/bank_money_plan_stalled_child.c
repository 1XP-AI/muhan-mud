/* Disposable deadline oracle: deliberately never consumes input or exits. */
#include <unistd.h>
int main(void) { for(;;) pause(); }
