package voiceassistant

import (
	"fmt"

	"github.com/Laky-64/gologging"
	"github.com/amarnathcjd/gogram/telegram"

	"main/internal/core"
	"main/ntgcalls"
)

var vaEngine *Engine

// InitVoiceAssistant initializes the voice assistant and registers handlers.
// Call this after core.Init() in main.go.
func InitVoiceAssistant(engine *Engine) {
	vaEngine = engine
}

// vaStartHandler handles /va_start command
func VaStartHandler(m *telegram.NewMessage) error {
	chatID := m.ChannelID()

	if vaEngine == nil {
		m.Reply("Voice assistant is not available")
		return telegram.ErrEndGroup
	}

	if err := vaEngine.StartListening(chatID); err != nil {
		m.Reply(fmt.Sprintf("Failed to start voice assistant: %v", err))
		return telegram.ErrEndGroup
	}

	// Set callbacks for this chat
	vaEngine.SetTranscriptionCallback(chatID, func(chatID int64, text string) {
		// Optional: send transcription to chat
		// bot.SendMessage(chatID, fmt.Sprintf("🗣 %s", text))
	})

	vaEngine.SetCommandCallback(chatID, handleVoiceCommand)

	ass, err := core.Assistants.ForChat(chatID)
	if err == nil && ass != nil && ass.Ntg != nil {
		if ass.Ntg.Calls()[chatID] != nil {
			if err := ass.Ntg.Record(chatID, PlaybackFrameDescription()); err != nil {
				gologging.ErrorF("[va:%d] failed to arm playback stream on /va_start: %v", chatID, err)
			} else {
				gologging.InfoF("[va:%d] playback stream armed on /va_start", chatID)
			}
		}
	}

	m.Reply("🎙 Voice assistant started")
	return telegram.ErrEndGroup
}

// vaStopHandler handles /va_stop command
func VaStopHandler(m *telegram.NewMessage) error {
	chatID := m.ChannelID()

	if vaEngine == nil {
		return telegram.ErrEndGroup
	}

	vaEngine.StopListening(chatID)

	// m.Reply(F(chatID, "va_stopped"))
	m.Reply("🔇 Voice assistant stopped")

	return telegram.ErrEndGroup
}

// vaStatusHandler handles /va_status command
func VaStatusHandler(m *telegram.NewMessage) error {
	chatID := m.ChannelID()

	if vaEngine == nil {
		m.Reply("Voice assistant is not available")
		return telegram.ErrEndGroup
	}

	status := "🔴 Inactive"
	if vaEngine.IsListening(chatID) {
		status = "🟢 Listening"
	}

	// m.Reply(fmt.Sprintf("Voice Assistant: %s\nLanguage: %s", status, vaEngine.config.Language))
	m.Reply(fmt.Sprintf("Voice Assistant: %s\nLanguage: %s", status, vaEngine.GetLanguage()))

	return telegram.ErrEndGroup
}

// handleVoiceCommand processes a parsed voice command and executes it.
func handleVoiceCommand(chatID int64, cmd VACommand) {
	ass, err := core.Assistants.ForChat(chatID)
	if err != nil {
		return
	}

	r, ok := core.GetRoom(chatID, ass, false)
	if !ok || r == nil {
		return
	}

	switch cmd.Type {
	case "pause":
		if _, err := r.Pause(); err != nil {
			gologging.ErrorF("[va:%d] pause error: %v", chatID, err)
		}
	case "resume":
		if _, err := r.Resume(); err != nil {
			gologging.ErrorF("[va:%d] resume error: %v", chatID, err)
		}
	case "skip":
		r.NextTrack()
	case "stop":
		core.DeleteRoom(chatID)
	case "volume_up":
		vol := r.Volume() + 0.10
		if vol > 1.0 {
			vol = 1.0
		}
		r.SetVolume(vol)
	case "volume_down":
		vol := r.Volume() - 0.10
		if vol < 0 {
			vol = 0
		}
		r.SetVolume(vol)
	case "mute":
		r.Mute()
	case "unmute":
		r.Unmute()
	}
}

func PlaybackFrameDescription() ntgcalls.MediaDescription {
	return ntgcalls.MediaDescription{
		Microphone: &ntgcalls.AudioDescription{
			MediaSource:  ntgcalls.MediaSourceExternal,
			Input:        "",
			SampleRate:   ntgSampleRate,
			ChannelCount: ntgChannels,
			KeepOpen:     true,
		},
	}
}
