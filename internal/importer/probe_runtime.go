package importer

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/endpoint"
	"github.com/sagernet/sing-box/adapter/inbound"
	sbOutbound "github.com/sagernet/sing-box/adapter/outbound"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/dns"
	"github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/service"
)

const probeLocalDNSTag = "probe-local-dns"

type sharedProbeRuntime struct {
	registry            *sbOutbound.Registry
	manager             *sbOutbound.Manager
	ctx                 context.Context
	logFactory          log.Factory
	dnsTransportManager *dns.TransportManager
	dnsRouter           *dns.Router
	closeOnce           sync.Once
	closeErr            error
}

func newSharedProbeRuntime() (*sharedProbeRuntime, error) {
	ctx := include.Context(context.Background())
	logFactory := log.NewNOPFactory()
	logger := logFactory.NewLogger("probe-runtime")

	endpointManager := endpoint.NewManager(logger, service.FromContext[adapter.EndpointRegistry](ctx))
	service.MustRegister[adapter.EndpointManager](ctx, endpointManager)
	inboundManager := inbound.NewManager(logger, service.FromContext[adapter.InboundRegistry](ctx), endpointManager)
	service.MustRegister[adapter.InboundManager](ctx, inboundManager)
	outboundManager := sbOutbound.NewManager(logger, service.FromContext[adapter.OutboundRegistry](ctx), endpointManager, "")
	service.MustRegister[adapter.OutboundManager](ctx, outboundManager)

	dnsTransportManager := dns.NewTransportManager(
		logger,
		service.FromContext[adapter.DNSTransportRegistry](ctx),
		outboundManager,
		probeLocalDNSTag,
	)
	service.MustRegister[adapter.DNSTransportManager](ctx, dnsTransportManager)
	if err := dnsTransportManager.Create(ctx, logger, probeLocalDNSTag, C.DNSTypeLocal, &option.LocalDNSServerOptions{}); err != nil {
		return nil, fmt.Errorf("create local DNS transport: %w", err)
	}
	if err := dnsTransportManager.Start(adapter.StartStateInitialize); err != nil {
		_ = dnsTransportManager.Close()
		return nil, fmt.Errorf("initialize DNS transport manager: %w", err)
	}
	if err := dnsTransportManager.Start(adapter.StartStateStart); err != nil {
		_ = dnsTransportManager.Close()
		return nil, fmt.Errorf("start DNS transport manager: %w", err)
	}

	dnsRouter := dns.NewRouter(ctx, logFactory, option.DNSOptions{})
	service.MustRegister[adapter.DNSRouter](ctx, dnsRouter)
	if err := dnsRouter.Initialize(nil); err != nil {
		_ = dnsTransportManager.Close()
		return nil, fmt.Errorf("initialize DNS router: %w", err)
	}
	if err := dnsRouter.Start(adapter.StartStateStart); err != nil {
		_ = dnsRouter.Close()
		_ = dnsTransportManager.Close()
		return nil, fmt.Errorf("start DNS router: %w", err)
	}

	registry, ok := service.FromContext[adapter.OutboundRegistry](ctx).(*sbOutbound.Registry)
	if !ok {
		_ = dnsRouter.Close()
		_ = dnsTransportManager.Close()
		return nil, fmt.Errorf("unexpected outbound registry type %T", service.FromContext[adapter.OutboundRegistry](ctx))
	}
	if err := outboundManager.Create(ctx, nil, logger, probeDirectTag, C.TypeDirect, &option.DirectOutboundOptions{}); err != nil {
		_ = dnsRouter.Close()
		_ = dnsTransportManager.Close()
		return nil, fmt.Errorf("create probe direct outbound: %w", err)
	}
	for _, stage := range adapter.ListStartStages {
		if err := outboundManager.Start(stage); err != nil {
			_ = outboundManager.Close()
			_ = dnsRouter.Close()
			_ = dnsTransportManager.Close()
			return nil, fmt.Errorf("start probe outbound manager at %s: %w", stage, err)
		}
	}
	return &sharedProbeRuntime{
		registry:            registry,
		manager:             outboundManager,
		ctx:                 ctx,
		logFactory:          logFactory,
		dnsTransportManager: dnsTransportManager,
		dnsRouter:           dnsRouter,
	}, nil
}

const probeDirectTag = "probe-direct"

func (r *sharedProbeRuntime) BuildChain(configs []option.Outbound) (adapter.Outbound, func(), error) {
	if len(configs) == 0 {
		return nil, nil, fmt.Errorf("chain has no outbounds")
	}
	created := make([]string, 0, len(configs))
	logger := r.logFactory.NewLogger("probe/chain")
	for _, config := range configs {
		if err := r.manager.Create(r.ctx, nil, logger, config.Tag, config.Type, config.Options); err != nil {
			for index := len(created) - 1; index >= 0; index-- {
				_ = r.manager.Remove(created[index])
			}
			return nil, nil, fmt.Errorf("create chain outbound %s: %w", config.Type, err)
		}
		created = append(created, config.Tag)
	}
	terminal, ok := r.manager.Outbound(configs[len(configs)-1].Tag)
	if !ok {
		return nil, nil, fmt.Errorf("chain terminal outbound was not registered")
	}
	cleanup := func() {
		for index := len(created) - 1; index >= 0; index-- {
			_ = r.manager.Remove(created[index])
		}
	}
	return terminal, cleanup, nil
}

func (r *sharedProbeRuntime) Build(config option.Outbound) (adapter.Outbound, error) {
	logger := r.logFactory.NewLogger("probe/" + config.Type)
	outbound, err := r.registry.CreateOutbound(r.ctx, nil, logger, config.Tag, config.Type, config.Options)
	if err != nil {
		return nil, fmt.Errorf("create outbound %s: %w", config.Type, err)
	}
	for _, stage := range adapter.ListStartStages {
		if err := adapter.LegacyStart(outbound, stage); err != nil {
			_ = common.Close(outbound)
			return nil, fmt.Errorf("start outbound %s at %s: %w", config.Type, stage, err)
		}
	}
	return outbound, nil
}

func (r *sharedProbeRuntime) Close() error {
	if r == nil {
		return nil
	}
	r.closeOnce.Do(func() {
		var errs []error
		if r.manager != nil {
			errs = append(errs, r.manager.Close())
		}
		if r.dnsRouter != nil {
			errs = append(errs, r.dnsRouter.Close())
		}
		if r.dnsTransportManager != nil {
			errs = append(errs, r.dnsTransportManager.Close())
		}
		r.closeErr = errors.Join(errs...)
	})
	return r.closeErr
}
