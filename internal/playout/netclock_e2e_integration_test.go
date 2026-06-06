//go:build integration

/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package playout

import (
	"fmt"
	"net"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/gstnet"
	"github.com/go-gst/go-gst/gst"
)

// Each scenario runs for this long. Long enough that independent clocks
// drift visibly but short enough to keep the test responsive.
const captureSec = 5

type rtpSample struct {
	seq     uint16
	arrived time.Time
}

// runScenario spawns two programmatic pipelines (master, slave) emitting
// 440 Hz sine via RTP-L16 to the given ports. If withNetClock is true,
// slave's clock is bound to master via gstnet.NewNetTimeProvider/
// NewNetClientClock.
//
// What we measure: rate-skew between the two pipelines. seqnum-offset=0 +
// timestamp-offset=0 force identical RTP numbering, so matching seq =
// same position in the audio stream. For each matched seq we record the
// wall-clock arrival delta (master_arrival - slave_arrival). If the two
// clocks tick at the same rate, the delta is constant; if they drift,
// the delta grows monotonically. The returned skewUs is
// (delta_at_end - delta_at_start), in microseconds.
//
// Returns:
//   - skewUs: absolute change in arrival-delta from first matched seq to
//     last, in microseconds; this is the drift signal
//   - common: how many sequence numbers were seen on BOTH sides
func runScenario(t *testing.T, masterPort, slavePort, netClockPort int, withNetClock bool) (skewUs int64, common int) {
	t.Helper()
	gst.Init(nil)

	masterPipeline, err := gst.NewPipelineFromString(fmt.Sprintf(
		"audiotestsrc is-live=true freq=440 wave=sine ! "+
			"audio/x-raw,rate=44100,channels=2,format=S16BE ! "+
			"rtpL16pay pt=10 mtu=1400 seqnum-offset=0 timestamp-offset=0 ! udpsink host=127.0.0.1 port=%d sync=true",
		masterPort))
	if err != nil {
		t.Fatal(err)
	}
	defer masterPipeline.SetState(gst.StateNull)

	slavePipeline, err := gst.NewPipelineFromString(fmt.Sprintf(
		"audiotestsrc is-live=true freq=440 wave=sine ! "+
			"audio/x-raw,rate=44100,channels=2,format=S16BE ! "+
			"rtpL16pay pt=10 mtu=1400 seqnum-offset=0 timestamp-offset=0 ! udpsink host=127.0.0.1 port=%d sync=true",
		slavePort))
	if err != nil {
		t.Fatal(err)
	}
	defer slavePipeline.SetState(gst.StateNull)

	if withNetClock {
		// Master needs to reach PAUSED so its pipeline clock is selected
		// & stable before we expose it. SetState(PAUSED) blocks until the
		// state transition completes for non-live elements; audiotestsrc
		// is live so we briefly enter PLAYING then read the clock.
		if err := masterPipeline.SetState(gst.StatePlaying); err != nil {
			t.Fatal(err)
		}
		// Give GStreamer a beat to actually pick the clock.
		time.Sleep(200 * time.Millisecond)
		masterClock := masterPipeline.GetPipelineClock()
		if masterClock == nil {
			t.Fatal("master pipeline has no clock after entering PLAYING")
		}
		provider := gstnet.NewNetTimeProvider(masterClock, "127.0.0.1", netClockPort)
		if provider == nil {
			t.Fatal("NetTimeProvider failed")
		}
		defer provider.Close()

		client := gstnet.NewNetClientClock("e2e-test", "127.0.0.1", netClockPort)
		if client == nil {
			t.Fatal("NetClientClock failed")
		}
		if !client.WaitForSync(5 * time.Second) {
			t.Fatal("client never synced to master")
		}
		slavePipeline.ForceClock(client.Clock)
	}

	// Start capture FIRST so we don't miss early packets
	masterCh := make(chan rtpSample, 10000)
	slaveCh := make(chan rtpSample, 10000)
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go capturePort(t, masterPort, masterCh, stop, &wg)
	go capturePort(t, slavePort, slaveCh, stop, &wg)
	time.Sleep(100 * time.Millisecond)

	// Drain pipeline busses so errors surface and the bus doesn't back up.
	go drainBus(t, masterPipeline, "master", stop)
	go drainBus(t, slavePipeline, "slave", stop)

	// Master may already be PLAYING (NetClock path); SetState is idempotent.
	if err := masterPipeline.SetState(gst.StatePlaying); err != nil {
		t.Fatal(err)
	}
	if err := slavePipeline.SetState(gst.StatePlaying); err != nil {
		t.Fatal(err)
	}
	// Block until both pipelines have actually reached PLAYING. Live sources
	// stay in PAUSED until told to run; an async-handled SetState that never
	// completes means no buffers ever push.
	if r, _ := masterPipeline.GetState(gst.StatePlaying, gst.ClockTime(2*time.Second.Nanoseconds())); r != gst.StateChangeSuccess {
		t.Logf("master GetState returned %v", r)
	}
	if r, _ := slavePipeline.GetState(gst.StatePlaying, gst.ClockTime(2*time.Second.Nanoseconds())); r != gst.StateChangeSuccess {
		t.Logf("slave GetState returned %v", r)
	}

	time.Sleep(captureSec * time.Second)
	close(stop)
	wg.Wait()
	close(masterCh)
	close(slaveCh)

	masterMap := map[uint16]time.Time{}
	for s := range masterCh {
		masterMap[s.seq] = s.arrived
	}
	slaveMap := map[uint16]time.Time{}
	for s := range slaveCh {
		slaveMap[s.seq] = s.arrived
	}

	var seqs []uint16
	for seq := range masterMap {
		if _, ok := slaveMap[seq]; ok {
			seqs = append(seqs, seq)
		}
	}
	sort.Slice(seqs, func(i, j int) bool { return seqs[i] < seqs[j] })

	// Drop the first 25% of samples; pipeline startup is noisy as both
	// pipelines reach steady state at different points. Steady-state drift
	// is what matters for the clock-sync claim.
	skip := len(seqs) / 4
	if len(seqs)-skip < 20 {
		return 0, len(seqs)
	}
	stable := seqs[skip:]

	// Average the first 10 & last 10 deltas to smooth single-packet jitter.
	const window = 10
	if len(stable) < 2*window {
		return 0, len(seqs)
	}
	var firstSum, lastSum int64
	for i := 0; i < window; i++ {
		firstSum += masterMap[stable[i]].Sub(slaveMap[stable[i]]).Microseconds()
		lastSum += masterMap[stable[len(stable)-1-i]].Sub(slaveMap[stable[len(stable)-1-i]]).Microseconds()
	}
	firstAvg := firstSum / window
	lastAvg := lastSum / window
	skewUs = lastAvg - firstAvg
	if skewUs < 0 {
		skewUs = -skewUs
	}
	return skewUs, len(seqs)
}

func drainBus(t *testing.T, p *gst.Pipeline, label string, stop <-chan struct{}) {
	bus := p.GetPipelineBus()
	for {
		select {
		case <-stop:
			return
		default:
		}
		msg := bus.TimedPop(gst.ClockTime(100 * time.Millisecond.Nanoseconds()))
		if msg == nil {
			continue
		}
		if msg.Type() == gst.MessageError {
			t.Logf("%s bus error: %v", label, msg.ParseError())
		}
	}
}

func capturePort(t *testing.T, port int, ch chan<- rtpSample, stop <-chan struct{}, wg *sync.WaitGroup) {
	defer wg.Done()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: port})
	if err != nil {
		t.Logf("port %d listen err: %v", port, err)
		return
	}
	// Larger UDP read buffer so we don't drop bursts of RTP packets.
	_ = conn.SetReadBuffer(1 << 20)
	defer conn.Close()
	buf := make([]byte, 1500)
	gotCount := 0
	for {
		select {
		case <-stop:
			t.Logf("port %d capture done, packets=%d", port, gotCount)
			return
		default:
		}
		_ = conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			continue
		}
		if n < 12 {
			continue
		}
		// Record arrival time immediately so kernel-buffered jitter is
		// minimised; the goroutine schedulers add some noise either way.
		arrived := time.Now()
		gotCount++
		seq := uint16(buf[2])<<8 | uint16(buf[3])
		select {
		case ch <- rtpSample{seq: seq, arrived: arrived}:
		default:
		}
	}
}

func TestNetClock_AlignmentEndToEnd(t *testing.T) {
	// Scenario A: no NetClock (baseline). On a single host both pipelines
	// fall back to GstSystemClock, so the baseline is already well-aligned;
	// it serves as a sanity floor, not as a value to beat.
	baseSkew, baseCommon := runScenario(t, 15004, 15005, 0, false)
	t.Logf("Baseline (no NetClock):  rate-skew = %d us (%.3f ms) over capture window, common-seq = %d",
		baseSkew, float64(baseSkew)/1000.0, baseCommon)

	// Scenario B: with NetClock
	netSkew, netCommon := runScenario(t, 15006, 15007, 19094, true)
	t.Logf("With NetClock:           rate-skew = %d us (%.3f ms) over capture window, common-seq = %d",
		netSkew, float64(netSkew)/1000.0, netCommon)

	if baseCommon < 50 || netCommon < 50 {
		t.Fatalf("not enough common packets to compare; base=%d net=%d", baseCommon, netCommon)
	}

	// Load-bearing assertion: NetClock-bound pipelines must stay rate-aligned
	// over the capture window. 10ms drift on a 5s window means ~2ms/s, which
	// would be audible & unacceptable for broadcast. A correctly-bound
	// NetClient clock on localhost should land well under 1ms drift.
	if netSkew > 10_000 {
		t.Errorf("NetClock rate-skew = %d us over %ds (>10ms = clock binding not actually sync'd)",
			netSkew, captureSec)
	}

	// Sanity floor: baseline shouldn't be wildly worse than NetClock on
	// the same host (both share GstSystemClock when unbound). If baseline
	// is huge it usually means the test is observing UDP/scheduling noise
	// rather than clock behaviour, & the NetClock comparison is suspect.
	if baseSkew > 50_000 {
		t.Logf("WARNING: baseline rate-skew = %d us is unusually high; jitter floor may dominate",
			baseSkew)
	}
}
