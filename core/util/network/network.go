package network

import (
	"net"
	"os"
	"strings"
)

// IPv4 获取本机 IPv4 地址
// 通过连接外部 DNS 服务器来确定本地网络接口的 IP
func IPv4() string {
	conn, err := net.Dial("udp", "8.8.8.8:53")
	if err != nil {
		return ""
	}
	defer conn.Close()

	addr := conn.LocalAddr().(*net.UDPAddr)
	ip := strings.Split(addr.String(), ":")[0]
	return ip
}

// Hostname 获取主机名
func Hostname() string {
	hostname, err := os.Hostname()
	if err != nil {
		return ""
	}
	return hostname
}

// LocalIP 获取本地 IP 地址（别名）
func LocalIP() string {
	return IPv4()
}
