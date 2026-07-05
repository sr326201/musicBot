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
		gologging.DebugF("[va:%d] VAD: Speech detected", s.chatID)
	}

	// 3. Check if utterance is complete
	if s.vad.IsUtteranceComplete(s.lastSpeech) {
		// Prevent concurrent processing
		if s.processMu.TryLock() {
			gologging.InfoF("[va:%d] VAD: Utterance complete, starting transcription...", s.chatID)
			go func() {
				defer s.processMu.Unlock()
				s.processUtterance()
			}()
		}
	}
}

// processUtterance takes the accumulated audio, converts it, and transcribes.
// internal/voiceassistant/session.go

func (s *VASession) processUtterance() {
	pcmChunk := s.buffer.GetAndCopy()
	if len(pcmChunk) == 0 {
		gologging.DebugF("[va:%d] processUtterance: buffer is empty", s.chatID)
		return
	}

	samples := convertS16LEToFloat32Mono(pcmChunk, ntgSampleRate, ntgChannels)
	if len(samples) == 0 {
		return
	}

	gologging.DebugF("[va:%d] processing %d samples (%.1f sec)", s.chatID, len(samples), float64(len(samples))/16000.0)

	gologging.InfoF("[va:%d] sending audio to Whisper for transcription...", s.chatID)

	text, err := s.engine.transcriber.Transcribe(samples, s.engine.config.Language)
	if err != nil {
		gologging.ErrorF("[va:%d] transcription error: %v", s.chatID, err)
		return
	}

	text = strings.TrimSpace(text)
	if text == "" {
		gologging.DebugF("[va:%d] Whisper returned empty text", s.chatID)
		return
	}

	gologging.InfoF("[va:%d] transcribed text: %q", s.chatID, text)

	if s.onTranscription != nil {
		s.onTranscription(s.chatID, text)
	}

	cmd := parseVoiceCommand(text)
	if cmd.Type != "unknown" {
		gologging.InfoF("[va:%d] matched command: %s (args: %s)", s.chatID, cmd.Type, cmd.Args)
		if s.onCommand != nil {
			s.onCommand(s.chatID, cmd)
		}
	} else {
		gologging.DebugF("[va:%d] text did not match any known command", s.chatID)
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
		{[]string{"صدا زیاد", "volume up", "صداتو زیاد", " louder"}, "volume_up"},
		{[]string{"صدا کم", "volume down", "صداتو کم", "quieter"}, "volume_down"},
		{[]string{"بیصدا", "mute", "سکوت"}, "mute"},
		{[]string{"باصدا", "unmute"}, "unmute"},
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
