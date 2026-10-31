#!/bin/sh
set -eu

repository=${MIP_REPOSITORY:-qq1527949891/singboxtoips}
requested_version=${MIP_VERSION:-}
download_only=
no_menu=0
temporary_dir=

fail() { echo "错误：$*" >&2; exit 1; }
cleanup() { if [ -n "$temporary_dir" ]; then rm -rf -- "$temporary_dir"; fi; }
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

while [ "$#" -gt 0 ]; do
    case "$1" in
        --download-only)
            [ "$#" -ge 2 ] && [ -n "$2" ] || fail "--download-only 需要输出目录。"
            download_only=$2; shift 2 ;;
        --no-menu) no_menu=1; shift ;;
        *) fail "用法：sh install.sh [--no-menu | --download-only DIR]" ;;
    esac
done

[ "$(uname -s)" = Linux ] || fail "仅支持 Linux。"
case "$(uname -m)" in
    x86_64) arch=amd64 ;;
    aarch64|arm64) arch=arm64 ;;
    *) fail "仅支持 amd64/arm64。" ;;
esac
printf '%s\n' "$repository" | grep -Eq '^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$' || fail "仓库格式应为 owner/repo。"
if [ -n "$requested_version" ]; then
    case "$requested_version" in v*) ;; *) requested_version="v$requested_version" ;; esac
    printf '%s\n' "$requested_version" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+([.-][A-Za-z0-9.-]+)?$' || fail "版本格式无效，例如 v0.1.5。"
fi

if [ -z "$download_only" ]; then
    [ "$(id -u)" -eq 0 ] || fail "请使用 sudo sh install.sh。"
    [ -r /etc/os-release ] || fail "无法识别发行版。"
    . /etc/os-release
    [ "$ID" = debian ] || fail "安装入口支持 Debian 12+。"
    debian_major=${VERSION_ID%%.*}
    [ "$debian_major" -ge 12 ] || fail "需要 Debian 12 或更新版本。"
    [ -d /run/systemd/system ] && command -v systemctl >/dev/null 2>&1 || fail "需要正在运行的 systemd。"
    missing_dependencies=0
    for dependency in ip ss sysctl curl sha256sum tar gzip; do
        command -v "$dependency" >/dev/null 2>&1 || missing_dependencies=1
    done
    if [ "$missing_dependencies" -eq 1 ] || [ ! -s /etc/ssl/certs/ca-certificates.crt ]; then
        apt-get update
        DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends iproute2 procps ca-certificates curl coreutils tar gzip
    fi
fi

fetch() {
    command -v curl >/dev/null 2>&1 || fail "需要 curl，请先安装 curl。"
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
            echo "使用本地 Linux $arch 程序：$candidate_version"
            break
        fi
    done
fi

if [ -z "$manager_binary" ]; then
    temporary_dir=$(mktemp -d)
    tag=$requested_version
    if [ -z "$tag" ]; then
        fetch "https://api.github.com/repos/$repository/releases/latest" "$temporary_dir/release.json" || fail "无法读取最新 Release；请先在仓库发布，或用 MIP_VERSION 指定版本。"
        tag=$(sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$temporary_dir/release.json")
    fi
    printf '%s\n' "$tag" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+([.-][A-Za-z0-9.-]+)?$' || fail "Release 标签格式无效。"
    installed_version=$(manager_version /usr/local/bin/mip-proxy) || installed_version=
    if [ -z "$download_only" ] && [ "$installed_version" = "${tag#v}" ]; then
        manager_binary=/usr/local/bin/mip-proxy
        echo "已安装 $installed_version，无需重新下载。"
    else
        bundle_name="mip-proxy-linux-$arch"
        asset="$bundle_name.tar.gz"
        base="https://github.com/$repository/releases/download/$tag"
        fetch "$base/SHA256SUMS" "$temporary_dir/SHA256SUMS" || fail "无法下载校验清单。"
        expected=$(awk -v wanted="$asset" 'NF == 2 && $2 == wanted { print $1 }' "$temporary_dir/SHA256SUMS")
        printf '%s\n' "$expected" | grep -Eq '^[0-9a-f]{64}$' || fail "校验清单中缺少唯一、有效的 $asset 摘要。"
        echo "下载 $repository $tag / Linux $arch……"
        fetch "$base/$asset" "$temporary_dir/$asset" || fail "安装包下载失败。"
        actual=$(sha256sum "$temporary_dir/$asset" | awk '{print $1}')
        [ "$actual" = "$expected" ] || fail "SHA256 校验失败，未安装。"
        if [ -n "$download_only" ]; then
            mkdir -p -- "$download_only"
            cp -- "$temporary_dir/$asset" "$download_only/$asset"
            printf '%s  %s\n' "$expected" "$asset" > "$download_only/SHA256SUMS"
            echo "已下载并校验：$download_only/$asset"
            exit 0
        fi
        # Read only the expected member; never extract archive paths as root.
        tar -xOf "$temporary_dir/$asset" "$bundle_name/mip-proxy" > "$temporary_dir/mip-proxy" || fail "安装包中没有预期程序。"
        chmod 0755 "$temporary_dir/mip-proxy"
        manager_binary="$temporary_dir/mip-proxy"
        actual_version=$(manager_version "$manager_binary") || fail "下载的程序无法在当前架构运行。"
        [ "$actual_version" = "${tag#v}" ] || fail "程序版本与 Release 标签不一致。"
    fi
fi

if [ "$manager_binary" != /usr/local/bin/mip-proxy ]; then
    install -d -m 0755 /usr/local/bin
    staged_binary=$(mktemp /usr/local/bin/.mip-proxy-install.XXXXXX)
    if ! install -m 0755 "$manager_binary" "$staged_binary" || ! mv -f -- "$staged_binary" /usr/local/bin/mip-proxy; then
        rm -f -- "$staged_binary"
        fail "管理程序安装失败。"
    fi
fi
cleanup
temporary_dir=
echo "管理工具已就绪；部署成功后将自动禁用服务器 IPv6。"
if [ "$no_menu" -eq 0 ]; then exec /usr/local/bin/mip-proxy; fi
