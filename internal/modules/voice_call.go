package modules

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Laky-64/gologging"
	"github.com/amarnathcjd/gogram/telegram"
	tg "github.com/amarnathcjd/gogram/telegram"

	"main/internal/core"
	"main/internal/locales"
	"main/internal/utils"
	"main/ubot"
)

const (
	voiceCallSpeedStep  = 0.25
	voiceCallVolumeStep = 0.10
	voiceCallMinSpeed   = 0.50
	voiceCallMaxSpeed   = 4.00
)

const playStickerPath = "public/search-bot.tgs"

func sendPlaySticker(m *tg.NewMessage, caption string) (*tg.NewMessage, error) {
	if m == nil || m.Client == nil {
		return nil, nil
	}

	stickerPath, err := filepath.Abs(playStickerPath)
	if err != nil {
		return nil, err
	}

	if _, err := os.Stat(stickerPath); err != nil {
		return nil, err
	}

	msg, err := m.ReplyMedia(stickerPath, &tg.MediaOptions{
		FileName: "AnimatedSticker.tgs",
		MimeType: "application/x-tgsticker",
		Attributes: []tg.DocumentAttribute{
			&tg.DocumentAttributeSticker{
				Alt:        "🎵",
				Stickerset: &tg.InputStickerSetEmpty{},
			},
		},
		UpdateStickerOrder: true,
	})
	if err != nil || msg == nil {
		return nil, err
	}

	if caption != "" {
		_, editErr := msg.Edit(caption, &tg.SendOptions{
			ParseMode: "HTML",
		})
		if editErr != nil {
			gologging.ErrorF("Failed to edit play sticker caption: %v", editErr)

			_, _ = msg.Delete()

			fallback, replyErr := m.Reply(caption)
			return fallback, replyErr
		}
	}

	return msg, nil
}

func reactToCommandMessage(m *telegram.NewMessage, emoji string) {
	if m == nil || m.Client == nil || m.Peer == nil || m.ID == 0 {
		return
	}

	_, _ = m.Client.MessagesSendReaction(&telegram.MessagesSendReactionParams{
		Big:         false,
		AddToRecent: true,
		Peer:        m.Peer,
		MsgID:       m.ID,
		Reaction: []telegram.Reaction{
			&telegram.ReactionEmoji{
				Emoticon: emoji,
			},
		},
	})
}

func replyAndDeleteAfter(m *telegram.NewMessage, text string) {
	msg, err := m.Reply(text)
	if err != nil || msg == nil {
		return
	}

	go func() {
		time.Sleep(5 * time.Second)
		_, _ = msg.Delete()
		_, _ = m.Delete()
	}()
}

func voiceCallPermissionMessage(chatID int64) string {
	return F(chatID, "voice_chat_permission_required")
}

func voiceCallStartErrorMessage(chatID int64, err error) string {
	switch {
	case errors.Is(err, ubot.ErrAlreadyInGroupCall):
		return "کال درحال اجراست ✅"
	case errors.Is(err, ubot.ErrGroupCallPermissionDenied):
		return voiceCallPermissionMessage(chatID)
	default:
		return "شروع ویس‌کال ناموفق بود"
	}
}

func voiceCallEndErrorMessage(chatID int64, err error) string {
	switch {
	case errors.Is(err, ubot.ErrGroupCallAlreadyClosed):
		return "کال از قبل پایان رسیده است"
	case errors.Is(err, ubot.ErrGroupCallPermissionDenied):
		return voiceCallPermissionMessage(chatID)
	default:
		return "بستن ویس‌کال ناموفق بود"
	}
}

func voiceCallInviteErrorMessage(chatID int64, err error) string {
	switch {
	case errors.Is(err, ubot.ErrGroupCallAlreadyClosed):
		return F(chatID, "err_no_active_voicechat")
	case errors.Is(err, ubot.ErrGroupCallPermissionDenied):
		return voiceCallPermissionMessage(chatID)
	default:
		return "دریافت لینک ویس‌کال ناموفق بود: " + err.Error()
	}
}

func voiceCallCallbackErrorMessage(chatID int64, err error) string {
	if errors.Is(err, ubot.ErrGroupCallPermissionDenied) {
		return voiceCallPermissionMessage(chatID)
	}

	return F(chatID, "voice_chat_start_failed", locales.Arg{"error": err.Error()})
}

func startCallHandler(m *telegram.NewMessage) error {
	chatID := m.ChannelID()

	ass, err := core.Assistants.ForChat(chatID)
	if err != nil {
		gologging.ErrorF("failed to get assistant for call start in chat %d: %v", chatID, err)
		return telegram.ErrEndGroup
	}

	if err := ass.Ntg.StartGroupCall(chatID); err != nil {
		if errors.Is(err, ubot.ErrAlreadyInGroupCall) {
			if cs, csErr := core.GetChatState(chatID); csErr == nil {
				cs.SetVoiceChatActive(true)
			}
			replyAndDeleteAfter(m, voiceCallStartErrorMessage(chatID, err))
			return telegram.ErrEndGroup
		}

		gologging.ErrorF("failed to start voice call in chat %d: %v", chatID, err)
		replyAndDeleteAfter(m, voiceCallStartErrorMessage(chatID, err))
		return telegram.ErrEndGroup
	}

	if cs, csErr := core.GetChatState(chatID); csErr == nil {
		cs.SetVoiceChatActive(true)
	}

	reactToCommandMessage(m, "👍")
	return telegram.ErrEndGroup
}

func endCallHandler(m *telegram.NewMessage) error {
	chatID := m.ChannelID()

	ass, err := core.Assistants.ForChat(chatID)
	if err != nil {
		gologging.ErrorF("failed to get assistant for call end in chat %d: %v", chatID, err)
		return telegram.ErrEndGroup
	}

	r, ok := core.GetRoom(chatID, ass, false)

	if err := ass.Ntg.EndGroupCall(chatID); err != nil {
		if errors.Is(err, ubot.ErrGroupCallAlreadyClosed) {
			if cs, csErr := core.GetChatState(chatID); csErr == nil {
				cs.SetVoiceChatActive(false)
			}
			core.DeleteRoom(chatID)
			replyAndDeleteAfter(m, voiceCallEndErrorMessage(chatID, err))
			return telegram.ErrEndGroup
		}

		gologging.ErrorF("failed to end voice call in chat %d: %v", chatID, err)
		replyAndDeleteAfter(m, voiceCallEndErrorMessage(chatID, err))
		return telegram.ErrEndGroup
	}

	if cs, csErr := core.GetChatState(chatID); csErr == nil {
		cs.SetVoiceChatActive(false)
	}

	reactToCommandMessage(m, "👍")

	if ok && r != nil {
		track := r.Track()
		if track == nil {
			core.DeleteRoom(chatID)
			return telegram.ErrEndGroup
		}

		title := utils.EscapeHTML(utils.ShortTitle(track.Title, 35))

		closePlaybackPanel(r, F(
			chatID,
			"stopped",
			locales.Arg{
				"user":     utils.MentionHTML(m.Sender),
				"title":    title,
				"duration": utils.FormatDuration(track.Duration),
				"url":      track.URL,
			},
		))
		scheduleOldPlayingMessage(r)
		core.DeleteRoom(chatID)
	}

	return telegram.ErrEndGroup
}

func voiceChatConfirmCB(cb *tg.CallbackQuery) error {
	opt := &tg.CallbackOptions{Alert: false}
	chatID := cb.ChannelID()

	parts := strings.SplitN(cb.DataString(), ":", 3)
	if len(parts) < 2 {
		cb.Answer(F(chatID, "invalid_request"), opt)
		cb.Delete()
		return tg.ErrEndGroup
	}

	action := parts[1]
	pendingID := ""
	if len(parts) == 3 {
		pendingID = parts[2]
	}

	if action == "cancel" {
		if pendingID != "" {
			pendingPlays.Delete(pendingID)
		}

		msg := F(chatID, "voice_chat_cancelled")
		if _, err := cb.Edit(msg); err != nil {
			gologging.ErrorF("Voice chat confirm edit failed: %v", err)
		}

		cb.Answer(msg, opt)
		return tg.ErrEndGroup
	}

	if action != "start" {
		cb.Answer(F(chatID, "invalid_request"), opt)
		return tg.ErrEndGroup
	}

	ass, err := core.Assistants.ForChat(chatID)
	if err != nil {
		gologging.ErrorF("failed to get assistant for voice chat confirm in chat %d: %v", chatID, err)
		msg := F(chatID, "voice_chat_start_failed", locales.Arg{"error": err.Error()})
		cb.Edit(msg)
		cb.Answer(msg, opt)
		return tg.ErrEndGroup
	}

	err = ass.Ntg.StartGroupCall(chatID)
	if err != nil && !errors.Is(err, ubot.ErrAlreadyInGroupCall) {
		gologging.ErrorF("failed to start voice call from callback in chat %d: %v", chatID, err)
		msg := voiceCallCallbackErrorMessage(chatID, err)
		cb.Edit(msg)
		cb.Answer(msg, opt)
		return tg.ErrEndGroup
	}

	if cs, csErr := core.GetChatState(chatID); csErr == nil {
		cs.SetVoiceChatActive(true)
	}

	if pendingID != "" {
		if _, err := cb.Edit(F(chatID, "voice_chat_started")); err != nil {
			gologging.ErrorF("Voice chat started edit failed: %v", err)
		}

		return resumePendingPlay(pendingID, cb)
	}

	msgKey := "voice_chat_started"
	if errors.Is(err, ubot.ErrAlreadyInGroupCall) {
		msgKey = "voice_chat_already_active"
	}

	msg := F(chatID, msgKey)

	if _, err := cb.Edit(msg); err != nil {
		gologging.ErrorF("Voice chat started edit failed: %v", err)
	}

	cb.Answer(msg, opt)
	return tg.ErrEndGroup
}

func callLinkHandler(m *telegram.NewMessage) error {
	chatID := m.ChannelID()

	ass, err := core.Assistants.ForChat(chatID)
	if err != nil {
		m.Reply(err.Error())
		return telegram.ErrEndGroup
	}

	link, err := ass.Ntg.ExportGroupCallInvite(chatID, false)
	if err != nil {
		m.Reply(voiceCallInviteErrorMessage(chatID, err))
		return telegram.ErrEndGroup
	}

	m.Reply("🔗 لینک ویس‌کال:\n" + link)
	return telegram.ErrEndGroup
}

func speedDownHandler(m *telegram.NewMessage) error {
	return adjustSpeed(m, -voiceCallSpeedStep)
}

func speedUpHandler(m *telegram.NewMessage) error {
	return adjustSpeed(m, voiceCallSpeedStep)
}

func adjustSpeed(m *telegram.NewMessage, delta float64) error {
	r, err := getEffectiveRoom(m, false)
	if err != nil {
		m.Reply(err.Error())
		return telegram.ErrEndGroup
	}

	chatID := m.ChannelID()
	t := r.Track()

	if !r.IsActiveChat() || t == nil {
		m.Reply(F(chatID, "room_no_active"))
		return telegram.ErrEndGroup
	}

	newSpeed := r.Speed() + delta
	if newSpeed < voiceCallMinSpeed {
		newSpeed = voiceCallMinSpeed
	}
	if newSpeed > voiceCallMaxSpeed {
		newSpeed = voiceCallMaxSpeed
	}

	if newSpeed == r.Speed() {
		m.Reply(F(chatID, "speed_already_set", locales.Arg{
			"speed": fmt.Sprintf("%.2f", newSpeed),
			"title": utils.EscapeHTML(utils.ShortTitle(t.Title, 25)),
		}))
		return telegram.ErrEndGroup
	}

	if err := r.SetSpeed(newSpeed); err != nil {
		m.Reply(F(chatID, "speed_failed", locales.Arg{
			"speed": fmt.Sprintf("%.2f", newSpeed),
			"error": err.Error(),
		}))
		return telegram.ErrEndGroup
	}

	m.Reply(F(chatID, "speed_set", locales.Arg{
		"speed": fmt.Sprintf("%.2f", newSpeed),
		"user":  utils.MentionHTML(m.Sender),
	}))
	return telegram.ErrEndGroup
}

func volumeDownHandler(m *telegram.NewMessage) error {
	return adjustVolume(m, -voiceCallVolumeStep)
}

func volumeUpHandler(m *telegram.NewMessage) error {
	return adjustVolume(m, voiceCallVolumeStep)
}

func adjustVolume(m *telegram.NewMessage, delta float64) error {
	r, err := getEffectiveRoom(m, false)
	if err != nil {
		m.Reply(err.Error())
		return telegram.ErrEndGroup
	}

	chatID := m.ChannelID()
	t := r.Track()

	if !r.IsActiveChat() || t == nil {
		m.Reply(F(chatID, "room_no_active"))
		return telegram.ErrEndGroup
	}

	newVolume := r.Volume() + delta
	if newVolume < 0 {
		newVolume = 0
	}
	if newVolume > 1 {
		newVolume = 1
	}

	if newVolume == r.Volume() {
		m.Reply(F(chatID, "volume_already_set", locales.Arg{
			"volume": fmt.Sprintf("%.0f%%", newVolume*100),
			"title":  utils.EscapeHTML(utils.ShortTitle(t.Title, 25)),
		}))
		return telegram.ErrEndGroup
	}

	if err := r.SetVolume(newVolume); err != nil {
		m.Reply(F(chatID, "volume_failed", locales.Arg{
			"volume": fmt.Sprintf("%.0f%%", newVolume*100),
			"error":  err.Error(),
		}))
		return telegram.ErrEndGroup
	}

	m.Reply(F(chatID, "volume_set", locales.Arg{
		"volume": fmt.Sprintf("%.0f%%", newVolume*100),
		"user":   utils.MentionHTML(m.Sender),
	}))
	return telegram.ErrEndGroup
}
