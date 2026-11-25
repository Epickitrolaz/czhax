/***************************************************************************
 *                                                                         *
 *          ###########   ###########   ##########    ##########           *
 *         ############  ############  ############  ############          *
 *         ##            ##            ##   ##   ##  ##        ##          *
 *         ##            ##            ##   ##   ##  ##        ##          *
 *         ###########   ####  ######  ##   ##   ##  ##    ######          *
 *          ###########  ####  #       ##   ##   ##  ##    #    #          *
 *                   ##  ##    ######  ##   ##   ##  ##    #    #          *
 *                   ##  ##    #       ##   ##   ##  ##    #    #          *
 *         ############  ##### ######  ##   ##   ##  ##### ######          *
 *         ###########    ###########  ##   ##   ##   ##########           *
 *                                                                         *
 *            S E C U R E   M O B I L E   N E T W O R K I N G              *
 *                                                                         *
 * This file is part of NexMon.                                            *
 *                                                                         *
 * Copyright (c) 2023 NexMon Team                                          *
 *                                                                         *
 * NexMon is free software: you can redistribute it and/or modify          *
 * it under the terms of the GNU General Public License as published by    *
 * the Free Software Foundation, either version 3 of the License, or       *
 * (at your option) any later version.                                     *
 *                                                                         *
 * NexMon is distributed in the hope that it will be useful,               *
 * but WITHOUT ANY WARRANTY; without even the implied warranty of          *
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the           *
 * GNU General Public License for more details.                            *
 *                                                                         *
 * You should have received a copy of the GNU General Public License       *
 * along with NexMon. If not, see <http://www.gnu.org/licenses/>.          *
 *                                                                         *
 **************************************************************************/

#pragma NEXMON targetregion "patch"

#include <firmware_version.h>   // definition of firmware version macros
#include <wrapper.h>            // wrapper definitions for functions that already exist in the firmware
#include <structs.h>            // structures that are used by the code in the firmware
#include <helper.h>             // useful helper functions
#include <patcher.h>            // macros used to create patches such as BLPatch, BPatch, ...
#include <nexioctls.h>          // ioctls added in the nexmon patch
#include <capabilities.h>       // capabilities included in a nexmon patch
#include <sendframe.h>          // sendframe functionality
#include <argprintf.h>

// Defined in injection.c. Parses the radiotap header off an skb and hands the
// bare 802.11 frame to sendframe(); consumes (tx) or frees the skb on error.
int inject_frame(struct wlc_info *wlc, struct sk_buff *p);
extern int nexmon_tx_stats[8];

// Headroom reserved in front of the injected frame so wlc_d11hdrs() can prepend
// the d11 TX header without reallocating (the 43439 d11 tx header tops out near
// 0xb0 bytes).
#define INJECT_HEADROOM 202

int
wlc_ioctl_hook(struct wlc_info *wlc, int cmd, char *arg, int len, void *wlc_if)
{
    argprintf_init(arg, len);
    int ret = IOCTL_ERROR;

    switch (cmd) {
        case NEX_GET_CAPABILITIES:
            if (len == 4) {
                memcpy(arg, &capabilities, 4);
                ret = IOCTL_SUCCESS;
            }
            break;

        case NEX_WRITE_TO_CONSOLE:
            if (len > 0) {
                arg[len-1] = 0;
                printf("ioctl: %s\n", arg);
                ret = IOCTL_SUCCESS;
            }
            break;

        case NEX_INJECT_FRAME:
            // arg holds one radiotap-prefixed 802.11 frame of length len
            if (len > 0) {
                struct sk_buff *p = pkt_buf_get_skb(wlc->osh, len + INJECT_HEADROOM);

                if (p != 0) {
                    skb_pull(p, INJECT_HEADROOM);
                    memcpy(p->data, arg, len);
                    // inject_frame() frees the skb on a malformed radiotap
                    // header, so only report success for a queued frame
                    ret = inject_frame(wlc, p) == 0 ? IOCTL_SUCCESS : IOCTL_ERROR;
                } else {
                    ret = IOCTL_ERROR;
                }
            }
            break;

        case 0x604: // nexmon tx diagnostics
            if (len >= 32) {
                memcpy(arg, nexmon_tx_stats, 32);
                ret = IOCTL_SUCCESS;
            }
            break;

        case 0x603: // read from memory: arg holds the source pointer, len bytes are copied back
            {
                memcpy(arg, *(char **) arg, len);
                ret = IOCTL_SUCCESS;
            }
            break;

        default:
            ret = wlc_ioctl(wlc, cmd, arg, len, wlc_if);
    }

    return ret;
}

__attribute__((at(0x2024C, "", CHIP_VER_BCM43439a0, FW_VER_7_95_75_0a0104c)))
GenericPatch4(wlc_ioctl_hook, wlc_ioctl_hook + 1);

