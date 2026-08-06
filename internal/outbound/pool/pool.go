package pool

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"easy_proxies/internal/monitor"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/outbound"
	singlog "github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/sagernet/sing/service"
)

const (
	// Type is the outbound type name exposed to sing-box.
	Type = "pool"
	// Tag is the default outbound tag used by builder.
	Tag = "proxy-pool"

	modeSequential = "sequential"
	modeRandom     = "random"
	modeBalance    = "balance"
	modeRotate     = "rotate"
)

// Options controls pool outbound behaviour.
type Options struct {
	Mode              string
	Members           []string
	FailureThreshold  int
	BlacklistDuration time.Duration
	RotationInterval  time.Duration
	Metadata          map[string]MemberMeta
}

// MemberMeta carries optional descriptive information for monitoring UI.
type MemberMeta struct {
	Name          string
	URI           string
	Mode          string
	ListenAddress string
	Port          uint16
	Region        string // GeoIP region code: "jp", "kr", "us", "hk", "tw", "other"
	Country       string // Full country name from GeoIP
}

// Register wires the pool outbound into the registry.
func Register(registry *outbound.Registry) {
	outbound.Register[Options](registry, Type, newPool)
}

type memberState struct {
	outbound adapter.Outbound
	tag      string
	entry    *monitor.EntryHandle
	shared   *sharedMemberState
}

type poolOutbound struct {
	outbound.Adapter
	ctx             context.Context
	logger          singlog.ContextLogger
	manager         adapter.OutboundManager
	options         Options
	mode            string
	members         []*memberState
	mu              sync.Mutex
	rrCounter       atomic.Uint32
	rng             *rand.Rand
	rngMu           sync.Mutex // protects rng for random mode
	rotateMu        sync.Mutex
	rotateTag       string
	rotateMember    *memberState
	rotateSince     time.Time
	monitor         *monitor.Manager
	selectionChecks atomic.Uint64
}

func newPool(ctx context.Context, _ adapter.Router, logger singlog.ContextLogger, tag string, options Options) (adapter.Outbound, error) {
	if len(options.Members) == 0 {
		return nil, E.New("pool requires at least one member")
	}
	manager := service.FromContext[adapter.OutboundManager](ctx)
	if manager == nil {
		return nil, E.New("missing outbound manager in context")
	}
	monitorMgr := monitor.FromContext(ctx)
	normalized := normalizeOptions(options)
	p := &poolOutbound{
		Adapter: outbound.NewAdapter(Type, tag, []string{N.NetworkTCP, N.NetworkUDP}, normalized.Members),
		ctx:     ctx,
		logger:  logger,
		manager: manager,
		options: normalized,
		mode:    normalized.Mode,
		rng:     rand.New(rand.NewSource(time.Now().UnixNano())),
		monitor: monitorMgr,
	}

	// Register nodes immediately if monitor is available
	if monitorMgr != nil {
		logger.Info("registering ", len(normalized.Members), " nodes to monitor")
		registered := 0
		for _, memberTag := range normalized.Members {
			// Acquire shared state for this tag (creates if not exists)
			state := acquireSharedState(memberTag)

			meta := normalized.Metadata[memberTag]
			info := monitor.NodeInfo{
				Tag:           memberTag,
				Name:          meta.Name,
				URI:           meta.URI,
				Mode:          meta.Mode,
				ListenAddress: meta.ListenAddress,
				Port:          meta.Port,
				Region:        meta.Region,
				Country:       meta.Country,
			}
			entry := monitorMgr.Register(info)
			if entry != nil {
				// Attach entry to shared state so all pool instances share it
				state.attachEntry(entry)
				registered++
				// Set probe, release, and blacklist functions immediately
				entry.SetRelease(p.makeReleaseByTagFunc(memberTag))
				entry.SetBlacklistFn(p.makeBlacklistByTagFunc(memberTag))
				if probeFn := p.makeProbeByTagFunc(memberTag); probeFn != nil {
					entry.SetProbe(probeFn)
				}
			} else {
				logger.Warn("failed to register node: ", memberTag)
			}
		}
		logger.Info("registered ", registered, " nodes to monitor")
	} else {
		logger.Warn("monitor manager is nil, skipping node registration")
	}

	// Register this pool outbound in the dialer registry for GeoIP router
	registerDialer(tag, p)

	return p, nil
}

func normalizeOptions(options Options) Options {
	if options.FailureThreshold <= 0 {
		options.FailureThreshold = 2
	}
	if options.BlacklistDuration <= 0 {
		options.BlacklistDuration = 10 * time.Minute
	}
	if options.RotationInterval <= 0 {
		options.RotationInterval = 2 * time.Minute
	}
	if options.Metadata == nil {
		options.Metadata = make(map[string]MemberMeta)
	}
	switch strings.ToLower(strings.TrimSpace(options.Mode)) {
	case "", modeRotate:
		options.Mode = modeRotate
	case modeSequential:
		options.Mode = modeSequential
	case modeRandom:
		options.Mode = modeRandom
	case modeBalance:
		options.Mode = modeBalance
	default:
		options.Mode = modeSequential
	}
	return options
}

func (p *poolOutbound) Start(stage adapter.StartStage) error {
	if stage != adapter.StartStateStart {
		return nil
	}
	p.mu.Lock()
	err := p.initializeMembersLocked()
	p.mu.Unlock()
	if err != nil {
		return err
	}
	return nil
}

// initializeMembersLocked must be called with p.mu held
func (p *poolOutbound) initializeMembersLocked() error {
	if len(p.members) > 0 {
		return nil // Already initialized
	}

	members := make([]*memberState, 0, len(p.options.Members))
	for _, tag := range p.options.Members {
		detour, loaded := p.manager.Outbound(tag)
		if !loaded {
			return E.New("pool member not found: ", tag)
		}

		// Acquire shared state (creates if not exists, reuses if already created)
		state := acquireSharedState(tag)

		member := &memberState{
			outbound: detour,
			tag:      tag,
			shared:   state,
			entry:    state.entryHandle(),
		}

		// Connect to existing monitor entry if available
		if p.monitor != nil {
			meta := p.options.Metadata[tag]
			info := monitor.NodeInfo{
				Tag:           tag,
				Name:          meta.Name,
				URI:           meta.URI,
				Mode:          meta.Mode,
				ListenAddress: meta.ListenAddress,
				Port:          meta.Port,
				Region:        meta.Region,
				Country:       meta.Country,
			}
			entry := p.monitor.Register(info)
			if entry != nil {
				state.attachEntry(entry)
				member.entry = entry
				entry.SetRelease(p.makeReleaseFunc(member))
				entry.SetBlacklistFn(p.makeBlacklistByTagFunc(member.tag))
				if probe := p.makeProbeFunc(member); probe != nil {
					entry.SetProbe(probe)
				}
			}
		}
		members = append(members, member)
	}
	p.members = members
	p.logger.Info("pool initialized with ", len(members), " members")

	return nil
}

func (p *poolOutbound) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	member, err := p.pickMember(network)
	if err != nil {
		return nil, err
	}
	p.incActive(member)
	conn, err := member.outbound.DialContext(ctx, network, destination)
	if err != nil {
		p.decActive(member)
		p.recordFailure(member, err)
		return nil, err
	}
	p.recordSuccess(member)
	return p.wrapConn(conn, member), nil
}

func (p *poolOutbound) ListenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	member, err := p.pickMember(N.NetworkUDP)
	if err != nil {
		return nil, err
	}
	p.incActive(member)
	conn, err := member.outbound.ListenPacket(ctx, destination)
	if err != nil {
		p.decActive(member)
		p.recordFailure(member, err)
		return nil, err
	}
	p.recordSuccess(member)
	return p.wrapPacketConn(conn, member), nil
}

func (p *poolOutbound) pickMember(network string) (*memberState, error) {
	now := time.Now()
	p.mu.Lock()
	if len(p.members) == 0 {
		if err := p.initializeMembersLocked(); err != nil {
			p.mu.Unlock()
			return nil, err
		}
	}
	member := p.selectAvailableLocked(now, network)
	p.mu.Unlock()

	if member == nil {
		return nil, E.New("no healthy proxy available")
	}
	return member, nil
}

func (p *poolOutbound) selectAvailableLocked(now time.Time, network string) *memberState {
	switch p.mode {
	case modeRandom:
		return p.selectRandomAvailableLocked(now, network)
	case modeBalance:
		return p.selectBalanceAvailableLocked(now, network)
	case modeRotate:
		return p.selectRotateAvailableLocked(now, network)
	default:
		return p.selectSequentialAvailableLocked(now, network)
	}
}

func (p *poolOutbound) memberAvailable(member *memberState, now time.Time, network string) bool {
	p.selectionChecks.Add(1)
	if member == nil {
		return false
	}
	if network != "" && (member.outbound == nil || !common.Contains(member.outbound.Network(), network)) {
		return false
	}
	return member.shared == nil || member.shared.tryAcquire(now)
}

func (p *poolOutbound) selectSequentialAvailableLocked(now time.Time, network string) *memberState {
	for range p.members {
		idx := int(p.rrCounter.Add(1)-1) % len(p.members)
		if member := p.members[idx]; p.memberAvailable(member, now, network) {
			return member
		}
	}
	return nil
}

func (p *poolOutbound) selectRandomAvailableLocked(now time.Time, network string) *memberState {
	for range min(len(p.members), 8) {
		if member := p.members[p.randomIndex(len(p.members))]; p.memberAvailable(member, now, network) {
			return member
		}
	}
	start := p.randomIndex(len(p.members))
	for offset := range p.members {
		if member := p.members[(start+offset)%len(p.members)]; p.memberAvailable(member, now, network) {
			return member
		}
	}
	return nil
}

func (p *poolOutbound) selectBalanceAvailableLocked(now time.Time, network string) *memberState {
	var first, second *memberState
	for range min(len(p.members)*2, 8) {
		member := p.members[p.randomIndex(len(p.members))]
		if member == first || !p.memberAvailable(member, now, network) {
			continue
		}
		if first == nil {
			first = member
			if first.shared != nil && first.shared.isHalfOpen() {
				return first
			}
		} else {
			second = member
			break
		}
	}
	if first == nil || second == nil {
		start := p.randomIndex(len(p.members))
		for offset := range p.members {
			member := p.members[(start+offset)%len(p.members)]
			if member == first || !p.memberAvailable(member, now, network) {
				continue
			}
			if first == nil {
				first = member
			} else {
				second = member
				break
			}
		}
	}
	if second == nil {
		return first
	}
	if memberActive(second) < memberActive(first) {
		return second
	}
	return first
}

func memberActive(member *memberState) int32 {
	if member == nil || member.shared == nil {
		return 0
	}
	return member.shared.activeCount()
}

func (p *poolOutbound) randomIndex(size int) int {
	p.rngMu.Lock()
	idx := p.rng.Intn(size)
	p.rngMu.Unlock()
	return idx
}

func (p *poolOutbound) selectRotateAvailableLocked(now time.Time, network string) *memberState {
	p.rotateMu.Lock()
	defer p.rotateMu.Unlock()
	interval := p.options.RotationInterval
	if interval <= 0 {
		interval = 2 * time.Minute
	}
	if p.rotateMember != nil && now.Sub(p.rotateSince) < interval && p.memberAvailable(p.rotateMember, now, network) {
		return p.rotateMember
	}
	start := 0
	if p.rotateMember != nil {
		for i, member := range p.members {
			if member == p.rotateMember {
				start = i + 1
				break
			}
		}
	} else if p.rotateTag != "" {
		for i, member := range p.members {
			if member.tag == p.rotateTag {
				start = i + 1
				break
			}
		}
	}
	for offset := range p.members {
		member := p.members[(start+offset)%len(p.members)]
		if !p.memberAvailable(member, now, network) {
			continue
		}
		p.rotateMember = member
		p.rotateTag = member.tag
		p.rotateSince = now
		return member
	}
	return nil
}

func (p *poolOutbound) recordFailure(member *memberState, cause error) {
	if member.shared == nil {
		p.logger.Warn("proxy ", member.tag, " failure (no shared state): ", cause)
		return
	}
	failures, blacklisted, _ := member.shared.recordFailure(cause, p.options.FailureThreshold, p.options.BlacklistDuration)
	if blacklisted {
		p.logger.Warn("proxy ", member.tag, " blacklisted for ", p.options.BlacklistDuration, ": ", cause)
		log.Printf("[pool] %s blacklisted for %s: %v", member.tag, p.options.BlacklistDuration, cause)
	} else {
		p.logger.Warn("proxy ", member.tag, " failure ", failures, "/", p.options.FailureThreshold, ": ", cause)
		log.Printf("[pool] %s failure %d/%d: %v", member.tag, failures, p.options.FailureThreshold, cause)
	}
}

func (p *poolOutbound) recordSuccess(member *memberState) {
	if member.shared != nil {
		member.shared.recordSuccess()
	}
}

func (p *poolOutbound) wrapConn(conn net.Conn, member *memberState) net.Conn {
	return &trackedConn{Conn: conn, release: func() {
		p.decActive(member)
	}}
}

func (p *poolOutbound) wrapPacketConn(conn net.PacketConn, member *memberState) net.PacketConn {
	return &trackedPacketConn{PacketConn: conn, release: func() {
		p.decActive(member)
	}}
}

func (p *poolOutbound) makeReleaseFunc(member *memberState) func() {
	return func() {
		if member.shared != nil {
			member.shared.forceRelease()
		}
	}
}

func (p *poolOutbound) makeProbeFunc(member *memberState) func(ctx context.Context) (time.Duration, error) {
	if p.monitor == nil {
		return nil
	}
	target, ok := p.monitor.ProbeTarget()
	if !ok {
		return nil
	}
	return func(ctx context.Context) (time.Duration, error) {
		duration, err := probeHTTP(ctx, target, member.outbound.DialContext)
		if err != nil {
			if member.entry != nil {
				member.entry.RecordFailure(err)
			}
			return 0, err
		}
		if member.entry != nil {
			member.entry.RecordSuccessWithLatency(duration)
		}
		// Clear pool blacklist on successful probe — a node that passes
		// health check should be available for selection immediately,
		// not remain blacklisted for the full duration (fixes #8, #9).
		if member.shared != nil {
			member.shared.forceRelease()
		}
		return duration, nil
	}
}

// makeProbeByTagFunc creates a probe function that works before member initialization
func (p *poolOutbound) makeProbeByTagFunc(tag string) func(ctx context.Context) (time.Duration, error) {
	if p.monitor == nil {
		return nil
	}
	target, ok := p.monitor.ProbeTarget()
	if !ok {
		return nil
	}
	return func(ctx context.Context) (time.Duration, error) {
		// Ensure members are initialized
		p.mu.Lock()
		if len(p.members) == 0 {
			if err := p.initializeMembersLocked(); err != nil {
				p.mu.Unlock()
				return 0, err
			}
		}

		// Find the member by tag
		var member *memberState
		for _, m := range p.members {
			if m.tag == tag {
				member = m
				break
			}
		}
		p.mu.Unlock()

		if member == nil {
			return 0, E.New("member not found: ", tag)
		}

		duration, err := probeHTTP(ctx, target, member.outbound.DialContext)
		if err != nil {
			if member.entry != nil {
				member.entry.RecordFailure(err)
			}
			return 0, err
		}

		if member.entry != nil {
			member.entry.RecordSuccessWithLatency(duration)
		}
		// Clear pool blacklist on successful probe (fixes #8, #9)
		if member.shared != nil {
			member.shared.forceRelease()
		}
		return duration, nil
	}
}

func probeHTTP(ctx context.Context, target string, dial func(context.Context, string, M.Socksaddr) (net.Conn, error)) (time.Duration, error) {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			return dial(ctx, network, M.ParseSocksaddr(address))
		},
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
	}
	defer transport.CloseIdleConnections()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return 0, err
	}
	start := time.Now()
	response, err := (&http.Client{Transport: transport}).Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	if response.StatusCode != http.StatusNoContent {
		return 0, fmt.Errorf("probe got HTTP %d, expected 204", response.StatusCode)
	}
	return time.Since(start), nil
}

// makeReleaseByTagFunc creates a release function that works before member initialization
func (p *poolOutbound) makeReleaseByTagFunc(tag string) func() {
	return func() {
		releaseSharedMember(tag)
	}
}

// makeBlacklistByTagFunc creates a blacklist function for manual ban via API
func (p *poolOutbound) makeBlacklistByTagFunc(tag string) func(time.Duration) {
	return func(duration time.Duration) {
		blacklistSharedMember(tag, duration)
	}
}

type trackedConn struct {
	net.Conn
	once    sync.Once
	release func()
}

func (c *trackedConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(c.release)
	return err
}

type trackedPacketConn struct {
	net.PacketConn
	once    sync.Once
	release func()
}

func (c *trackedPacketConn) Close() error {
	err := c.PacketConn.Close()
	c.once.Do(c.release)
	return err
}

func (p *poolOutbound) incActive(member *memberState) {
	if member.shared != nil {
		member.shared.incActive()
	}
}

func (p *poolOutbound) decActive(member *memberState) {
	if member.shared != nil {
		member.shared.decActive()
	}
}
