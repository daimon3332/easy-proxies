package builder

import (
	"fmt"

	"easy_proxies/internal/config"

	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/auth"
)

func BuildSingleNodeOutbound(tag, uri string, skipCertVerify bool) (option.Outbound, error) {
	return buildNodeOutbound(tag, uri, skipCertVerify)
}

func NodeTag(name string) string {
	return sanitizeTag(name)
}

func BuildMultiPortInbound(cfg *config.Config, node config.NodeConfig, outboundTag string) (option.Inbound, error) {
	addr, err := parseAddr(cfg.MultiPort.Address)
	if err != nil {
		return option.Inbound{}, fmt.Errorf("parse multi-port address: %w", err)
	}
	options := &option.HTTPMixedInboundOptions{
		ListenOptions: option.ListenOptions{Listen: addr, ListenPort: node.Port},
	}
	if cfg.MultiPort.Username != "" {
		options.Users = []auth.User{{Username: cfg.MultiPort.Username, Password: cfg.MultiPort.Password}}
	}
	return option.Inbound{Type: C.TypeMixed, Tag: "in-" + outboundTag, Options: options}, nil
}
