package builder

import (
	"fmt"
	"net/url"
	"strings"

	"easy_proxies/internal/config"

	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/auth"
)

func BuildSingleNodeOutbound(tag, uri string, skipCertVerify bool) (option.Outbound, error) {
	return buildNodeOutbound(tag, uri, skipCertVerify)
}

func SetOutboundDetour(outbound *option.Outbound, detour string) error {
	wrapper, ok := outbound.Options.(option.DialerOptionsWrapper)
	if !ok {
		return fmt.Errorf("outbound type %q does not support detour", outbound.Type)
	}
	dialerOptions := wrapper.TakeDialerOptions()
	dialerOptions.Detour = detour
	wrapper.ReplaceDialerOptions(dialerOptions)
	return nil
}

func BuildChainOutbounds(terminalTag, terminalURI string, hopURIs []string, skipCertVerify bool) ([]option.Outbound, error) {
	outbounds := make([]option.Outbound, 0, len(hopURIs)+1)
	previousTag := ""
	for index, hopURI := range hopURIs {
		if previousTag != "" {
			if err := validateDetourTransport(hopURI, hopURIs[index-1]); err != nil {
				return nil, fmt.Errorf("chain hop %d: %w", index+1, err)
			}
		}
		tag := fmt.Sprintf("%s-hop-%d", terminalTag, index+1)
		outbound, err := buildNodeOutbound(tag, hopURI, skipCertVerify)
		if err != nil {
			return nil, fmt.Errorf("build chain hop %d: %w", index+1, err)
		}
		if previousTag != "" {
			if err := SetOutboundDetour(&outbound, previousTag); err != nil {
				return nil, fmt.Errorf("configure chain hop %d: %w", index+1, err)
			}
		}
		outbounds = append(outbounds, outbound)
		previousTag = tag
	}
	terminal, err := buildNodeOutbound(terminalTag, terminalURI, skipCertVerify)
	if err != nil {
		return nil, fmt.Errorf("build terminal outbound: %w", err)
	}
	if previousTag != "" {
		if err := validateDetourTransport(terminalURI, hopURIs[len(hopURIs)-1]); err != nil {
			return nil, fmt.Errorf("terminal outbound: %w", err)
		}
		if err := SetOutboundDetour(&terminal, previousTag); err != nil {
			return nil, fmt.Errorf("configure terminal detour: %w", err)
		}
	}
	outbounds = append(outbounds, terminal)
	return outbounds, nil
}

func validateDetourTransport(outboundURI, detourURI string) error {
	outboundScheme := uriScheme(outboundURI)
	detourScheme := uriScheme(detourURI)
	if (outboundScheme == "hysteria2" || outboundScheme == "hy2" || outboundScheme == "tuic") &&
		(detourScheme == "http" || detourScheme == "https") {
		return fmt.Errorf("%s requires UDP packet dialing, but HTTP detour cannot provide it", outboundScheme)
	}
	return nil
}

func uriScheme(rawURI string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURI))
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Scheme)
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
