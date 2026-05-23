package monitor

import (
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"easy_proxies/internal/config"
)

// isPortListening reports whether something is currently accepting TCP
// connections on address:port. This mirrors netstat semantics: a port is
// "occupied" only when a listener is actually accepting connections, not
// merely because some other socket in this process could bind there.
func isPortListening(address string, port uint16) bool {
	probe := address
	if probe == "" || probe == "0.0.0.0" || probe == "::" {
		probe = "127.0.0.1"
	}
	addr := net.JoinHostPort(probe, strconv.Itoa(int(port)))
	conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

type portStatus struct {
	Port       uint16 `json:"port"`
	NodeName   string `json:"node_name,omitempty"`
	Configured bool   `json:"configured"`
	Available  bool   `json:"available"`
	Reason     string `json:"reason,omitempty"`
}

type portsStatusResponse struct {
	Address      string              `json:"address"`
	BasePort     uint16              `json:"base_port"`
	TargetCount  int                 `json:"target_count"`
	Recommended  *portRecommendation `json:"recommended,omitempty"`
	Ports        []portStatus        `json:"ports"`
	SkippedPorts []uint16            `json:"skipped_ports,omitempty"`
	SkippedAt    string              `json:"skipped_at,omitempty"`
}

type portSource struct {
	Port  uint16
	Name  string
	Known bool
}

type portRecommendation struct {
	Start       uint16       `json:"start"`
	End         uint16       `json:"end"`
	Needed      int          `json:"needed"`
	Available   int          `json:"available"`
	Skipped     []portStatus `json:"skipped,omitempty"`
	Complete    bool         `json:"complete"`
	Description string       `json:"description"`
}

const maxPortScanRange = 500

func (s *Server) handlePortsStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		w.WriteHeader(http.StatusMethodNotAllowed)
		writeJSON(w, map[string]string{"error": "仅支持 GET 请求"})
		return
	}

	s.cfgMu.RLock()
	cfgSrc := s.cfgSrc
	s.cfgMu.RUnlock()

	if cfgSrc == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		writeJSON(w, map[string]string{"error": "config not available"})
		return
	}

	address := cfgSrc.MultiPort.Address
	if host, _, err := net.SplitHostPort(address); err == nil {
		address = host
	}

	from := int(cfgSrc.MultiPort.BasePort)
	// Pool DB gives nice user-facing names; fall back to cfg.Nodes name when missing.
	poolNames := make(map[uint16]string)
	if s.importSvc != nil {
		if nodes, err := s.importSvc.ListPool(); err == nil {
			for _, n := range nodes {
				if n.Port > 0 {
					poolNames[n.Port] = n.Name
				}
			}
		}
	}
	// Sources reflect what sing-box actually binds (cfgSrc.Nodes), so the scan
	// count matches reality regardless of how many of those are in the pool DB.
	sources := make(map[uint16]portSource, len(cfgSrc.Nodes))
	for _, n := range cfgSrc.Nodes {
		if n.Port > 0 {
			name := poolNames[n.Port]
			if name == "" {
				name = n.Name
			}
			sources[n.Port] = portSource{Port: n.Port, Name: name, Known: true}
		}
	}
	targetCount := len(cfgSrc.Nodes)
	if targetCount < 1 {
		targetCount = 1
	}
	to := from + targetCount + 20

	qFrom := r.URL.Query().Get("from")
	qTo := r.URL.Query().Get("to")
	qCount := r.URL.Query().Get("count")
	if qFrom != "" || qTo != "" {
		if qFrom != "" {
			if v, err := strconv.Atoi(qFrom); err == nil && v >= 1 && v <= 65535 {
				from = v
			} else {
				w.WriteHeader(http.StatusBadRequest)
				writeJSON(w, map[string]string{"error": "invalid 'from' parameter"})
				return
			}
		}
		if qTo != "" {
			if v, err := strconv.Atoi(qTo); err == nil && v >= 1 && v <= 65535 {
				to = v
			} else {
				w.WriteHeader(http.StatusBadRequest)
				writeJSON(w, map[string]string{"error": "invalid 'to' parameter"})
				return
			}
		}
	}
	if qCount != "" {
		if v, err := strconv.Atoi(qCount); err == nil && v >= 1 && v <= maxPortScanRange {
			targetCount = v
			if qTo == "" {
				to = from + targetCount + 20
			}
		} else {
			w.WriteHeader(http.StatusBadRequest)
			writeJSON(w, map[string]string{"error": "invalid 'count' parameter"})
			return
		}
	}
	if to-from > maxPortScanRange {
		to = from + maxPortScanRange
	}
	if to < from {
		to = from
	}
	if to > 65535 {
		to = 65535
	}

	listenerPort := int(cfgSrc.Listener.Port)

	ports := make([]portStatus, to-from+1)
	const scanWorkers = 32
	var wg sync.WaitGroup
	sem := make(chan struct{}, scanWorkers)
	for i, p := 0, from; p <= to; i, p = i+1, p+1 {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx, port int) {
			defer wg.Done()
			defer func() { <-sem }()
			ports[idx] = s.portStatusFor(address, listenerPort, sources, port)
		}(i, p)
	}
	wg.Wait()

	skipped, skippedAt := config.LastPortSkips()
	skippedAtStr := ""
	if !skippedAt.IsZero() {
		skippedAtStr = skippedAt.Format(time.RFC3339)
	}
	writeJSON(w, portsStatusResponse{
		Address:      address,
		BasePort:     uint16(from),
		TargetCount:  targetCount,
		Recommended:  recommendPorts(address, listenerPort, sources, from, targetCount),
		Ports:        ports,
		SkippedPorts: skipped,
		SkippedAt:    skippedAtStr,
	})
}

func (s *Server) portStatusFor(address string, listenerPort int, configured map[uint16]portSource, p int) portStatus {
	ps := portStatus{Port: uint16(p)}

	if nc, ok := configured[uint16(p)]; ok {
		ps.NodeName = nc.Name
		ps.Configured = nc.Known
		ps.Available = false
		ps.Reason = "used_by_pool"
		return ps
	}
	if p == listenerPort {
		ps.Available = false
		ps.Reason = "listener_conflict"
		return ps
	}
	if isPortListening(address, uint16(p)) {
		ps.Available = false
		ps.Reason = "occupied_by_os"
		return ps
	}
	ps.Available = true
	return ps
}

func recommendPorts(address string, listenerPort int, configured map[uint16]portSource, from, needed int) *portRecommendation {
	if needed < 1 || from < 1 || from > 65535 {
		return nil
	}
	rec := &portRecommendation{Start: uint16(from), Needed: needed}
	for p := from; p <= 65535 && p-from <= maxPortScanRange; p++ {
		ps := portStatus{Port: uint16(p)}
		if nc, ok := configured[uint16(p)]; ok {
			ps.NodeName = nc.Name
			ps.Configured = nc.Known
			ps.Available = false
			ps.Reason = "used_by_pool"
		} else if p == listenerPort {
			ps.Available = false
			ps.Reason = "listener_conflict"
		} else if isPortListening(address, uint16(p)) {
			ps.Available = false
			ps.Reason = "occupied_by_os"
		} else {
			ps.Available = true
		}

		if ps.Available {
			rec.Available++
			rec.End = uint16(p)
			if rec.Available == needed {
				rec.Complete = true
				break
			}
			continue
		}
		rec.Skipped = append(rec.Skipped, ps)
	}
	if rec.Complete {
		rec.Description = "从起始端口开始已找到足够节点数量的可分配端口"
	} else {
		rec.Description = "扫描范围内没有找到足够的可分配端口"
	}
	return rec
}
