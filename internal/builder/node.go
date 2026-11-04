package builder

import (
	"fmt"
	"net/url"
	"strings"

	"easy_proxies/internal/config"
	"easy_proxies/internal/proxychain"

	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/auth"
)

type NodeValidationFailure struct {
	Name   string
	Port   uint16
	Reason string
}

func ValidateNodeConfig(cfg *config.Config, node config.NodeConfig) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}
	tag := NodeTag(node.Name)
	if tag == "" {
		tag = "node-validation"
	}
	if strings.TrimSpace(node.ChainProfileID) == "" {
		_, err := buildNodeOutbound(tag, node.URI, cfg.SkipCertVerify)
		return err
	}
	profile, ok := proxychain.Find(cfg.ChainProfiles, node.ChainProfileID)
	if !ok || !profile.Enabled {
		return fmt.Errorf("chain profile %q is missing or disabled", node.ChainProfileID)
	}
	if len(profile.Hops) == 0 {
		return fmt.Errorf("chain profile %q has no hops", profile.Name)
	}
	hops := make([]string, 0, len(profile.Hops))
	for _, hop := range profile.Hops {
		hops = append(hops, hop.URI)
	}
	chainOutbounds, err := BuildChainOutbounds(tag+"-baseline", hops[len(hops)-1], hops[:len(hops)-1], cfg.SkipCertVerify)
	if err != nil {
		return err
	}
	terminal, err := buildNodeOutbound(tag, node.URI, cfg.SkipCertVerify)
	if err != nil {
		return err
	}
	if err := validateDetourTransport(node.URI, hops[len(hops)-1]); err != nil {
		return err
	}
	return SetOutboundDetour(&terminal, chainOutbounds[len(chainOutbounds)-1].Tag)
}

func ValidateNodeConfigs(cfg *config.Config) []NodeValidationFailure {
	if cfg == nil {
		return []NodeValidationFailure{{Reason: "config is nil"}}
	}
	failures := make([]NodeValidationFailure, 0)
	for _, node := range cfg.Nodes {
		if err := ValidateNodeConfig(cfg, node); err != nil {
			failures = append(failures, NodeValidationFailure{
				Name:   node.Name,
				Port:   node.Port,
				Reason: err.Error(),
			})
		}
	}
	return failures
}

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

// NodeTags assigns each node a unique outbound tag. Generated suffixes are
// reserved too, so a node literally named "x-2" never collides with the
// second "x". reserved holds tags already taken (e.g. chain hops).
func NodeTags(nodes []config.NodeConfig, reserved map[string]struct{}) []string {
	used := make(map[string]struct{}, len(nodes)+len(reserved))
	for tag := range reserved {
		used[tag] = struct{}{}
	}
	tags := make([]string, len(nodes))
	for i, node := range nodes {
		base := sanitizeTag(node.Name)
		if base == "" {
			base = fmt.Sprintf("node-%d", i+1)
		}
		tag := base
		for n := 2; ; n++ {
			if _, taken := used[tag]; !taken {
				break
			}
			tag = fmt.Sprintf("%s-%d", base, n)
		}
		used[tag] = struct{}{}
		tags[i] = tag
	}
	return tags
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
