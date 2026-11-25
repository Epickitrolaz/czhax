// Package ir implements the Cardputer Zero infrared path: raw receive,
// protocol/database encoding, learned-signal storage and transmission.
//
// Hardware bindings (verified against m5stack-linux-dtoverlays, Makefile
// CONFIG_CARDPUTERO_V0_5):
//
//	RX: dtoverlay=gpio-ir,gpio_pin=13,gpio_pull=up  -> /dev/lirc* (LIRC_MODE2)
//	TX: dtoverlay=gpio-ir-tx,gpio_pin=12             -> /dev/lirc* (LIRC_MODE_PULSE)
//
// The two lirc nodes can enumerate in either order, so devices are identified
// by their advertised capabilities rather than by number. /dev/lirc* is
// normally group video; scripts/setup-cardputer.sh adds the target user to
// that group and ensures both overlays are present.
package ir
