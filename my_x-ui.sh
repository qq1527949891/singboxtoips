#!/bin/bash

# ==============================================================================
# 脚本名称: Ultimate Auto-Setup Script for X-UI (Safe Edition)
# 脚本功能: 自动创建Swap、全面优化系统网络、并无人值守安装 X-UI 面板
# 作者: Gemini
# 版本: 6.0 (终极安全版，集成自动Swap功能)
# ==============================================================================

# --- 请在这里设置您的信息 ---
ADMIN_USER="cwl"      # 设置您想使用的后台登录用户名
ADMIN_PASS="666888"   # 设置您想使用的后台登录密码
PANEL_PORT="11888"         # 设置您想使用的后台访问端口

# [可选] IRQ 硬件中断负载均衡开关
# 说明: 对多核高流量服务器有益。若要开启，请将 "no" 修改为 "yes"。
ENABLE_IRQ_BALANCE="no"

# [可选] Swap交换文件大小
# 说明: 单位可以是 G, M。例如 1G, 2G, 512M。
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
    # 检查当前是否存在任何swap空间
    if [ "$(swapon --show | wc -l)" -gt 0 ]; then
        log "检测到已存在Swap，跳过创建。$(swapon --show)"
        return
    fi
    
    warn "未发现Swap，正在为您创建 ${SWAP_SIZE} 的Swap文件..."
    # 使用fallocate创建指定大小的文件
    sudo fallocate -l ${SWAP_SIZE} /swapfile
    if [ $? -ne 0 ]; then
        error "创建swapfile失败，可能空间不足或系统不支持fallocate。"
        warn "正在尝试使用dd命令创建，速度可能较慢..."
        sudo dd if=/dev/zero of=/swapfile bs=1024 count=$(echo ${SWAP_SIZE} | sed 's/G/000000/' | sed 's/M/000/')
    fi

    sudo chmod 600 /swapfile
    sudo mkswap /swapfile
    sudo swapon /swapfile
    
    # 检查是否成功启用
    if [ "$(swapon --show | wc -l)" -gt 0 ]; then
        log "Swap文件已成功创建并启用！"
        # 写入fstab，实现开机自启
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
    # ... 此函数内容与之前版本相同，为简洁此处省略，但在实际脚本中是完整的 ...
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
    # ... 此函数内容与之前版本相同，为简洁此处省略，但在实际脚本中是完整的 ...
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

# 2. 创建Swap
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

# 5. 下载并执行X-UI安装
INSTALLER_URL="https://raw.githubusercontent.com/FranzkafkaYu/x-ui/master/install.sh"
INSTALLER_FILE="xui_installer_latest.sh"
log "第五步：正在下载最新的 X-UI 安装脚本..."
wget -O ${INSTALLER_FILE} ${INSTALLER_URL}
if [ -s "${INSTALLER_FILE}" ]; then
    log "下载成功！"
    chmod +x ${INSTALLER_FILE}
    log "即将开始无人值守安装..."
    echo "预设用户名: ${ADMIN_USER}"
    echo "预设密码: [已隐藏]"
    echo "预设端口: ${PANEL_PORT}"
    (echo "y" && echo "${ADMIN_USER}" && echo "${ADMIN_PASS}" && echo "${PANEL_PORT}") | ./${INSTALLER_FILE}
    log "安装过程已执行完毕！"
    rm -f ${INSTALLER_FILE}
else
    error "下载安装脚本失败！"
fi

# 最终提示
echo "----------------------------------------------------"
warn "重要提示：关于“文件描述符限制”的优化，需要您「重新登录SSH会话」后才能对您的当前用户生效。"
warn "不过，由systemd启动的服务（如X-UI）通常能自动应用更高的限制，一般无需担心。"
log "所有任务已完成。"