/*
 * nexctl - minimal host tool for the nexmon netlink ioctl channel.
 *
 * The nexmon-patched brcmfmac opens NETLINK_USER (31) and forwards frames of
 * the form
 *
 *   struct nexudp_header  { "NEX", type=NEXUDP_IOCTL, securitycookie }
 *   u32 cmd
 *   u32 set
 *   u8  payload[]
 *
 * to the firmware's wlc_ioctl. Requires: patched firmware (ioctl hook) and a
 * driver built with the nexmon netlink handler.
 *
 * Build: gcc -O2 -o nexctl nexctl.c
 */
#include <arpa/inet.h>
#include <errno.h>
#include <fcntl.h>
#include <linux/netlink.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <unistd.h>

#define NETLINK_USER 31
#define NEXUDP_IOCTL 0

struct nexudp_header {
	char nex[3];
	char type;
	int securitycookie;
} __attribute__((packed));

struct nexudp_ioctl_header {
	struct nexudp_header nexudphdr;
	unsigned int cmd;
	unsigned int set;
	char payload[1];
} __attribute__((packed));

#define HDR_LEN (sizeof(struct nexudp_ioctl_header) - 1)

static struct sockaddr_nl nl_kernel = { .nl_family = AF_NETLINK };
static struct sockaddr_nl nl_local = { .nl_family = AF_NETLINK };
static int nl_sock;
static int nl_seq;

static int ioctl_xfer(unsigned int cmd, int set, void *payload, unsigned int len,
		      unsigned int *out_len)
{
	struct nlmsghdr *nlh;
	char *buf;
	int ret;

	buf = calloc(1, NLMSG_SPACE(HDR_LEN + len));
	if (!buf)
		return -1;

	nlh = (struct nlmsghdr *)buf;
	nlh->nlmsg_len = NLMSG_LENGTH(HDR_LEN + len);
	nlh->nlmsg_type = 0;
	nlh->nlmsg_flags = 0;
	nlh->nlmsg_seq = ++nl_seq;
	nlh->nlmsg_pid = nl_local.nl_pid;

	struct nexudp_ioctl_header *f = (struct nexudp_ioctl_header *)NLMSG_DATA(nlh);
	memcpy(f->nexudphdr.nex, "NEX", 3);
	f->nexudphdr.type = NEXUDP_IOCTL;
	f->nexudphdr.securitycookie = 0;
	f->cmd = cmd;
	f->set = set;
	memcpy(f->payload, payload, len);

	ret = sendto(nl_sock, nlh, nlh->nlmsg_len, 0,
		     (struct sockaddr *)&nl_kernel, sizeof(nl_kernel));
	if (ret < 0) {
		perror("sendto");
		free(buf);
		return -1;
	}

	for (;;) {
		ret = recv(nl_sock, buf, NLMSG_SPACE(HDR_LEN + len), 0);
		if (ret < 0) {
			perror("recv");
			free(buf);
			return -1;
		}
		nlh = (struct nlmsghdr *)buf;
		f = (struct nexudp_ioctl_header *)NLMSG_DATA(nlh);
		if (f->nexudphdr.type != NEXUDP_IOCTL)
			continue;
		if (set) {
			/* driver replies with a 4 byte "ACK" */
			if (out_len)
				*out_len = 0;
			free(buf);
			return 0;
		}
		unsigned int plen = nlh->nlmsg_len - NLMSG_HDRLEN - HDR_LEN;
		if (plen < len)
			len = plen;
		memcpy(payload, f->payload, len);
		if (out_len)
			*out_len = len;
		free(buf);
		return 0;
	}
}

static void usage(void)
{
	fprintf(stderr,
		"usage: nexctl <command>\n"
		"  caps                 read the nexmon capability flags\n"
		"  mon <0|1|2>          firmware monitor mode (0=off 1=802.11 2=radiotap)\n"
		"  get <cmd> <len>      raw ioctl get\n"
		"  set <cmd> <hexdata>  raw ioctl set\n"
		"  mem <addr> <len> <file>   read firmware/ROM memory to a local file\n"
		"  inject <file>        send one radiotap frame to NEX_INJECT_FRAME\n");
}

static int parse_num(const char *s, unsigned long *out)
{
	char *end;

	*out = strtoul(s, &end, 0);
	return *end == '\0' && end != s ? 0 : -1;
}

int main(int argc, char **argv)
{
	if (argc < 2) {
		usage();
		return 1;
	}

	nl_sock = socket(AF_NETLINK, SOCK_RAW, NETLINK_USER);
	if (nl_sock < 0) {
		perror("socket(NETLINK_USER)");
		fprintf(stderr, "is the nexmon driver loaded?\n");
		return 1;
	}
	memset(&nl_local, 0, sizeof(nl_local));
	nl_local.nl_family = AF_NETLINK;
	nl_local.nl_pid = getpid();
	if (bind(nl_sock, (struct sockaddr *)&nl_local, sizeof(nl_local)) < 0) {
		perror("bind");
		return 1;
	}
	memset(&nl_kernel, 0, sizeof(nl_kernel));
	nl_kernel.nl_family = AF_NETLINK;

	const char *cmd = argv[1];

	if (!strcmp(cmd, "caps")) {
		uint32_t caps = 0;
		if (ioctl_xfer(400 /* NEX_GET_CAPABILITIES */, 0, &caps, 4, NULL))
			return 1;
		printf("capabilities: 0x%08x%s%s%s\n", caps,
		       (caps & 1) ? " monitor" : "",
		       (caps & 2) ? " radiotap" : "",
		       (caps & 4) ? " injection" : "");
		return 0;
	}

	if (!strcmp(cmd, "mon")) {
		unsigned long m;
		uint32_t val;
		if (argc < 3 || parse_num(argv[2], &m)) {
			usage();
			return 1;
		}
		val = (uint32_t)m;
		return ioctl_xfer(108 /* WLC_SET_MONITOR */, 1, &val, 4, NULL) ? 1 : 0;
	}

	if (!strcmp(cmd, "get")) {
		unsigned long c, l;
		uint8_t *buf;
		if (argc < 4 || parse_num(argv[2], &c) || parse_num(argv[3], &l)) {
			usage();
			return 1;
		}
		buf = calloc(1, l + 16);
		if (ioctl_xfer(c, 0, buf, l, NULL))
			return 1;
		for (unsigned long i = 0; i < l; i++) {
			printf("%02x", buf[i]);
			if (i % 32 == 31)
				printf("\n");
		}
		printf("\n");
		return 0;
	}

	if (!strcmp(cmd, "set")) {
		unsigned long c;
		uint8_t buf[2048];
		unsigned int n = 0;
		if (argc < 4 || parse_num(argv[2], &c)) {
			usage();
			return 1;
		}
		const char *hex = argv[3];
		while (hex[0] && hex[1] && n < sizeof(buf)) {
			if (sscanf(hex, "%2hhx", &buf[n]) != 1)
				break;
			n++;
			hex += 2;
		}
		return ioctl_xfer(c, 1, buf, n, NULL) ? 1 : 0;
	}

	if (!strcmp(cmd, "mem")) {
		unsigned long addr, len;
		const char *path;
		uint8_t *buf;
		FILE *f;
		if (argc < 5 || parse_num(argv[2], &addr) || parse_num(argv[3], &len)) {
			usage();
			return 1;
		}
		path = argv[4];
		buf = calloc(1, len + 8);
		if (!buf)
			return 1;
		f = fopen(path, "wb");
		if (!f) {
			perror(path);
			return 1;
		}
		/* chunked: the firmware ioctl buffer is small */
		const unsigned chunk = 1800;
		unsigned long done = 0;
		while (done < len) {
			unsigned long n = len - done > chunk ? chunk : len - done;
			uint32_t target = (uint32_t)(addr + done);
			unsigned int got = 0;
			memcpy(buf, &target, 4);
			if (ioctl_xfer(0x603, 0, buf, n, &got)) {
				fprintf(stderr, "ioctl failed at 0x%lx\n", addr + done);
				fclose(f);
				remove(path);
				return 1;
			}
			if (fwrite(buf, 1, n, f) != n) {
				perror("fwrite");
				return 1;
			}
			done += n;
			if ((done % (1800 * 100)) == 0)
				fprintf(stderr, "\r%lu/%lu", done, len);
		}
		fclose(f);
		fprintf(stderr, "read %lu bytes from 0x%lx -> %s\n", len, addr, path);
		return 0;
	}

	if (!strcmp(cmd, "inject")) {
		unsigned char buf[2048];
		FILE *f = fopen(argv[2], "rb");
		size_t n;
		if (!f) {
			perror(argv[2]);
			return 1;
		}
		n = fread(buf, 1, sizeof(buf), f);
		fclose(f);
		return ioctl_xfer(408 /* NEX_INJECT_FRAME */, 1, buf, n, NULL) ? 1 : 0;
	}

	usage();
	return 1;
}
