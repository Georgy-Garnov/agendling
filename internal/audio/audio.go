// Package audio plays reminder sounds (MP3/WAV or a built-in chime) with instant stop.
//
// The oto context is created lazily on first use and suspended whenever nothing is
// playing, so the audio device costs no CPU while the app is idle.
package audio

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ebitengine/oto/v3"
	"github.com/gopxl/beep/v2"
	"github.com/gopxl/beep/v2/mp3"
	"github.com/gopxl/beep/v2/wav"
)

const (
	sampleRate = 48000
	// Small device buffer: Stop() is audible within ~40 ms.
	deviceBuffer = 40 * time.Millisecond
	// Looping alarms give up eventually so a forgotten popup doesn't ring forever.
	maxLoopDuration = 3 * time.Minute
)

type Manager struct {
	mu      sync.Mutex
	ctx     *oto.Context
	player  *oto.Player
	current *stream
}

func NewManager() *Manager { return &Manager{} }

func (m *Manager) ensureContext() error {
	if m.ctx != nil {
		return m.ctx.Resume()
	}
	ctx, ready, err := oto.NewContext(&oto.NewContextOptions{
		SampleRate:   sampleRate,
		ChannelCount: 2,
		Format:       oto.FormatFloat32LE,
		BufferSize:   deviceBuffer,
	})
	if err != nil {
		return fmt.Errorf("audio init: %w", err)
	}
	<-ready
	m.ctx = ctx
	return nil
}

// Play stops any current sound and starts the given file (empty path = built-in chime).
// It returns immediately; decoding and output run on background goroutines.
func (m *Manager) Play(path string, loop bool) error {
	m.Stop()

	src, format, err := open(path)
	if err != nil {
		return err
	}

	var s beep.Streamer = src
	if loop {
		s = &looper{s: src, limit: format.SampleRate.N(maxLoopDuration)}
	}
	if format.SampleRate != sampleRate {
		s = beep.Resample(3, format.SampleRate, sampleRate, s)
	}
	st := &stream{s: s, closer: src, done: make(chan struct{})}

	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ensureContext(); err != nil {
		src.Close()
		return err
	}
	p := m.ctx.NewPlayer(st)
	p.SetBufferSize(int(float64(sampleRate*8) * deviceBuffer.Seconds()))
	p.Play()
	m.player, m.current = p, st

	// Dedicated goroutine: release the device once the sound ends naturally.
	go func() {
		<-st.done
		for {
			m.mu.Lock()
			if m.current != st {
				m.mu.Unlock()
				return // superseded or stopped explicitly
			}
			if !p.IsPlaying() {
				m.stopLocked()
				m.mu.Unlock()
				return
			}
			m.mu.Unlock()
			time.Sleep(50 * time.Millisecond)
		}
	}()
	return nil
}

// Stop terminates playback immediately without waiting for the file to finish.
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopLocked()
}

func (m *Manager) stopLocked() {
	if m.player != nil {
		m.player.Pause()
		m.player = nil
	}
	if m.current != nil {
		m.current.Close() // audioStreamer.Close(): no more samples are produced
		m.current = nil
	}
	if m.ctx != nil {
		_ = m.ctx.Suspend()
	}
}

func (m *Manager) IsPlaying() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.player != nil && m.player.IsPlaying()
}

func open(path string) (beep.StreamSeekCloser, beep.Format, error) {
	if path == "" {
		return newChime(), beep.Format{SampleRate: sampleRate, NumChannels: 2, Precision: 4}, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, beep.Format{}, err
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp3":
		return mp3.Decode(f)
	case ".wav":
		s, format, err := wav.Decode(f)
		if err != nil {
			f.Close()
			return nil, format, err
		}
		return &fileCloser{StreamSeekCloser: s, f: f}, format, nil
	default:
		f.Close()
		return nil, beep.Format{}, fmt.Errorf("unsupported audio format %q (use .mp3 or .wav)", filepath.Ext(path))
	}
}

// fileCloser closes the underlying file too (the wav decoder doesn't own it).
type fileCloser struct {
	beep.StreamSeekCloser
	f *os.File
}

func (c *fileCloser) Close() error {
	err := c.StreamSeekCloser.Close()
	c.f.Close()
	return err
}

// stream adapts a beep.Streamer to the io.Reader that oto pulls from its own goroutine.
type stream struct {
	mu     sync.Mutex
	s      beep.Streamer
	closer io.Closer
	closed bool
	done   chan struct{}
	once   sync.Once
	buf    [][2]float64
}

func (st *stream) Read(p []byte) (int, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.closed {
		return 0, io.EOF
	}
	frames := len(p) / 8
	if frames == 0 {
		return 0, nil
	}
	if cap(st.buf) < frames {
		st.buf = make([][2]float64, frames)
	}
	buf := st.buf[:frames]
	n, ok := st.s.Stream(buf)
	for i := 0; i < n; i++ {
		binary.LittleEndian.PutUint32(p[i*8:], math.Float32bits(float32(buf[i][0])))
		binary.LittleEndian.PutUint32(p[i*8+4:], math.Float32bits(float32(buf[i][1])))
	}
	if !ok || n == 0 {
		st.once.Do(func() { close(st.done) })
		if n == 0 {
			return 0, io.EOF
		}
	}
	return n * 8, nil
}

func (st *stream) Close() {
	st.mu.Lock()
	defer st.mu.Unlock()
	if !st.closed {
		st.closed = true
		st.closer.Close()
		st.once.Do(func() { close(st.done) })
	}
}

// looper restarts the source at EOF until `limit` samples have been produced.
type looper struct {
	s     beep.StreamSeeker
	limit int
	done  int
}

func (l *looper) Stream(samples [][2]float64) (int, bool) {
	total := 0
	for total < len(samples) && l.done < l.limit {
		want := len(samples) - total
		if rem := l.limit - l.done; want > rem {
			want = rem
		}
		n, ok := l.s.Stream(samples[total : total+want])
		total += n
		l.done += n
		if !ok || n == 0 {
			if l.s.Len() == 0 || l.s.Seek(0) != nil {
				break
			}
		}
	}
	return total, total > 0
}

func (l *looper) Err() error { return l.s.Err() }

// chime is a synthesized two-tone alert used when no custom sound is configured.
type chime struct {
	pos, length int
}

func newChime() *chime { return &chime{length: sampleRate * 3 / 2} }

func (c *chime) Stream(samples [][2]float64) (int, bool) {
	if c.pos >= c.length {
		return 0, false
	}
	n := 0
	for n < len(samples) && c.pos < c.length {
		t := float64(c.pos) / sampleRate
		freq := 880.0
		local := t
		if t >= 0.75 {
			freq, local = 660.0, t-0.75
		}
		env := math.Exp(-local*5) * 0.35
		v := env * math.Sin(2*math.Pi*freq*t)
		samples[n] = [2]float64{v, v}
		n++
		c.pos++
	}
	return n, true
}

func (c *chime) Err() error    { return nil }
func (c *chime) Len() int      { return c.length }
func (c *chime) Position() int { return c.pos }
func (c *chime) Close() error  { return nil }
func (c *chime) Seek(p int) error {
	if p < 0 || p > c.length {
		return errors.New("chime: seek out of range")
	}
	c.pos = p
	return nil
}
