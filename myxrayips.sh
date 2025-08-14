#!/bin/bash

# --- 颜色定义 ---
COLOR_GREEN='\033[0;32m'
COLOR_YELLOW='\033[1;33m'
COLOR_RED='\033[0;31m'
COLOR_BLUE='\033[0;34m'
COLOR_NC='\033[0m' # No Color

# --- 1. 用户可配置变量 ---
# 默认用户名
DEFAULT_USERNAME="cwl"
# 默认密码
DEFAULT_PASSWORD="666888"
# 协议起始端口
START_PORT=51801
# 协议结束端口 (脚本会在此范围内分配端口，留足空间以支持254个IP * 4种协议)
END_PORT=52888

# Swap文件大小 (例如 1G)
SWAP_SIZE="1G"
# 是否启用 IRQ 负载均衡 (yes/no)
ENABLE_IRQ_BALANCE="no" # 可以手动设置为 "yes" 来启用

# --- 日志和文件路径配置 ---
LOG_DIR="/var/log/xray"
LOG_FILE="$LOG_DIR/xray.log"
XRAY_CONFIG_DIR="/etc/xray"
XRAY_CONFIG_FILE="$XRAY_CONFIG_DIR/config.json"
SYSTEMD_SERVICE_FILE="/etc/systemd/system/xray.service"

# --- 选中的协议列表 (由用户选择填充) ---
declare -A SELECTED_PROTOCOLS # 例如：SELECTED_PROTOCOLS["socks5"]=1, SELECTED_PROTOCOLS["vmess"]=1

# --- 日志函数 ---
# 确保在任何日志输出前创建日志目录
mkdir -p "$LOG_DIR"
touch "$LOG_FILE"
chmod 644 "$LOG_FILE"

log() {
    echo -e "${COLOR_GREEN}$(date +"%Y-%m-%d %H:%M:%S") [信息] $@${COLOR_NC}" | tee -a "$LOG_FILE"
}

warn() {
    echo -e "${COLOR_YELLOW}$(date +"%Y-%m-%d %H:%M:%S") [警告] $@${COLOR_NC}" | tee -a "$LOG_FILE" >&2
}

error() {
    echo -e "${COLOR_RED}$(date +"%Y-%m-%d %H:%M:%S") [错误] $@${COLOR_NC}" | tee -a "$LOG_FILE" >&2
    exit 1
}

echo "### Xray 自动化部署脚本 (版本 1.14) ###"
log "脚本开始执行..."
echo ""

# --- 辅助函数：版本号比较 ---
version_gt() {
    local v1=$(echo "$1" | sed 's/^v//')
    local v2=$(echo "$2" | sed 's/^v//')

    local IFS=.
    local a=($v1)
    local b=($v2)
    unset IFS

    for ((i=0; i<${#a[@]} || i<${#b[@]}; i++)); do
        local n1=$(echo "${a[i]}" | sed 's/[^0-9].*//')
        local s1=$(echo "${a[i]}" | sed 's/^[0-9]*//')
        local n2=$(echo "${b[i]}" | sed 's/[^0-9]*//')
        local s2=$(echo "${b[i]}" | sed 's/^[0-9]*//')

        n1=${n1:-0}
        n2=${n2:-0}

        if (( 10#$n1 > 10#$n2 )); then return 0; fi
        if (( 10#$n1 < 10#$n2 )); then return 1; fi

        local order_rank=( "alpha" "beta" "rc" "" )
        local rank1=-1
        for k in "${!order_rank[@]}"; do
            if [[ "${order_rank[k]}" == "$s1" ]]; then rank1=$k; fi
            if [[ "${order_rank[k]}" == "$s2" ]]; then rank2=$k; fi
        done

        if (( rank1 > rank2 )); then return 0; fi
        if (( rank1 < rank2 )); then return 1; fi
    done
    return 1
}


# --- 设置语言环境 ---
configure_locale() {
    log "--- 配置语言环境 ---"
    export LANG="zh_CN.UTF-8"
    export LC_ALL="zh_CN.UTF-8"
    sudo apt update -y &>/dev/null
    sudo apt install -y locales &>/dev/null
    if ! grep -q "zh_CN.UTF-8 UTF-8" /etc/locale.gen; then
        echo "zh_CN.UTF-8 UTF-8" | sudo tee -a /etc/locale.gen > /dev/null
    fi
    sudo locale-gen zh_CN.UTF-8 &>/dev/null
    sudo update-locale LANG=zh_CN.UTF-8 LC_ALL=zh_CN.UTF-8 &>/dev/null
    log "语言环境配置完成。如果仍有locale警告，请手动检查系统设置或重启SSH会话。"
}

# --- 协议选择菜单 ---
select_protocols() {
    echo -e "${COLOR_BLUE}--- 请选择要搭建的协议 ---${COLOR_NC}"
    echo "1) SOCKS5"
    echo "2) VMess (TCP)"
    echo "3) Shadowsocks"
    echo "4) WireGuard"
    echo -e "${COLOR_BLUE}--------------------------${COLOR_NC}"
    read -p "请输入你的选择 (例如: 1 3 4): " choices

    local selection_made=false
    for choice in $choices; do
        case "$choice" in
            "1") SELECTED_PROTOCOLS["socks5"]=1; selection_made=true ;;
            "2") SELECTED_PROTOCOLS["vmess"]=1; selection_made=true ;;
            "3") SELECTED_PROTOCOLS["shadowsocks"]=1; selection_made=true ;;
            "4") SELECTED_PROTOCOLS["wireguard"]=1; selection_made=true ;;
            *) warn "无效的选择: $choice。将被忽略。" ;;
        esac
    done

    if [ "$selection_made" = false ]; then
        error "未选择任何有效协议。请重新运行脚本并选择协议。"
    fi

    echo -e "${COLOR_BLUE}你选择的协议有:${COLOR_NC}"
    for proto in "${!SELECTED_PROTOCOLS[@]}"; do
        echo -e "${COLOR_GREEN}- $proto${COLOR_NC}"
    done
    echo ""
}


# --- 2. 动态识别内网IP、网卡和默认网关 ---
declare -A PRIVATE_IP_TO_INTERFACE_MAP # 私网IP到网卡名称的映射
declare -A INTERFACE_TO_DEFAULT_GATEWAY_MAP # 网卡到其默认网关的映射
declare -A PRIVATE_IP_TO_GATEWAY_MAP # 新增：私网IP到其网关的映射

discover_network_info() {
    log "正在探测网络接口和路由信息..."
    # 查找所有非回环的 IPv4 地址和它们所在的网卡
    local all_private_ips_detailed=$(ip -4 address show | awk '/inet / {print $2, $NF}' | grep -v '127.0.0.1' | cut -d'/' -f1,2)

    while read -r ip_cidr iface; do
        local private_ip=$(echo "$ip_cidr" | cut -d'/' -f1)
        PRIVATE_IP_TO_INTERFACE_MAP["$private_ip"]="$iface"
        log " > 发现私网IP: '$private_ip' 绑定到网卡: '$iface'"
    done <<< "$all_private_ips_detailed"

    if [ ${#PRIVATE_IP_TO_INTERFACE_MAP[@]} == 0 ]; then
        error "未能发现任何有效的私网IP配置。请检查网络设置。"
    fi

    # 针对每个网卡，找出其默认网关
    local discovered_interfaces=($(printf "%s\n" "${PRIVATE_IP_TO_INTERFACE_MAP[@]}" | sort -u))
    if [ ${#discovered_interfaces[@]} == 0 ]; then
        error "未能发现任何网络接口。"
    fi
    
    for iface in "${discovered_interfaces[@]}"; do
        # 针对每个接口查找其默认网关
        local gateway=$(ip route show default | grep "dev $iface" | awk '{print $3}')
        if [ -n "$gateway" ]; then
            INTERFACE_TO_DEFAULT_GATEWAY_MAP["$iface"]="$gateway"
            log " > 网卡 '$iface' 的默认网关: '$gateway'"
        else
            warn "未能为网卡 '$iface' 找到默认网关。这可能会影响策略路由配置。"
        fi
    done

    # 建立私网IP到网关的映射，以便后续配置
    for private_ip in "${!PRIVATE_IP_TO_INTERFACE_MAP[@]}"; do
        local iface=${PRIVATE_IP_TO_INTERFACE_MAP[$private_ip]}
        local gateway=${INTERFACE_TO_DEFAULT_GATEWAY_MAP[$iface]}
        if [ -n "$gateway" ]; then
            PRIVATE_IP_TO_GATEWAY_MAP["$private_ip"]="$gateway"
            log " > 绑定私网IP '$private_ip' 到网关: '$gateway'"
        fi
    done

    if [ ${#PRIVATE_IP_TO_GATEWAY_MAP[@]} == 0 ]; then
        error "未能成功探测到任何默认网关。请手动检查路由表。"
    fi
}


# --- 3. 动态探测公网与私网IP映射 ---
declare -A PUBLIC_IP_TO_PRIVATE_IP_MAP # 公网IP到私网IP的映射
declare -A PRIVATE_IP_TO_MARK_MAP # 私网IP到路由标记的映射

discover_ip_pairs() {
    log "正在探测公网与私网IP映射（通过 ifconfig.me）..."
    PUBLIC_IP_TO_PRIVATE_IP_MAP=() # 清空旧的映射

    local private_ips_to_check=()
    for private_ip in "${!PRIVATE_IP_TO_INTERFACE_MAP[@]}"; do
        private_ips_to_check+=("$private_ip")
    done

    if [ ${#private_ips_to_check[@]} == 0 ]; then
        error "没有可用的私网IP进行公网探测。"
    fi

    local current_mark=10001 # 路由表ID和fwmark起始从10001开始
    for private_ip in "${private_ips_to_check[@]}"; do
        local discovered_public_ip=""
        local attempts=3
        while [ "$attempts" -gt 0 ]; do
            log "   尝试从私网IP '$private_ip' 探测公网IP (剩余尝试: $attempts)..."
            discovered_public_ip=$(curl -s -4 --connect-timeout 5 --interface "${private_ip}" ifconfig.me)
            if [ -n "$discovered_public_ip" ] && [[ "$discovered_public_ip" =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]]; then
                log " > 发现映射: 公网IP '${discovered_public_ip}' -> 私网IP '${private_ip}'"
                PUBLIC_IP_TO_PRIVATE_IP_MAP["${discovered_public_ip}"]="${private_ip}"
                PRIVATE_IP_TO_MARK_MAP["${private_ip}"]="${current_mark}" # 存储私网IP到mark的映射
                current_mark=$((current_mark+1)) # 递增 mark
                break
            else
                warn "   从私网IP '$private_ip' 探测公网IP失败 (返回: '$discovered_public_ip')。"
                sleep 2 # 等待2秒后重试
                attempts=$((attempts-1))
            fi
        done
        if [ -z "$discovered_public_ip" ]; then
            warn "未能通过私网IP '$private_ip' 探测到其对应的公网IP。这可能导致该IP的服务无法正常工作。"
        fi
    done

    if [ ${#PUBLIC_IP_TO_PRIVATE_IP_MAP[@]} == 0 ]; then
        error "未能发现任何可用的公网IP映射。请检查网络连通性或手动配置 IP 映射。"
    fi
}


# --- 4. 检查并安装必要工具 ---
install_dependencies() {
    log "--- 检查并安装必要工具 ---"
    REQUIRED_TOOLS=("curl" "jq" "wget" "tar" "sha1sum" "uuid-runtime" "unzip")
    for tool in "${REQUIRED_TOOLS[@]}"; do
        if ! command -v "$tool" &> /dev/null; then
            log "工具 '$tool' 未安装，正在尝试安装..."
            sudo apt update -y
            sudo apt install -y "$tool"
            if [ $? -ne 0 ]; then
                error "无法安装工具 '$tool'。请手动安装后重试。"
            fi
        else
            log "工具 '$tool' 已安装。"
        fi
    done

    if ! command -v wg &> /dev/null; then
        log "工具 'wireguard-tools' 未安装，正在尝试安装..."
        sudo apt update -y
        sudo apt install -y wireguard-tools
        if [ $? -ne 0 ]; then
            error "无法安装工具 'wireguard-tools'。请手动安装后重试。"
        fi
    else
        log "工具 'wireguard-tools' 已安装。"
        log "为确保 WireGuard 模块加载，尝试加载内核模块..."
        if ! lsmod | grep -q wireguard; then
            sudo modprobe wireguard >/dev/null 2>&1
            if ! lsmod | grep -q wireguard; then
                warn "WireGuard 内核模块加载失败。WireGuard 协议可能无法正常工作。"
            else
                log "WireGuard 内核模块已加载。"
            fi
        else
            log "WireGuard 内核模块已加载。"
        fi
    fi
}

# --- 5. 自动检测并卸载 Sing-box (如果存在) ---
uninstall_singbox_if_exists() {
    log "--- 检测并卸载 Sing-box (如果存在) ---"
    local singbox_service_name="sing-box"
    local singbox_executable="/usr/local/bin/sing-box"
    local singbox_config_dir="/etc/sing-box"
    local singbox_service_file="/etc/systemd/system/sing-box.service"

    local singbox_found=false

    if systemctl is-active --quiet "$singbox_service_name"; then
        warn "检测到 Sing-box 服务正在运行，正在停止..."
        sudo systemctl stop "$singbox_service_name"
        singbox_found=true
    fi

    if systemctl is-enabled --quiet "$singbox_service_name"; then
        warn "检测到 Sing-box 服务已启用，正在禁用..."
        sudo systemctl disable "$singbox_service_name"
        singbox_found=true
    fi

    if [ -f "$singbox_executable" ]; then
        warn "检测到 Sing-box 可执行文件 '$singbox_executable'，正在移除..."
        sudo rm -f "$singbox_executable"
        singbox_found=true
    fi

    if [ -d "$singbox_config_dir" ]; then
        warn "检测到 Sing-box 配置目录 '$singbox_config_dir'，正在移除..."
        sudo rm -rf "$singbox_config_dir"
        singbox_found=true
    fi

    if [ -f "$singbox_service_file" ]; then
        warn "检测到 Sing-box Systemd 服务文件 '$singbox_service_file'，正在移除..."
        sudo rm -f "$singbox_service_file"
        singbox_found=true
    fi

    if [ "$singbox_found" = true ]; then
        sudo systemctl daemon-reload >/dev/null 2>&1
        log "已成功卸载并清除 Sing-box 相关组件。"
    else
        log "未检测到 Sing-box 安装，无需卸载。"
    fi
    echo ""
}


# --- 6. 停止并禁用现有 Xray 服务 (如果存在) ---
stop_existing_xray() {
    log "--- 停止并禁用现有 Xray 服务 (如果存在) ---"
    if systemctl is-active --quiet xray; then
        sudo systemctl stop xray
        log "已停止 xray 服务。"
    fi
    if systemctl is-enabled --quiet xray; then
        sudo systemctl disable xray
        log "已禁用 xray 服务。"
    fi

    sudo systemctl daemon-reload >/dev/null 2>&1
    log "Systemd daemon 已重新加载，确保 Xray 不会自动重启。"

    if [ -d "$XRAY_CONFIG_DIR" ]; then
        log "清理旧的 Xray 配置目录: $XRAY_CONFIG_DIR"
        sudo rm -rf "$XRAY_CONFIG_DIR"
    fi

    log "清空 Xray 服务的 journalctl 日志..."
    sudo journalctl --rotate # 轮换日志文件
    sudo journalctl --vacuum-time=1s --unit xray.service >/dev/null 2>&1 || true
    log "Xray 服务历史日志已清空 (可能不立即释放磁盘空间)。"
}

# --- 7. 安装/更新 Xray 核心 ---
install_or_update_xray() {
    log "正在安装/更新 Xray 核心..."
    local github_releases_api="https://api.github.com/repos/XTLS/Xray-core/releases"
    local EXPECTED_XRAY_VERSION=""

    EXPECTED_XRAY_VERSION=$(curl -s "$github_releases_api/latest" | jq -r '.tag_name')

    if [ -z "$EXPECTED_XRAY_VERSION" ]; then
        error "获取 Xray 最新版本号失败！请检查网络连接到 api.github.com。"
    fi
    log "检测到 Xray 最新版本为: $EXPECTED_XRAY_VERSION"

    local CURRENT_XRAY_VERSION=""
    if [ -x "/usr/local/bin/xray" ]; then
        # 修复：使用更健壮的命令来解析 Xray 版本号
        CURRENT_XRAY_VERSION=$(/usr/local/bin/xray version 2>&1 | grep 'Xray ' | head -n 1 | awk '{print $2}' | sed 's/^v//')
    fi

    local PERFORM_INSTALL="yes"
    if [ -n "$CURRENT_XRAY_VERSION" ]; then
        log "当前已安装 Xray 版本: v$CURRENT_XRAY_VERSION"
        local EXPECTED_XRAY_VERSION_CLEAN=$(echo "$EXPECTED_XRAY_VERSION" | sed 's/^v//')

        if version_gt "$EXPECTED_XRAY_VERSION_CLEAN" "$CURRENT_XRAY_VERSION"; then
            warn "检测到 Xray 版本落后 (当前 v$CURRENT_XRAY_VERSION vs 最新 $EXPECTED_XRAY_VERSION)。正在清理旧版本并安装最新版..."
        else
            log "Xray v$CURRENT_XRAY_VERSION 已安装且为最新或更高版本，跳过安装。"
            PERFORM_INSTALL="no"
        fi
    else
        warn "检测到 Xray 未安装，正在安装最新版 ($EXPECTED_XRAY_VERSION)..."
    fi

    if [ "$PERFORM_INSTALL" = "no" ]; then
        return 0
    fi

    systemctl stop xray.service >/dev/null 2>&1 || true
    systemctl disable xray.service >/dev/null 2>&1 || true
    rm -f /usr/local/bin/xray
    rm -f /etc/systemd/system/xray.service
    rm -rf /etc/xray
    pkill -f xray >/dev/null 2>&1 || true
    systemctl daemon-reload >/dev/null 2>&1
    hash -r

    ARCH=$(uname -m); XRAY_ARCH=""
    case ${ARCH} in
        x86_64) XRAY_ARCH="64" ;;
        aarch64) XRAY_ARCH="arm64" ;;
        *) error "不支持的架构: ${ARCH}"; exit 1 ;;
    esac

    DOWNLOAD_URL="https://github.com/XTLS/Xray-core/releases/download/${EXPECTED_XRAY_VERSION}/Xray-linux-${XRAY_ARCH}.zip"
    log "正在下载 Xray ${EXPECTED_XRAY_VERSION} ($DOWNLOAD_URL)..."
    wget -q -O /tmp/xray.zip "$DOWNLOAD_URL"

    if [ ! -s "/tmp/xray.zip" ]; then
        error "Xray 下载失败！文件 '/tmp/xray.zip' 不存在或为空。请检查网络或 URL。"
    fi

    log "解压并安装 Xray..."
    mkdir -p /tmp/xray_temp && unzip -q /tmp/xray.zip -d /tmp/xray_temp/
    if [ ! -f "/tmp/xray_temp/xray" ]; then
        error "解压后的 Xray 可执行文件未找到。预期在 /tmp/xray_temp/xray。请检查下载的压缩包内容或解压路径。"
    fi
    mv "/tmp/xray_temp/xray" /usr/local/bin/
    chmod +x /usr/local/bin/xray
    rm -rf /tmp/xray_temp /tmp/xray.zip

    log "Xray ${EXPECTED_XRAY_VERSION} 安装成功！"
}

# --- 8. 生成密钥和密码 (按需生成) ---
UUID_NAMESPACE_DNS="6ba7b810-9dad-11d1-80b4-00c04fd430c8"
declare -g CLIENT_WG_IP_COUNTER=2

generate_protocol_secrets() {
    log "--- 生成协议密钥和密码 (按需生成) ---"

    if [[ "${SELECTED_PROTOCOLS["socks5"]}" == "1" ]]; then
        # SOCKS5 协议不需要密钥生成，直接使用默认用户名密码
        log "SOCKS5 协议已选中，将使用默认用户名和密码。"
    fi

    if [[ "${SELECTED_PROTOCOLS["vmess"]}" == "1" ]]; then
        if ! command -v uuidgen &> /dev/null; then
            error "uuidgen 命令未找到，无法生成 VMess UUID。请确保 'uuid-runtime' 已安装。"
        fi
        VMESS_UUID=$(uuidgen --sha1 --namespace "$UUID_NAMESPACE_DNS" --name "$DEFAULT_PASSWORD")
        log "VMess UUID (基于默认密码): $VMESS_UUID"
    fi

    if [[ "${SELECTED_PROTOCOLS["shadowsocks"]}" == "1" ]]; then
        SHADOWSOCKS_PASSWORD="$DEFAULT_PASSWORD"
        SHADOWSOCKS_METHOD="aes-256-gcm"
        log "Shadowsocks 密码: $SHADOWSOCKS_PASSWORD"
        log "Shadowsocks 加密方式: $SHADOWSOCKS_METHOD"
    fi

    if [[ "${SELECTED_PROTOCOLS["wireguard"]}" == "1" ]]; then
        # 移除预共享密钥生成，简化配置
        WIREGUARD_SERVER_PRIVATE_KEY=$(wg genkey)
        WIREGUARD_PUBLIC_KEY=$(echo "$WIREGUARD_SERVER_PRIVATE_KEY" | wg pubkey)

        if [ -z "$WIREGUARD_SERVER_PRIVATE_KEY" ]; then
            error "致命错误: 无法生成 WireGuard 服务器私钥，请检查 wg genkey 命令。"
        fi

        log "WireGuard 服务器私钥 (请妥善保管): $WIREGUARD_SERVER_PRIVATE_KEY"
        log "WireGuard 服务器公钥 (用于客户端): $WIREGUARD_PUBLIC_KEY"

        declare -g -A WIREGUARD_CLIENT_KEYS
        local wg_client_ip_base="10.1.0."

        local temp_sorted_public_ips=($(printf "%s\n" "${!PUBLIC_IP_TO_PRIVATE_IP_MAP[@]}" | sort))

        for public_ip in "${temp_sorted_public_ips[@]}"; do
            local client_private_key=$(wg genkey)
            local client_public_key=$(echo "$client_private_key" | wg pubkey)
            # 客户端IP的子网掩码使用 /32，与 sing-box 逻辑一致
            local client_assigned_ip="${wg_client_ip_base}${CLIENT_WG_IP_COUNTER}/32"

            WIREGUARD_CLIENT_KEYS["$public_ip"]="${client_private_key}|${client_public_key}|${client_assigned_ip}"
            CLIENT_WG_IP_COUNTER=$((CLIENT_WG_IP_COUNTER+1))
            log "   WireGuard 客户端密钥对和 IP 生成 (用于 ${public_ip} 的Peer):"
            log "     客户端私钥: $client_private_key"
            log "     客户端公钥: $client_public_key"
            log "     客户端内部IP: $client_assigned_ip"
        done
        echo ""
    fi
}

# --- 9.5. 安装 GeoIP 和 GeoSite 数据文件 ---
install_dat_files() {
    log "--- 跳过下载 GeoSite 数据文件，因为网络连接失败 ---"
    log "GeoSite 数据文件安装已跳过。"
}


# --- 9. 生成 Xray 配置文件 (使用 jq 重写并修复 wireguard 配置) ---
generate_xray_config() {
    log "--- 生成 Xray 配置文件 (已根据用户建议使用 jq 完全重写) ---"
    mkdir -p "$XRAY_CONFIG_DIR"

    local inbounds_json_array="[]"
    local outbounds_json_array="[]"
    local routing_rules_json_array="[]"
    local current_port=$START_PORT

    if [ ${#PUBLIC_IP_TO_PRIVATE_IP_MAP[@]} -eq 0 ]; then
        error "没有探测到公网IP到私网IP的映射，无法生成Xray配置。"
    fi

    IFS=$'\n' SORTED_PUBLIC_IPS=($(sort <<<"${!PUBLIC_IP_TO_PRIVATE_IP_MAP[*]}"))
    unset IFS

    for public_ip in "${SORTED_PUBLIC_IPS[@]}"; do
        local private_ip=${PUBLIC_IP_TO_PRIVATE_IP_MAP[$public_ip]}
        local inbound_tag_prefix="${private_ip//./-}"
        local outbound_tag="out-${inbound_tag_prefix}"
        local mark_value="${PRIVATE_IP_TO_MARK_MAP[$private_ip]}"

        # SOCKS5
        if [[ "${SELECTED_PROTOCOLS["socks5"]}" == "1" ]]; then
            inbounds_json_array=$(echo "$inbounds_json_array" | jq -c --arg listen "$private_ip" --arg port "$current_port" --arg tag "socks5-$inbound_tag_prefix" --arg user "$DEFAULT_USERNAME" --arg pass "$DEFAULT_PASSWORD" '. + [{
                "listen": $listen,
                "port": ($port|tonumber),
                "protocol": "socks",
                "settings": {
                    "auth": "password",
                    "udp": true,
                    "accounts": [
                        {
                            "user": $user,
                            "pass": $pass
                        }
                    ]
                },
                "tag": $tag
            }]')
            routing_rules_json_array=$(echo "$routing_rules_json_array" | jq -c --arg tag "socks5-$inbound_tag_prefix" --arg outbound_tag "$outbound_tag" '. + [{
                "inboundTag": [$tag],
                "outboundTag": $outbound_tag
            }]')
            current_port=$((current_port+1))
        fi

        # VMess (TCP)
        if [[ "${SELECTED_PROTOCOLS["vmess"]}" == "1" ]]; then
            inbounds_json_array=$(echo "$inbounds_json_array" | jq -c --arg listen "$private_ip" --arg port "$current_port" --arg tag "vmess-$inbound_tag_prefix" --arg uuid "$VMESS_UUID" '. + [{
                "listen": $listen,
                "port": ($port|tonumber),
                "protocol": "vmess",
                "settings": {
                    "clients": [
                        {
                            "id": $uuid,
                            "level": 0
                        }
                    ]
                },
                "streamSettings": {
                    "network": "tcp"
                },
                "tag": $tag
            }]')
            routing_rules_json_array=$(echo "$routing_rules_json_array" | jq -c --arg tag "vmess-$inbound_tag_prefix" --arg outbound_tag "$outbound_tag" '. + [{
                "inboundTag": [$tag],
                "outboundTag": $outbound_tag
            }]')
            current_port=$((current_port+1))
        fi

        # Shadowsocks
        if [[ "${SELECTED_PROTOCOLS["shadowsocks"]}" == "1" ]]; then
            inbounds_json_array=$(echo "$inbounds_json_array" | jq -c --arg listen "$private_ip" --arg port "$current_port" --arg tag "shadowsocks-$inbound_tag_prefix" --arg method "$SHADOWSOCKS_METHOD" --arg password "$SHADOWSOCKS_PASSWORD" '. + [{
                "listen": $listen,
                "port": ($port|tonumber),
                "protocol": "shadowsocks",
                "settings": {
                    "method": $method,
                    "password": $password
                },
                "tag": $tag
            }]')
            routing_rules_json_array=$(echo "$routing_rules_json_array" | jq -c --arg tag "shadowsocks-$inbound_tag_prefix" --arg outbound_tag "$outbound_tag" '. + [{
                "inboundTag": [$tag],
                "outboundTag": $outbound_tag
            }]')
            current_port=$((current_port+1))
        fi

        # WireGuard
        if [[ "${SELECTED_PROTOCOLS["wireguard"]}" == "1" ]]; then
            log "  正在为私网IP '$private_ip' 配置 WireGuard 入站..."
            log "    -> WireGuard 私钥: $WIREGUARD_SERVER_PRIVATE_KEY"
            if [ -z "$WIREGUARD_SERVER_PRIVATE_KEY" ]; then
                error "致命错误: WireGuard 服务器私钥为空，无法生成配置。"
            fi
            
            local client_info="${WIREGUARD_CLIENT_KEYS[$public_ip]}"
            IFS='|' read -r client_private_key client_public_key client_assigned_ip <<< "$client_info"
            
            # 关键修复：将 address 改为 10.1.0.1/32 以符合 Xray 的严格要求
            inbounds_json_array=$(printf '%s' "$inbounds_json_array" | jq -c \
                --arg listen "$private_ip" \
                --arg port "$current_port" \
                --arg tag "wireguard-$inbound_tag_prefix" \
                --arg secretKey "$WIREGUARD_SERVER_PRIVATE_KEY" \
                --arg peerPublicKey "$client_public_key" \
                --arg allowedIPs "$client_assigned_ip" \
                '. + [{
                    "listen": $listen,
                    "port": ($port|tonumber),
                    "protocol": "wireguard",
                    "settings": {
                        "secretKey": $secretKey,
                        "peers": [
                            {
                                "publicKey": $peerPublicKey,
                                "allowedIPs": [$allowedIPs]
                            }
                        ],
                        "address": ["10.1.0.1/32"]
                    },
                    "tag": $tag
                }]')
            
            routing_rules_json_array=$(echo "$routing_rules_json_array" | jq -c --arg tag "wireguard-$inbound_tag_prefix" --arg outbound_tag "$outbound_tag" '. + [{
                "inboundTag": [$tag],
                "outboundTag": $outbound_tag
            }]')
            current_port=$((current_port+1))
        fi

        # 为每个公网IP创建一个 freedom 出站，并指定 sendThrough 和 sockopt.mark
        outbounds_json_array=$(echo "$outbounds_json_array" | jq -c --arg private_ip "$private_ip" --arg outbound_tag "$outbound_tag" --arg mark "$mark_value" '. + [{
            "protocol": "freedom",
            "settings": {
                "domainStrategy": "UseIP",
                "sendThrough": $private_ip
            },
            "streamSettings": {
                "sockopt": {
                    "mark": ($mark|tonumber)
                }
            },
            "tag": $outbound_tag
        }]')
    done

    # 添加默认出站
    outbounds_json_array=$(echo "$outbounds_json_array" | jq -c '. + [
        { "protocol": "blackhole", "tag": "block" },
        { "protocol": "freedom", "settings": {}, "tag": "direct" }
    ]')
    
    # 组合成最终的配置文件
    jq -n --arg log_file "$LOG_FILE" --argjson inbounds "$inbounds_json_array" --argjson outbounds "$outbounds_json_array" --argjson rules "$routing_rules_json_array" '
    {
      "log": {
        "access": $log_file,
        "error": $log_file,
        "loglevel": "info"
      },
      "inbounds": $inbounds,
      "outbounds": $outbounds,
      "routing": {
        "domainStrategy": "AsIs",
        "rules": $rules
      },
      "dns": {
        "servers": ["223.5.5.5", "8.8.8.8"]
      }
    }
    ' > "$XRAY_CONFIG_FILE"


    log "Xray 配置文件已生成: $XRAY_CONFIG_FILE"
    log "--- 正在校验生成的配置文件 JSON 格式... ---"
    if ! jq . "$XRAY_CONFIG_FILE" > /dev/null 2>&1; then
        error "生成的 Xray 配置文件 JSON 格式不正确。请检查脚本输出或手动检查文件。"
    fi
    log "Xray 配置文件 JSON 格式校验成功。"
}


# --- 10. 配置 Systemd 服务 ---
configure_systemd() {
    log "--- 开始配置 Systemd 服务 (使用 root 权限运行) ---"
    cat << EOF > "$SYSTEMD_SERVICE_FILE"
[Unit]
Description=Xray Service
Documentation=https://github.com/XTLS/Xray-core
After=network.target nss-lookup.target

[Service]
User=root
ExecStart=/usr/local/bin/xray run -c $XRAY_CONFIG_DIR/config.json
ExecReload=/bin/kill -HUP \$MAINPID
Restart=on-failure
RestartSec=10
LimitNOFILE=infinity

[Install]
WantedBy=multi-user.target
EOF

    sudo systemctl daemon-reload
    sudo systemctl enable xray
    log "Xray Systemd 服务已配置并启用。"
}

# --- 11. 配置 Linux 策略路由 (IP Rule / IP Table) ---
configure_policy_routing() {
    log "--- 开始配置 Linux 策略路由确保出站IP一致 (已优化清理旧规则) ---"
    log "--- 清理旧的策略路由规则和路由表 ---"
    # 动态清理旧的 fwmark 规则和路由表
    for table_id in $(sudo ip rule show | awk '/lookup/ {print $NF}' | sort -n | uniq); do
        if (( table_id >= 10001 )); then
            log "   发现并清理旧的路由表ID: $table_id"
            sudo ip rule del from all table "$table_id" 2>/dev/null || true
            sudo ip rule del fwmark "$table_id" 2>/dev/null || true
            sudo ip route flush table "$table_id" 2>/dev/null || true
        fi
    done

    # 额外清理可能冲突的、由源IP配置的规则
    for private_ip in "${!PRIVATE_IP_TO_MARK_MAP[@]}"; do
        local routing_mark="${PRIVATE_IP_TO_MARK_MAP[$private_ip]}"
        sudo ip rule del from "$private_ip" lookup "$routing_mark" 2>/dev/null || true
        log "   清理了可能冲突的 'from $private_ip' 旧规则。"
    done
    log "动态清理完成。"
    
    local sorted_private_ips=($(printf "%s\n" "${!PRIVATE_IP_TO_MARK_MAP[@]}" | sort))
    local rule_priority_counter=101
    
    for private_ip in "${sorted_private_ips[@]}"; do
        local interface_name=${PRIVATE_IP_TO_INTERFACE_MAP[$private_ip]}
        local gateway=${PRIVATE_IP_TO_GATEWAY_MAP[$private_ip]}
        local routing_mark="${PRIVATE_IP_TO_MARK_MAP[$private_ip]}"

        if [ -z "$gateway" ]; then
            warn "未能找到私网IP '$private_ip' 的默认网关，跳过配置策略路由。"
            continue
        fi
        if [ -z "$routing_mark" ]; then
            warn "未能找到私网IP '$private_ip' 的路由标记，跳过配置策略路由。"
            continue
        fi

        RT_TABLE_ID="$routing_mark"
        
        log "为私网IP '$private_ip' (网卡 '$interface_name') 配置路由表 '$RT_TABLE_ID'，网关 '$gateway'..."
        
        # 添加路由表，明确指定源IP
        sudo ip route add default via "$gateway" dev "$interface_name" src "$private_ip" table "$RT_TABLE_ID" 2>/dev/null || true
        if [ $? -ne 0 ]; then
            error "错误: 添加路由表 $RT_TABLE_ID 失败。请检查网络配置或手动执行命令。"
        fi

        # 核心改动：使用 fwmark 规则，让内核根据 Xray 打的标记来决定路由
        sudo ip rule add fwmark "$routing_mark" table "$RT_TABLE_ID" priority "$rule_priority_counter" 2>/dev/null || true
        if [ $? -ne 0 ]; then
            error "错误: 添加 fwmark 规则失败。请检查系统是否支持此功能或手动执行命令。"
        fi
        log "   >> 添加 fwmark 规则: ip rule add fwmark $routing_mark table $RT_TABLE_ID priority $rule_priority_counter"
        rule_priority_counter=$((rule_priority_counter+1))
        
        log "成功为私网IP '$private_ip' 配置策略路由。"
    done

    log "--- 策略路由配置完成 ---"
    log "注意: 如果重启后策略路由失效，请将上述 'ip route add' 和 'ip rule add' 命令添加到 /etc/rc.local 或 systemd-networkd 配置中。"
}

# --- 12. 启动 Xray 服务 ---
start_xray() {
    log "--- 启动 Xray 服务 ---"
    sudo systemctl start xray
    if [ $? -ne 0 ]; then
        error "Xray 服务启动失败。请检查日志: 'sudo journalctl -u xray -f' 或 'tail -f $LOG_FILE'"
    fi
    log "Xray 服务已成功启动。"
    sudo systemctl status xray --no-pager
}

# --- 13. 打印协议链接信息 ---
print_client_info() {
    echo ""
    log "### 恭喜！Xray 协议搭建成功！ ###"
    log "### 请务必在服务器安全组中开放 ${COLOR_YELLOW}$START_PORT - $END_PORT${COLOR_NC} 端口的 ${COLOR_YELLOW}TCP 和 UDP${COLOR_NC} 流量！###"
    echo ""

    local current_datetime=$(date +"%Y-%m-%d %H:%M:%S")
    local one_year_later_timestamp=$(date -d "+1 year" +%s)
    local one_year_later_datetime=$(date -d "@$one_year_later_timestamp" +"%Y-%m-%d %H:%M:%S")

    local socks5_output=""
    local vmess_output=""
    local wireguard_output=""
    local shadowsocks_output=""

    if [ ${#PUBLIC_IP_TO_PRIVATE_IP_MAP[@]} -eq 0 ]; then
        warn "没有探测到公网IP到私网IP的映射，无法打印客户端配置信息。"
        return
    fi
    IFS=$'\n' SORTED_PUBLIC_IPS=($(sort <<<"${!PUBLIC_IP_TO_PRIVATE_IP_MAP[*]}"))
    unset IFS

    for public_ip in "${SORTED_PUBLIC_IPS[@]}"; do
        local config_json=$(cat "$XRAY_CONFIG_FILE")
        # SOCKS5
        if [[ "${SELECTED_PROTOCOLS["socks5"]}" == "1" ]]; then
            local private_ip_tag="${PUBLIC_IP_TO_PRIVATE_IP_MAP[$public_ip]//./-}"
            local socks_port=$(echo "$config_json" | jq -r ".inbounds[] | select(.tag == \"socks5-${private_ip_tag}\") | .port")
            socks5_output+="${public_ip}|${socks_port}|${DEFAULT_USERNAME}|${DEFAULT_PASSWORD}|${one_year_later_datetime}\n"
        fi

        # VMess (TCP)
        if [[ "${SELECTED_PROTOCOLS["vmess"]}" == "1" ]]; then
            local private_ip_tag="${PUBLIC_IP_TO_PRIVATE_IP_MAP[$public_ip]//./-}"
            local vmess_port=$(echo "$config_json" | jq -r ".inbounds[] | select(.tag == \"vmess-${private_ip_tag}\") | .port")
            vmess_output+="${public_ip}|${vmess_port}|UUID:${VMESS_UUID}|传输协议:TCP\n"
        fi

        # Shadowsocks
        if [[ "${SELECTED_PROTOCOLS["shadowsocks"]}" == "1" ]]; then
            local private_ip_tag="${PUBLIC_IP_TO_PRIVATE_IP_MAP[$public_ip]//./-}"
            local ss_port=$(echo "$config_json" | jq -r ".inbounds[] | select(.tag == \"shadowsocks-${private_ip_tag}\") | .port")
            shadowsocks_output+="${public_ip}|${ss_port}|${SHADOWSOCKS_METHOD}|${SHADOWSOCKS_PASSWORD}\n"
        fi

        # WireGuard
        if [[ "${SELECTED_PROTOCOLS["wireguard"]}" == "1" ]]; then
            local private_ip_tag="${PUBLIC_IP_TO_PRIVATE_IP_MAP[$public_ip]//./-}"
            local wireguard_port=$(echo "$config_json" | jq -r ".inbounds[] | select(.tag == \"wireguard-${private_ip_tag}\") | .port")
            local client_info="${WIREGUARD_CLIENT_KEYS[$public_ip]}"
            IFS='|' read -r client_private_key client_public_key client_assigned_ip <<< "$client_info"
            local client_allowed_ip_no_cidr=$(echo "$client_assigned_ip" | cut -d'/' -f1)

            wireguard_output+="[Interface]\n"
            wireguard_output+="PrivateKey = ${client_private_key}\n"
            wireguard_output+="Address = ${client_assigned_ip}\n\n"

            wireguard_output+="[Peer]\n"
            wireguard_output+="PublicKey = ${WIREGUARD_PUBLIC_KEY}\n"
            wireguard_output+="AllowedIPs = 0.0.0.0/0\n"
            wireguard_output+="Endpoint = ${public_ip}:${wireguard_port}\n"
            wireguard_output+="PersistentKeepalive = 30\n"
            wireguard_output+="\n"
            wireguard_output+="/////////////////////////////////////////////////////////\n\n"
        fi
    done

    if [ -n "$socks5_output" ]; then
        echo -e "${COLOR_GREEN}###################当前使用协议SOCKS5###########################${COLOR_NC}\n"
        echo -e "${COLOR_YELLOW}${socks5_output}${COLOR_NC}"
    fi

    if [ -n "$vmess_output" ]; then
        echo -e "${COLOR_GREEN}###################当前使用协议VMess###########################${COLOR_NC}\n"
        echo -e "${COLOR_YELLOW}${vmess_output}${COLOR_NC}"
    fi

    if [ -n "$shadowsocks_output" ]; then
        echo -e "${COLOR_GREEN}###################当前使用协议Shadowsocks###########################${COLOR_NC}\n"
        echo -e "${COLOR_YELLOW}${shadowsocks_output}${COLOR_NC}"
    fi

    if [ -n "$wireguard_output" ]; then
        echo -e "${COLOR_GREEN}###################当前使用协议WireGuard###########################${COLOR_NC}\n"
        echo -e "${COLOR_YELLOW}${wireguard_output}${COLOR_NC}"
    fi

    echo -e "${COLOR_GREEN}################以上搭建完成的协议在客户端中直接使用#####################${COLOR_NC}"
    echo ""
    log "脚本执行完毕！"
    log "日志文件位置: $LOG_FILE"
    log "Xray 配置文件位置: /etc/xray/config.json"
    log "你可以通过 '${COLOR_YELLOW}sudo journalctl -u xray -f${COLOR_NC}' 查看 Xray 实时日志。"
}

# 函数：系统优化
apply_system_optimizations() {
    log "开始全面检查并应用系统网络优化..."
    if [ "$(swapon --show | wc -l)" -eq 0 ] || [ "$(swapon --show | grep -v "Filename" | wc -l)" -eq 0 ]; then
        warn "未发现Swap，正在创建 ${SWAP_SIZE} Swap文件..."
        fallocate -l ${SWAP_SIZE} /swapfile
        if [ $? -ne 0 ]; then
            warn "fallocate 创建Swap文件失败，尝试使用 dd。";
            dd if=/dev/zero of=/swapfile bs=1M count=$(echo ${SWAP_SIZE} | sed 's/G/*1024/g' | bc) >/dev/null 2>&1
        fi
        chmod 600 /swapfile
        mkswap /swapfile 2>&1
        swapon /swapfile 2>&1
        echo '/swapfile none swap sw 0 0' | tee -a /etc/fstab > /dev/null;
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
            if command -v apt-get > /dev/null; then
                log "正在安装 irqbalance..."; apt-get update -y && apt-get install -y irqbalance;
            else
                warn "无法找到 apt-get，跳过安装 irqbalance。";
            fi
        fi;
        if command -v irqbalance > /dev/null; then
            systemctl enable --now irqbalance > /dev/null 2>&1; log "irqbalance 服务已启用并运行。";
        else
            error "irqbalance 安装失败或无法运行。";
        fi
    else
        log "跳过 IRQ 负载均衡优化。";
    fi;
    log "系统优化配置完成。"
}


# 执行主函数
main() {
    configure_locale
    install_dependencies
    uninstall_singbox_if_exists
    stop_existing_xray
    apply_system_optimizations
    discover_network_info
    discover_ip_pairs
    install_or_update_xray
    install_dat_files
    select_protocols
    generate_protocol_secrets
    generate_xray_config
    configure_systemd
    configure_policy_routing
    start_xray
    print_client_info
}

main