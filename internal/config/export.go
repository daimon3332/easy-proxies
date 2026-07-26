package config

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type clashExportConfig struct {
	Proxies []clashExportProxy `yaml:"proxies"`
}

type clashExportProxy struct {
	Name              string                     `yaml:"name"`
	Type              string                     `yaml:"type"`
	Server            string                     `yaml:"server"`
	Port              int                        `yaml:"port"`
	UUID              string                     `yaml:"uuid,omitempty"`
	Username          string                     `yaml:"username,omitempty"`
	Password          string                     `yaml:"password,omitempty"`
	Cipher            string                     `yaml:"cipher,omitempty"`
	AlterID           int                        `yaml:"alterId,omitempty"`
	Network           string                     `yaml:"network,omitempty"`
	TLS               bool                       `yaml:"tls,omitempty"`
	SkipCertVerify    bool                       `yaml:"skip-cert-verify,omitempty"`
	ServerName        string                     `yaml:"servername,omitempty"`
	SNI               string                     `yaml:"sni,omitempty"`
	Flow              string                     `yaml:"flow,omitempty"`
	UDP               bool                       `yaml:"udp,omitempty"`
	UDPOverTCP        bool                       `yaml:"udp-over-tcp,omitempty"`
	WSOpts            *clashExportWSOptions      `yaml:"ws-opts,omitempty"`
	GrpcOpts          *clashExportGRPCOptions    `yaml:"grpc-opts,omitempty"`
	RealityOpts       *clashExportRealityOptions `yaml:"reality-opts,omitempty"`
	ClientFingerprint string                     `yaml:"client-fingerprint,omitempty"`
	Obfs              string                     `yaml:"obfs,omitempty"`
	ObfsPassword      string                     `yaml:"obfs-password,omitempty"`
	Ports             string                     `yaml:"ports,omitempty"`
	Plugin            string                     `yaml:"plugin,omitempty"`
	PluginOpts        map[string]string          `yaml:"plugin-opts,omitempty"`
	ALPN              []string                   `yaml:"alpn,omitempty"`
	CongestionControl string                     `yaml:"congestion-controller,omitempty"`
	UDPRelayMode      string                     `yaml:"udp-relay-mode,omitempty"`
}

type clashExportWSOptions struct {
	Path    string            `yaml:"path,omitempty"`
	Headers map[string]string `yaml:"headers,omitempty"`
}

type clashExportGRPCOptions struct {
	ServiceName string `yaml:"grpc-service-name"`
}

type clashExportRealityOptions struct {
	PublicKey string `yaml:"public-key,omitempty"`
	ShortID   string `yaml:"short-id,omitempty"`
}

func ExportClashYAML(nodes []NodeConfig) ([]byte, error) {
	proxies := make([]clashExportProxy, 0, len(nodes))
	for i, node := range nodes {
		proxy, err := nodeToClashProxy(node)
		if err != nil {
			return nil, fmt.Errorf("节点 %q 导出失败: %w", node.Name, err)
		}
		if proxy.Name == "" {
			proxy.Name = fmt.Sprintf("node-%d", i+1)
		}
		proxies = append(proxies, proxy)
	}
	var document yaml.Node
	if err := document.Encode(clashExportConfig{Proxies: proxies}); err != nil {
		return nil, err
	}
	setProxyFlowStyle(&document)
	return yaml.Marshal(&document)
}

func setProxyFlowStyle(document *yaml.Node) {
	if document == nil {
		return
	}
	root := document
	if document.Kind == yaml.DocumentNode && len(document.Content) > 0 {
		root = document.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != "proxies" || root.Content[i+1].Kind != yaml.SequenceNode {
			continue
		}
		for _, proxy := range root.Content[i+1].Content {
			proxy.Style = yaml.FlowStyle
		}
		return
	}
}

func nodeToClashProxy(node NodeConfig) (clashExportProxy, error) {
	raw := strings.TrimSpace(node.URI)
	if strings.HasPrefix(strings.ToLower(raw), "vmess://") {
		return vmessToClash(node.Name, raw)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return clashExportProxy{}, err
	}
	host, port, err := uriEndpoint(u)
	if err != nil && strings.EqualFold(u.Scheme, "ss") {
		return shadowsocksToClash(node.Name, raw)
	}
	if err != nil {
		return clashExportProxy{}, err
	}
	name := node.Name
	if name == "" {
		name, _ = url.QueryUnescape(u.Fragment)
	}
	q := u.Query()
	p := clashExportProxy{Name: name, Type: strings.ToLower(u.Scheme), Server: host, Port: port}
	applyTransport(&p, q)
	p.ClientFingerprint = q.Get("fp")
	p.SkipCertVerify = queryBool(q, "allowInsecure", "insecure")
	p.ServerName = firstQuery(q, "sni", "peer")
	switch p.Type {
	case "vless":
		p.UUID = userValue(u)
		p.Flow = q.Get("flow")
		security := strings.ToLower(q.Get("security"))
		p.TLS = security == "tls" || security == "reality"
		if security == "reality" {
			p.RealityOpts = &clashExportRealityOptions{PublicKey: q.Get("pbk"), ShortID: q.Get("sid")}
		}
	case "trojan":
		p.Password = userValue(u)
	case "anytls":
		p.Password = userValue(u)
	case "hysteria2", "hy2":
		p.Type = "hysteria2"
		p.Password = userValue(u)
		p.Obfs = q.Get("obfs")
		p.ObfsPassword = q.Get("obfs-password")
		p.Ports = strings.ReplaceAll(firstQuery(q, "ports", "server_ports", "mport"), ":", "-")
	case "tuic":
		p.UUID = u.User.Username()
		p.Password, _ = u.User.Password()
		p.CongestionControl = firstQuery(q, "congestion_control", "congestion-controller")
		p.UDPRelayMode = firstQuery(q, "udp_relay_mode", "udp-relay-mode")
		if value := q.Get("alpn"); value != "" {
			p.ALPN = splitNonEmpty(value, ",")
		}
	case "ss":
		return shadowsocksToClash(name, raw)
	case "http", "https":
		p.Type = "http"
		p.TLS = strings.EqualFold(u.Scheme, "https")
		p.Username = u.User.Username()
		p.Password, _ = u.User.Password()
	case "socks", "socks5":
		p.Type = "socks5"
		p.Username = u.User.Username()
		p.Password, _ = u.User.Password()
	default:
		return clashExportProxy{}, fmt.Errorf("YAML 暂不支持协议 %q", u.Scheme)
	}
	return p, nil
}

func vmessToClash(name, raw string) (clashExportProxy, error) {
	payload := strings.TrimPrefix(raw, "vmess://")
	if decoded, err := decodeBase64(payload); err == nil && len(decoded) > 0 && decoded[0] == '{' {
		var value struct {
			PS   string      `json:"ps"`
			Add  string      `json:"add"`
			Port interface{} `json:"port"`
			ID   string      `json:"id"`
			Aid  interface{} `json:"aid"`
			Net  string      `json:"net"`
			Type string      `json:"type"`
			Host string      `json:"host"`
			Path string      `json:"path"`
			TLS  string      `json:"tls"`
			SNI  string      `json:"sni"`
			ALPN string      `json:"alpn"`
			FP   string      `json:"fp"`
			Scy  string      `json:"scy"`
		}
		if err := json.Unmarshal(decoded, &value); err != nil {
			return clashExportProxy{}, err
		}
		port, err := flexibleInt(value.Port)
		if err != nil {
			return clashExportProxy{}, err
		}
		alterID, _ := flexibleInt(value.Aid)
		if name == "" {
			name = value.PS
		}
		p := clashExportProxy{Name: name, Type: "vmess", Server: value.Add, Port: port, UUID: value.ID, AlterID: alterID, Cipher: value.Scy, Network: value.Net, TLS: value.TLS != "", ServerName: value.SNI, ClientFingerprint: value.FP}
		if p.Cipher == "" {
			p.Cipher = "auto"
		}
		if p.Network == "ws" {
			p.WSOpts = &clashExportWSOptions{Path: value.Path}
			if value.Host != "" {
				p.WSOpts.Headers = map[string]string{"Host": value.Host}
			}
		} else if p.Network == "grpc" && value.Path != "" {
			p.GrpcOpts = &clashExportGRPCOptions{ServiceName: value.Path}
		}
		return p, nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return clashExportProxy{}, err
	}
	host, port, err := uriEndpoint(u)
	if err != nil {
		return clashExportProxy{}, err
	}
	if name == "" {
		name, _ = url.QueryUnescape(u.Fragment)
	}
	q := u.Query()
	p := clashExportProxy{Name: name, Type: "vmess", Server: host, Port: port, UUID: userValue(u), Cipher: "auto", TLS: strings.EqualFold(q.Get("security"), "tls"), ServerName: q.Get("sni"), ClientFingerprint: q.Get("fp")}
	applyTransport(&p, q)
	return p, nil
}

func shadowsocksToClash(name, raw string) (clashExportProxy, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return clashExportProxy{}, err
	}
	if u.Host == "" {
		payload := strings.TrimPrefix(strings.SplitN(raw, "#", 2)[0], "ss://")
		decoded, err := decodeBase64(payload)
		if err != nil {
			return clashExportProxy{}, errorsForURI("invalid shadowsocks payload")
		}
		u, err = url.Parse("ss://" + string(decoded))
		if err != nil {
			return clashExportProxy{}, err
		}
	}
	host, port, err := uriEndpoint(u)
	if err != nil {
		return clashExportProxy{}, err
	}
	userinfo := u.User.String()
	if decoded, err := decodeBase64(userinfo); err == nil {
		userinfo = string(decoded)
	}
	parts := strings.SplitN(userinfo, ":", 2)
	if len(parts) != 2 {
		return clashExportProxy{}, errorsForURI("invalid shadowsocks credentials")
	}
	if name == "" {
		name, _ = url.QueryUnescape(u.Fragment)
	}
	q := u.Query()
	p := clashExportProxy{Name: name, Type: "ss", Server: host, Port: port, Cipher: parts[0], Password: parts[1], UDP: true, UDPOverTCP: queryBool(q, "udp-over-tcp")}
	plugin := q.Get("plugin")
	if plugin != "" {
		if plugin == "obfs-local" {
			plugin = "obfs"
		}
		p.Plugin = plugin
		options := make(map[string]string)
		for _, item := range strings.Split(q.Get("plugin_opts"), ";") {
			key, value, ok := strings.Cut(item, "=")
			if !ok {
				continue
			}
			switch key {
			case "obfs":
				options["mode"] = value
			case "obfs-host":
				options["host"] = value
			}
		}
		if len(options) > 0 {
			p.PluginOpts = options
		}
	}
	return p, nil
}

func applyTransport(p *clashExportProxy, q url.Values) {
	network := strings.ToLower(firstQuery(q, "type", "network"))
	if network == "" || network == "tcp" {
		return
	}
	p.Network = network
	switch network {
	case "ws":
		p.WSOpts = &clashExportWSOptions{Path: q.Get("path")}
		if host := q.Get("host"); host != "" {
			p.WSOpts.Headers = map[string]string{"Host": host}
		}
	case "grpc":
		if service := firstQuery(q, "serviceName", "service_name"); service != "" {
			p.GrpcOpts = &clashExportGRPCOptions{ServiceName: service}
		}
	}
}

func uriEndpoint(u *url.URL) (string, int, error) {
	host := u.Hostname()
	portText := u.Port()
	if host == "" || portText == "" {
		return "", 0, errorsForURI("missing server or port")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return "", 0, errorsForURI("invalid port")
	}
	return host, port, nil
}

func userValue(u *url.URL) string {
	if u.User == nil {
		return ""
	}
	return u.User.Username()
}

func queryBool(values url.Values, keys ...string) bool {
	value := strings.ToLower(firstQuery(values, keys...))
	return value == "1" || value == "true" || value == "yes"
}

func firstQuery(values url.Values, keys ...string) string {
	for _, key := range keys {
		if value := values.Get(key); value != "" {
			return value
		}
	}
	return ""
}

func splitNonEmpty(value, separator string) []string {
	parts := strings.Split(value, separator)
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			result = append(result, part)
		}
	}
	return result
}

func decodeBase64(value string) ([]byte, error) {
	value = strings.TrimSpace(strings.SplitN(value, "#", 2)[0])
	encodings := []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding}
	for _, encoding := range encodings {
		if decoded, err := encoding.DecodeString(value); err == nil {
			return decoded, nil
		}
	}
	return nil, errorsForURI("invalid base64")
}

func flexibleInt(value interface{}) (int, error) {
	switch typed := value.(type) {
	case float64:
		return int(typed), nil
	case int:
		return typed, nil
	case string:
		return strconv.Atoi(typed)
	case nil:
		return 0, nil
	default:
		return strconv.Atoi(fmt.Sprint(value))
	}
}

func errorsForURI(message string) error {
	return fmt.Errorf("%s", message)
}
