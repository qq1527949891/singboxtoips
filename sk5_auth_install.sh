#!/bin/bash

# 多系统SOCKS5代理脚本 (Debian/Ubuntu)
# 功能：自动识别系统 + 智能镜像加速 + 3proxy服务
# 用户名：cwl  密码：666888

# 颜色定义
RED='\033[31m'; GREEN='\033[32m'
YELLOW='\033[33m'; BLUE='\033[34m'
CYAN='\033[36m'; NC='\033[0m'

# 配置参数
PORT="11888"
USER="cwl"
PASS="666888"
CONFIG_DIR="/etc/3proxy"
LOG_FILE="/var/log/3proxy/3proxy.log"

# --------------------- 系统检测函数 ---------------------
detect_os() {
    echo -e "${GREEN}▶ 正在检测操作系统..." >&2

    if [ -f /etc/debian_version ]; then
        if [ -f /etc/lsb-release ]; then
            # Ubuntu系统
            OS="ubuntu"
            CODENAME=$(lsb_release -cs)
            OS_VER=$(lsb_release -rs)
            OS_NAME="Ubuntu ${OS_VER} ($(lsb_release -ds | awk -F'"' '{print $2}'))"
            SECURITY_SUFFIX="-security"
            echo -e "${CYAN}✓ 检测到系统：${BLUE}Ubuntu ${OS_VER}${CYAN} (代号:${YELLOW}${CODENAME}${CYAN})${NC}" >&2
        else
            # Debian系统
            OS="debian"
            CODENAME=$(lsb_release -cs)
            OS_VER=$(cat /etc/debian_version)
            OS_NAME="Debian ${OS_VER} ($(lsb_release -ds | sed 's/^Description:\t//'))"
            SECURITY_SUFFIX="-security"
            echo -e "${CYAN}✓ 检测到系统：${BLUE}Debian ${OS_VER}${CYAN} (代号:${YELLOW}${CODENAME}${CYAN})${NC}" >&2
        fi

        echo -e "${CYAN}▶ 系统详细信息：" >&2
        echo -e "   - 架构:    ${YELLOW}$(uname -m)${CYAN}" >&2
        echo -e "   - 内核版本:${YELLOW}$(uname -r)${CYAN}" >&2
        echo -e "   - 主机名:  ${YELLOW}$(hostname)${NC}" >&2
    else
        echo -e "${RED}✖ 不支持的操作系统${NC}" >&2
        echo -e "${YELLOW}支持的发行版：${NC}" >&2
        echo -e "  - Debian 10/11/12" >&2
        echo -e "  - Ubuntu 20.04/22.04" >&2
        exit 1
    fi
}

# --------------------- 镜像源配置 ---------------------
MIRROR_LIST() {
    if [ "$OS" = "debian" ]; then
        # Debian镜像源列表
        echo "腾讯云|http://mirrors.tencent.com/debian/|http://mirrors.tencent.com/debian-security/"
        echo "阿里云|http://mirrors.aliyun.com/debian/|http://mirrors.aliyun.com/debian-security/"
        echo "华为云|http://repo.huaweicloud.com/debian/|http://repo.huaweicloud.com/debian-security/"
    elif [ "$OS" = "ubuntu" ]; then
        # Ubuntu镜像源列表
        echo "腾讯云|http://mirrors.tencent.com/ubuntu/|http://mirrors.tencent.com/ubuntu/"
        echo "阿里云|http://mirrors.aliyun.com/ubuntu/|http://mirrors.aliyun.com/ubuntu/"
        echo "华为云|http://repo.huaweicloud.com/ubuntu/|http://repo.huaweicloud.com/ubuntu/"
    fi
}

# --------------------- 功能函数 ---------------------
print_status() {
    echo -e "${GREEN}[$(date +%T)] ${CYAN}$1${NC}" >&2
}

print_error() {
    echo -e "${RED}✖ $1${NC}" >&2
    exit 1
}

select_mirror() {
    local fastest_mirror=""
    local fastest_time=999999

    print_status "开始镜像测速（精确到毫秒）..."
    
    # 生成完整镜像列表
    MIRROR_LIST > /tmp/mirrors.list
    
    while IFS='|' read -r name base_url security_url; do
        # 规范化URL格式（必须操作！）
        base_url=${base_url%/}       # 移除末尾/
        security_url=${security_url%/}
        
        # Ubuntu/Debian使用不同测试文件
        if [ "$OS" = "debian" ]; then
            test_url="${base_url}/dists/$CODENAME/Release"
        else
            test_url="${base_url}/dists/$CODENAME/Release.gpg"
        fi

        echo -n "  测试 ${CYAN}${name}${NC} ... " >&2
        
        # 毫秒级测速
        start_time=$(date +%s%3N)
        if curl -sSfL --connect-timeout 5 -o /dev/null "$test_url"; then
            end_time=$(date +%s%3N)
            cost=$(( end_time - start_time ))
            printf "耗时：${YELLOW}%dms${NC}\n" "$cost" >&2
            
            if [ $cost -lt $fastest_time ]; then
                fastest_time=$cost
                fastest_mirror="${name}|${base_url}|${security_url}"
                echo -e "  → 当前最佳：${BLUE}${name}${NC}" >&2
            fi
        else
            printf "${RED}连接失败${NC}\n" >&2
        fi
    done < /tmp/mirrors.list
    
    rm -f /tmp/mirrors.list
    
    [ -z "$fastest_mirror" ] && print_error "所有镜像均不可用"
    echo "$fastest_mirror"  # 返回完整镜像信息 name|base_url|security_url
}

configure_sources() {
    local selected_mirror=$(select_mirror)
    IFS='|' read -r name base_url security_url <<< "$selected_mirror"
    
    # 二次验证URL格式
    [ -z "$security_url" ] && print_error "安全源URL配置异常，请检查镜像列表"
    
    print_status "配置镜像源：${CYAN}${name}${NC}"
    cp /etc/apt/sources.list /etc/apt/sources.list.bak
    
    # 生成精确配置（关键修复！）
    if [ "$OS" = "debian" ]; then
        cat > /etc/apt/sources.list <<EOF
deb ${base_url}/ $CODENAME main contrib non-free
deb ${base_url}/ $CODENAME-updates main contrib non-free
deb ${base_url}/ $CODENAME-backports main contrib non-free
deb ${security_url}/ $CODENAME-security main contrib non-free
EOF
    else
        cat > /etc/apt/sources.list <<EOF
deb ${base_url}/ $CODENAME main restricted universe multiverse
deb ${base_url}/ $CODENAME-updates main restricted universe multiverse
deb ${base_url}/ $CODENAME-backports main restricted universe multiverse
deb ${base_url}/ $CODENAME-security main restricted universe multiverse
EOF
    fi

    # 调试输出
    echo -e "${CYAN}生成的软件源配置：${NC}"
    cat /etc/apt/sources.list
    echo "----------------------------------------"

    # 预验证配置
    if ! apt-get update -o APT::Update::Pre-Invoke::="echo Checking..." >/dev/null 2>&1; then
        print_error "软件源配置错误，请检查：\n$(cat /etc/apt/sources.list)"
    fi
}

configure_sources() {
    local selected_mirror=$(select_mirror)
    IFS='|' read -r name base_url security_url <<< "$selected_mirror"
    
    # 二次验证URL格式
    [ -z "$security_url" ] && print_error "安全源URL配置异常，请检查镜像列表"
    
    print_status "配置镜像源：${CYAN}${name}${NC}"
    cp /etc/apt/sources.list /etc/apt/sources.list.bak
    
    # 生成精确配置（关键修复！）
    if [ "$OS" = "debian" ]; then
        cat > /etc/apt/sources.list <<EOF
deb ${base_url}/ $CODENAME main contrib non-free
deb ${base_url}/ $CODENAME-updates main contrib non-free
deb ${base_url}/ $CODENAME-backports main contrib non-free
deb ${security_url}/ $CODENAME-security main contrib non-free
EOF
    else
        cat > /etc/apt/sources.list <<EOF
deb ${base_url}/ $CODENAME main restricted universe multiverse
deb ${base_url}/ $CODENAME-updates main restricted universe multiverse
deb ${base_url}/ $CODENAME-backports main restricted universe multiverse
deb ${base_url}/ $CODENAME-security main restricted universe multiverse
EOF
    fi

    # 调试输出
    echo -e "${CYAN}生成的软件源配置：${NC}"
    cat /etc/apt/sources.list
    echo "----------------------------------------"

    # 预验证配置
    if ! apt-get update -o APT::Update::Pre-Invoke::="echo Checking..." >/dev/null 2>&1; then
        print_error "软件源配置错误，请检查：\n$(cat /etc/apt/sources.list)"
    fi
}

install_3proxy() {
    print_status "安装3proxy..."
    if ! apt-get install -y 3proxy >/dev/null 2>&1; then
        print_status "尝试源码编译安装..."
        apt-get install -y build-essential wget >/dev/null
        wget https://github.com/3proxy/3proxy/archive/refs/tags/0.9.4.tar.gz || print_error "下载失败"
        tar xzf 0.9.4.tar.gz
        cd 3proxy-0.9.4
        make -f Makefile.Linux >/dev/null || print_error "编译失败"
        make install -f Makefile.Linux >/dev/null || print_error "安装失败"
        cd ..
        rm -rf 3proxy-0.9.4*
    fi
}

configure_proxy() {
    print_status "生成代理配置..."
    mkdir -p $CONFIG_DIR
    
    cat > $CONFIG_DIR/3proxy.cfg <<EOF
daemon
maxconn 2000
nserver 8.8.8.8
nserver 1.1.1.1
auth strong
users $USER:CL:$PASS
allow $USER
log $LOG_FILE
logformat "L[%Y-%m-%d %H:%M:%S] %N.%p %E %U %C:%c %R:%r %O %I %h %T"
rotate 10
socks -p$PORT
EOF

    mkdir -p /var/log/3proxy
    touch $LOG_FILE
    chmod 666 $LOG_FILE
}

setup_firewall() {
    print_status "配置防火墙..."
    if command -v ufw >/dev/null; then
        ufw allow $PORT/tcp >/dev/null
    else
        iptables -I INPUT -p tcp --dport $PORT -j ACCEPT
        iptables-save > /etc/iptables/rules.v4
    fi
}

setup_service() {
    print_status "配置系统服务..."
    cat > /etc/systemd/system/3proxy.service <<EOF
[Unit]
Description=3Proxy SOCKS5 Server
After=network.target

[Service]
Type=forking
ExecStart=/usr/bin/3proxy $CONFIG_DIR/3proxy.cfg
Restart=on-failure
RestartSec=30

[Install]
WantedBy=multi-user.target
EOF

    systemctl daemon-reload
    systemctl enable 3proxy >/dev/null
    systemctl restart 3proxy || print_error "服务启动失败"
}

# --------------------- 主流程 ---------------------
main() {
    detect_os
    configure_sources
    install_3proxy
    configure_proxy
    setup_firewall
    setup_service
    show_result
}

# --------------------- 初始化 ---------------------
check_root() {
    [ "$(id -u)" != "0" ] && print_error "必须使用root权限执行"
}

show_result() {
    clear
    echo -e "${GREEN}
    ███████╗ ██████╗  ██████╗██╗  ██╗███████╗
    ██╔════╝██╔═══██╗██╔════╝██║ ██╔╝██╔════╝
    ███████╗██║   ██║██║     █████╔╝ ███████╗
    ╚════██║██║   ██║██║     ██╔═██╗ ╚════██║
    ███████║╚██████╔╝╚██████╗██║  ██╗███████║
    ╚══════╝ ╚═════╝  ╚═════╝╚═╝  ╚═╝╚══════╝
    ${NC}"
    echo -e "${GREEN}════════ 代理配置信息 [${OS_NAME}] ════════${NC}"
    echo -e "服务器IP  : ${CYAN}$(curl -s ifconfig.me)${NC}"
    echo -e "端口号    : ${BLUE}$PORT${NC}"
    echo -e "用户名    : ${YELLOW}$USER${NC}"
    echo -e "密码      : ${YELLOW}$PASS${NC}"
    echo -e "${GREEN}════════ 使用说明 ════════${NC}"
    echo -e "测试命令  : ${YELLOW}curl --socks5 $USER:$PASS@127.0.0.1:$PORT ifconfig.me${NC}"
    echo -e "浏览器设置: ${YELLOW}socks5://服务器IP:$PORT${NC}"
	echo -e "${RED}安全提示：建议立即修改默认密码！${NC}"
}

check_root
main