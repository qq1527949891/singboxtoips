#!/bin/sh
set -eu
cd "$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
mkdir -p .cache
as_root() { if [ "$(id -u)" -eq 0 ]; then "$@"; else sudo "$@"; fi; }
manager=./dist/mip-proxy-linux-amd64
"$manager" core --out .cache/sing-box-linux
if [ ! -x .cache/mihomo-linux ]; then
    curl -fL --retry 2 'https://github.com/MetaCubeX/mihomo/releases/download/v1.19.33/mihomo-linux-amd64-v1-v1.19.33.gz' -o .cache/mihomo.gz
    echo '8ec92e7b2fdc6b853ba00156bab7e4e965c8b0d843b8c4ac8ab98795f02ad524  .cache/mihomo.gz' > .cache/mihomo.sha256
    # The release digest is fixed; a changed asset must never execute.
    sha256sum -c .cache/mihomo.sha256
    gzip -dc .cache/mihomo.gz > .cache/mihomo-linux
    chmod 0755 .cache/mihomo-linux
fi
if [ ! -x .cache/pebble ]; then
    curl -fL --retry 2 'https://github.com/letsencrypt/pebble/releases/download/v2.10.1/pebble-linux-amd64.tar.gz' -o .cache/pebble.tar.gz
    echo '4f2fcb5bca8c85c9cf73ad140fccfc0d2be40bd81ab99879c79b7b8a0b4f70ed  .cache/pebble.tar.gz' | sha256sum -c -
    tar -xOf .cache/pebble.tar.gz pebble-linux-amd64/linux/amd64/pebble > .cache/pebble
    chmod 0755 .cache/pebble
fi
"$manager" demo --protocol all --out .cache/demo
for example in .cache/demo/*; do
    .cache/sing-box-linux check -c "$example/server.json"
    .cache/sing-box-linux check -c "$example/export/sing-box.json"
    .cache/mihomo-linux -t -d .cache/mihomo-check -f "$example/export/mihomo.yaml"
done
go test -c -o .cache/integration.test ./integration
go test -c -o .cache/network.test ./internal/network
go test -c -o .cache/system.test ./internal/system
as_root env MIP_PEBBLE=./.cache/pebble MIP_CORE=./.cache/sing-box-linux MIP_MIHOMO=./.cache/mihomo-linux unshare -n sh integration/run-isolated.sh ./.cache/integration.test -test.v -test.timeout=180s
as_root env MIP_TEST_DISABLE_IPV6=1 MIP_PEBBLE=./.cache/pebble MIP_CORE=./.cache/sing-box-linux MIP_MIHOMO=./.cache/mihomo-linux unshare -n sh integration/run-isolated.sh ./.cache/integration.test -test.v -test.timeout=180s
as_root env MIP_ROUTE_INTEGRATION=1 unshare -n ./.cache/network.test -test.run=TestLinuxRoutesIntegration -test.v
as_root env MIP_IPV6_INTEGRATION=1 unshare -n ./.cache/system.test -test.run=TestLinuxIPv6DisableAndRestore -test.v
as_root env PATH="$PATH" python3 -m unittest discover -s scripts -p 'test_*.py' -v
