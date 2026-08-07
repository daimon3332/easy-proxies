package importer

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"easy_proxies/internal/config"
)

func parseHostPortList(content, protocol string) ([]config.NodeConfig, error) {
	protocol = strings.ToLower(strings.TrimSpace(protocol))
	if protocol != "http" && protocol != "socks5" {
		return nil, fmt.Errorf("代理列表协议必须为 HTTP 或 SOCKS5")
	}
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	nodes := make([]config.NodeConfig, 0, len(lines))
	seen := make(map[string]struct{}, len(lines))
	for index, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		uri, err := normalizeHostPortLine(line, protocol)
		if err != nil {
			return nil, fmt.Errorf("第 %d 行：%s", index+1, err)
		}
		if _, exists := seen[uri]; exists {
			continue
		}
		seen[uri] = struct{}{}
		nodes = append(nodes, config.NodeConfig{Name: fmt.Sprintf("proxy-%d", index+1), URI: uri})
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("代理列表中没有有效节点")
	}
	return nodes, nil
}

func normalizeHostPortLine(line, protocol string) (string, error) {
	credentials := ""
	address := line
	if at := strings.LastIndex(line, "@"); at >= 0 {
		credentials = line[:at]
		address = line[at+1:]
		if credentials == "" || !strings.Contains(credentials, ":") {
			return "", fmt.Errorf("认证格式无效")
		}
	}
	host, rawPort, err := net.SplitHostPort(address)
	if err != nil || strings.TrimSpace(host) == "" {
		return "", fmt.Errorf("地址或端口格式无效")
	}
	port, err := strconv.Atoi(rawPort)
	if err != nil || port < 1 || port > 65535 {
		return "", fmt.Errorf("端口超出范围")
	}
	parsed := &url.URL{Scheme: protocol, Host: net.JoinHostPort(host, strconv.Itoa(port))}
	if credentials != "" {
		username, password, _ := strings.Cut(credentials, ":")
		if username == "" {
			return "", fmt.Errorf("用户名不能为空")
		}
		parsed.User = url.UserPassword(username, password)
	}
	return parsed.String(), nil
}
