#!/bin/bash

# ==============================================================================
# 脚本名称: Gost Multi-IP SOCKS5 Ultimate Setup Script
# 脚本功能: 自动创建Swap、全面优化系统网络，并为所有公网IP自动搭建SOCKS5代理
# 作者: Gemini
# 版本: 4.1 (gost部署 + 自动Swap + 全面优化集成版)
# ==============================================================================

# --- 请在这里设置您的信息 ---
SOCKS5_USER="cwl"         # 设置您想使用的 SOCKS5 用户名
SOCKS5_PASS="666888"     # 设置您想使用的 SOCKS5 密码
START_PORT="15888"           # 设置您想使用的起始端口

# [可选] IRQ 硬件中断负载均衡开关
# 说明: 对多核高流量服务器有益。若要开启，请将 "no" 修改为 "yes"。
ENABLE_IRQ_BALANCE="no"

# [可选] Swap交换文件大小
# 说明: 单位可以是 G, M。例如 1G, 2G, 512M。对于0.5G内存的服务器，推荐 "1G"。
SWAP_SIZE="5G"

# --- 下面的内容无需修改 ---

# 定义颜色常量和打印函数
GREEN='\033[0;32m'; RED='\033[0;31m'; YELLOW='\033[1;33m'; NC='\033[0m'
log() { echo -e "${GREEN}[INFO] ${1}${NC}"; }
warn() { echo -e "${YELLOW}[WARN] ${1}${NC}"; }
error() { echo -e "${RED}[ERROR] ${1}${NC}"; }

# 函数：创建并启用Swap文件
create_swap_if_needed() {
    log "开始检查Swap交换文件..."
    if [ "$(swapon --show | wc -l)" -gt 0 ]; then
        log "检测到已存在Swap，跳过创建。$(swapon --show)"
        return
    fi
    warn "未发现Swap，正在为您创建 ${SWAP_SIZE} 的Swap文件..."
    sudo fallocate -l ${SWAP_SIZE} /swapfile
    if [ $? -ne 0 ]; then
        error "创建swapfile失败，可能空间不足或系统不支持fallocate。"
        warn "正在尝试使用dd命令创建，速度可能较慢..."
        sudo dd if=/dev/zero of=/swapfile bs=1024 count=$(echo ${SWAP_SIZE} | sed 's/G/000000/' | sed 's/M/000/')
    fi
    sudo chmod 600 /swapfile
    sudo mkswap /swapfile
    sudo swapon /swapfile
    if [ "$(swapon --show | wc -l)" -gt 0 ]; then
        log "Swap文件已成功创建并启用！"
        if ! grep -q "/swapfile" /etc/fstab; then
            echo '/swapfile none swap sw 0 0' | sudo tee -a /etc/fstab
            log "已将Swap配置写入 /etc/fstab，实现开机自启。"
        fi
    else
        error "启用Swap文件失败！请检查系统日志。"
    fi
}

# 函数：更换APT源为阿里云镜像
change_apt_source() {
    if ! command -v apt-get &> /dev/null; then return; fi
    warn "正在检查APT软件源..."
    if grep -q "aliyun.com" /etc/apt/sources.list; then log "已使用阿里云镜像源。"; return; fi
    local country=$(curl -s --connect-timeout 5 ipinfo.io/country)
    if [ "$country" == "CN" ]; then
        warn "服务器位于中国大陆，更换为阿里云镜像..."; cp /etc/apt/sources.list /etc/apt/sources.list.bak
        log "原始源文件已备份至 /etc/apt/sources.list.bak"; source /etc/os-release
        local codename=$VERSION_CODENAME
        if [[ "$ID" == "ubuntu" ]]; then
            cat > /etc/apt/sources.list <<EOF
deb http://mirrors.aliyun.com/ubuntu/ ${codename} main restricted universe multiverse
deb http://mirrors.aliyun.com/ubuntu/ ${codename}-security main restricted universe multiverse
deb http://mirrors.aliyun.com/ubuntu/ ${codename}-updates main restricted universe multiverse
deb http://mirrors.aliyun.com/ubuntu/ ${codename}-backports main restricted universe multiverse
EOF
        elif [[ "$ID" == "debian" ]]; then
            cat > /etc/apt/sources.list <<EOF
deb http://mirrors.aliyun.com/debian/ ${codename} main contrib non-free
deb http://mirrors.aliyun.com/debian/ ${codename}-updates main contrib non-free
deb http://mirrors.aliyun.com/debian-security ${codename}/updates main contrib non-free
EOF
        fi; log "APT源已成功更换为阿里云镜像。"
    else log "服务器不在中国大陆，使用默认软件源。"; fi
}

# 函数：应用所有系统优化
apply_all_optimizations() {
    log "开始全面检查并应用「游戏加速」系统网络优化..."
    local sysctl_optimizations=(
        "fs.file-max=1048576" "net.core.default_qdisc=fq" "net.ipv4.tcp_congestion_control=bbr"
        "net.ipv4.tcp_fastopen=3" "net.ipv4.tcp_low_latency=1" "net.core.rmem_max=16777216"
        "net.core.wmem_max=16777216" "net.ipv4.tcp_rmem=4096 87380 16777216"
        "net.ipv4.tcp_wmem=4096 16384 16777216" "net.netfilter.nf_conntrack_max=262144"
        "net.nf_conntrack_max=262144" "net.ipv4.tcp_mtu_probing=1" "net.ipv4.tcp_syncookies=1"
        "net.ipv4.tcp_timestamps=0" "net.ipv4.tcp_tw_reuse=1"
    )
    log "检查并应用TCP/IP内核参数 (游戏低延迟版)..."
    for opt in "${sysctl_optimizations[@]}"; do
        local key=$(echo "$opt" | cut -d'=' -f1 | xargs); if ! grep -q "^${key}" /etc/sysctl.conf; then
        echo "$opt" >> /etc/sysctl.conf; else sed -i "s/^${key}.*/$opt/" /etc/sysctl.conf; fi
    done
    sysctl -p > /dev/null 2>&1; log "内核参数已更新为游戏优化配置。"

    log "检查并设置文件描述符限制..."
    if ! grep -q "^\* soft nofile 1048576" /etc/security/limits.conf; then
        echo "* soft nofile 1048576" >> /etc/security/limits.conf; echo "* hard nofile 1048576" >> /etc/security/limits.conf; log "文件描述符限制已设置为 1048576。";
    else log "文件描述符限制已是最佳配置。"; fi
    
    log "检查 IRQ 负载均衡选项..."
    if [[ "${ENABLE_IRQ_BALANCE}" == "yes" ]]; then
        warn "检测到用户选择开启 IRQ 负载均衡，开始配置..."
        if ! command -v irqbalance &> /dev/null; then
            warn "irqbalance 未安装，正在尝试安装..."; if command -v apt-get &> /dev/null; then apt-get update && apt-get install -y irqbalance; elif command -v yum &> /dev/null; then yum install -y irqbalance; fi
        fi
        if command -v irqbalance &> /dev/null; then
            if ! systemctl is-active --quiet irqbalance; then systemctl enable --now irqbalance >/dev/null 2>&1; fi
            log "irqbalance 服务已在运行。"
        else error "irqbalance 安装失败，请手动安装。"; fi
    else log "用户选择不开启 IRQ 负载均衡，跳过此项优化。"; fi
    log "系统网络优化检查与配置完成。"
}

# --- 脚本主流程 ---
# 1. 权限检查
log "第一步：正在检查脚本执行权限..."
if [ "$(id -u)" -ne 0 ]; then error "此脚本必须以 root 用户身份运行！"; exit 1; fi
log "权限检查通过。"
echo "----------------------------------------------------"

# 2. [新增] 创建Swap，保障系统稳定
log "第二步：检查并按需创建Swap交换文件..."
create_swap_if_needed
echo "----------------------------------------------------"

# 3. 更换软件源
log "第三步：正在检查并按需更换软件源..."
if ! command -v curl &> /dev/null; then
    warn "curl 未安装，正在尝试安装..."; if command -v apt-get &> /dev/null; then apt-get update && apt-get install -y curl; elif command -v yum &> /dev/null; then yum install -y curl; else error "无法自动安装curl。"; exit 1; fi
fi
change_apt_source
echo "----------------------------------------------------"

# 4. 应用所有优化
log "第四步：应用所有系统优化 (游戏加速版)..."
apply_all_optimizations
echo "----------------------------------------------------"

# 5. 检查并安装Gost
log "第五步：正在检查和安装 gost..."
if command -v gost &> /dev/null; then
    log "gost 已安装，跳过安装步骤。"; GOST_PATH=$(command -v gost);
else
    log "gost 未找到，现在开始自动安装..."; ARCH=$(uname -m);
    case ${ARCH} in x86_64) GOST_ARCH="amd64" ;; aarch64) GOST_ARCH="armv8" ;; *) error "不支持的架构: ${ARCH}"; exit 1 ;; esac
    LATEST_VERSION=$(curl -s "https://api.github.com/repos/ginuerzh/gost/releases/latest" | grep -oP '"tag_name": "\K(v[0-9-.]+)' | head -n 1);
    if [ -z "$LATEST_VERSION" ]; then error "无法获取gost版本号。"; exit 1; fi
    LATEST_VERSION_NUM=${LATEST_VERSION//v/}; DOWNLOAD_URL="https://github.com/ginuerzh/gost/releases/download/${LATEST_VERSION}/gost_${LATEST_VERSION_NUM}_linux_${GOST_ARCH}.tar.gz";
    log "正在下载gost..."; wget -O gost.tar.gz ${DOWNLOAD_URL};
    if [ ! -s "gost.tar.gz" ]; then error "gost 下载失败！"; rm -f gost.tar.gz; exit 1; fi
    tar -zxvf gost.tar.gz; if [ ! -f "./gost" ]; then error "解压失败。"; rm -f gost.tar.gz; exit 1; fi
    mv ./gost /usr/local/bin/gost; chmod +x /usr/local/bin/gost; rm -f gost.tar.gz README.md LICENSE;
    GOST_PATH="/usr/local/bin/gost"; log "gost 安装成功！";
fi
echo "----------------------------------------------------"

# 6. 发现IP并构建服务
log "第六步：正在发现所有公网IP并构建gost服务..."
log "正在通过本机网络接口主动探测所有公网IP..."
PRIVATE_IPS=$(hostname -I | sed 's/127.0.0.1//g'); ALL_FOUND_IPS=""
for PRIVATE_IP in $PRIVATE_IPS; do
    echo "正在探测接口 ${PRIVATE_IP} ..."; PROBED_PUBLIC_IP=$(curl -s --connect-timeout 5 --interface "${PRIVATE_IP}" ifconfig.me)
    if [ -n "$PROBED_PUBLIC_IP" ]; then echo " > 发现公网IP: ${PROBED_PUBLIC_IP}"; ALL_FOUND_IPS+="${PROBED_PUBLIC_IP} "; else echo " > 接口 ${PRIVATE_IP} 未能探测到公网IP。"; fi
done
PUBLIC_IPS=$(echo "$ALL_FOUND_IPS" | tr ' ' '\n' | sort -u | tr '\n' ' ')
if [ -z "$PUBLIC_IPS" ]; then error "最终未能通过任何网络接口探测到公网IP。脚本无法继续。"; exit 1; fi
log "成功发现以下公网IP: ${PUBLIC_IPS}"

GOST_LISTEN_FLAGS=""; FINAL_CONNECTIONS_INFO=""; CURRENT_PORT=$START_PORT
for IP in $PUBLIC_IPS; do
    GOST_LISTEN_FLAGS+=" -L socks5://${SOCKS5_USER}:${SOCKS5_PASS}@0.0.0.0:${CURRENT_PORT}"
    FINAL_CONNECTIONS_INFO+="${IP}|${CURRENT_PORT}|${SOCKS5_USER}|${SOCKS5_PASS}\n"
    CURRENT_PORT=$((CURRENT_PORT + 1))
done

SERVICE_FILE="/etc/systemd/system/gost-multi-socks5.service"
log "正在配置并强力重置服务..."
pkill -f "${GOST_PATH}" || true
systemctl stop gost-multi-socks5.service >/dev/null 2>&1
systemctl disable gost-multi-socks5.service >/dev/null 2>&1
rm -f ${SERVICE_FILE}
systemctl daemon-reload
cat > ${SERVICE_FILE} <<EOF
[Unit]
Description=Gost Multi-IP SOCKS5 Proxy Service
After=network.target
Wants=network.target
[Service]
Type=simple
ExecStart=${GOST_PATH}${GOST_LISTEN_FLAGS}
Restart=always
RestartSec=5
LimitNOFILE=1048576
[Install]
WantedBy=multi-user.target
EOF
log "systemd 服务文件已创建。"
systemctl enable ${SERVICE_FILE}; systemctl start ${SERVICE_FILE}
log "请稍候，正在检查服务状态..."
sleep 2
if systemctl is-active --quiet gost-multi-socks5.service; then log "SOCKS5 多端口服务已成功启动并运行！"
else error "SOCKS5 服务启动失败！请使用 'journalctl -u gost-multi-socks5.service --no-pager' 查看日志。"; exit 1; fi
echo "----------------------------------------------------"

# 7. 显示所有连接信息
echo; log "====================  部署成功! ===================="; echo
warn "已为您在所有公网IP上创建了独立的SOCKS5代理，连接信息如下："; echo
warn "连接信息 (格式: IP|端口|用户名|密码)，请逐行复制:"; echo
echo -e "${GREEN}${FINAL_CONNECTIONS_INFO}${NC}"
warn "重要提示：关于“文件描述符限制”的优化对新登录的SSH会话生效。由本脚本启动的服务已自动应用高限制。"
log "所有任务已完成。"