#!/bin/bash

DEFAULT_START_PORT=51801                         #默认起始端口
DEFAULT_SOCKS_USERNAME="cwl"                   #默认socks账号
DEFAULT_SOCKS_PASSWORD="666888"               #默认socks密码
DEFAULT_WS_PATH="/ws"                            #默认ws路径
DEFAULT_UUID=$(cat /proc/sys/kernel/random/uuid) #默认随机UUID
DEFAULT_SHADOWSOCKS_PASSWORD="666888"         #默认shadowsocks密码
DEFAULT_SHADOWSOCKS_METHOD="aes-256-gcm"         #默认shadowsocks加密方法
DEFAULT_VLESS_UUID=$(cat /proc/sys/kernel/random/uuid) #默认VLESS UUID
DEFAULT_VLESS_WS_PATH="/weiyuegs"                   #默认VLESS WebSocket路径

IP_ADDRESSES=($(hostname -I))

install_xray() {
	echo "安装 Xray..."
	apt-get install unzip -y || yum install unzip -y
	wget https://github.com/XTLS/Xray-core/releases/download/v1.8.3/Xray-linux-64.zip
	unzip Xray-linux-64.zip
	mv xray /usr/local/bin/xrayL
	chmod +x /usr/local/bin/xrayL
	cat <<EOF >/etc/systemd/system/xrayL.service
[Unit]
Description=XrayL Service
After=network.target

[Service]
ExecStart=/usr/local/bin/xrayL -c /etc/xrayL/config.toml
Restart=on-failure
User=nobody
RestartSec=3

[Install]
WantedBy=multi-user.target
EOF
	systemctl daemon-reload
	systemctl enable xrayL.service
	systemctl start xrayL.service
	echo "Xray 安装完成."
}

config_xray() {
	config_type=$1
	mkdir -p /etc/xrayL
	if [ "$config_type" != "socks" ] && [ "$config_type" != "vmess" ] && [ "$config_type" != "shadowsocks" ] && [ "$config_type" != "vless" ]; then
		echo "类型错误！仅支持socks、vmess、shadowsocks和vless."
		exit 1
	fi

	read -p "起始端口 (默认 $DEFAULT_START_PORT): " START_PORT
	START_PORT=${START_PORT:-$DEFAULT_START_PORT}
	
	if [ "$config_type" == "socks" ]; then
		read -p "SOCKS 账号 (默认 $DEFAULT_SOCKS_USERNAME): " SOCKS_USERNAME
		SOCKS_USERNAME=${SOCKS_USERNAME:-$DEFAULT_SOCKS_USERNAME}

		read -p "SOCKS 密码 (默认 $DEFAULT_SOCKS_PASSWORD): " SOCKS_PASSWORD
		SOCKS_PASSWORD=${SOCKS_PASSWORD:-$DEFAULT_SOCKS_PASSWORD}
	elif [ "$config_type" == "vmess" ]; then
		read -p "UUID (默认随机): " UUID
		UUID="a44683f4-a789-5008-b9f8-eec7aa3c1ca4"
		read -p "WebSocket 路径 (默认 $DEFAULT_WS_PATH): " WS_PATH
		WS_PATH=${WS_PATH:-$DEFAULT_WS_PATH}
	elif [ "$config_type" == "shadowsocks" ]; then
		read -p "Shadowsocks 密码 (默认 $DEFAULT_SHADOWSOCKS_PASSWORD): " SHADOWSOCKS_PASSWORD
		SHADOWSOCKS_PASSWORD=${SHADOWSOCKS_PASSWORD:-$DEFAULT_SHADOWSOCKS_PASSWORD}
		read -p "Shadowsocks 加密方法 (默认 $DEFAULT_SHADOWSOCKS_METHOD): " SHADOWSOCKS_METHOD
		SHADOWSOCKS_METHOD=${SHADOWSOCKS_METHOD:-$DEFAULT_SHADOWSOCKS_METHOD}
	elif [ "$config_type" == "vless" ]; then
		read -p "VLESS UUID (默认随机): " VLESS_UUID
		VLESS_UUID="a44683f4-a789-5008-b9f8-eec7aa3c1ca4"
		read -p "VLESS WebSocket 路径 (默认 $DEFAULT_VLESS_WS_PATH): " VLESS_WS_PATH
		VLESS_WS_PATH=${VLESS_WS_PATH:-$DEFAULT_VLESS_WS_PATH}
		
		echo "选择VLESS传输方式:"
		echo "1. TCP (直连)"
		echo "2. WebSocket"
		read -p "请选择 (默认为2-WebSocket): " transport_choice
		transport_choice=${transport_choice:-2}
		
		if [ "$transport_choice" == "1" ]; then
			VLESS_TRANSPORT="tcp"
		else
			VLESS_TRANSPORT="ws"
		fi
		
		read -p "是否启用TLS? (y/n, 默认n): " enable_tls
		enable_tls=${enable_tls:-n}
	fi

	config_content=""
	for ((i = 0; i < ${#IP_ADDRESSES[@]}; i++)); do
		config_content+="[[inbounds]]\n"
		config_content+="port = $((START_PORT + i))\n"
		config_content+="protocol = \"$config_type\"\n"
		config_content+="tag = \"tag_$((i + 1))\"\n"
		config_content+="[inbounds.settings]\n"
		
		if [ "$config_type" == "socks" ]; then
			config_content+="auth = \"password\"\n"
			config_content+="udp = true\n"
			config_content+="ip = \"${IP_ADDRESSES[i]}\"\n"
			config_content+="[[inbounds.settings.accounts]]\n"
			config_content+="user = \"$SOCKS_USERNAME\"\n"
			config_content+="pass = \"$SOCKS_PASSWORD\"\n"
		elif [ "$config_type" == "vmess" ]; then
			config_content+="[[inbounds.settings.clients]]\n"
			config_content+="id = \"$UUID\"\n"
			config_content+="[inbounds.streamSettings]\n"
			config_content+="network = \"ws\"\n"
			config_content+="[inbounds.streamSettings.wsSettings]\n"
			config_content+="path = \"$WS_PATH\"\n\n"
		elif [ "$config_type" == "shadowsocks" ]; then
			config_content+="method = \"$SHADOWSOCKS_METHOD\"\n"
			config_content+="password = \"$SHADOWSOCKS_PASSWORD\"\n"
			config_content+="network = \"tcp,udp\"\n"
			config_content+="ip = \"${IP_ADDRESSES[i]}\"\n"
		elif [ "$config_type" == "vless" ]; then
		     config_content+="decryption = \"none\"\n" 
			config_content+="[[inbounds.settings.clients]]\n"
			config_content+="id = \"$VLESS_UUID\"\n"
			config_content+="[inbounds.streamSettings]\n"
			config_content+="network = \"$VLESS_TRANSPORT\"\n"
			
			if [ "$VLESS_TRANSPORT" == "ws" ]; then
				config_content+="[inbounds.streamSettings.wsSettings]\n"
				config_content+="path = \"$VLESS_WS_PATH\"\n"
			fi
			
			if [ "$enable_tls" == "y" ]; then
				config_content+="security = \"tls\"\n"
				config_content+="[inbounds.streamSettings.tlsSettings]\n"
				config_content+="serverName = \"example.com\"\n"
				config_content+="certificates = []\n"
			fi
			config_content+="\n"
		fi
		
		config_content+="[[outbounds]]\n"
		config_content+="sendThrough = \"${IP_ADDRESSES[i]}\"\n"
		config_content+="protocol = \"freedom\"\n"
		config_content+="tag = \"tag_$((i + 1))\"\n\n"
		config_content+="[[routing.rules]]\n"
		config_content+="type = \"field\"\n"
		config_content+="inboundTag = \"tag_$((i + 1))\"\n"
		config_content+="outboundTag = \"tag_$((i + 1))\"\n\n\n"
	done
	
	echo -e "$config_content" >/etc/xrayL/config.toml
	systemctl restart xrayL.service
	systemctl --no-pager status xrayL.service
	echo ""
	echo "生成 $config_type 配置完成"
	echo "起始端口:$START_PORT"
	echo "结束端口:$(($START_PORT + ${#IP_ADDRESSES[@]} - 1))"
	
	if [ "$config_type" == "socks" ]; then
		echo "socks账号:$SOCKS_USERNAME"
		echo "socks密码:$SOCKS_PASSWORD"
	elif [ "$config_type" == "vmess" ]; then
		echo "UUID:$UUID"
		echo "ws路径:$WS_PATH"
	elif [ "$config_type" == "shadowsocks" ]; then
		echo "Shadowsocks密码:$SHADOWSOCKS_PASSWORD"
		echo "Shadowsocks加密方法:$SHADOWSOCKS_METHOD"
	elif [ "$config_type" == "vless" ]; then
		echo "VLESS UUID:$VLESS_UUID"
		echo "传输方式:$VLESS_TRANSPORT"
		if [ "$VLESS_TRANSPORT" == "ws" ]; then
			echo "WebSocket路径:$VLESS_WS_PATH"
		fi
		if [ "$enable_tls" == "y" ]; then
			echo "TLS:已启用"
		else
			echo "TLS:未启用"
		fi
	fi
	echo ""
	
	# 生成客户端连接信息
	if [ "$config_type" == "vless" ]; then
		echo "=== VLESS 客户端连接信息 ==="
		for ((i = 0; i < ${#IP_ADDRESSES[@]}; i++)); do
			port=$((START_PORT + i))
			if [ "$VLESS_TRANSPORT" == "ws" ]; then
				if [ "$enable_tls" == "y" ]; then
					echo "vless://${VLESS_UUID}@${IP_ADDRESSES[i]}:${port}?type=ws&path=${VLESS_WS_PATH}&security=tls#VLESS-WS-TLS-${i}"
				else
					echo "vless://${VLESS_UUID}@${IP_ADDRESSES[i]}:${port}?type=ws&path=${VLESS_WS_PATH}#VLESS-WS-${i}"
				fi
			else
				if [ "$enable_tls" == "y" ]; then
					echo "vless://${VLESS_UUID}@${IP_ADDRESSES[i]}:${port}?security=tls#VLESS-TCP-TLS-${i}"
				else
					echo "vless://${VLESS_UUID}@${IP_ADDRESSES[i]}:${port}#VLESS-TCP-${i}"
				fi
			fi
		done
		echo "=========================="
	fi
}

show_menu() {
	echo "================================="
	echo "  Xray 多协议节点配置脚本"
	echo "================================="
	echo "支持的协议类型:"
	echo "1. SOCKS5 代理"
	echo "2. VMess"
	echo "3. Shadowsocks"
	echo "4. VLESS"
	echo "================================="
}

main() {
	[ -x "$(command -v xrayL)" ] || install_xray
	
	if [ $# -eq 1 ]; then
		config_type="$1"
	else
		show_menu
		read -p "选择生成的节点类型 (socks/vmess/shadowsocks/vless): " config_type
	fi
	
	case "$config_type" in
		"1"|"socks")
			config_xray "socks"
			;;
		"2"|"vmess")
			config_xray "vmess"
			;;
		"3"|"shadowsocks")
			config_xray "shadowsocks"
			;;
		"4"|"vless")
			config_xray "vless"
			;;
		*)
			echo "未正确选择类型，使用默认socks配置."
			config_xray "socks"
			;;
	esac
}

main "$@"
