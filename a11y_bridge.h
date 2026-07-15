#ifndef A11Y_BRIDGE_H
#define A11Y_BRIDGE_H

#include <stdbool.h>

char* getWindowOwners(void);
bool hasAccessibilityPermission(void);
void requestAccessibilityPermission(void);

#endif
