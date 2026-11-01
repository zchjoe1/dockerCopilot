package module

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"time"
)

// registryhttp.go —— 访问 registry 用的 HTTP 客户端（2026-11-01）
//
// 为什么单独一个客户端：这台 NAS 的 **IPv6 通路不通**，而 registry-1.docker.io 会返回
// AAAA 记录，Go 默认双栈拨号会先试 IPv6 并一直等到超时：
//
//	dial tcp [2a03:2880:f11b:83:face:b00c:0:25de]:443: i/o timeout
//
// 表现就是「检查更新全部失败 / 一直 401 / 一直 context deadline exceeded」——
// 之前那些加速站（docker.1ms.run 等）本质上就是在绕这个。
// 修法：拨号固定用 tcp4（IPv4），其它保持标准行为。

// newIPv4Transport 返回一个强制 IPv4 拨号的 Transport。
func newIPv4Transport(insecureTLS bool) *http.Transport {
	dialer := &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
	tr := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.DialContext(ctx, "tcp4", addr)
		},
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          32,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	if insecureTLS {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	return tr
}

// registryClient 是给 registry / token 端点用的客户端（强制 IPv4）。
func registryClient(timeout time.Duration, insecureTLS bool) *http.Client {
	return &http.Client{Transport: newIPv4Transport(insecureTLS), Timeout: timeout}
}

// 占位：保留 io 引用，避免后续删代码时 import 报错（本文件暂时不用 io）
var _ = io.Discard
