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
# irqbalance 尝试优化多核CPU上的中断请求分配，对于高并发网络服务可能有益。
# 默认设置为 no，在系统资源有限或不清楚影响时，可手动设置为 yes。
ENABLE_IRQ_BALANCE="no" # 可以手动设置为 "yes" 来启用

# --- 日志和文件路径配置 ---
LOG_DIR="/var/log/sing-box"
LOG_FILE="$LOG_DIR/sing-box.log"
SINGBOX_CONFIG_DIR="/etc/sing-box"
SINGBOX_CONFIG_FILE="$SINGBOX_CONFIG_DIR/config.json"
SYSTEMD_SERVICE_FILE="/etc/systemd/system/sing-box.service"

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
    echo -e "${COLOR_YELLOW}$(date +"%Y-%m-%d %H:%M:%S") [警告] $@${COLOR_NC}" | tee -a "$LOG_FILE"
}

error() {
    echo -e "${COLOR_RED}$(date +"%Y-%m-%d %H:%M:%S") [错误] $@${COLOR_NC}" | tee -a "$LOG_FILE" >&2
    exit 1
}

echo "### Sing-box 自动化部署脚本 (版本 1.0) ###"
log "脚本开始执行..."
echo ""

# --- 辅助函数：版本号比较 ---
# usage: version_gt "v1.12.0-beta.26" "v1.11.14" -> returns 0 (true)
#        version_gt "1.11.14" "1.12.0" -> returns 1 (false)
#        version_gt "v1.12.0" "v1.12.0-beta.1" -> returns 0 (true), assuming stable > beta
version_gt() {
    # 清理版本号中的 'v' 前缀
    local v1=$(echo "$1" | sed 's/^v//')
    local v2=$(echo "$2" | sed 's/^v//')

    # 将版本号分割成数组，并处理beta/rc后缀
    local IFS=.
    local a=($v1)
    local b=($v2)
    unset IFS

    for ((i=0; i<${#a[@]} || i<${#b[@]}; i++)); do
        local n1=$(echo "${a[i]}" | sed 's/[^0-9].*//') # 只取数字部分
        local s1=$(echo "${a[i]}" | sed 's/^[0-9]*//') # 只取后缀部分
        local n2=$(echo "${b[i]}" | sed 's/[^0-9].*//') # 只取数字部分
        local s2=$(echo "${b[i]}" | sed 's/^[0-9]*//') # 只取后缀部分

        n1=${n1:-0} # 如果是空，赋0
        n2=${n2:-0} # 如果是空，赋0

        if (( 10#$n1 > 10#$n2 )); then return 0; fi
        if (( 10#$n1 < 10#$n2 )); then return 1; fi

        # 如果数字部分相同，比较字母后缀
        # 约定: "" (release) > "rc" > "beta" > "alpha"
        local order_rank=( "alpha" "beta" "rc" "" )
        local rank1=-1
        local rank2=-1
        for k in "${!order_rank[@]}"; do
            if [[ "${order_rank[k]}" == "$s1" ]]; then rank1=$k; fi
            if [[ "${order_rank[k]}" == "$s2" ]]; then rank2=$k; fi
        done

        if (( rank1 > rank2 )); then return 0; fi
        if (( rank1 < rank2 )); then return 1; fi
    done
    return 1 # 版本相同或无法区分，认为不大于
}


# --- 设置语言环境 ---
configure_locale() {
    log "--- 配置语言环境 ---"
    # 临时设置，用于当前脚本执行
    export LANG="zh_CN.UTF-8"
    export LC_ALL="zh_CN.UTF-8"
    # 尝试安装并生成语言环境
    sudo apt update -y &>/dev/null
    sudo apt install -y locales &>/dev/null
    if ! grep -q "zh_CN.UTF-8 UTF-8" /etc/locale.gen; then
        echo "zh_CN.UTF-8 UTF-8" | sudo tee -a /etc/locale.gen > /dev/null
    fi
    sudo locale-gen zh_CN.UTF-8 &>/dev/null
    sudo update-locale LANG=zh_CN.UTF-8 LC_ALL=zh_CN.UTF-8 &>/dev/null
    log "语言环境配置完成。如果仍有locale警告，请手动检查系统设置或重启SSH会话。"
}

# --- 协议选择菜单 (移到主函数末尾调用) ---
select_protocols() {
    echo -e "${COLOR_BLUE}--- 请选择要搭建的协议 ---${COLOR_NC}"
    echo "1) SOCKS5"
    echo "2) VMess"
    echo "3) TUIC"
    echo "4) WireGuard"
    # 移除 "5) 全部协议"
    echo -e "${COLOR_BLUE}--------------------------${COLOR_NC}"
    read -p "请输入你的选择 (例如: 1 3 4): " choices

    local selection_made=false
    # 遍历所有可能的选择，并检查用户输入
    for choice in $choices; do
        case "$choice" in
            "1") SELECTED_PROTOCOLS["socks5"]=1; selection_made=true ;;
            "2") SELECTED_PROTOCOLS["vmess"]=1; selection_made=true ;;
            "3") SELECTED_PROTOCOLS["tuic"]=1; selection_made=true ;;
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
DEFAULT_GATEWAY="" # 默认网关 (如果只有一个)

discover_network_info() {
    log "正在探测网络接口和路由信息..."
    local all_private_ips_detailed=$(ip -4 address show | awk '/inet / {print $2, $NF}' | grep -v '127.0.0.1' | cut -d'/' -f1,2) # 获取 IP/掩码 和 网卡名

    # 填充 PRIVATE_IP_TO_INTERFACE_MAP
    while read -r ip_cidr iface; do
        local private_ip=$(echo "$ip_cidr" | cut -d'/' -f1)
        PRIVATE_IP_TO_INTERFACE_MAP["$private_ip"]="$iface"
        log " > 发现私网IP: '$private_ip' 绑定到网卡: '$iface'"
    done <<< "$all_private_ips_detailed"

    if [ ${#PRIVATE_IP_TO_INTERFACE_MAP[@]} == 0 ]; then
        error "未能发现任何有效的私网IP配置。请检查网络设置。"
    fi

    # 填充 INTERFACE_TO_DEFAULT_GATEWAY_MAP 和 DEFAULT_GATEWAY
    # 尝试解析每个网卡的默认路由
    local default_routes=$(ip route show default | awk '{print $3, $5}')
    if [ -z "$default_routes" ]; then
        error "未能发现任何默认路由。请检查网络配置。"
    fi

    local gateway_count=0
    while read -r gw_ip iface; do
        INTERFACE_TO_DEFAULT_GATEWAY_MAP["$iface"]="$gw_ip"
        if [ -z "$DEFAULT_GATEWAY" ]; then
            DEFAULT_GATEWAY="$gw_ip"
        elif [ "$DEFAULT_GATEWAY" != "$gw_ip" ]; then
            warn "发现多个不同的默认网关 ($DEFAULT_GATEWAY 和 $gw_ip)。策略路由可能需要更精细调整。"
            DEFAULT_GATEWAY="$gw_ip" # 取最后一个作为主要默认网关，但策略路由会处理每个网卡自己的网关
        fi
        log " > 网卡 '$iface' 的默认网关: '$gw_ip'"
        gateway_count=$((gateway_count+1))
    done <<< "$default_routes"

    if [ "$gateway_count" -gt 1 ]; then
        warn "检测到多个网卡都有默认路由，策略路由将基于每个网卡对应的网关进行配置。"
    elif [ -z "$DEFAULT_GATEWAY" ]; then
        error "未能成功探测到默认网关。请手动检查路由表。"
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

    local current_mark=10001 # 路由表ID起始从10001开始
    for private_ip in "${private_ips_to_check[@]}"; do
        local discovered_public_ip=""
        local attempts=3
        while [ "$attempts" -gt 0 ]; do
            # 尝试从该私网IP作为源地址，探测外部公网IP
            log "   尝试从私网IP '$private_ip' 探测公网IP (剩余尝试: $attempts)..."
            # 增加 -4 确保使用IPv4，避免IPv6问题
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
    # 重新引入 uuid-runtime，用于可靠生成 UUIDv5
    REQUIRED_TOOLS=("curl" "jq" "wget" "tar" "sha1sum" "uuid-runtime") 
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
    
    # 特殊处理 wireguard-tools 的 wg 命令
    if ! command -v wg &> /dev/null; then
        log "工具 'wireguard-tools' (wg command) 未安装，正在尝试安装..."
        sudo apt update -y
        sudo apt install -y wireguard-tools
        if [ $? -ne 0 ]; then
            error "无法安装工具 'wireguard-tools'。请手动安装后重试。"
        else
            log "工具 'wireguard-tools' (wg command) 已安装。"
        fi
    fi
}

# --- 5. 停止并禁用现有 Sing-box 服务 (如果存在) ---
stop_existing_singbox() {
    log "--- 停止并禁用现有 Sing-box 服务 (如果存在) ---"
    if systemctl is-active --quiet sing-box; then
        sudo systemctl stop sing-box
        log "已停止 sing-box 服务。"
    fi
    if systemctl is-enabled --quiet sing-box; then
        sudo systemctl disable sing-box
        log "已禁用 sing-box 服务。"
    fi
    
    # 关键修正：重新加载daemon，确保systemd取消挂起的重启job
    sudo systemctl daemon-reload >/dev/null 2>&1
    log "Systemd daemon 已重新加载，确保 Sing-box 不会自动重启。"

    # 清理旧的配置文件和日志
    if [ -d "$SINGBOX_CONFIG_DIR" ]; then
        log "清理旧的 Sing-box 配置目录: $SINGBOX_CONFIG_DIR"
        sudo rm -rf "$SINGBOX_CONFIG_DIR"
    fi

    # 清空 Sing-box 服务的 journalctl 日志
    log "清空 Sing-box 服务的 journalctl 日志..."
    sudo journalctl --rotate # 轮换日志文件
    # 这里尝试了一个相对温和的方法，确保仅删除 sing-box 服务的历史日志
    sudo journalctl --vacuum-time=1s --unit sing-box.service >/dev/null 2>&1 || true
    log "Sing-box 服务历史日志已清空 (可能不立即释放磁盘空间)。"
}

# --- 6. 安装/更新 Sing-box 核心 ---
install_or_update_singbox() {
    log "正在安装/更新 sing-box 核心...";

    local github_releases_api="https://api.github.com/repos/SagerNet/sing-box/releases"
    local EXPECTED_SB_VERSION=""

    # 尝试获取最新预发布版本
    EXPECTED_SB_VERSION=$(curl -s "$github_releases_api" | jq -r '.[] | select(.prerelease == true) | .tag_name' | head -n 1)
    
    if [ -z "$EXPECTED_SB_VERSION" ]; then
        log "未能获取到最新的预发布版本，尝试获取最新的稳定版本..."
        # 尝试获取最新稳定版本
        EXPECTED_SB_VERSION=$(curl -s "$github_releases_api/latest" | jq -r '.tag_name')
    fi

    if [ -z "$EXPECTED_SB_VERSION" ]; then
        error "获取sing-box最新版本号失败！请检查网络连接到 api.github.com。"
    fi
    log "检测到 sing-box 最新版本为: $EXPECTED_SB_VERSION (可能包含预发布版本)"

    local CURRENT_SB_VERSION=""
    if [ -x "/usr/local/bin/sing-box" ]; then # 检查文件是否存在且可执行
        CURRENT_SB_VERSION=$(/usr/local/bin/sing-box version 2>&1 | awk '
            /sing-box\// {
                split($0, a, "/");
                gsub(/^v/, "", a[2]);
                print a[2];
                exit;
            }
            /sing-box-cli version/ {
                gsub(/^v/, "", $NF);
                print $NF;
                exit;
            }' | head -n 1)
    fi

    local PERFORM_INSTALL="yes" # 默认执行安装
    if [ -n "$CURRENT_SB_VERSION" ]; then
        log "当前已安装 sing-box 版本: v$CURRENT_SB_VERSION"
        # 移除 EXPECTED_SB_VERSION 中的 'v' 前缀进行比较
        local EXPECTED_SB_VERSION_CLEAN=$(echo "$EXPECTED_SB_VERSION" | sed 's/^v//')
        
        if version_gt "$EXPECTED_SB_VERSION_CLEAN" "$CURRENT_SB_VERSION"; then # 如果期望版本 > 当前版本，则升级
            warn "检测到 sing-box 版本落后 (当前 v$CURRENT_SB_VERSION vs 最新 $EXPECTED_SB_VERSION)。正在清理旧版本并安装最新版..."
        else
            log "sing-box v$CURRENT_SB_VERSION 已安装且为最新或更高版本，跳过安装。"
            PERFORM_INSTALL="no"
        fi
    else
        warn "检测到 sing-box 未安装，正在安装最新版 ($EXPECTED_SB_VERSION)..."
    fi

    if [ "$PERFORM_INSTALL" = "no" ]; then
        return 0 # 跳过安装
    fi

    # 执行清理和安装
    systemctl stop sing-box.service >/dev/null 2>&1 || true
    systemctl disable sing-box.service >/dev/null 2>&1 || true
    rm -f /usr/local/bin/sing-box # 删除可执行文件
    rm -f /etc/systemd/system/sing-box.service # 删除服务文件
    rm -rf /etc/sing-box # 清理旧的配置文件目录
    pkill -f sing-box >/dev/null 2>&1 || true # 杀死所有 sing-box 进程
    systemctl daemon-reload >/dev/null 2>&1 # 重新加载 systemd 配置
    hash -r # 强制刷新shell的hash表，确保执行的是新安装的sing-box

    ARCH=$(uname -m);
    BOX_ARCH=""
    case ${ARCH} in
        x86_64) BOX_ARCH="amd64" ;;
        aarch64) BOX_ARCH="arm64" ;;
        *) error "不支持的架构: ${ARCH}"; exit 1 ;;
    esac 
    
    # 修正下载 URL 的文件名，适配预发布版本可能带 beta/rc 等后缀
    local DOWNLOAD_FILE_VERSION=$(echo "$EXPECTED_SB_VERSION" | sed 's/^v//') # 买 'v'
    DOWNLOAD_URL="https://github.com/SagerNet/sing-box/releases/download/${EXPECTED_SB_VERSION}/sing-box-${DOWNLOAD_FILE_VERSION}-linux-${BOX_ARCH}.tar.gz"
    log "正在下载 sing-box ${EXPECTED_SB_VERSION} ($DOWNLOAD_URL)...";
    wget -q -O /tmp/sing-box.tar.gz "$DOWNLOAD_URL"
    
    if [ ! -s "/tmp/sing-box.tar.gz" ]; then
        error "sing-box 下载失败！文件 '/tmp/sing-box.tar.gz' 不存在或为空。请检查网络或 URL。"
    fi
    
    log "解压并安装 sing-box...";
    mkdir -p /tmp/sing-box_temp && tar -zxvf /tmp/sing-box.tar.gz -C /tmp/sing-box_temp/
    local EXTRACTED_DIR_NAME=$(tar -tf /tmp/sing-box.tar.gz | head -n 1 | cut -d/ -f1) # 动态获取解压后的目录名
    if [ ! -f "/tmp/sing-box_temp/${EXTRACTED_DIR_NAME}/sing-box" ]; then
        error "解压后的 sing-box 可执行文件未找到。预期在 /tmp/sing-box_temp/${EXTRACTED_DIR_NAME}/sing-box。请检查下载的压缩包内容或解压路径。"
    fi
    mv "/tmp/sing-box_temp/${EXTRACTED_DIR_NAME}/sing-box" /usr/local/bin/
    chmod +x /usr/local/bin/sing-box
    rm -rf /tmp/sing-box_temp /tmp/sing-box.tar.gz
    
    log "sing-box ${EXPECTED_SB_VERSION} 安装成功！"
}

# --- 7. 生成密钥和密码 (按需生成) ---
# UUID Namespace for DNS (fixed) - 用于生成确定性 UUIDv5
UUID_NAMESPACE_DNS="6ba7b810-9dad-11d1-80b4-00c04fd430c8"

# 客户端 WireGuard IP 地址计数器
declare -g CLIENT_WG_IP_COUNTER=2 # 从 10.1.0.2 开始分配客户端 IP

generate_protocol_secrets() {
    log "--- 生成协议密钥和密码 (按需生成) ---"
    
    # VMess/TUIC UUID (根据 DEFAULT_PASSWORD 生成确定性 UUIDv5)
    if [[ "${SELECTED_PROTOCOLS["vmess"]}" == "1" ]] || [[ "${SELECTED_PROTOCOLS["tuic"]}" == "1" ]]; then
        # 使用 uuidgen 命令来生成 UUIDv5，最可靠的方式
        if ! command -v uuidgen &> /dev/null; then
            error "uuidgen 命令未找到，无法生成 VMess/TUIC UUID。请确保 'uuid-runtime' 已安装。"
        fi
        # 直接使用 uuidgen --sha1 --namespace 和 --name 来生成 UUIDv5
        VMESS_UUID=$(uuidgen --sha1 --namespace "$UUID_NAMESPACE_DNS" --name "$DEFAULT_PASSWORD")
        
        log "VMess/TUIC UUID (基于默认密码): $VMESS_UUID"
    fi

    # TUIC 密码 (仍然使用 DEFAULT_PASSWORD)
    if [[ "${SELECTED_PROTOCOLS["tuic"]}" == "1" ]]; then
        TUIC_PASSWORD="$DEFAULT_PASSWORD"
        log "TUIC 密码: $TUIC_PASSWORD"
    fi

    # WireGuard 密钥 (包括客户端自动生成的部分)
    if [[ "${SELECTED_PROTOCOLS["wireguard"]}" == "1" ]]; then
        WIREGUARD_SERVER_PRIVATE_KEY=$(wg genkey)
        WIREGUARD_PUBLIC_KEY=$(echo "$WIREGUARD_SERVER_PRIVATE_KEY" | wg pubkey)
        WIREGUARD_PRESHARED_KEY=$(wg genpsk)
        log "WireGuard 服务器私钥 (请妥善保管): $WIREGUARD_SERVER_PRIVATE_KEY"
        log "WireGuard 服务器公钥 (用于客户端): $WIREGUARD_PUBLIC_KEY"
        log "WireGuard 预共享密钥 (用于客户端): $WIREGUARD_PRESHARED_KEY"

        # 为每个可能的 WireGuard 客户端生成并存储其密钥和IP (为了在服务器配置的 peers 中使用)
        declare -g -A WIREGUARD_CLIENT_KEYS # Map public_ip -> {private_key, public_key, client_ip}
        local wg_client_ip_base="10.1.0." # WireGuard 客户端内部 IP 子网
        
        # 再次排序公网IP，确保客户端IP分配与打印顺序一致
        local temp_sorted_public_ips=($(printf "%s\n" "${!PUBLIC_IP_TO_PRIVATE_IP_MAP[@]}" | sort))
        
        for public_ip in "${temp_sorted_public_ips[@]}"; do
            local client_private_key=$(wg genkey) # 自动生成客户端私钥
            local client_public_key=$(echo "$client_private_key" | wg pubkey) # 自动生成客户端公钥
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

# --- 8. 生成 Sing-box 配置文件 ---
generate_singbox_config() {
    log "--- 生成 Sing-box 配置文件 ---"
    # 确保配置目录在生成文件之前就已经存在
    mkdir -p "$SINGBOX_CONFIG_DIR"

    local current_port=$START_PORT
    local inbounds_config_obj="" # 用于构建inbounds数组的JSON对象字符串
    local outbounds_config_obj="" # 用于构建outbounds数组的JSON对象字符串
    local routes_config_obj="" # 用于构建route rules数组的JSON对象字符串
    local endpoints_config_obj="" # 用于构建endpoints数组的JSON对象字符串
    local selected_protocol_count=0

    # 遍历所有探测到的公网IP和对应的私网IP，为每个组合生成协议配置
    if [ ${#PUBLIC_IP_TO_PRIVATE_IP_MAP[@]} -eq 0 ]; then
        error "没有探测到公网IP到私网IP的映射，无法生成Sing-box配置。"
    fi

    # 排序公网IP，确保端口分配顺序可预测
    IFS=$'\n' SORTED_PUBLIC_IPS=($(sort <<<"${!PUBLIC_IP_TO_PRIVATE_IP_MAP[*]}"))
    unset IFS

    for public_ip in "${SORTED_PUBLIC_IPS[@]}"; do
        local private_ip=${PUBLIC_IP_TO_PRIVATE_IP_MAP[$public_ip]}
        local routing_mark="${PRIVATE_IP_TO_MARK_MAP[$private_ip]}" # 获取对应的路由标记

        # 预估此IP下所有选中协议所需的端口数
        local ports_needed_for_ip=0
        if [[ "${SELECTED_PROTOCOLS["socks5"]}" == "1" ]]; then ports_needed_for_ip=$((ports_needed_for_ip+1)); fi
        if [[ "${SELECTED_PROTOCOLS["vmess"]}" == "1" ]]; then ports_needed_for_ip=$((ports_needed_for_ip+1)); fi
        if [[ "${SELECTED_PROTOCOLS["tuic"]}" == "1" ]]; then ports_needed_for_ip=$((ports_needed_for_ip+1)); fi
        if [[ "${SELECTED_PROTOCOLS["wireguard"]}" == "1" ]]; then ports_needed_for_ip=$((ports_needed_for_ip+1)); fi

        if [ $((current_port + ports_needed_for_ip -1)) -gt "$END_PORT" ]; then # 检查当前IP的所有协议端口是否超出范围
            error "端口分配超出预设范围 $START_PORT-$END_PORT。请增加 END_PORT 值或减少协议数量。"
        fi

        # SOCKS5
        if [[ "${SELECTED_PROTOCOLS["socks5"]}" == "1" ]]; then
            inbounds_config_obj+="{\"type\": \"socks\", \"tag\": \"socks-$public_ip\", \"listen\": \"$private_ip\", \"listen_port\": $current_port, \"users\": [{\"username\": \"$DEFAULT_USERNAME\", \"password\": \"$DEFAULT_PASSWORD\"}], \"routing_mark\": $routing_mark},"
            routes_config_obj+="{\"inbound\": \"socks-$public_ip\", \"outbound\": \"direct-$public_ip\"},"
            current_port=$((current_port+1)) # 递增端口
            selected_protocol_count=$((selected_protocol_count+1))
        fi

        # 检查是否选择 VMess
        if [[ "${SELECTED_PROTOCOLS["vmess"]}" == "1" ]]; then
            # 添加 TLS 配置，使用伪装域名并允许不安全连接（自签证书）
            inbounds_config_obj+="{\"type\": \"vmess\", \"tag\": \"vmess-$public_ip\", \"listen\": \"$private_ip\", \"listen_port\": $current_port, \"users\": [{\"uuid\": \"$VMESS_UUID\", \"alterId\": 0}], \"transport\": {\"type\": \"ws\", \"path\": \"/weiyuegs/\"}, \"tls\": {\"enabled\": true, \"server_name\": \"xiangmuqifei.com\", \"insecure\": true}, \"routing_mark\": $routing_mark},"
            routes_config_obj+="{\"inbound\": \"vmess-$public_ip\", \"outbound\": \"direct-$public_ip\"},"
            current_port=$((current_port+1)) # 递增端口
            selected_protocol_count=$((selected_protocol_count+1))
        fi

        # 检查是否选择 TUIC
        if [[ "${SELECTED_PROTOCOLS["tuic"]}" == "1" ]]; then
            # 关键修正：TUIC 的 TLS 配置，明确指定 min_version, max_version 和 alpn
            inbounds_config_obj+="{\"type\": \"tuic\", \"tag\": \"tuic-$public_ip\", \"listen\": \"$private_ip\", \"listen_port\": $current_port, \"users\": [{\"uuid\": \"$VMESS_UUID\", \"password\": \"$TUIC_PASSWORD\"}], \"congestion_control\": \"bbr\", \"zero_rtt_handshake\": false, \"tls\": {\"enabled\": true, \"server_name\": \"xiangmuqifei.com\", \"insecure\": true, \"min_version\": \"1.3\", \"max_version\": \"1.3\", \"alpn\": [\"h3\"]}, \"routing_mark\": $routing_mark}," # TUIC TLS 版本和ALPN
            routes_config_obj+="{\"inbound\": \"tuic-$public_ip\", \"outbound\": \"direct-$public_ip\"},"
            current_port=$((current_port+1)) # 递增端口
            selected_protocol_count=$((selected_protocol_count+1))
        fi

        # 检查是否选择 WireGuard
        # WireGuard 作为一个 endpoint 定义
        if [[ "${SELECTED_PROTOCOLS["wireguard"]}" == "1" ]]; then
            local client_info="${WIREGUARD_CLIENT_KEYS[$public_ip]}"
            IFS='|' read -r client_private_key client_public_key client_assigned_ip <<< "$client_info"
            
            # 修正：为WireGuard endpoint添加 server 端的内部 address，并添加 pre_shared_key 到 peers
            endpoints_config_obj+="{\"type\": \"wireguard\", \"tag\": \"wireguard-ep-$public_ip\", \"listen_port\": $current_port, \"private_key\": \"$WIREGUARD_SERVER_PRIVATE_KEY\", \"address\": [\"10.1.0.1/24\"], \"peers\": [{\"public_key\": \"$client_public_key\", \"allowed_ips\": [\"$client_assigned_ip\"], \"pre_shared_key\": \"$WIREGUARD_PRESHARED_KEY\"}], \"routing_mark\": $routing_mark},"
            routes_config_obj+="{\"inbound\": \"wireguard-ep-$public_ip\", \"outbound\": \"direct-$public_ip\"},"
            current_port=$((current_port+1)) # 递增端口
            selected_protocol_count=$((selected_protocol_count+1))
        fi

        # 每个公网IP对应一个出站，无论选择了多少协议
        outbounds_config_obj+="{\"type\": \"direct\", \"tag\": \"direct-$public_ip\", \"inet4_bind_address\": \"$private_ip\", \"tcp_fast_open\": true},"
    done

    if [ "$selected_protocol_count" -eq 0 ]; then
        error "未选择任何协议，无法生成Sing-box配置。请重新运行脚本并选择至少一种协议。"
    fi

    # 移除最后一个逗号，并手动添加方括号形成JSON数组
    inbounds_config_final="[${inbounds_config_obj%,}]"
    outbounds_config_final="[${outbounds_config_obj%,}]"
    routes_config_final="[${routes_config_obj%,}]"
    endpoints_config_final="[${endpoints_config_obj%,}]"

    # 如果某个协议没有被选择，导致其片段为空，则确保生成空JSON数组
    if [ "${inbounds_config_final}" == "[]" ]; then inbounds_config_final="[]"; fi
    if [ "${outbounds_config_final}" == "[]" ]; then outbounds_config_final="[]"; fi
    if [ "${routes_config_final}" == "[]" ]; then routes_config_final="[]"; fi
    if [ "${endpoints_config_final}" == "[]" ]; then endpoints_config_final="[]"; fi

    # 组合完整的 Sing-box 配置
    # 使用jq来合并outbounds和routes的额外部分，确保JSON语法正确
    local final_outbounds_array=$(echo "$outbounds_config_final" | jq '. + [{"type": "block", "tag": "block"}, {"type": "direct", "tag": "direct"}]')
    local final_routes_array=$(echo "$routes_config_final" | jq '. + [{"ip_is_private": true, "outbound": "block"}, {"network": "udp", "port": 53, "outbound": "direct"}]') # 还原 DNS 直连规则

    cat << EOF > "$SINGBOX_CONFIG_FILE"
{
  "log": {
    "disabled": false,
    "output": "$LOG_FILE",
    "level": "info",
    "timestamp": true
  },
  "inbounds": $inbounds_config_final,
  "outbounds": $final_outbounds_array,
  "endpoints": $endpoints_config_final,
  "route": {
    "rules": $final_routes_array
  }
}
EOF

    log "Sing-box 配置文件已生成: $SINGBOX_CONFIG_FILE"
    if ! jq . "$SINGBOX_CONFIG_FILE" > /dev/null 2>&1; then
        error "生成的 Sing-box 配置文件 JSON 格式不正确。请检查脚本输出或手动检查文件。`jq` 校验失败，请检查 `/etc/sing-box/config.json` 的内容。"
    fi
    log "Sing-box 配置文件 JSON 格式校验成功。"
}

# --- 9. 配置 Systemd 服务 ---
configure_systemd() {
    log "--- 配置 Systemd 服务 ---"
    cat << EOF > "$SYSTEMD_SERVICE_FILE"
[Unit]
Description=Sing-box Service
Documentation=https://sing-box.sagernet.org
After=network.target nss-lookup.target

[Service]
User=root
# 需要 CAP_NET_ADMIN 和 CAP_NET_RAW 来进行策略路由和原始套接字操作
# CAP_NET_BIND_SERVICE 用于绑定小于1024的端口
CapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_BIND_SERVICE CAP_NET_RAW
AmbientCapabilities=CAP_NET_ADMIN CAP_NET_BIND_SERVICE CAP_NET_RAW
ExecStart=/usr/local/bin/sing-box run -C $SINGBOX_CONFIG_DIR
ExecReload=/bin/kill -HUP \$MAINPID
Restart=on-failure
RestartSec=10
LimitNOFILE=infinity

[Install]
WantedBy=multi-user.target
EOF

    sudo systemctl daemon-reload
    sudo systemctl enable sing-box
    log "Sing-box Systemd 服务已配置并启用。"
}

# --- 10. 配置 Linux 策略路由 (IP Rule / IP Table) ---
configure_policy_routing() {
    log "--- 配置 Linux 策略路由确保出站IP一致 ---"

    # 清理旧的策略路由规则 (动态地，只清理当前将要配置的这些路由表ID)
    log "清理旧的策略路由规则 (动态清理，基于当前探测到的IP数量)..."
    # 收集当前将被使用的路由表ID (新的递增mark)
    local current_rt_ids=()
    for private_ip in "${!PRIVATE_IP_TO_MARK_MAP[@]}"; do
        current_rt_ids+=("${PRIVATE_IP_TO_MARK_MAP[$private_ip]}")
    done

    # 同时也清理上次运行可能产生的旧哈希ID（如果用户没有完全清空服务器或者之前没有彻底清理）
    local all_private_ips_detected=$(ip -4 address show | awk '/inet / {print $2}' | grep -v '127.0.0.1' | cut -d'/' -f1)
    for private_ip_tmp in $all_private_ips_detected; do
        local old_hashed_id=$(echo "$private_ip_tmp" | tr '.' ' ' | awk '{print $1 + $2 + $3 + $4}')
        if [[ ! " ${current_rt_ids[@]} " =~ " ${old_hashed_id} " ]]; then # 如果旧哈希ID不在当前要使用的ID列表中，则尝试清理
            current_rt_ids+=("$old_hashed_id") # 临时添加到清理列表
        fi
    done
    current_rt_ids=($(printf "%s\n" "${current_rt_ids[@]}" | sort -u)) # 确保唯一性并排序

    for rt_id in "${current_rt_ids[@]}"; do
        # 删除所有使用这个表 ID 的规则和路由
        sudo ip rule del fwmark "$rt_id" table "$rt_id" 2>/dev/null || true
        # 清理从该路由表 ID 对应的私网 IP 发出的规则
        for private_ip_tmp in "${!PRIVATE_IP_TO_MARK_MAP[@]}"; do
            # 只有当 PRIVATE_IP_TO_MARK_MAP 中存在这个 IP 且其 Mark 与 rt_id 说对应的私网IP匹配时，才尝试删除其 from 规则
            local expected_mark_for_private_ip="${PRIVATE_IP_TO_MARK_MAP[$private_ip_tmp]}"
            if [ -n "$expected_mark_for_private_ip" ] && [ "$expected_mark_for_private_ip" == "$rt_id" ]; then
                sudo ip rule del from "$private_ip_tmp" table "$rt_id" priority "$((rt_id + 1))" 2>/dev/null || true
            fi
        done
        sudo ip route flush table "$rt_id" 2>/dev/null || true
        log " > 清理路由表 $rt_id 和相关规则。"
    done
    
    # 为每个内网IP创建独立的路由表和规则
    for public_ip in "${!PUBLIC_IP_TO_PRIVATE_IP_MAP[@]}"; do
        local private_ip=${PUBLIC_IP_TO_PRIVATE_IP_MAP[$public_ip]}
        local interface_name=${PRIVATE_IP_TO_INTERFACE_MAP[$private_ip]}
        local gateway=${INTERFACE_TO_DEFAULT_GATEWAY_MAP[$interface_name]}
        local routing_mark="${PRIVATE_IP_TO_MARK_MAP[$private_ip]}" # 获取对应的路由标记

        if [ -z "$gateway" ]; then
            warn "未能找到网卡 '$interface_name' 的默认网关，跳过为私网IP '$private_ip' 配置策略路由。"
            continue
        fi
        if [ -z "$routing_mark" ]; then
            warn "未能找到私网IP '$private_ip' 的路由标记，跳过配置策略路由。"
            continue
        fi

        RT_TABLE_ID="$routing_mark" # 使用 routing_mark 作为路由表ID，更具语义

        log "为私网IP '$private_ip' (公网IP '$public_ip', 网卡 '$interface_name') 配置路由表 '$RT_TABLE_ID'，网关 '$gateway'..."

        # 1. 添加路由表
        sudo ip route add default via "$gateway" dev "$interface_name" table "$RT_TABLE_ID" 2>/dev/null
        if [ $? -ne 0 ]; then
            warn "警告: 添加路由表 $RT_TABLE_ID 失败，可能已存在。此警告通常是正常的，因为脚本动态清理，但不排除更深层问题。"
        fi
        
        # 2. 添加 IP 规则 (fwmark)
        sudo ip rule add fwmark "$RT_TABLE_ID" table "$RT_TABLE_ID" priority "$RT_TABLE_ID" 2>/dev/null
        if [ $? -ne 0 ]; then
            warn "警告: 添加 fwmark 规则 (fwmark $RT_TABLE_ID) 失败，可能已存在。此警告通常是正常的。"
        fi

        # 3. 添加基于源 IP 的规则作为 fallback/补充。
        sudo ip rule add from "$private_ip" table "$RT_TABLE_ID" priority "$((RT_TABLE_ID + 1))" 2>/dev/null
        if [ $? -ne 0 ]; then
            warn "警告: 添加 from 规则 (from $private_ip) 失败，可能已存在。此警告通常是正常的。"
        fi

        # 最终检查是否成功添加了关键规则 (只检查是否至少一条成功，避免过多误报)
        if ! sudo ip rule show | grep -q "from $private_ip lookup $RT_TABLE_ID" && ! sudo ip rule show | grep -q "fwmark $RT_TABLE_ID lookup $RT_TABLE_ID"; then
            warn "警告: 为 '$private_ip' 配置策略路由失败。这可能会导致出口IP不一致。请手动检查并排除故障。"
        else
            log "成功为 '$private_ip' 配置策略路由。"
        fi
    done

    log "--- 策略路由配置完成 ---"
    log "注意: 如果重启后策略路由失效，可能需要将上述 'ip route add' 和 'ip rule add' 命令添加到 /etc/rc.local 或 systemd-networkd 配置中。"
}

# --- 11. 启动 Sing-box 服务 ---
start_singbox() {
    log "--- 启动 Sing-box 服务 ---"
    sudo systemctl start sing-box
    if [ $? -ne 0 ]; then
        error "Sing-box 服务启动失败。请检查日志: 'sudo journalctl -u sing-box -f' 或 'tail -f $LOG_FILE'"
    fi
    log "Sing-box 服务已成功启动。"
    sudo systemctl status sing-box --no-pager
}

# --- 12. 打印协议链接信息 ---
print_client_info() {
    echo ""
    log "### 恭喜！Sing-box 协议搭建成功！ ###"
    log "### 请务必在服务器安全组中开放 ${COLOR_YELLOW}$START_PORT - $END_PORT${COLOR_NC} 端口的 ${COLOR_YELLOW}TCP 和 UDP${COLOR_NC} 流量！###" # 修正提示信息
    echo ""

    # 获取当前日期时间并计算加一年的时间
    local current_datetime=$(date +"%Y-%m-%d %H:%M:%S")
    local one_year_later_timestamp=$(date -d "+1 year" +%s)
    local one_year_later_datetime=$(date -d "@$one_year_later_timestamp" +"%Y-%m-%d %H:%M:%S")

    # 用于汇总所有协议信息，按协议类型分组
    local socks5_output=""
    local vmess_output=""
    local tuic_output=""
    local wireguard_output=""

    # 遍历所有探测到的公网IP，收集协议信息
    if [ ${#PUBLIC_IP_TO_PRIVATE_IP_MAP[@]} -eq 0 ]; then
        warn "没有探测到公网IP到私网IP的映射，无法打印客户端配置信息。"
        return
    fi

    # 排序公网IP，确保输出顺序一致
    IFS=$'\n' SORTED_PUBLIC_IPS=($(sort <<<"${!PUBLIC_IP_TO_PRIVATE_IP_MAP[*]}"))
    unset IFS

    for public_ip in "${SORTED_PUBLIC_IPS[@]}"; do
        # SOCKS5
        if [[ "${SELECTED_PROTOCOLS["socks5"]}" == "1" ]]; then
            local socks_port=$(jq -r ".inbounds[] | select(.tag == \"socks-$public_ip\") | .listen_port" "$SINGBOX_CONFIG_FILE")
            socks5_output+="${public_ip}|${socks_port}|${DEFAULT_USERNAME}|${DEFAULT_PASSWORD}|${one_year_later_datetime}\n"
        fi
        
        # VMess
        if [[ "${SELECTED_PROTOCOLS["vmess"]}" == "1" ]]; then
            local vmess_port=$(jq -r ".inbounds[] | select(.tag == \"vmess-$public_ip\") | .listen_port" "$SINGBOX_CONFIG_FILE")
            local vmess_path="/weiyuegs/" # 固定Path
            local vmess_sni="xiangmuqifei.com" # VMess TLS 伪装域名
            vmess_output+="${public_ip}|${vmess_port}|UUID:${VMESS_UUID}|Path:${vmess_path}|TLS:true|SNI:${vmess_sni}|AllowInsecure:true\n" # 修正格式，包含Path，TLS信息
        fi
        
        # TUIC
        if [[ "${SELECTED_PROTOCOLS["tuic"]}" == "1" ]]; then
            local tuic_port=$(jq -r ".inbounds[] | select(.tag == \"tuic-$public_ip\") | .listen_port" "$SINGBOX_CONFIG_FILE")
            local tuic_sni="xiangmuqifei.com" # 硬编码，用于打印
            tuic_output+="${public_ip}|${tuic_port}|UUID:${VMESS_UUID}|PASSWORD:${TUIC_PASSWORD}|SNI:${tuic_sni}\n" # 修正格式，添加UUID，移除时间，添加SNI
        fi
        
        # WireGuard
        if [[ "${SELECTED_PROTOCOLS["wireguard"]}" == "1" ]]; then
            local wireguard_port=$(jq -r ".endpoints[] | select(.tag == \"wireguard-ep-$public_ip\") | .listen_port" "$SINGBOX_CONFIG_FILE")
            local client_info="${WIREGUARD_CLIENT_KEYS[$public_ip]}"
            IFS='|' read -r client_private_key client_public_key client_assigned_ip <<< "$client_info"

            wireguard_output+="[Interface]\n"
            wireguard_output+="PrivateKey = ${client_private_key}\n" # 自动生成客户端私钥
            wireguard_output+="Address = ${client_assigned_ip}\n\n"   # 自动生成客户端内网IP

            wireguard_output+="[Peer]\n"
            wireguard_output+="PublicKey = ${WIREGUARD_PUBLIC_KEY}\n" # 服务器公钥
            wireguard_output+="PresharedKey = ${WIREGUARD_PRESHARED_KEY}\n" # 打印预共享密钥
            wireguard_output+="AllowedIPs = 0.0.0.0/0\n"
            wireguard_output+="Endpoint = ${public_ip}:${wireguard_port}\n"
            wireguard_output+="PersistentKeepalive = 30\n\n"
            # 根据用户要求，每个 WireGuard 配置块后添加分隔线
            wireguard_output+="/////////////////////////////////////////////////////////\n\n"
        fi
    done

    # 打印 SOCKS5 信息
    if [ -n "$socks5_output" ]; then
        echo -e "${COLOR_GREEN}###################当前使用协议SOCKS5###########################${COLOR_NC}\n" # 增加两行空打印
        echo -e "${COLOR_YELLOW}${socks5_output}${COLOR_NC}"
    fi

    # 打印 VMess 信息
    if [ -n "$vmess_output" ]; then
        echo -e "${COLOR_GREEN}###################当前使用协议VMess###########################${COLOR_NC}\n" # 增加两行空打印
        echo -e "${COLOR_YELLOW}${vmess_output}${COLOR_NC}"
    fi

    # 打印 TUIC 信息
    if [ -n "$tuic_output" ]; then
        echo -e "${COLOR_GREEN}###################当前使用协议TUIC###########################${COLOR_NC}\n" # 增加两行空打印
        echo -e "${COLOR_YELLOW}${tuic_output}${COLOR_NC}"
    fi

    # 打印 WireGuard 信息
    if [ -n "$wireguard_output" ]; then
        echo -e "${COLOR_GREEN}###################当前使用协议WireGuard###########################${COLOR_NC}\n" # 增加两行空打印
        echo -e "${COLOR_YELLOW}${wireguard_output}${COLOR_NC}"
    fi

    echo -e "${COLOR_GREEN}################以上搭建完成的协议在客户端中直接使用#####################${COLOR_NC}" # 改为绿色
    echo ""
    log "脚本执行完毕！"
    log "日志文件位置: $LOG_FILE"
    log "Sing-box 配置文件位置: /etc/sing-box/config.json"
    log "你可以通过 '${COLOR_YELLOW}sudo journalctl -u sing-box -f${COLOR_NC}' 查看 Sing-box 实时日志。"
}

# --- 脚本执行流程 ---
main() {
    configure_locale # 先配置语言环境
    install_dependencies
    # 系统优化在安装 Sing-box 之前进行，确保环境最优
    apply_system_optimizations # 新增系统优化调用
    stop_existing_singbox
    install_or_update_singbox # 使用新的安装逻辑
    discover_network_info # 探测内网IP、网卡和默认网关
    discover_ip_pairs     # 探测公网IP与私网IP映射
    
    # 协议选择在所有前置安装和探测完成后，紧接着是密钥生成、配置生成、服务启动
    select_protocols # 用户选择协议
    
    generate_protocol_secrets # 修正为按需生成密钥/密码
    generate_singbox_config
    configure_systemd
    configure_policy_routing # 在启动 Sing-box 之前配置策略路由
    start_singbox
    print_client_info
}

# 函数：系统优化
apply_system_optimizations() {
    log "开始全面检查并应用系统网络优化...";
    
    # 检查并创建Swap文件
    if [ "$(swapon --show | wc -l)" -eq 0 ] || [ "$(swapon --show | grep -v "Filename" | wc -l)" -eq 0 ]; then # 检查是否有swap或没有实际挂载的swap文件
        warn "未发现Swap，正在创建 ${SWAP_SIZE} Swap文件...";
        fallocate -l ${SWAP_SIZE} /swapfile
        if [ $? -ne 0 ]; then
            warn "fallocate 创建Swap文件失败，尝试使用 dd。";
            dd if=/dev/zero of=/swapfile bs=1M count=$(echo ${SWAP_SIZE} | sed 's/G/*1024/g' | bc) >/dev/null 2>&1
        fi
        chmod 600 /swapfile
        mkswap /dev/null 2>&1
        swapon /dev/null 2>&1
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
net.ipv4.tcp_tw_reuse=1
EOF
    sysctl -p > /dev/null 2>&1;
    log "内核参数已更新。";
    
    # 设置文件描述符限制
    if ! grep -q "^\* soft nofile 1048576" /etc/security/limits.conf; then
        echo "* soft nofile 1048576" >> /etc/security/limits.conf;
        echo "* hard nofile 1048576" >> /etc/limits.conf; # 修正这里，应该是limits.conf而不是limits.conf;
        log "文件描述符限制已设置。";
    fi
    
    # IRQ 负载均衡
    if [[ "${ENABLE_IRQ_BALANCE}" == "yes" ]]; then
        warn "检测到用户选择开启 IRQ 负载均衡...";
        if ! command -v irqbalance > /dev/null; then
            if command -v apt-get > /dev/null; then 
                log "正在安装 irqbalance...";
                apt-get update -y && apt-get install -y irqbalance; 
            else 
                warn "无法找到 apt-get，跳过安装 irqbalance。"; 
            fi
        fi;
        if command -v irqbalance > /dev/null; then 
            systemctl enable --now irqbalance > /dev/null 2>&1; 
            log "irqbalance 服务已启用并运行。"; 
        else 
            error "irqbalance 安装失败或无法运行。"; 
        fi
    else
        log "跳过 IRQ 负载均衡优化。";
    fi;
    log "系统优化配置完成。"
}


# 执行主函数
main