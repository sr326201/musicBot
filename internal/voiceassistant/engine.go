package voiceassistant

import (
	"fmt"
	"sync"

	"github.com/Laky-64/gologging"

	"main/internal/voiceassistant/whisper"
	"main/ntgcalls"
)

// Engine is the central voice assistant controller.
type Engine struct {
	sessions    map[int64]*VASession
	transcriber *whisper.Processor
	config      Config
	mu          sync.RWMutex
}

// NewEngine creates and initializes a new voice assistant engine.
func NewEngine(cfg Config) (*Engine, error) {
	if !cfg.Enabled {
		gologging.Info("[voiceassistant] voice assistant is disabled")
		return &Engine{
			sessions: make(map[int64]*VASession),
			config:   cfg,
		}, nil
	}

	// Load whisper model
	model, err := whisper.NewModel(cfg.ModelPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load whisper model: %w", err)
	}

	processor := whisper.NewProcessor(model, cfg.NThreads)

	gologging.Info("[voiceassistant] engine initialized successfully")

	return &Engine{
		sessions:    make(map[int64]*VASession),
		transcriber: processor,
		config:      cfg,
	}, nil
}

// OnVoiceFrame is the callback registered with ntgcalls.OnFrame.
// It receives raw audio frames from the voice call.
func (e *Engine) OnVoiceFrame(chatID int64, mode ntgcalls.StreamMode, device ntgcalls.StreamDevice, frames []ntgcalls.Frame) {
	gologging.DebugF("[va:%d] RAW FRAME: mode=%v, device=%v, count=%d", chatID, mode, device, len(frames))

	if mode != ntgcalls.PlaybackStream || device != ntgcalls.MicrophoneStream {
		return
	}

	session := e.getOrCreateSession(chatID)
	if session == nil || !session.isActive.Load() {
		return
	}

	for _, frame := range frames {
		session.FeedAudio(frame.Data)
	}
}

// StartListening activates voice assistant for a chat.
func (e *Engine) StartListening(chatID int64) error {
	if !e.config.Enabled {
		return fmt.Errorf("voice assistant is disabled")
	}

	session := e.getOrCreateSession(chatID)
	if session == nil {
		return fmt.Errorf("failed to create session for chat %d", chatID)
	}

	session.isActive.Store(true)
	gologging.InfoF("[va:%d] listening started", chatID)
	return nil
}

// StopListening deactivates voice assistant for a chat.
func (e *Engine) StopListening(chatID int64) {
	e.mu.Lock()
	session, exists := e.sessions[chatID]
	e.mu.Unlock()

	if exists && session != nil {
		session.isActive.Store(false)
		session.buffer.Clear()
		session.vad.Reset()
		gologging.InfoF("[va:%d] listening stopped", chatID)
	}
}

// IsListening returns whether voice assistant is active for a chat.
func (e *Engine) IsListening(chatID int64) bool {
	e.mu.RLock()
	session, exists := e.sessions[chatID]
	e.mu.RUnlock()

	if !exists || session == nil {
		return false
	}
	return session.isActive.Load()
}

// getOrCreateSession retrieves or creates a session for a chat.
func (e *Engine) getOrCreateSession(chatID int64) *VASession {
	e.mu.RLock()
	session, exists := e.sessions[chatID]
	e.mu.RUnlock()

	if exists {
		return session
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	// Double-check after acquiring write lock
	if session, exists = e.sessions[chatID]; exists {
		return session
	}

	session = newVASession(chatID, e)
	e.sessions[chatID] = session
	return session
}

// SetTranscriptionCallback sets the callback for transcription results.
func (e *Engine) SetTranscriptionCallback(chatID int64, fn func(chatID int64, text string)) {
	session := e.getOrCreateSession(chatID)
	if session != nil {
		session.onTranscription = fn
	}
}

// SetCommandCallback sets the callback for parsed voice commands.
func (e *Engine) SetCommandCallback(chatID int64, fn func(chatID int64, command VACommand)) {
	session := e.getOrCreateSession(chatID)
	if session != nil {
		session.onCommand = fn
	}
}

// Close shuts down all sessions and frees resources.
func (e *Engine) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()

	for chatID, session := range e.sessions {
		session.isActive.Store(false)
		session.buffer.Clear()
		delete(e.sessions, chatID)
	}

	gologging.Info("[voiceassistant] engine closed")
}

func (e *Engine) GetLanguage() string {
	return e.config.Language
}

// func PlaybackFrameDescription() ntgcalls.MediaDescription {
// 	return ntgcalls.MediaDescription{
// 		Microphone: &ntgcalls.AudioDescription{
// 			MediaSource:  ntgcalls.MediaSourceExternal,
// 			Input:        "",
// 			SampleRate:   ntgSampleRate,
// 			ChannelCount: ntgChannels,
// 			KeepOpen:     true,
// 		},
// 	}
// }
