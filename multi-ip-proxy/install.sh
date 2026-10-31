#!/bin/sh
set -eu

repository=${MIP_REPOSITORY:-qq1527949891/singboxtoips}
requested_version=${MIP_VERSION:-}
download_only=
no_menu=0
temporary_dir=

fail() { echo "����$*" >&2; exit 1; }
cleanup() { if [ -n "$temporary_dir" ]; then rm -rf -- "$temporary_dir"; fi; }
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

while [ "$#" -gt 0 ]; do
    case "$1" in
        --download-only)
            [ "$#" -ge 2 ] && [ -n "$2" ] || fail "--download-only ��Ҫ���Ŀ¼��"
            download_only=$2; shift 2 ;;
        --no-menu) no_menu=1; shift ;;
        *) fail "�÷���sh install.sh [--no-menu | --download-only DIR]" ;;
    esac
done

[ "$(uname -s)" = Linux ] || fail "��֧�� Linux��"
case "$(uname -m)" in
    x86_64) arch=amd64 ;;
    aarch64|arm64) arch=arm64 ;;
    *) fail "��֧�� amd64/arm64��" ;;
esac
printf '%s\n' "$repository" | grep -Eq '^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$' || fail "�ֿ��ʽӦΪ owner/repo��"
if [ -n "$requested_version" ]; then
    case "$requested_version" in v*) ;; *) requested_version="v$requested_version" ;; esac
    printf '%s\n' "$requested_version" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+([.-][A-Za-z0-9.-]+)?$' || fail "�汾��ʽ��Ч������ v0.1.1��"
fi

if [ -z "$download_only" ]; then
    [ "$(id -u)" -eq 0 ] || fail "��ʹ�� sudo sh install.sh��"
    [ -r /etc/os-release ] || fail "�޷�ʶ���а档"
    . /etc/os-release
    [ "$ID" = debian ] || fail "��װ���֧�� Debian 12+��"
    debian_major=${VERSION_ID%%.*}
    [ "$debian_major" -ge 12 ] || fail "��Ҫ Debian 12 ����°汾��"
    [ -d /run/systemd/system ] && command -v systemctl >/dev/null 2>&1 || fail "��Ҫ�������е� systemd��"
    missing_dependencies=0
    for dependency in ip sysctl curl sha256sum tar; do
        command -v "$dependency" >/dev/null 2>&1 || missing_dependencies=1
    done
    if [ "$missing_dependencies" -eq 1 ] || [ ! -s /etc/ssl/certs/ca-certificates.crt ]; then
        apt-get update
        DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends iproute2 procps ca-certificates curl coreutils tar gzip
    fi
fi

fetch() {
    command -v curl >/dev/null 2>&1 || fail "��Ҫ curl�����Ȱ�װ curl��"
    curl --fail --location --silent --show-error --retry 2 --connect-timeout 15 --max-time 180 --proto '=https' --tlsv1.2 "$1" -o "$2"
}
manager_version() {
    [ -f "$1" ] && [ -x "$1" ] || return 1
    manager_output=$("$1" version 2>/dev/null) || return 1
    printf '%s\n' "$manager_output" | sed -n 's/^mip-proxy \([0-9][0-9A-Za-z.-]*\) \/ sing-box .*/\1/p'
}

bundle_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
manager_binary=
if [ -z "$download_only" ]; then
    for candidate in "$bundle_dir/mip-proxy" "$bundle_dir/dist/mip-proxy-linux-$arch"; do
        candidate_version=$(manager_version "$candidate") || continue
        [ -n "$candidate_version" ] || continue
        if [ -z "$requested_version" ] || [ "$candidate_version" = "${requested_version#v}" ]; then
            manager_binary=$candidate
            echo "ʹ�ñ��� Linux $arch ����$candidate_version"
            break
        fi
    done
fi

if [ -z "$manager_binary" ]; then
    temporary_dir=$(mktemp -d)
    tag=$requested_version
    if [ -z "$tag" ]; then
        fetch "https://api.github.com/repos/$repository/releases/latest" "$temporary_dir/release.json" || fail "�޷���ȡ���� Release�������ڲֿⷢ�������� MIP_VERSION ָ���汾��"
        tag=$(sed -n 's/^[[:space:]]*"tag_name":[[:space:]]*"\([^"]*\)".*/\1/p' "$temporary_dir/release.json")
    fi
    printf '%s\n' "$tag" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+([.-][A-Za-z0-9.-]+)?$' || fail "Release ��ǩ��ʽ��Ч��"
    installed_version=$(manager_version /usr/local/bin/mip-proxy) || installed_version=
    if [ -z "$download_only" ] && [ "$installed_version" = "${tag#v}" ]; then
        manager_binary=/usr/local/bin/mip-proxy
        echo "�Ѱ�װ $installed_version�������������ء�"
    else
        bundle_name="mip-proxy-linux-$arch"
        asset="$bundle_name.tar.gz"
        base="https://github.com/$repository/releases/download/$tag"
        fetch "$base/SHA256SUMS" "$temporary_dir/SHA256SUMS" || fail "�޷�����У���嵥��"
        expected=$(awk -v wanted="$asset" 'NF == 2 && $2 == wanted { print $1 }' "$temporary_dir/SHA256SUMS")
        printf '%s\n' "$expected" | grep -Eq '^[0-9a-f]{64}$' || fail "У���嵥��ȱ��Ψһ����Ч�� $asset ժҪ��"
        echo "���� $repository $tag / Linux $arch����"
        fetch "$base/$asset" "$temporary_dir/$asset" || fail "��װ������ʧ�ܡ�"
        actual=$(sha256sum "$temporary_dir/$asset" | awk '{print $1}')
        [ "$actual" = "$expected" ] || fail "SHA256 У��ʧ�ܣ�δ��װ��"
        if [ -n "$download_only" ]; then
            mkdir -p -- "$download_only"
            cp -- "$temporary_dir/$asset" "$download_only/$asset"
            printf '%s  %s\n' "$expected" "$asset" > "$download_only/SHA256SUMS"
            echo "�����ز�У�飺$download_only/$asset"
            exit 0
        fi
        # Read only the expected member; never extract archive paths as root.
        tar -xOf "$temporary_dir/$asset" "$bundle_name/mip-proxy" > "$temporary_dir/mip-proxy" || fail "��װ����û��Ԥ�ڳ���"
        chmod 0755 "$temporary_dir/mip-proxy"
        manager_binary="$temporary_dir/mip-proxy"
        actual_version=$(manager_version "$manager_binary") || fail "���صĳ����޷��ڵ�ǰ�ܹ����С�"
        [ "$actual_version" = "${tag#v}" ] || fail "����汾�� Release ��ǩ��һ�¡�"
    fi
fi

if [ "$manager_binary" != /usr/local/bin/mip-proxy ]; then
    install -d -m 0755 /usr/local/bin
    staged_binary=$(mktemp /usr/local/bin/.mip-proxy-install.XXXXXX)
    if ! install -m 0755 "$manager_binary" "$staged_binary" || ! mv -f -- "$staged_binary" /usr/local/bin/mip-proxy; then
        rm -f -- "$staged_binary"
        fail "��������װʧ�ܡ�"
    fi
fi
cleanup
temporary_dir=
echo "���������Ѿ���������ɹ����Զ����÷����� IPv6��"
if [ "$no_menu" -eq 0 ]; then exec /usr/local/bin/mip-proxy; fi
