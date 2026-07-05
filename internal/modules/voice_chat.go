package modules

import (
	"fmt"
	"strings"

	"main/internal/core"
	"main/internal/database"
	"main/ubot"

	"github.com/Laky-64/gologging"
)

func InitVoiceChatHandlers(assistants *core.AssistantManager) {
	if assistants == nil {
		return
	}

	assistants.ForEach(func(a *core.Assistant) {
		a.Ntg.OnGroupCallMessage(SafeGroupCallMessageHandler(handleGroupCallMessage))
	})
}

func replyVoiceChat(event *ubot.GroupCallMessageEvent, text string) {
	assistant, err := core.Assistants.ForChat(event.ChatID)
	if err != nil {
		return
	}

	_ = assistant.Ntg.SendGroupCallMessage(event.Call, text)
}

func parseVoiceChatCommand(text string) (string, []string) {
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "/")

	fields := strings.Fields(text)
	if len(fields) == 0 {
		return "", nil
	}

	cmd := strings.ToLower(fields[0])

	if at := strings.Index(cmd, "@"); at != -1 {
		cmd = cmd[:at]
	}

	return cmd, fields[1:]
}

func getVoiceChatRoom(event *ubot.GroupCallMessageEvent) (*core.RoomState, bool) {
	room, ok := core.GetRoom(event.ChatID, nil, false)
	if !ok || room == nil || room.IsDestroyed() {
		return nil, false
	}

	if !room.IsActiveChat() {
		return nil, false
	}

	return room, true
}

func handleVoiceChatPause(event *ubot.GroupCallMessageEvent, senderName string) {
	room, ok := getVoiceChatRoom(event)
	if !ok {
		replyVoiceChat(event, "🌚 یه چیز اول پخش کن بعد دستور بده")
		return
	}

	if room.IsPaused() {
		replyVoiceChat(event, "خو وایسادم دیگه")
		return
	}

	if _, err := room.Pause(); err != nil {
		gologging.ErrorF("VC pause failed in chat %d: %v", event.ChatID, err)
		replyVoiceChat(event, "مکث کردن آهنگ ناموفق بود ⏸️")
		return
	}

	schedulePlaybackPanelRefresh(event.ChatID, room, "paused", senderName)
	replyVoiceChat(event, "👍")
}

func handleVoiceChatResume(event *ubot.GroupCallMessageEvent, senderName string) {
	room, ok := getVoiceChatRoom(event)
	if !ok {
		replyVoiceChat(event, "چیزی نیس برا پخش")
		return
	}

	if !room.IsPaused() {
		replyVoiceChat(event, "خو پخشه الان دیگه")
		return
	}

	if _, err := room.Resume(); err != nil {
		gologging.ErrorF("VC resume failed in chat %d: %v", event.ChatID, err)
		replyVoiceChat(event, "نمیتونم ادامه بدم یه مشکلی هس")
		return
	}

	schedulePlaybackPanelRefresh(event.ChatID, room, "playing", senderName)
	replyVoiceChat(event, "👍")
}

func handleGroupCallMessage(event *ubot.GroupCallMessageEvent) {
	text := strings.TrimSpace(event.Text)
	if text == "" {
		return
	}

	// فیلتر maintenance
	if !canBypassMaintenence(event.SenderID) {
		return
	}

	// فیلتر blacklisted chat
	if blocked, _ := database.IsBlacklistedChat(event.ChatID); blocked {
		return
	}

	// فیلتر blacklisted user
	if blocked, _ := database.IsBlacklistedUser(event.SenderID); blocked {
		return
	}

	senderName := event.SenderName
	if senderName == "" {
		senderName = fmt.Sprintf("کاربر %d", event.SenderID)
	}

	cmd, _ := parseVoiceChatCommand(text)

	switch cmd {
	case "vcping", "ping":
		replyVoiceChat(event, "پونگ 🏓")

	case "سلام", "hi", "hello":
		texss := fmt.Sprintf("سلام، %s داخل ویس‌کال هستم 🎧", senderName)
		replyVoiceChat(event, texss)

	case "help":
		replyVoiceChat(event, "دستورات ویس‌کال:\n/vcping\n/help")

	case "pause", "vcpause", "vc_pause", "مکث", "توقف":
		handleVoiceChatPause(event, senderName)

	case "resume", "vcresume", "vc_resume", "ادامه", "پخش":
		handleVoiceChatResume(event, senderName)

	default:
		return
	}

	gologging.InfoF("=> %s sent command %s in VC chat %d", senderName, cmd, event.ChatID)

}

func SafeGroupCallMessageHandler(
	handler func(*ubot.GroupCallMessageEvent),
) func(*ubot.GroupCallMessageEvent) {
	return func(event *ubot.GroupCallMessageEvent) {
		defer func() {
			if r := recover(); r != nil {
				gologging.ErrorF(
					"panic in voice chat message handler: %v",
					r,
				)
			}
		}()

		handler(event)
	}
}
