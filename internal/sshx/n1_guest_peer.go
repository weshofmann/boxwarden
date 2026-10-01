//go:build n1clipboarddiagnostic && !n1candidate

package sshx

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"regexp"
	"time"
)

type N1Peer struct {
	SessionUUID    string `json:"session_uuid"`
	GenerationUUID string `json:"generation_uuid"`
	Backend        string `json:"backend"`
	Address        string `json:"address"`
	MAC            string `json:"mac"`
}
type N1PeerPair struct {
	Candidate N1Peer `json:"candidate"`
	Control   N1Peer `json:"control"`
}
type N1PeerWatch struct {
	Version    int    `json:"version"`
	Domain     string `json:"domain"`
	Role       string `json:"role"`
	Interface  string `json:"interface"`
	Candidate  N1Peer `json:"candidate"`
	Control    N1Peer `json:"control"`
	Gateway    string `json:"gateway"`
	DurationMS int    `json:"duration_ms"`
}
type N1PeerReady struct {
	Record             string     `json:"record"`
	Version            int        `json:"version"`
	Domain             string     `json:"domain"`
	Role               string     `json:"role"`
	Interface          string     `json:"interface"`
	DurationMS         int        `json:"duration_ms"`
	Gateway            string     `json:"gateway"`
	Binding            N1PeerPair `json:"binding"`
	IntervalOrigin     string     `json:"interval_origin"`
	ReadinessBudgetUS  int        `json:"readiness_budget_us"`
	ClosingToleranceUS int        `json:"closing_tolerance_us"`
}
type N1PeerInference struct {
	Scope                                           string `json:"scope"`
	CandidatePreEmissionAbsenceIfDriverControlsPass bool   `json:"candidate_pre_emission_absence_if_driver_controls_pass"`
	DriverControlsRequired                          bool   `json:"driver_controls_required"`
	QualifiedGuestCaptureRequired                   bool   `json:"qualified_guest_capture_required"`
	GlobalAbsence                                   bool   `json:"global_absence"`
	DownstreamAbsence                               bool   `json:"downstream_absence"`
	Enqueue                                         bool   `json:"enqueue"`
	Delivery                                        bool   `json:"delivery"`
}
type N1RouteState struct {
	Status         string `json:"status"`
	DeviceMatches  bool   `json:"device_matches"`
	SourceMatches  bool   `json:"source_matches"`
	GatewayMatches *bool  `json:"gateway_matches"`
	Metric         *int   `json:"metric"`
}
type N1NeighborState struct {
	Status     string `json:"status"`
	State      string `json:"state"`
	MACMatches *bool  `json:"mac_matches"`
}
type N1PeerSummary struct {
	N1PeerReady
	ReadinessDelayUS     int64           `json:"readiness_delay_us"`
	CoverageScope        string          `json:"coverage_scope"`
	ZeroCountAttribution bool            `json:"zero_count_attribution"`
	NegativeInference    N1PeerInference `json:"negative_inference"`
	Ready                bool            `json:"ready"`
	DeadlineReached      bool            `json:"deadline_reached"`
	ElapsedMS            int64           `json:"elapsed_ms"`
	Complete             bool            `json:"complete"`
	Overflow             bool            `json:"overflow"`
	Drops                uint32          `json:"drops"`
	ObservedPackets      int             `json:"observed_packets"`
	IncompleteReasons    []string        `json:"incomplete_reasons"`
	Counters             map[string]int  `json:"counters"`
	RouteBefore          N1RouteState    `json:"route_before"`
	NeighborBefore       N1NeighborState `json:"neighbor_before"`
	RouteAfter           N1RouteState    `json:"route_after"`
	NeighborAfter        N1NeighborState `json:"neighbor_after"`
}

var n1Interface = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,14}$`)
var n1CounterCodes = []string{"arp_local_request", "arp_local_reply", "arp_peer_request_expected_unicast", "arp_peer_request_unexpected_unicast", "arp_peer_request_expected_broadcast", "arp_peer_request_unexpected_broadcast", "arp_peer_reply_expected_unicast", "arp_peer_reply_unexpected_unicast", "arp_peer_reply_expected_broadcast", "arp_peer_reply_unexpected_broadcast", "tcp22_syn_out", "tcp22_syn_in", "tcp22_synack_out", "tcp22_synack_in", "tcp22_rst_out", "tcp22_rst_in", "tcp22_other_out", "tcp22_other_in", "icmp_unreachable_tcp22"}
var n1IncompleteCodes = []string{"capture_setup", "capture_error", "counter_overflow", "packet_drops", "truncated_packet", "unsupported_header", "stopped_early", "route_query_incomplete", "neighbor_query_incomplete", "ready_emit_failed", "statistics_unavailable", "observer_clock_error", "ready_missing", "packet_accounting_mismatch", "packet_direction_invalid", "packet_metadata_invalid", "readiness_late", "observer_interval_overrun", "packet_outside_interval"}

func n1Member(value string, allowed []string) bool {
	for _, s := range allowed {
		if s == value {
			return true
		}
	}
	return false
}
func (p N1Peer) valid() bool {
	m, e := net.ParseMAC(p.MAC)
	return validUUID(p.SessionUUID) && validUUID(p.GenerationUUID) && n1BackendPattern.MatchString(p.Backend) && n1PrivateIPv4(p.Address) && e == nil && len(m) == 6 && m[0]&1 == 0 && p.MAC != "00:00:00:00:00:00" && m.String() == p.MAC
}
func (w N1PeerWatch) valid() bool {
	if w.Version != 1 || w.Domain != "n1qualification" || !(w.Role == "candidate" || w.Role == "control") || !n1Interface.MatchString(w.Interface) || w.DurationMS < 1 || w.DurationMS > 30000 || !w.Candidate.valid() || !w.Control.valid() || !n1PrivateIPv4(w.Gateway) || w.Candidate.Backend == w.Control.Backend || w.Candidate.Address == w.Control.Address || w.Candidate.MAC == w.Control.MAC || w.Gateway == w.Candidate.Address || w.Gateway == w.Control.Address {
		return false
	}
	ids := []string{w.Candidate.SessionUUID, w.Candidate.GenerationUUID, w.Control.SessionUUID, w.Control.GenerationUUID}
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}
func (r N1PeerReady) matches(w N1PeerWatch) bool {
	return r.Version == 1 && r.Domain == w.Domain && r.Role == w.Role && r.Interface == w.Interface && r.DurationMS == w.DurationMS && r.Gateway == w.Gateway && r.Binding == (N1PeerPair{w.Candidate, w.Control}) && r.IntervalOrigin == "ready_emit_start" && r.ReadinessBudgetUS == min(100000, w.DurationMS*250) && r.ClosingToleranceUS == min(250000, w.DurationMS*250)
}
func (s N1PeerSummary) valid(w N1PeerWatch) bool {
	if s.Record != "summary" || !s.matches(w) || s.ReadinessDelayUS < 0 || s.ReadinessDelayUS > 60000000 || s.ElapsedMS < 0 || s.ElapsedMS > 60000 || s.ObservedPackets < 0 || s.ObservedPackets > 4097 || s.CoverageScope != "identified_pair_headers" || s.ZeroCountAttribution || len(s.Counters) != len(n1CounterCodes) || len(s.IncompleteReasons) > len(n1IncompleteCodes) {
		return false
	}
	for k, v := range s.Counters {
		if !n1Member(k, n1CounterCodes) || v < 0 || v > 4096 {
			return false
		}
	}
	seen := map[string]bool{}
	for _, reason := range s.IncompleteReasons {
		if !n1Member(reason, n1IncompleteCodes) || seen[reason] {
			return false
		}
		seen[reason] = true
	}
	inference := s.NegativeInference
	if inference.Scope != "supported_well_formed_outgoing_candidate_arp_or_tcp22_syn_at_guest_tap" || !inference.DriverControlsRequired || !inference.QualifiedGuestCaptureRequired || inference.GlobalAbsence || inference.DownstreamAbsence || inference.Enqueue || inference.Delivery {
		return false
	}
	// The producer checks the actual monotonic deadlines before round(delta*units).
	// Readiness has an integral microsecond budget. Closing may end on a half-ms
	// tie; floating subtraction from the origin can round that tie either way.
	// Admit its upper representable millisecond, without extending the interval.
	maxElapsedMS := int64(w.DurationMS) + (int64(s.ClosingToleranceUS)+500)/1000
	if s.Complete && (!s.Ready || !s.DeadlineReached || s.Overflow || s.Drops != 0 || len(s.IncompleteReasons) != 0 || s.ObservedPackets > 4096 || s.ReadinessDelayUS > int64(s.ReadinessBudgetUS) || s.ReadinessDelayUS >= int64(w.DurationMS)*1000 || s.ElapsedMS < int64(w.DurationMS) || s.ElapsedMS > maxElapsedMS) {
		return false
	}
	eligible := w.Role == "candidate" && s.Ready && s.Complete && s.DeadlineReached && !s.Overflow && s.Drops == 0 && len(s.IncompleteReasons) == 0 && s.Counters["arp_local_request"] == 0 && s.Counters["arp_local_reply"] == 0 && s.Counters["tcp22_syn_out"] == 0 && s.Counters["tcp22_synack_out"] == 0
	if inference.CandidatePreEmissionAbsenceIfDriverControlsPass != eligible {
		return false
	}
	return s.RouteBefore.valid() && s.RouteAfter.valid() && s.NeighborBefore.valid() && s.NeighborAfter.valid()
}
func (r N1RouteState) valid() bool {
	return n1Member(r.Status, []string{"ok", "invalid", "unavailable"}) && (r.Metric == nil || *r.Metric >= 0 && *r.Metric <= 2147483647)
}
func (n N1NeighborState) valid() bool {
	return n1Member(n.Status, []string{"absent", "present", "invalid", "unavailable"}) && n1Member(n.State, []string{"UNKNOWN", "NONE", "INCOMPLETE", "REACHABLE", "STALE", "DELAY", "PROBE", "FAILED", "NOARP", "PERMANENT"})
}

type n1PeerParser struct {
	watch   N1PeerWatch
	onReady func(N1PeerReady) error
	ready   bool
	final   bool
	summary N1PeerSummary
}

func (p *n1PeerParser) record(raw []byte) error {
	if !p.ready {
		var r N1PeerReady
		if n1Decode(raw, &r, 8192) != nil || r.Record != "ready" || !r.matches(p.watch) || p.onReady == nil {
			return ErrN1Guest
		}
		p.ready = true
		if p.onReady(r) != nil {
			return ErrN1Guest
		}
		return nil
	}
	if p.final {
		return ErrN1Guest
	}
	var s N1PeerSummary
	if n1Decode(raw, &s, 8192) != nil || !s.valid(p.watch) {
		return ErrN1Guest
	}
	p.summary = s
	p.final = true
	return nil
}

// private stream seam keeps arbitrary command execution out of exported APIs.
type n1PeerStreamRunner interface {
	runN1Peer(context.Context, Command, func(io.Reader) error) (n1ProcessResult, error)
}

// ObserveN1Peer runs one fixed staged observer, admits exactly READY then final,
// and returns only after the directly retained child/streams have terminated.
// onReady must return promptly; the trusted coordinator uses a bounded channel
// barrier and owns original approval, ARM and generation deadline admission.
func (c *Client) ObserveN1Peer(ctx context.Context, conn Connection, w N1PeerWatch, onReady func(N1PeerReady) error) (N1PeerSummary, error) {
	if !w.valid() || onReady == nil {
		return N1PeerSummary{}, ErrN1Guest
	}
	local := w.Candidate
	if w.Role == "control" {
		local = w.Control
	}
	b := N1GuestBinding{1, w.Domain, local.SessionUUID, "tart", local.Backend, local.GenerationUUID}
	if c.n1Admit(ctx, conn, b) != nil || conn.Address != local.Address {
		return N1PeerSummary{}, ErrN1Guest
	}
	input, _ := json.Marshal(w)
	if len(input) > 8192 {
		return N1PeerSummary{}, ErrN1Guest
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	args := strictOpenSSHArguments(conn)
	args = append(args, "-p", "22", "boxwarden@"+conn.Address, "/usr/bin/sudo", "-n", "--", "/usr/bin/python3", "-I", "-S", "/usr/local/libexec/boxwarden-n1-guest-metadata-observer.py", "observe")
	parser := n1PeerParser{watch: w, onReady: onReady}
	consume := func(reader io.Reader) error { return n1ReadRecords(reader, parser.record) }
	command := Command{Path: sshPath, Args: args, Stdin: input}
	var result n1ProcessResult
	var err error
	switch runner := c.runner.(type) {
	case ExecRunner, *ExecRunner:
		result, err = n1Execute(ctx, command, 16384, consume)
	case n1PeerStreamRunner:
		result, err = runner.runN1Peer(ctx, command, consume)
	default:
		return N1PeerSummary{}, ErrN1Guest
	}
	if err != nil || ctx.Err() != nil || !parser.final || !(result.exit == 0 && parser.summary.Complete || result.exit == 1 && !parser.summary.Complete) {
		return parser.summary, ErrN1Guest
	}
	return parser.summary, nil
}
