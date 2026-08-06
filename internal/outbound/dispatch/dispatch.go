package dispatch

import (
	"context"
	"fmt"
	"net"
	"sync/atomic"

	"github.com/sagernet/sing-box/adapter"
	sboutbound "github.com/sagernet/sing-box/adapter/outbound"
	singlog "github.com/sagernet/sing-box/log"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/sagernet/sing/service"
)

const (
	Type = "multi-port-dispatch"
	Tag  = "multi-port-dispatch"
)

type Options struct {
	Mappings map[string]string
	Fallback string
}

type MappingUpdater interface {
	UpdateMappings(map[string]string)
}

type outbound struct {
	sboutbound.Adapter
	manager  adapter.OutboundManager
	fallback string
	mappings atomic.Value
}

func Register(registry *sboutbound.Registry) {
	sboutbound.Register[Options](registry, Type, newOutbound)
}

func newOutbound(ctx context.Context, _ adapter.Router, _ singlog.ContextLogger, tag string, options Options) (adapter.Outbound, error) {
	manager := service.FromContext[adapter.OutboundManager](ctx)
	if manager == nil {
		return nil, fmt.Errorf("missing outbound manager")
	}
	d := &outbound{
		Adapter:  sboutbound.NewAdapter(Type, tag, []string{N.NetworkTCP, N.NetworkUDP}, nil),
		manager:  manager,
		fallback: options.Fallback,
	}
	d.UpdateMappings(options.Mappings)
	return d, nil
}

func (d *outbound) UpdateMappings(mappings map[string]string) {
	cloned := make(map[string]string, len(mappings))
	for inboundTag, outboundTag := range mappings {
		cloned[inboundTag] = outboundTag
	}
	d.mappings.Store(cloned)
}

func (d *outbound) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	target, err := d.resolve(ctx)
	if err != nil {
		return nil, err
	}
	return target.DialContext(ctx, network, destination)
}

func (d *outbound) ListenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	target, err := d.resolve(ctx)
	if err != nil {
		return nil, err
	}
	return target.ListenPacket(ctx, destination)
}

func (d *outbound) resolve(ctx context.Context) (adapter.Outbound, error) {
	tag := d.fallback
	if metadata := adapter.ContextFrom(ctx); metadata != nil {
		if mappings, ok := d.mappings.Load().(map[string]string); ok {
			if mapped := mappings[metadata.Inbound]; mapped != "" {
				tag = mapped
			}
		}
	}
	if tag == "" {
		return nil, fmt.Errorf("no outbound mapping for inbound")
	}
	target, loaded := d.manager.Outbound(tag)
	if !loaded || target == d {
		return nil, fmt.Errorf("outbound %q is unavailable", tag)
	}
	return target, nil
}
