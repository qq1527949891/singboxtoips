#!/bin/bash

# ==============================================================================
# 脚本名称: The Final Cooperative Script
# 脚本功能: 采用最可靠的下载逻辑和协同架构，实现全协议、多IP出口分流
# 作者: Gemini
# 版本: 1.3 (修复 sing-box 服务启动超时问题，优化 systemd 配置，增强启动前检查)
# ==============================================================================

# --- 用户自定义配置区 ---
PROXY_USER="cwl"
PROXY_PASS="666888"
START_PORT=15888 # sing-box对外监听的起始端口，每个公网IP一个
WG_PORT=18888    # WireGuard使用的端口，默认值18888
ENABLE_IRQ_BALANCE="no"
SWAP_SIZE="3G"

# --- 下面的内容无需修改 ---

# 定义颜色常量和打印函数
GREEN='\033[0;32m'; RED='\033[0;31m'; YELLOW='\033[1;33m'; NC='\033[0m'
log() { echo -e "${GREEN}[INFO] ${1}${NC}"; }
warn() { echo -e "${YELLOW}[WARN] ${1}${NC}"; }
error() { echo -e "${RED}[ERROR] ${1}${NC}"; }

# 函数：系统优化
apply_system_optimizations() {
    log "开始全面检查并应用系统网络优化...";
    if [ "$(swapon --show | wc -l)" -eq 0 ]; then
        warn "未发现Swap，正在创建 ${SWAP_SIZE} Swap文件...";
        fallocate -l ${SWAP_SIZE} /swapfile && chmod 600 /swapfile && mkswap /swapfile && swapon /swapfile
        echo '/swapfile none swap sw 0 0' | tee -a /etc/fstab > /dev/null;
        log "Swap文件已创建并启用。";
    else
        log "检测到已存在Swap。";
    fi
    log "正在应用TCP/IP内核优化...";
    cat > /etc/sysctl.conf <<EOF
fs.file-max=1048576
net.core.default_qdisc=fq
net.ipv4.tcp_congestion_control=bbr
net.ipv4.tcp_fastopen=3
net.ipv4.tcp_low_latency=1
net.core.rmem_max=16777216
net.core.wmem_max=16777216
net.ipv4.tcp_rmem=4096 87380 16777216
net.ipv4.tcp_wmem=4096 16384 16777216
net.netfilter.nf_conntrack_max=262144
net.nf_conntrack_max=262144
net.ipv4.tcp_mtu_probing=1
net.ipv4.tcp_syncookies=1
net.ipv4.tcp_timestamps=0
net.ipv4.tcp_tw_reuse=1
EOF
    sysctl -p > /dev/null 2>&1;
    log "内核参数已更新。";
    if ! grep -q "^\* soft nofile 1048576" /etc/security/limits.conf; then
        echo "* soft nofile 1048576" >> /etc/security/limits.conf;
        echo "* hard nofile 1048576" >> /etc/security/limits.conf;
        log "文件描述符限制已设置。";
    fi
    if [[ "${ENABLE_IRQ_BALANCE}" == "yes" ]]; then
        warn "检测到用户选择开启 IRQ 负载均衡...";
        if ! command -v irqbalance > /dev/null; then
            if command -v apt-get > /dev/null; then apt-get update && apt-get install -y irqbalance; elif command -v yum > /dev/null; then yum install -y irqbalance; fi
        fi;
        if command -v irqbalance > /dev/null; then systemctl enable --now irqbalance > /dev/null 2>&1; log "irqbalance 服务已在运行。"; else error "irqbalance 安装失败。"; fi
    else
        log "跳过 IRQ 负载均衡优化。";
    fi;
    log "系统优化配置完成。"
}

# 函数：安装依赖程序
install_dependencies() {
    if ! command -v curl > /dev/null || ! command -v wget > /dev/null; then
        warn "curl/wget 未安装...";
        if command -v apt-get > /dev/null; then apt-get update && apt-get install -y curl wget; elif command -v yum > /dev-null; then yum install -y curl wget; else error "无法自动安装curl/wget。"; exit 1; fi
    fi

    # 安装 jq 工具
    if ! command -v jq > /dev/null; then
        log "正在安装 jq (JSON处理器) 工具...";
        if command -v apt-get > /dev/null; then
            apt-get update && apt-get install -y jq;
        elif command -v yum > /dev/null; then
            yum install -y jq;
        else
            error "无法自动安装 jq。请手动安装 jq (apt install jq 或 yum install jq)，然后重试脚本。";
            exit 1;
        fi
        if ! command -v jq > /dev/null; then
            error "jq 安装失败。请手动检查安装情况或网络连接。";
            exit 1;
        fi
        log "jq 安装成功！";
    else
        log "jq 已安装。";
    fi
    
    # [修正 v63.0] 优化 3proxy 安装逻辑：版本检测与跳过
    log "正在安装/更新 3proxy 核心...";
    local EXPECTED_3PROXY_VERSION=$(curl -s "https://api.github.com/repos/3proxy/3proxy/releases/latest" | grep -oP '"tag_name": "\K([0-9\.]+)' | head -n 1)
    if [ -z "$EXPECTED_3PROXY_VERSION" ]; then error "获取3proxy最新版本号失败!"; exit 1; fi

    local CURRENT_3PROXY_VERSION=""
    if [ -x "/usr/local/bin/3proxy" ]; then # 检查文件是否存在且可执行
        CURRENT_3PROXY_VERSION=$(/usr/local/bin/3proxy -h 2>&1 | grep "3proxy tiny proxy server" | grep -oP '3proxy-\K([0-9\.]+)' | head -n 1)
    fi

    if [ -z "$CURRENT_3PROXY_VERSION" ] || [ "$CURRENT_3PROXY_VERSION" != "$EXPECTED_3PROXY_VERSION" ]; then
        warn "检测到 3proxy 未安装或版本不匹配 (${CURRENT_3PROXY_VERSION:-未安装} vs ${EXPECTED_3PROXY_VERSION})，正在清理旧版本并安装最新版...";
        # 彻底清理所有旧的 3proxy 相关文件，确保全新安装
        systemctl stop 3proxy.service 3proxy@*.service >/dev/null 2>&1 || true
        systemctl disable 3proxy.service 3proxy@*.service >/dev/null 2>&1 || true
        rm -f /etc/systemd/system/3proxy.service /etc/systemd/system/3proxy@*.service
        rm -f /usr/lib/systemd/system/3proxy.service # 可能的软链接
        rm -f /etc/init.d/3proxy # SysV init script
        rm -f /usr/local/bin/3proxy # 旧的二进制或软链接
        rm -f /etc/3proxy/3proxy*.cfg # 旧的配置文件
        rm -rf /usr/local/3proxy # .deb包可能安装的目录
        systemctl daemon-reload >/dev/null 2>&1
        hash -r # 刷新shell的hash表

        local LATEST_3PROXY_JSON=$(curl -s "https://api.github.com/repos/3proxy/3proxy/releases/latest") # 获取整个JSON
        local DOWNLOAD_URL=$(echo "$LATEST_3PROXY_JSON" | grep 'browser_download_url' | grep -i "x86_64" | grep -E "(\.zip|\.tar\.gz|\.deb)" | head -n 1 | awk -F '"' '{print $4}')
        local FILE_TYPE="archive" # 默认值

        if [ -z "$DOWNLOAD_URL" ]; then
            error "获取3proxy下载链接失败! 未找到任何适用于 x86_64 的 3proxy 可下载包。请检查网络或稍后重试。"
            exit 1
        fi

        log "正在下载 3proxy ${EXPECTED_3PROXY_VERSION} 从: $DOWNLOAD_URL ..."; 
        local FILENAME="3proxy_download_package"
        if [[ "$DOWNLOAD_URL" == *.zip ]]; then FILENAME+=".zip"; FILE_TYPE="zip";
        elif [[ "$DOWNLOAD_URL" == *.tar.gz ]]; then FILENAME+=".tar.gz"; FILE_TYPE="tar.gz";
        elif [[ "$DOWNLOAD_URL" == *.deb ]]; then FILENAME+=".deb"; FILE_TYPE="deb";
        fi
        
        wget -O "$FILENAME" "$DOWNLOAD_URL"
        if [ ! -s "$FILENAME" ]; then error "3proxy 下载失败！"; exit 1; fi
        
        local extract_dir=""
        case "$FILE_TYPE" in
            "zip")
                if ! command -v unzip > /dev/null; then
                    warn "unzip 未安装，正在尝试安装..."
                    if command -v apt-get > /dev/null; then apt-get update && apt-get install -y unzip; elif command -v yum > /dev/null; then yum install -y unzip; else error "无法自动安装unzip。"; exit 1; fi
                fi
                unzip "$FILENAME"
                # 尝试更健壮地获取解压目录名
                extract_dir=$(unzip -l "$FILENAME" | awk 'NR==5 {print $NF}' | sed 's/\///g')
                if [ -z "$extract_dir" ]; then
                    extract_dir=$(find . -maxdepth 1 -type d -name "3proxy-*" -print -quit | sed 's#./##')
                fi
                if [ -z "$extract_dir" ]; then error "无法确定解压目录。"; exit 1; fi
                mv "${extract_dir}"/bin/3proxy /usr/local/bin/
                chmod +x /usr/local/bin/3proxy
                rm -rf "$FILENAME" "${extract_dir}"
                ;;
            "tar.gz")
                tar -zxvf "$FILENAME"
                # 尝试更健壮地获取解压目录名
                extract_dir=$(tar -tf "$FILENAME" | head -n 1 | sed 's/\///g')
                if [ -z "$extract_dir" ]; then
                     extract_dir=$(find . -maxdepth 1 -type d -name "3proxy-*" -print -quit | sed 's#./##')
                fi
                if [ -z "$extract_dir" ]; then error "无法确定解压目录。"; exit 1; fi
                mv "${extract_dir}"/bin/3proxy /usr/local/bin/
                chmod +x /usr/local/bin/3proxy
                rm -rf "$FILENAME" "${extract_dir}"
                ;;
            "deb")
                if ! command -v dpkg > /dev/null; then
                    error "dpkg 未安装，无法安装 .deb 包。请手动安装 dpkg 或联系开发者。"
                    exit 1
                fi
                dpkg -i "$FILENAME"
                if [ $? -ne 0 ]; then
                    warn "dpkg 安装 3proxy 可能存在依赖问题，尝试修复..."
                    if command -v apt-get > /dev/null; then apt-get install -f -y; fi
                fi
                
                local installed_3proxy_path=""
                if [ -f "/usr/bin/3proxy" ]; then installed_3proxy_path="/usr/bin/3proxy";
                elif [ -f "/usr/sbin/3proxy" ]; then installed_3proxy_path="/usr/sbin/3proxy";
                fi

                if [ -n "$installed_3proxy_path" ]; then
                    log "找到 3proxy 可执行文件在 ${installed_3proxy_path}，创建软链接到 /usr/local/bin/3proxy..."
                    ln -sf "$installed_3proxy_path" /usr/local/bin/3proxy
                    chmod +x /usr/local/bin/3proxy # 确保软链接有执行权限
                else
                    error "通过 .deb 包安装 3proxy 后未在 /usr/bin 或 /usr/sbin 找到可执行文件。请手动检查安装情况。"
                    exit 1
                fi
                rm -f "$FILENAME"
                ;;
        esac
        log "3proxy ${EXPECTED_3PROXY_VERSION} 安装成功！"
    else
        log "3proxy ${CURRENT_3PROXY_VERSION} 已安装且为最新版本。"
    fi
    
    # 最终验证 /usr/local/bin/3proxy 是否可执行
    if [ ! -x "/usr/local/bin/3proxy" ]; then
        error "3proxy 安装成功，但 /usr/local/bin/3proxy 不可执行。请手动检查文件权限。"
        exit 1
    fi


    # [修正 v63.0] 优化 sing-box 安装逻辑：版本检测与跳过，并增强版本号获取
    log "正在安装/更新 sing-box 核心..."; 
    local EXPECTED_SB_VERSION=$(curl -s "https://api.github.com/repos/SagerNet/sing-box/releases/latest" | grep -oP '"tag_name": "\K(v[0-9-.]+)' | head -n 1)
    if [ -z "$EXPECTED_SB_VERSION" ]; then error "获取sing-box最新版本号失败!"; exit 1; fi

    local CURRENT_SB_VERSION=""
    if [ -x "/usr/local/bin/sing-box" ]; then # 检查文件是否存在且可执行
        # 更鲁棒地获取 sing-box 版本号
        # 尝试多种常见输出格式
        CURRENT_SB_VERSION=$(/usr/local/bin/sing-box version 2>&1 | awk '
            /sing-box\// {
                split($0, a, "/"); # sing-box/v1.11.13 -> a[1]="sing-box", a[2]="v1.11.13"
                sub(/^v/, "", a[2]); # remove 'v' prefix if present
                print a[2];
                exit;
            }
            /sing-box-cli version/ { # Fallback for CLI output, might be different
                print $NF; # print last field
                exit;
            }
            ' | head -n 1)
    fi

    if [ -z "$CURRENT_SB_VERSION" ] || [ "$CURRENT_SB_VERSION" != "$EXPECTED_SB_VERSION" ]; then
        warn "检测到 sing-box 未安装或版本不匹配 (${CURRENT_SB_VERSION:-未安装} vs ${EXPECTED_SB_VERSION})，正在清理旧版本并安装最新版...";
        systemctl stop sing-box.service >/dev/null 2>&1 || true
        systemctl disable sing-box.service >/dev/null 2>&1 || true
        rm -f /usr/local/bin/sing-box # 删除可执行文件
        rm -f /etc/systemd/system/sing-box.service # 删除服务文件
        rm -rf /etc/sing-box # 清理旧的配置文件目录
        pkill -f sing-box || true
        systemctl daemon-reload >/dev/null 2>&1 # 重新加载 systemd 配置
        # 强制刷新shell的hash表，确保执行的是新安装的sing-box
        hash -r

        ARCH=$(uname -m); case ${ARCH} in x86_64) BOX_ARCH="amd64" ;; aarch64) BOX_ARCH="arm64" ;; *) error "不支持的架构: ${ARCH}"; exit 1 ;; esac
        DOWNLOAD_URL="https://github.com/SagerNet/sing-box/releases/download/${EXPECTED_SB_VERSION}/sing-box-${EXPECTED_SB_VERSION##v}-linux-${BOX_ARCH}.tar.gz"
        log "正在下载 sing-box ${EXPECTED_SB_VERSION}..."; wget -O sing-box.tar.gz "$DOWNLOAD_URL"
        if [ ! -s "sing-box.tar.gz" ]; then error "sing-box 下载失败！"; exit 1; fi
        tar -zxvf sing-box.tar.gz && mv sing-box-*/sing-box /usr/local/bin/ && chmod +x /usr/local/bin/sing-box && rm -rf sing-box-*
        log "sing-box ${EXPECTED_SB_VERSION} 安装成功！"
    else 
        log "sing-box ${CURRENT_SB_VERSION} 已安装且为最新版本。"
    fi
}

# 函数：发现所有公网IP及其对应的内网IP
discover_ip_pairs() {
    log "正在通过本机网络接口主动探测所有公网IP及对应内网IP..."; declare -g -A IP_PAIRS; IP_PAIRS=()
    # 修复：修改 hostname -I 的输出处理，只提取 IPv4 地址
    local private_ips=$(hostname -I | sed 's/127.0.0.1//g' | grep -oE '\b([0-9]{1,3}\.){3}[0-9]{1,3}\b'); 
    for private_ip_tmp in $private_ips; do 
        local probed_public_ip=$(curl -s --connect-timeout 5 --interface "${private_ip_tmp}" ifconfig.me)
        if [ -n "$probed_public_ip" ]; then 
            log " > 发现IP对: ${probed_public_ip} (公) <-> ${private_ip_tmp} (私)"; 
            IP_PAIRS["${probed_public_ip}"]="${private_ip_tmp}"; 
        fi
    done; 
    if [ ${#IP_PAIRS[@]} -eq 0 ]; then error "未能发现任何公网IP。"; exit 1; fi
    log "成功发现 ${#IP_PAIRS[@]} 个公网/私网IP对。"
}

# --- 脚本主流程 ---
clear; log "欢迎使用 sing-box + 3proxy 协同作战终极部署脚本 v1.3"
# 1. 系统准备
apply_system_optimizations
install_dependencies
discover_ip_pairs

# 2. 清理所有旧服务 (调整清理逻辑，以适应单 3proxy 实例的部署)
systemctl stop sing-box.service 3proxy.service >/dev/null 2>&1 || true
systemctl disable sing-box.service 3proxy@*.service >/dev/null 2>&1 || true
rm -f /etc/systemd/system/sing-box.service /etc/systemd/system/3proxy.service >/dev/null 2>&1 # 确保旧服务文件被清除
pkill -f 3proxy >/dev/null 2>&1 || true; pkill -f sing-box >/dev/null 2>&1 || true; systemctl daemon-reload; log "已清理所有旧服务。"


# 3. 配置并启动单个3proxy后端服务（多端口绑定多出口IP）
log "正在配置并启动单个 3proxy 后端服务，绑定多个出口IP..."
mkdir -p /etc/3proxy
SINGLE_3PROXY_CONF_PATH="/etc/3proxy/3proxy.cfg"
PID_FILE="/var/run/3proxy.pid"

cat > ${SINGLE_3PROXY_CONF_PATH} <<EOF
daemon
nscache 65536
nserver 8.8.8.8
nserver 1.1.1.1
pidfile ${PID_FILE}
log /var/log/3proxy.log
logformat "- +_L%t.%. %N.%p %E %U %C:%c %R:%r %O %I %h %T"
rotate 1
# 每个内网IP对应一个独立的socks服务，用于指定出口IP
EOF

INTERNAL_PORT=50000
for public_ip in "${!IP_PAIRS[@]}"; do
    private_ip=${IP_PAIRS[$public_ip]}
    echo "socks -p${INTERNAL_PORT} -i127.0.0.1 -e${private_ip}" | tee -a ${SINGLE_3PROXY_CONF_PATH} > /dev/null
    INTERNAL_PORT=$((INTERNAL_PORT + 1))
done

cat > /etc/systemd/system/3proxy.service <<EOF
[Unit]
Description=3proxy Multi-Exit Backend Service
After=network.target
[Service]
Type=forking
ExecStart=/usr/local/bin/3proxy ${SINGLE_3PROXY_CONF_PATH}
PIDFile=${PID_FILE}
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal
SyslogIdentifier=3proxy-main
[Install]
WantedBy=multi-user.target
EOF
log "单个 3proxy.service 已创建，配置文件：${SINGLE_3PROXY_CONF_PATH}"

# 启动 3proxy 服务
systemctl daemon-reload
systemctl enable 3proxy.service
systemctl restart 3proxy.service
sleep 2

if ! systemctl is-active --quiet 3proxy.service; then
    error "3proxy 主服务启动失败! 请检查 'journalctl -u 3proxy.service' 获取详细日志。"
    local specific_error=$(journalctl -u 3proxy.service --since "1 minute ago" -o cat | grep "3proxy-main" | tail -n 1)
    if [ -n "$specific_error" ]; then
        error "来自 3proxy 的具体错误信息: ${specific_error}"
    fi
    exit 1
fi
log "单个 3proxy 后端服务已成功启动！"


# 4. 选择代理协议
log "正在配置并启动 sing-box 前端服务，实现入口IP和出口IP一致的路由..."
warn "请选择您要部署的代理协议 (此协议将用于实现多IP入站出站一致性)："
echo " 1. VLESS"; echo " 2. VMess"; echo " 3. Shadowsocks"; echo " 4. Hysteria2"; echo " 5. SOCKS5"; echo " 6. TUIC"; echo " 7. WireGuard (仅提供VPN功能，不保证入站出站IP一致)"
read -p "请输入选项 [1-7]: " protocol_choice

# 如果选择 WireGuard (选项 7)，则进行 wg-quick 搭建
if [[ "$protocol_choice" == "7" ]]; then
    log "您选择了 WireGuard 协议。正在配置系统级 wg-quick WireGuard 服务器..."
    # 检查并安装 wireguard-tools 和 wireguard
    if ! command -v wg > /dev/null || ! modprobe wireguard > /dev/null 2>&1; then
        warn "WireGuard 工具或内核模块未安装/加载...";
        if command -v apt-get > /dev/null; then
            apt-get update && apt-get install -y wireguard wireguard-tools;
        elif command -v yum > /dev/null; then
            yum install -y epel-release && yum install -y wireguard-tools wireguard-dkms wireguard; # For CentOS/RHEL
        else
            error "无法自动安装 WireGuard。请手动安装 (apt install wireguard 或 yum install wireguard)。";
            exit 1 # 如果 WireGuard 无法安装，就退出
        fi
    fi
    log "WireGuard 工具和模块已就绪。"

    mkdir -p /etc/wireguard
    chmod 700 /etc/wireguard

    # --- wg-quick 清理旧配置和接口 ---
    log "检测并清理旧的 wg-quick 配置和服务..."
    if systemctl is-active --quiet wg-quick@wg0; then
        warn "检测到 wg-quick@wg0 服务正在运行，正在尝试停止和关闭接口..."
        sudo wg-quick down wg0 >/dev/null 2>&1 || true # 尝试关闭接口，执行 PostDown
        sudo systemctl stop wg-quick@wg0.service >/dev/null 2>&1 || true
        sudo systemctl disable wg-quick@wg0.service >/dev/null 2>&1 || true
        log "旧的 wg-quick 服务已停止和禁用。"
    fi
    if [ -d "/etc/wireguard" ]; then
        sudo rm -rf /etc/wireguard/* >/dev/null 2>&1 # 清理所有旧的 WireGuard 配置文件
        log "已清理 /etc/wireguard/ 下的旧配置文件。"
    fi
    # 确保目录和权限
    mkdir -p /etc/wireguard
    chmod 700 /etc/wireguard
    log "已准备好用于 wg-quick 的干净配置目录。"

    # 生成服务器密钥对 (gw = gateway)
    wg genkey | tee /etc/wireguard/gw-privatekey > /dev/null
    wg pubkey < /etc/wireguard/gw-privatekey > /etc/wireguard/gw-publickey
    GW_PRIVATE_KEY=$(cat /etc/wireguard/gw-privatekey)
    GW_PUBLIC_KEY=$(cat /etc/wireguard/gw-publickey)

    # 生成 PC 客户端密钥对示例 (只生成一个)
    wg genkey | tee /etc/wireguard/mypc-privatekey > /dev/null
    wg pubkey < /etc/wireguard/mypc-privatekey > /etc/wireguard/mypc-publickey
    MYPC_PRIVATE_KEY=$(cat /etc/wireguard/mypc-privatekey)
    MYPC_PUBLIC_KEY=$(cat /etc/wireguard/mypc-publickey)

    # 获取第一个公网IP作为wg-quick的Endpoint (通常是主公网IP)
    declare -a public_ips_array=("${!IP_PAIRS[@]}")
    FIRST_PUBLIC_IP=${public_ips_array[0]}

    # 生成 wg0.conf
    cat > /etc/wireguard/wg0.conf <<EOF
[Interface]
ListenPort = ${WG_PORT} # 使用脚本自定义的 WG_PORT
Address = 10.1.0.1/24
PrivateKey = ${GW_PRIVATE_KEY}

PostUp = iptables -A FORWARD -i wg0 -j ACCEPT; iptables -A FORWARD -o wg0 -j ACCEPT; iptables -t nat -A POSTROUTING -o eth0 -j MASQUERADE
PostDown = iptables -D FORWARD -i wg0 -j ACCEPT; iptables -D FORWARD -o wg0 -j ACCEPT; iptables -t nat -D POSTROUTING -o eth0 -j MASQUERADE

[Peer]
PublicKey = ${MYPC_PUBLIC_KEY}
AllowedIPs = 10.1.0.2/32

EOF
    log "wg0.conf 已生成。"

    # 设置IP转发 (全局生效)
    sysctl -w net.ipv4.ip_forward=1 > /dev/null 2>&1
    grep -qE "^net.ipv4.ip_forward=1" /etc/sysctl.conf || echo "net.ipv4.ip_forward=1" | tee -a /etc/sysctl.conf > /dev/null
    sysctl -p > /dev/null 2>&1
    log "IP 转发已启用。"

    # --- 移除 ufw 配置 wg-quick 端口 ---
    log "跳过 ufw 端口 ${WG_PORT}/udp 配置，服务器默认放行所有端口。"

    # 启动 wg-quick 服务
    log "正在启动 wg-quick 服务..."
    # 检查 wg-quick 命令是否存在，以防万一
    if ! command -v wg-quick > /dev/null; then
        error "'wg-quick' 命令未找到，无法启动 wg-quick 服务。请手动安装 wireguard-tools。";
        exit 1
    fi
    systemctl enable wg-quick@wg0 > /dev/null 2>&1
    systemctl start wg-quick@wg0
    sleep 2
    if ! systemctl is-active --quiet wg-quick@wg0; then
        error "wg-quick 服务启动失败! 请检查 'journalctl -u wg-quick@wg0.service' 获取详细日志。"
        exit 1 # 如果 wg-quick 失败，则直接退出脚本，因为它无法提供 WireGuard VPN 服务
    else
        log "wg-quick 服务已成功启动！"
        log "----------------------------------------------------"
        warn "以下是您的 WireGuard PC 客户端配置（请直接复制粘贴使用）："
        echo -e "${GREEN}[Interface]
PrivateKey = ${MYPC_PRIVATE_KEY}
Address = 10.1.0.2/24

[Peer]
PublicKey = ${GW_PUBLIC_KEY}
AllowedIPs = 0.0.0.0/0
Endpoint = ${FIRST_PUBLIC_IP}:${WG_PORT}
PersistentKeepalive = 30
${NC}"
        log "----------------------------------------------------"
        warn "请注意：此 wg-quick VPN 流量默认从服务器的默认公网 IP 出站，不保证入站出站 IP 一致。"
    fi
    exit 0 # WireGuard 搭建完毕，脚本直接退出
fi

# 以下是 sing-box + 3proxy 的配置部分，只在未选择 WireGuard 时执行

# 存储 JSON 片段的数组
endpoint_objects_raw=() # sing-box不再管理WireGuard，所以endpoints为空
inbound_objects_raw=()
outbound_objects_raw=()
routing_rule_objects_raw=()
connection_infos=()

current_port=$START_PORT
STABLE_UUID=$(echo -n "$PROXY_PASS" | md5sum | awk '{print $1}' | sed -E 's/(.{8})(.{4})(.{4})(.{4})(.{12})/\1-\2-\3-\4-\5/')
INTERNAL_SOCKS_PORT=50000

mkdir -p /etc/sing-box

# 遍历IP对，生成inbounds, outbounds, rules片段
for public_ip in "${!IP_PAIRS[@]}"; do
    private_ip=${IP_PAIRS[$public_ip]}
    
    # 构建 outbound JSON 对象
    outbound_tag="out-for-${private_ip//./-}" 
    outbound_objects_raw+=( "$(printf '{"tag":"%s","type":"socks","server":"127.0.0.1","server_port":%d}' "$outbound_tag" "$INTERNAL_SOCKS_PORT")" )

    # 构建 inbound JSON 对象，根据协议类型精确传递参数
    inbound_tag="in-from-${private_ip//./-}"
    inbound_obj="" # 定义变量以确保其存在
    SNIFF_CONFIG_COMMON='"sniff": true' # VLESS, VMess, Shadowsocks, Hysteria2, SOCKS5, TUIC 共享

    case $protocol_choice in
        1) # VLESS
            inbound_obj=$(printf '{"tag":"%s","type":"vless","listen":"%s","listen_port":%d,"users":[{"uuid":"%s"}],%s}' "$inbound_tag" "$private_ip" "$current_port" "$STABLE_UUID" "$SNIFF_CONFIG_COMMON")
            connection_infos+=("VLESS | ${public_ip}:${current_port} | UUID=${STABLE_UUID}") 
            ;;
        2) # VMess
            inbound_obj=$(printf '{"tag":"%s","type":"vmess","listen":"%s","listen_port":%d,"users":[{"uuid":"%s"}],%s}' "$inbound_tag" "$private_ip" "$current_port" "$STABLE_UUID" "$SNIFF_CONFIG_COMMON")
            connection_infos+=("VMess | ${public_ip}:${current_port} | UUID=${STABLE_UUID}") 
            ;;
        3) # Shadowsocks
            inbound_obj=$(printf '{"tag":"%s","type":"shadowsocks","listen":"%s","listen_port":%d,"method":"aes-256-gcm","password":"%s",%s}' "$inbound_tag" "$private_ip" "$current_port" "$PROXY_PASS" "$SNIFF_CONFIG_COMMON")
            connection_infos+=("Shadowsocks(aes-256-gcm) | ${public_ip}:${current_port} | aes-256-gcm | ${PROXY_PASS}") 
            ;;
        4) # Hysteria2
            inbound_obj=$(printf '{"tag":"%s","type":"hysteria2","listen":"%s","listen_port":%d,"users":{"%s":""},%s}' "$inbound_tag" "$private_ip" "$current_port" "$PROXY_PASS" "$SNIFF_CONFIG_COMMON")
            connection_infos+=("Hysteria2 | ${public_ip}:${current_port} | Password=${PROXY_PASS}") 
            ;;
        5) # SOCKS5
            inbound_obj=$(printf '{"tag":"%s","type":"socks","listen":"%s","listen_port":%d,"users":[{"username":"%s","password":"%s"}],%s}' "$inbound_tag" "$private_ip" "$current_port" "$PROXY_USER" "$PROXY_PASS" "$SNIFF_CONFIG_COMMON")
            connection_infos+=("SOCKS5 | ${public_ip}:${current_port} | User=${PROXY_USER} | Pass=${PROXY_PASS}") 
            ;;
        6) # TUIC
            # 生成 TUIC UUID (UUID基于PROXY_PASS)
            TUIC_UUID=$(echo -n "$PROXY_PASS" | md5sum | awk '{print $1}' | sed -E 's/(.{8})(.{4})(.{4})(.{4})(.{12})/\1-\2-\3-\4-\5/')
            TUIC_PASSWORD="${PROXY_PASS}"
            
            # 根据最新官方文档，TUIC 入站配置不再有 "network" 和 "udp_relay_mode" 字段
            inbound_obj=$(printf '{"tag":"%s","type":"tuic","listen":"%s","listen_port":%d,"users":[{"uuid":"%s","password":"%s"}],"congestion_control":"cubic","heartbeat":"10s","tls":{"enabled":true,"server_name":"%s","insecure":true},%s}' \
                "$inbound_tag" "$private_ip" "$current_port" "$TUIC_UUID" "$TUIC_PASSWORD" "$public_ip" "$SNIFF_CONFIG_COMMON")
            connection_infos+=("TUIC | ${public_ip}:${current_port} | UUID=${TUIC_UUID} | Password=${TUIC_PASSWORD} | TLS=Insecure")
            ;;
        *) error "无效选项"; exit 1 ;;
    esac
    
    inbound_objects_raw+=( "${inbound_obj}" )
    # 构建路由规则 JSON 对象
    routing_rule_obj=$(printf '{"inbound":["%s"],"outbound":"%s"}' "$inbound_tag" "$outbound_tag")
    routing_rule_objects_raw+=( "${routing_rule_obj}" )
    
    current_port=$((current_port + 1))
    INTERNAL_SOCKS_PORT=$((INTERNAL_SOCKS_PORT + 1))
done

# --- 移除 ufw 配置 sing-box 端口 ---
log "跳过 ufw 端口配置，服务器默认放行所有端口。"


# 使用 jq 来构建最终的 JSON 配置文件
jq_cmd_base="{
  \"log\": {
    \"level\": \"warn\"
  },
  \"dns\": {},
  \"ntp\": {},
  \"endpoints\": [],
  \"inbounds\": [],
  \"outbounds\": [],
  \"route\": {
    \"rules\": [],
    \"final\": \"direct\"
  },
  \"experimental\": {}
}"

endpoints_json_arr_str="[]" # WireGuard由wg-quick管理，sing-box不配置endpoints

inbounds_json_arr_str=$(printf '%s\n' "${inbound_objects_raw[@]}" | jq -s '.' 2>/dev/null)
if [ -z "$inbounds_json_arr_str" ] || ! echo "$inbounds_json_arr_str" | jq . >/dev/null 2>&1; then inbounds_json_arr_str="[]"; warn "Inbounds JSON 数组构建异常或为空，已重置为 []"; fi

outbounds_json_arr_str=$(printf '%s\n' "${outbound_objects_raw[@]}" | jq -s '. + [{"tag":"direct","type":"direct"}]' 2>/dev/null)
if [ -z "$outbounds_json_arr_str" ] || ! echo "$outbounds_json_arr_str" | jq . >/dev/null 2>&1; then outbounds_json_arr_str="[]"; warn "Outbounds JSON 数组构建异常或为空，已重置为 []"; fi

routing_rules_json_arr_str=$(printf '%s\n' "${routing_rule_objects_raw[@]}" | jq -s '. + [{"outbound":"direct"}]' 2>/dev/null)
if [ -z "$routing_rules_json_arr_str" ] || ! echo "$routing_rules_json_arr_str" | jq . >/dev/null 2>&1; then routing_rules_json_arr_str="[]"; warn "Routing rules JSON 数组构建异常或为空，已重置为 []"; fi


# 使用 jq 命令来生成最终的配置文件，添加 LC_ALL=C 强制标准环境
LC_ALL=C echo "$jq_cmd_base" | \
jq \
  --argjson endpoints_arr "$endpoints_json_arr_str" \
  --argjson inbounds_arr "$inbounds_json_arr_str" \
  --argjson outbounds_arr "$outbounds_json_arr_str" \
  --argjson rules_arr "$routing_rules_json_arr_str" \
  '.endpoints = $endpoints_arr | .inbounds = $inbounds_arr | .outbounds = $outbounds_arr | .route.rules = $rules_arr' \
> /etc/sing-box/config.json

# 验证 sing-box 配置是否有效 (可选，但推荐)
log "正在验证 sing-box 配置文件并尝试获取详细日志..."
CONFIG_CHECK_OUTPUT=$(/usr/local/bin/sing-box check -c /etc/sing-box/config.json 2>&1)
if [ $? -ne 0 ]; then
    error "生成的 sing-box 配置文件存在语法错误或逻辑问题！"
    error "sing-box check 详细错误: ${CONFIG_CHECK_OUTPUT}"
    error "请检查 /etc/sing-box/config.json 内容。"
    error "您可以使用命令 'cat /etc/sing-box/config.json' 来查看文件内容。" 
    error "强烈建议您检查 sing-box 版本是否够新，使用其他可用协议，或尝试手动编译 sing-box，或更换服务器环境以解决此问题。"
    exit 1
fi
log "sing-box 配置文件已通过内部校验。"

log "sing-box 配置文件已生成。"

# 创建并启动 sing-box 服务
cat > /etc/systemd/system/sing-box.service <<EOF
[Unit]
Description=sing-box Frontend Service
After=network.target network-online.target 3proxy.service
[Service]
# 将 Type 从 forking 改为 exec，让 systemd 更好地追踪 sing-box 进程
Type=exec
# 增加启动超时时间，给予 sing-box 足够时间启动
TimeoutStartSec=30
# 在启动 sing-box 之前先进行配置检查，如果配置有问题则不会尝试启动
ExecStartPre=/usr/local/bin/sing-box check -c /etc/sing-box/config.json
ExecStart=/usr/local/bin/sing-box run -c /etc/sing-box/config.json
Restart=always
RestartSec=5
# 将 StandardOutput 和 StandardError 直接设置为 journal
StandardOutput=journal
StandardError=journal
SyslogIdentifier=sing-box-main
[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload && systemctl enable sing-box.service && systemctl restart sing-box.service
sleep 2;
if ! systemctl is-active --quiet sing-box.service; then
    error "sing-box 前端服务启动失败！请检查 'journalctl -u sing-box.service' 获取详细日志。"
    log "尝试直接运行 sing-box 获取更多日志..."
    # 再次尝试直接运行，看能否捕获任何即时输出
    DIRECT_RUN_OUTPUT=$(/usr/local/bin/sing-box run -c /etc/sing-box/config.json 2>&1 & sleep 3; pkill -f sing-box 2>/dev/null; wait $! 2>/dev/null)
    if [ -n "$DIRECT_RUN_OUTPUT" ]; then
        error "直接运行 sing-box 的输出: ${DIRECT_RUN_OUTPUT}"
    else
        warn "直接运行 sing-box 未产生额外输出，这可能表示 sing-box 启动后立即退出或没有错误输出到 stdout/stderr。"
    fi
    error "强烈建议您检查 sing-box 版本是否够新，使用其他可用协议，或尝试手动编译 sing-box，或更换服务器环境以解决此问题。"
    exit 1
fi
log "sing-box 前端服务已成功启动！"

# --- 最终信息和完成 ---
echo "----------------------------------------------------"
echo; log "====================  协同部署成功! ===================="; echo
warn "已为您自动创建了「入口IP匹配出口IP」的代理服务，连接信息如下："; echo
for info in "${connection_infos[@]}"; do echo -e "${GREEN}${info}${NC}"; done
echo; warn "现在，从您的服务器的哪个公网IP（对应内网IP）连接 sing-box，流量就会从该公网IP出口。"
log "所有任务已完成。"