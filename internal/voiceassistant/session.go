package voiceassistant

import (
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Laky-64/gologging"
)

// VASession represents a voice assistant session for a single chat.
type VASession struct {
	chatID int64
	engine *Engine

	buffer     *RingBuffer
	vad        *VAD
	isActive   atomic.Bool
	lastSpeech time.Time
	processMu  sync.Mutex // prevents concurrent utterance processing

	// Callbacks
	onTranscription func(chatID int64, text string)
	onCommand       func(chatID int64, command VACommand)
}

// VACommand represents a parsed voice command.
type VACommand struct {
	Type    string // "play", "pause", "skip", "volume_up", "volume_down", "stop", "unknown"
	Args    string
	RawText string
}

// newVASession creates a new session for a chat.
func newVASession(chatID int64, engine *Engine) *VASession {
	return &VASession{
		chatID: chatID,
		engine: engine,
		buffer: NewRingBuffer(10 * 1024 * 1024), // 10 MB ≈ ~26 sec @ 96kHz stereo s16le
		vad:    NewVAD(DefaultVADConfig()),
	}
}

// FeedAudio processes incoming PCM audio frames from ntgcalls.
func (s *VASession) FeedAudio(pcmData []byte) {
	if !s.isActive.Load() || len(pcmData) == 0 {
		return
	}

	// 1. Write to buffer
	s.buffer.Write(pcmData)

	// 2. Run VAD
	if s.vad.DetectSpeech(pcmData) {
		s.lastSpeech = time.Now()
	}

	// 3. Check if utterance is complete
	if s.vad.IsUtteranceComplete(s.lastSpeech) {
		// Prevent concurrent processing
		if s.processMu.TryLock() {
			go func() {
				defer s.processMu.Unlock()
				s.processUtterance()
			}()
		}
	}
}

// processUtterance takes the accumulated audio, converts it, and transcribes.
func (s *VASession) processUtterance() {
	pcmChunk := s.buffer.GetAndCopy()
	if len(pcmChunk) == 0 {
		return
	}

	// Convert s16le 96kHz stereo → float32 16kHz mono
	samples := convertS16LEToFloat32Mono(pcmChunk, ntgSampleRate, ntgChannels)
	if len(samples) == 0 {
		return
	}

	gologging.DebugF("[va:%d] processing %d samples (%.1f sec)", s.chatID, len(samples), float64(len(samples))/16000.0)

	// Transcribe
	text, err := s.engine.transcriber.Transcribe(samples, s.engine.config.Language)
	if err != nil {
		gologging.ErrorF("[va:%d] transcription error: %v", s.chatID, err)
		return
	}

	text = strings.TrimSpace(text)
	if text == "" {
		return
	}

	gologging.InfoF("[va:%d] transcribed: %q", s.chatID, text)

	// Notify transcription
	if s.onTranscription != nil {
		s.onTranscription(s.chatID, text)
	}

	// Parse and execute command
	cmd := parseVoiceCommand(text)
	if cmd.Type != "unknown" {
		gologging.InfoF("[va:%d] command: %s %s", s.chatID, cmd.Type, cmd.Args)
		if s.onCommand != nil {
			s.onCommand(s.chatID, cmd)
		}
	}
}

// parseVoiceCommand matches transcribed text against known command patterns.
func parseVoiceCommand(text string) VACommand {
	lower := strings.ToLower(strings.TrimSpace(text))

	// Persian + English command patterns
	type cmdPattern struct {
		keywords []string
		cmdType  string
	}

	patterns := []cmdPattern{
		{[]string{"پخش کن", "play", "پخش"}, "play"},
		{[]string{"مکث", "pause", "ایست"}, "pause"},
		{[]string{"ادامه", "resume", "continue"}, "resume"},
		{[]string{"رد کردن", "skip", "بعدی", "next"}, "skip"},
		{[]string{"توقف", "stop", "بس کن", ".end"}, "stop"},
		{[]string{"صدا زیاد", "volume up", "صداتو زیاد", " louder"}, "volume_up"}, // ← قبل از mute/unmute
		{[]string{"صدا کم", "volume down", "صداتو کم", "quieter"}, "volume_down"}, // ← قبل از mute/unmute
		{[]string{"بیصدا", "mute", "سکوت"}, "mute"},
		{[]string{"باصدا", "unmute"}, "unmute"}, // ← "صدا" رو حذف کن
	}

	for _, p := range patterns {
		for _, kw := range p.keywords {
			if strings.Contains(lower, kw) {
				// Extract args (text after the keyword)
				idx := strings.Index(lower, kw)
				args := strings.TrimSpace(lower[idx+len(kw):])
				return VACommand{
					Type:    p.cmdType,
					Args:    args,
					RawText: text,
				}
			}
		}
	}

	return VACommand{Type: "unknown", RawText: text}
}
