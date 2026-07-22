package builder

import "github.com/sagernet/sing-box/option"

func BuildSingleNodeOutbound(tag, uri string, skipCertVerify bool) (option.Outbound, error) {
	return buildNodeOutbound(tag, uri, skipCertVerify)
}
