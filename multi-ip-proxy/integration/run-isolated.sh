#!/bin/sh
set -eu
# Invoke this script inside `unshare -n`; it never configures the host namespace.
if [ "$(readlink /proc/self/ns/net)" = "$(readlink /proc/1/ns/net)" ]; then
    echo "Refusing to change the host network; run this script through unshare -n." >&2
    exit 1
fi
ip link set lo up
ip addr add 198.18.0.1/32 dev lo
ip -6 addr add 2001:db8::1/128 dev lo
export MIP_ECHO_IPV6=2001:db8::1
# WireGuard's network monitor needs a non-loopback default interface even when
# every test connection stays within this namespace.
ip link add mip-test type dummy
ip addr add 198.18.1.1/24 dev mip-test
ip link set mip-test up
ip route add default dev mip-test
export MIP_ECHO_IP=198.18.0.1
export MIP_ISOLATED_NETWORK=1
if [ "${MIP_TEST_DISABLE_IPV6:-0}" = 1 ]; then
    sysctl -q -w net.ipv6.conf.all.disable_ipv6=1 net.ipv6.conf.default.disable_ipv6=1
    unset MIP_ECHO_IPV6
fi
exec "$@"
