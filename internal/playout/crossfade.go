package playout

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// pcmCrossfadeSession decodes media to raw PCM using GStreamer and writes it into a single
// encoder pipeline stdin. Crossfades are implemented in Go by mixing S16LE samples.
//
// This avoids CGO and keeps Icecast/ffmpeg out of the critical path.
// It is intentionally scoped to scheduled playout (not Live DJ).
type pcmCrossfadeSession struct {
	cfg sessionConfig

	mu      sync.Mutex
	cur     *decoderProc
	next    *decoderProc
	xfade   *xfadeState
	closing bool

	// Encoder sink
	encoderIn io.WriteCloser

	// Track end callback (called after current decoder hits EOF)
	onTrackEnd func()

	logger zerolog.Logger
}

type sessionConfig struct {
	GStreamerBin string
	SampleRate   int
	Channels     int
}

type decoderProc struct {
	cmd    *exec.Cmd
	stdout io.ReadCloser
	cancel context.CancelFunc
}

type xfadeState struct {
	start    time.Time
	duration time.Duration
}

func newPCMCrossfadeSession(cfg sessionConfig, encoderIn io.WriteCloser, logger zerolog.Logger, onTrackEnd func()) *pcmCrossfadeSession {
	return &pcmCrossfadeSession{
		cfg:        cfg,
		encoderIn:  encoderIn,
		onTrackEnd: onTrackEnd,
		logger:     logger.With().Str("component", "pcm-xfade").Logger(),
	}
}

func (s *pcmCrossfadeSession) SetEncoderIn(w io.WriteCloser) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.encoderIn = w
}

func (s *pcmCrossfadeSession) SetOnTrackEnd(fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onTrackEnd = fn
}

func (s *pcmCrossfadeSession) Close() error {
	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		return nil
	}
	s.closing = true
	cur := s.cur
	next := s.next
	in := s.encoderIn
	s.mu.Unlock()

	if cur != nil {
		_ = cur.stop()
	}
	if next != nil {
		_ = next.stop()
	}
	if in != nil {
		_ = in.Close()
	}
	return nil
}

func (s *pcmCrossfadeSession) Play(ctx context.Context, filePath string, fade time.Duration) error {
	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		return fmt.Errorf("session closing")
	}
	prev := s.cur
	s.mu.Unlock()

	dec, err := s.startDecoder(ctx, filePath)
	if err != nil {
		return err
	}

	s.mu.Lock()
	if s.cur == nil {
		s.cur = dec
		s.mu.Unlock()
		return nil
	}
	// Crossfade from current to next.
	s.next = dec
	if fade > 0 {
		s.xfade = &xfadeState{start: time.Now(), duration: fade}
	} else {
		s.xfade = &xfadeState{start: time.Now(), duration: 0}
	}
	s.mu.Unlock()

	// We do not stop prev here; mixer loop will stop it after fade completes.
	_ = prev
	return nil
}

func (s *pcmCrossfadeSession) startDecoder(ctx context.Context, filePath string) (*decoderProc, error) {
	rate := s.cfg.SampleRate
	if rate <= 0 {
		rate = 44100
	}
	ch := s.cfg.Channels
	if ch <= 0 {
		ch = 2
	}

	// Real-time decode to S16LE PCM on stdout.
	pipeline := fmt.Sprintf(
		`filesrc location=%q ! decodebin ! audioconvert ! audioresample ! audio/x-raw,format=S16LE,rate=%d,channels=%d ! identity sync=true ! fdsink fd=1`,
		filePath, rate, ch,
	)

	cmdCtx, cancel := context.WithCancel(ctx)
	shellCmd := fmt.Sprintf("%s -e %s", s.cfg.GStreamerBin, pipeline)
	cmd := exec.CommandContext(cmdCtx, "sh", "-c", shellCmd)
	cmd.Stderr = nil

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("decoder stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start decoder: %w", err)
	}

	s.logger.Debug().Int("pid", cmd.Process.Pid).Str("pipeline", pipeline).Msg("decoder started")

	return &decoderProc{cmd: cmd, stdout: stdout, cancel: cancel}, nil
}

func (d *decoderProc) stop() error {
	if d == nil {
		return nil
	}
	if d.cancel != nil {
		d.cancel()
	}
	if d.stdout != nil {
		_ = d.stdout.Close()
	}
	if d.cmd != nil && d.cmd.Process != nil {
		_ = d.cmd.Process.Kill()
	}
	return nil
}

// Pump continuously writes mixed PCM to encoder stdin.
// It is safe to call Pump once per mount/session.
func (s *pcmCrossfadeSession) Pump(ctx context.Context) error {
	rate := s.cfg.SampleRate
	if rate <= 0 {
		rate = 44100
	}
	ch := s.cfg.Channels
	if ch <= 0 {
		ch = 2
	}

	// 20ms frames.
	frameSamples := rate / 50
	if frameSamples <= 0 {
		frameSamples = 882
	}
	frameBytes := frameSamples * ch * 2

	curBuf := make([]byte, frameBytes)
	nextBuf := make([]byte, frameBytes)
	mixBuf := make([]byte, frameBytes)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		s.mu.Lock()
		if s.closing {
			s.mu.Unlock()
			return nil
		}
		cur := s.cur
		next := s.next
		xf := s.xfade
		enc := s.encoderIn
		onEnd := s.onTrackEnd
		s.mu.Unlock()

		if enc == nil {
			time.Sleep(25 * time.Millisecond)
			continue
		}
		if cur == nil || cur.stdout == nil {
			time.Sleep(25 * time.Millisecond)
			continue
		}

		// Read current frame.
		if err := readFrame(cur.stdout, curBuf); err != nil {
			// EOF: signal and wait for the next Play() call to set a new current.
			if onEnd != nil {
				go onEnd()
			}
			s.mu.Lock()
			_ = cur.stop()
			s.cur = nil
			s.xfade = nil
			s.next = nil
			s.mu.Unlock()
			time.Sleep(25 * time.Millisecond)
			continue
		}

		if next == nil || next.stdout == nil || xf == nil {
			// No crossfade active.
			if _, err := enc.Write(curBuf); err != nil {
				return err
			}
			continue
		}

		// Crossfade active: read next frame and mix.
		if err := readFrame(next.stdout, nextBuf); err != nil {
			// Next not ready/ended; fall back to current.
			if _, err2 := enc.Write(curBuf); err2 != nil {
				return err2
			}
			continue
		}

		elapsed := time.Since(xf.start)
		dur := xf.duration
		var p float64
		if dur <= 0 {
			p = 1.0
		} else {
			p = float64(elapsed) / float64(dur)
		}
		if p < 0 {
			p = 0
		}
		if p > 1 {
			p = 1
		}
		curV := 1.0 - p
		nextV := p

		mixS16LE(curBuf, nextBuf, mixBuf, curV, nextV)
		if _, err := enc.Write(mixBuf); err != nil {
			return err
		}

		if p >= 1.0 {
			// Fade complete: promote next to current.
			s.mu.Lock()
			old := s.cur
			s.cur = s.next
			s.next = nil
			s.xfade = nil
			s.mu.Unlock()
			if old != nil {
				_ = old.stop()
			}
		}
	}
}

func readFrame(r io.Reader, buf []byte) error {
	_, err := io.ReadFull(r, buf)
	return err
}

func mixS16LE(a, b, out []byte, av, bv float64) {
	// Mix signed 16-bit little-endian samples.
	// Clamp to [-32768, 32767].
	for i := 0; i+1 < len(out); i += 2 {
		// little endian int16
		as := int16(uint16(a[i]) | uint16(a[i+1])<<8)
		bs := int16(uint16(b[i]) | uint16(b[i+1])<<8)
		m := int32(float64(as)*av + float64(bs)*bv)
		if m > 32767 {
			m = 32767
		} else if m < -32768 {
			m = -32768
		}
		u := uint16(int16(m))
		out[i] = byte(u & 0xff)
		out[i+1] = byte((u >> 8) & 0xff)
	}
}
