# �� IP ��Э������ڵ㹤��

���� Linux ����������װ���֧�� **Debian 12+ / systemd / amd64��arm64**��ͨ�����������н���ѡ��**һ��Э��**��ÿ�������õ� IPv4 ��ַ����һ�������ڵ㣬ȫ���ڵ�ͳһʹ��ѡ��Э�顣�ڵ����֧�ָ�Э��Ĳ�ͬ�����ͻ�����ʹ�á�

����ѡ�� Hysteria2��ѡ�� 10 �� IP�������� 10 �� Hysteria2 �ڵ㡣����Э��ʱ�����滻�����߲���ͬʱ���õڶ���Э�顣

֧�� **SOCKS5��Shadowsocks��VMess��VLESS��Trojan��Hysteria2��TUIC��WireGuard** �� 8 �ࡣVLESS ʹ�� REALITY��VMess �� Trojan ʹ�� TLS�������ֶ�ѡ���װ�������˺��ġ�

## ѡ����Ͳ˵�˳��

�˵�����������ʶ������̽�⣬�Լ� UDP ���޻����µĿ����������������**���̲ο�˳��**��������������ѡ�ͽ��飬û�п�����ʵ������֧��һ�����á�ͨ�õ�������

| �˵� | Э�� | ���������� | ��Ϸ UDP ���� |
|---|---|---|---|
| 1 | VLESS + REALITY | Ĭ�Ͻ��飻TLS ��ۡ�δ��֤̽����䣻ʹ�� TCP | ���Դ��� UDP������� TCP ���ش��ȴ��������Ӷ��� |
| 2 | Trojan + TLS | HTTPS ��ۣ�����������ҳ���䣻��ƥ�������Ŀ���֤�� | UDP �� TCP ���� |
| 3 | Hysteria2 | HTTP/3 ��ۣ���ѡ Salamander ������UDP/QUIC ����ɴ� | ��Ϸ���Ⱥ�ѡ��ʹ�����ݱ�ת�����Ƿ����Ӧʵ�� |
| 4 | VMess + TLS | TLS ����� AEAD��û�ж�����ҳ���� | UDP �� TCP ���� |
| 5 | TUIC v5 | TLS/QUIC��û��������ҳ�������������� UDP | native ���ݱ�ת�����ر� 0-RTT |
| 6 | Shadowsocks AEAD | ����ѡ����ܵ�û�б�׼ TLS/HTTP ��� | ֱ�Ӵ��� UDP |
| 7 | WireGuard | ��׼Э�鲻�ṩ�������������� UDP | �û�̬�����˵㣬ת�� IPv4 TCP/UDP |
| 8 | SOCKS5 | �û���/������֤���˺ź������������ܣ�û��αװ | ʹ��Э�̵Ķ�̬ UDP �м̶˿� |

**����������ʱĬ��ѡ REALITY����Ϸ UDP �������������� UDP ʱ����ѡ Hysteria2��** ���Զ����ӱ���Э�顣Salamander �ı� QUIC ��ۣ����ܰ����û������������������Ϊ��׼ HTTP/3 ��ۡ�IP �ѱ�����ʱ����Э����޷���֤�ָ���

�������������Ŀ�ٷ�˵������������Ŀ�����������������ܻ򿹷���ʵ������

- [REALITY](https://github.com/XTLS/REALITY)
- [sing-box REALITY/TLS](https://sing-box.sagernet.org/configuration/shared/tls/)
- [Hysteria2 Э��](https://v2.hysteria.network/docs/developers/Protocol/)
- [TUIC ���ݱ�ģʽ](https://sing-box.sagernet.org/configuration/outbound/tuic/)
- [QUIC ���ݱ� RFC 9221](https://www.rfc-editor.org/rfc/rfc9221.html)
- [WireGuard ����֪����](https://www.wireguard.com/known-limitations/)

## Debian ��װ

�� GitHub ���ض�����װ��ڣ��ڷ����������У�

```sh
curl -fL https://github.com/qq1527949891/singboxtoips/releases/latest/download/install.sh -o install.sh
sudo sh install.sh
```

�ű��Զ�ʶ�� amd64/arm64����ѯ�òֿ����� Release������Ѱ�װ��ͬ�汾���������г��򣻷������ض�Ӧ�� `mip-proxy-linux-<�ܹ�>.tar.gz`���˶� Release �� `SHA256SUMS` ��װ���ٽ������Ĳ˵�������ʧ�ܻ�У�鲻������ֹ��װ���ű�֧��ͨ�� `MIP_VERSION` �̶��汾��

```sh
sudo env MIP_VERSION=v0.1.1 sh install.sh
sudo sh install.sh --no-menu
sh install.sh --download-only ./packages
```

`--no-menu` ����װ��������`--download-only` ���ز�У��ѹ���������� root�����޸ķ��񡣽���װ�������򲻻���� IPv6����ʽ����ڵ�ʱ��ִ�С�

Ҳ�����ֶ��� [Releases](https://github.com/qq1527949891/singboxtoips/releases) ���ض�Ӧ�ܹ��ķ��������ϴ������������� amd64��

```sh
tar -xzf mip-proxy-linux-amd64.tar.gz
cd mip-proxy-linux-amd64
sudo sh install.sh
```

���� `install.sh` ����ʹ�ñ��ع������򣬲��ظ�����ѹ������Ҳ����ֱ�����а��ڵĹ�������

```sh
sudo ./mip-proxy
```

����ʱʹ�ùٷ� sing-box **1.14.4**���Ⱥ˶Թ̶� SHA256���ٰ�װ������·����������ϵͳԭ�е� sing-box �� Xray��������еĹ����������� Go��Python ���������ʱ���״����غ�����Ҫ���� GitHub��

����˳��

1. �Ӱ��������������еĲ˵�ѡ��**һ��**Э�顣
2. �Զ���ȡ���� IPv4 ��·�ɣ�ѡ����Ҫ�� IP��
3. ȷ��ÿ������ IP ��Ӧ�Ĺ��� IP�������˿ڣ��������ö���������·�ɡ�
4. ��д��Э������ SNI��֤�����Ϣ��
5. �鿴��������/����ժҪ��ѡ�����ֻ����Ԥ����

�� Ctrl+C ȡ������ʽ�޸ķ��������ü��ͱ������֮�󡣲���ʧ�᳢ܻ�Իָ������á���ƾ�ݼ�ԭ�з���״̬���ָ����󲻻����ء�

## IP ������Ҫ��

- ��ʹ�� **IPv4**����ʽ����������̨�������� IPv6�����ڵ�Ҳ�ܾ� IPv6 Ŀ�ꡣIP ������������ƽ̨���䣬����ȷ���õ����������߲������Ը������ӵ�ַ��
- ֧�ֹ��� IP ֱ�Ӱ󶨡�˽�� IP ��Ӧ���� EIP���Լ�������/�����ء�NAT ����ʵ��֧���������վ�ͳ�վӳ�䡣���ж��������վ��ʵ�ʹ���һ����վ�� NAT�������ɴ������ñ�ɶ���������ڡ�
- ��ָ��������ַ��ѯ���Ĺ��� IP���������۲⵽�ĳ��ڣ�����֤����վ NAT ��ȫ������ȷ���á�
- ÿ���ڵ�󶨶�Ӧ������ַ��վ���� WireGuard �⣬��վ��������������ַ����ͬ IP ���Ը��ö˿ڡ�WireGuard �ĺ���ʹ��ͨ�� UDP ��������˰��ڵ� ID ��������˿ڣ���������·�ɹ̶����ֻذ���Դ IP������ժҪ���г��� IP:�˿�Ϊ׼��
- ������������һ��ֻ���ַ�󶨡�������/�����ؿɿ�������·�ɣ�����Դ��ַ�� fwmark ���ֹ��򣬼�˴�����վ�Ϳͻ������ӻس̡��Զ��������Դ��ַ·�����ȼ���Ƿ���˷������ݡ�
- ֻ���������ߵǼǵľ�ȷ IPv4 ����/·�ɣ�������ŷ�Χ���������޸� IPv4 ��·�ɱ��� Swap��ȫ�� IPv6 ���صĹ��������ġ�
- ϵͳ����ǽ���ư�ȫ����Ҫ������Ӧ�˿ڡ����߲�����ջ��滻���з���ǽ��REALITY/Trojan/VMess Ϊ TCP��Hysteria2/TUIC/WireGuard Ϊ UDP��Shadowsocks Ϊ TCP+UDP��
- SOCKS5 �ļ����˿��� TCP��UDP ASSOCIATE ����Э�̶�̬ UDP �м̶˿ڣ�������ͬ�˿� UDP �������ɰ����ſͻ�����Դ���ж�̬ UDP �˿ڡ�NAT ���豣����Щ�˿ڣ��ҿͻ������ܴ�������˷��ص�˽���м̵�ַ��ֻ�й̶��˿�ӳ��ʱ���ܱ�֤ SOCKS5 UDP ���á�SOCKS5 ����Ϊ�������Ƽ���
- ͬһ�������Ķ�� IP ͨ������������·�ʹ��������� IP �����Զ�������ͬ�ļ�����·��

�Զ�̽�ⲻ����ʱ������ `samples/ip-map.csv` ������ʵ�����ݣ�

```sh
sudo mip-proxy wizard --inventory /root/ip-map.csv --apply
```

CSV ��ͷ��`local_ip,public_ip,interface,gateway,prefix`���� 3 ����ڲ���·����Ҫʱ��д��ʾ���еĵ�ַ���ĵ�������ַ������ֱ�Ӳ����ظ����ڻᱣ��Ϊ���д���飬���Ḳ�ǻ�ð�������������� IP��

### IPv6 �Ĵ���

��ʽ�������� `net.ipv6.conf.all.disable_ipv6=1` �� `net.ipv6.conf.default.disable_ipv6=1`������֤�������нӿھ��ѽ��á����ñ��浽 `/etc/sysctl.d/99-mip-proxy-ipv6.conf`��ÿ�δ�����������ǰ�ٴμ�飬���������������״̬���˲���Ӱ����̨������������ԭ�� IPv6 ��ַ�����ӣ�Ԥ�������޸���Щ���á�

������ڽڵ�·��ǰ�ܾ� IPv6 Ŀ�꣬DNS �����ͳ�վʹ�� IPv4��Mihomo ������������ `ipv6: false`��sing-box �ͻ���Ҳʹ�� IPv4 DNS ���Բ��ܾ� IPv6�������ķ������ӻ� provider �޷����Ƶ������ͻ��˵�ȫ�� IPv6 ���ã���������Ի�ܾ��ڵ��ڵ� IPv6 ����

����ǰ����ԭ�� IPv6 ���ء���ַ��·�ɡ�����ʧ�ܻ�ִ�� `rollback` ʱ�ָ�����ǰ״̬���������ǰ�ѽ��� IPv6���򱣳ֽ��á�

## ������֤��

REALITY ����Ҫ����֤�飬��Ҫ�������ɷ��ʵ� TLS ����Ŀ�ꣻSNI������Ŀ�������������д��Ĭ��Ŀ��ֻ����㣬Ӧ�������Ŀ��������ϵ� TLS �����ԺͿɴ��ԡ�

Trojan��VMess��Hysteria2 �� TUIC Ҫ������ PEM ֤����������˽Կ����֤��ƥ����д�� SNI�����нڵ�������Ӹ��Թ��� IP������ͬһ����֤�顣�ͻ��� TLS У�鱣�ֿ����������� `insecure=1` �� `skip-cert-verify: true`��

Hysteria2 ʹ�� ECDSA �� RSA ֤�飬���߻�ܾ��뵱ǰ���� Chrome ����ģ�ⲻ���ݵ� Ed25519 ֤�顣

֤��������� ACME ����ǩ�������ڡ�֤����º������������������֤�飻�����������֤���滻���ڵ�ƾ�ݱ��ֲ��䣺

```sh
sudo mip-proxy apply --state /var/lib/mip-proxy/state.json
```

���汾���Զ����������֤�顣���� demo ʹ�õĶ�����ǩ֤��������ü��/���ԣ�����������֤�顣

## �ڵ㵼��

Ĭ��Ŀ¼ `/var/lib/mip-proxy/export`��Ŀ¼Ȩ�� 0700���ļ�Ȩ�� 0600��

| �ļ� | ��; |
|---|---|
| `links.txt` | ÿ��һ���������� |
| `subscription.txt` | Base64 �������Ӽ��ϣ��ͻ�����ʶ����ӦЭ�� |
| `mihomo.yaml` | �ɵ�������� Mihomo ���ã��ֶ�ѡ��ڵ� |
| `mihomo-provider.yaml` | Mihomo proxy-provider ���� |
| `sing-box.json` | �ɵ���� sing-box �ͻ������ã��ֶ�ѡ��ڵ� |
| `nodes.csv` | IP���˿ڡ�Э�鼰���������嵥 |
| `<����IP>-wireguard.conf` | WireGuard ԭ���ͻ������ã�����ѡ�� WireGuard ʱ���� |

WireGuard û��ͳһ���� URI����˸�Э�鲻���� `links.txt`��`subscription.txt`��ʹ�� `.conf` �� Mihomo/sing-box ���õ��롣SOCKS5 �ķ������Ӹ�ʽ��ͻ��˶��죬�����ȵ��� YAML/JSON ���ֶ���д�˺š�����Э��ʱ���Ƴ��������ϴ����ɵľɵ����ļ�������Ŀ¼�е������ļ���

�������ڵ����ͨ�ͻ������ã������þ�����Ϸ��ӹܿͻ�����û���Զ������Ķ��� HTTP ���񣻵����ļ���ƾ�ݣ������а�ȫ���䡣�ͻ��˱���֧����ѡЭ�飬�����ò�֧��Э��Ŀͻ���ͨ����ʽת�����֧�֡�

���������򵼡���ɾ IP ���������Э������� IP �� ID ��ƾ�ݻᱣ�������ƾ�ݶ������ɣ�REALITY ˽Կֻ�����ڷ�����״̬/����/�����У�������ͻ��˵�����

### WireGuard �ľ�����Ϊ

ÿ�� IP �� UDP �˿�Ϊ `��ʼ�˿� + �ڵ� ID - 1`��ɾ���ڵ�󲻻��������ж˿ڡ���ʹֻ��һ��������Ҳ�����ø� IP �Ĳ���·�ɣ��Թ̶� UDP ���ֻذ���Դ IP�������ø� IP �����������������Ρ����ĵײ�˿�ʹ��ͨ�����������ʱӦ������ IP:�˿��ڷ���ǽ��������ڡ�

ÿ�� IP ����һ���ͻ��� peer��ͬһ���ò�Ҫ�ڶ�̨�豸ͬʱ���ã����� WireGuard �Ķ˵����λụ����ռ���ӡ��������������� peer������ `doctor` ����ռ���û��� peer�����˽Կֻ�����ڷ�������������ͻ��˵�����

�����ʹ�� sing-box �û�̬ WireGuard �˵�ת�� IPv4 TCP/UDP�����谲װ�ں� WireGuard������ȫ�� IP ת��������ȫ�� MASQUERADE���˰汾���ṩ�������ں� VPN��IPv6 �� ICMP ��Ϊ��֤��

## ״̬�������ع�

```sh
sudo mip-proxy status
sudo mip-proxy export
sudo mip-proxy doctor
sudo mip-proxy rollback
sudo journalctl -u mip-proxy.service -n 100 --no-pager
```

`doctor` �ڷ���������������ʱ�ͻ��ˣ���ÿ���ڵ��ʵ��Э������֤��TCP ת���͹������ڣ�ͨ�� SOCKS5 UDP ASSOCIATE �򹫹� STUN ������ 3 ��̽�⣬��� UDP ת�����������ڡ����̽�����������ʱ�ͻ��ˡ�STUN ����ӦҲ�������Թ���������������ƣ���ֱ�ӵȼ���Э��ʵ�������⡣

������鲻��֤�������������ڵĿɴ��ԣ�����Ҫ��������������ӡ������ STUN ����ʱ�Ӵӷ����������������������û����ڵ����Ϸ�ӳ١�

����ֻ������Щ����

- `mip-proxy.service`���������ġ�
- `mip-proxy-routes.service`��network-online ��ָ��Ǽǵľ�ȷ����·�ɣ����ڴ�������������
- `mip-proxy-cover.service`��Trojan ʹ�õı��� HTTP ����ҳ�������� `127.0.0.1:18080`��

���� `/etc/mip-proxy/server.json`��״̬/���� `/var/lib/mip-proxy`���������� `/usr/local/bin/mip-proxy`���������� `/usr/local/lib/mip-proxy/sing-box`������·�ɻָ�ֻʹ��״̬��������ԭʼ֤���ļ���

��������ͨ���ļ�������ִ�У�Ĭ��״̬�ĵ����ͼ��Ҳ����������������ǰ�־û��ָ���¼���жϺ���һ�� apply/rollback ���Ȼָ�δ�������`rollback` �ָ���һ�β���ǰ���ļ�������IPv6 ״̬�ͷ�������״̬������·���ͻָ��ļ��������ڱ����߹�����Χ������·��ǰ�˶������������������Ĺ��Ĺ����·�ɻᱣ���������ͻ��

## ��������֤

```sh
go test ./...
go vet ./...
python3 scripts/build.py
```

����ʹ�� Go 1.27.3������ű�ֻ�ڿ��������У����� `dist/` �� amd64/arm64 �ľ�̬ Linux ���򡢰�װѹ������Դ��������� `install.sh` �� `SHA256SUMS`����������ѹ������ `install.sh`������Ҫ Go �� Python��

GitHub �ֿ���Դ��λ�� `multi-ip-proxy/`����Ŀ¼�� `.github/workflows/mip-proxy-release.yml` ��Դ����º��Զ�������������֤��ͨ���󰴳���汾���� Release���Ѵ��ڵİ汾���Ḳ�ǣ��޸ĺ󷢲�����������汾��

�� Linux ��������װ Go��Python 3��iproute2��procps��curl �󣬿�����������֤����Ҫ root �� sudo �������������ռ䣩��

```sh
python3 scripts/build.py
sh scripts/verify-linux.sh
```

��֤�ű�ʹ�ù̶��汾���̶� SHA256 �ĺ��ĺ� Mihomo�����и�Э��ת������ȫ���� IPv6 ���ת��������·�ɡ�IPv6 ������ָ����Լ��Զ���װ/����/У��ʧ�ܲ��ԡ�

�������� 8 ��**�����ĵ�Э��**ʾ�������ù̶��汾���ļ����ݷ���˼��ͻ������ã�

```sh
./mip-proxy core --out ./sing-box
./mip-proxy demo --protocol all --out ./demo
./sing-box check -c ./demo/reality/server.json
./sing-box check -c ./demo/reality/export/sing-box.json
```

`all` ֻ�������ɻ������Ĳ���ʾ������ʵ״̬���򵼲����ܶ�Э�顣

��ʵ���ļ��ɲ������� 2 ������ IP �ڵ㣬�ֱ���֤ TCP �� 64/1200 �ֽ� UDP ����ת����ʵ��Դ��ַ������ `MIP_MIHOMO` ʱ��Ҳ��ʹ�� Mihomo ��֤���ڵ�� TCP/UDP ת��������֤�����ν������ڲ��������С�

```sh
MIP_CORE=/absolute/path/to/sing-box go test -v ./integration
```

����ȫ�� 8 �ࣨ�� WireGuard��ʱ�����ڶ��� Linux ���������ռ���ִ�С�WireGuard ���Ի�����������ռ�ı���·�����ȼ�����˽ű�����Զ��������������

```sh
go test -c -o /tmp/mip-core.test ./integration
sudo env MIP_CORE=/absolute/path/to/sing-box MIP_MIHOMO=/absolute/path/to/mihomo \
  unshare -n sh integration/run-isolated.sh /tmp/mip-core.test -test.v -test.timeout=120s
```

Linux ·�ɲ��Ա�����ȫ�����������ռ���ִ�У���֤�ظ�Ӧ�á���ȷɾ���������ѡ·�뱣���޹�·�ɱ���

```sh
go test -c -o /tmp/mip-network.test ./internal/network
sudo env MIP_ROUTE_INTEGRATION=1 unshare -n /tmp/mip-network.test -test.run TestLinuxRoutesIntegration -test.v
```

ʵ�ʿ繫����ͨ�ԡ���ƽ̨ NAT����ȫ�鼰���������еĿ��������֣�������Ŀ����������ⲿ�������ա�

������֤�� Debian 13 �ĸ������������ռ���ɡ�amd64 �汾���й���ʵ���ĺͿͻ���ת�����ԣ�arm64 Ϊ�����������δ�� arm64 ������ʵ�⡣��ǰ���Ի���δ���� systemd���������������ʧ�ܻع�ͨ��ģ�������֤����ʽ�������ϵķ������������������ա�
