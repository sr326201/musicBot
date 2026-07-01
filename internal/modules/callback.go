/*
 * ● YukkiMusic
 * ○ A high-performance engine for streaming music in Telegram voicechats.
 *
 * Copyright (C) 2026 TheTeamVivek
 *
 * This program is free software: you can redistribute it and/or modify it under the
 * terms of the GNU General Public License as published by the Free Software Foundation,
 * either version 3 of the License, or (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful, but WITHOUT ANY
 * WARRANTY; without even the implied warranty of MERCHANTABILITY or FITNESS FOR A
 * PARTICULAR PURPOSE. See the GNU General Public License for more details.
 *
 * Repository: https://github.com/TheTeamVivek/YukkiMusic
 */

package modules

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Laky-64/gologging"
	tg "github.com/amarnathcjd/gogram/telegram"

	"main/internal/config"
	"main/internal/core"
	"main/internal/database"
	"main/internal/locales"
	"main/internal/platforms"
	"main/internal/utils"
)

func cancelHandler(cb *tg.CallbackQuery) error {
	chatID := cb.ChannelID()
	opt := &tg.CallbackOptions{Alert: false}

	if !checkAdminOrAuth(cb, chatID) {
		return tg.ErrEndGroup
	}

	if cancel, ok := downloadCancels[chatID]; ok {
		cancel()
		delete(downloadCancels, chatID)
		cb.Answer(F(chatID, "download_cancelled"), opt)
	} else {
		cb.Answer(F(chatID, "no_download_to_cancel"), opt)
	}
	return tg.ErrEndGroup
}

func closeHandler(cb *tg.CallbackQuery) error {
	cb.Answer("")
	cb.Delete()
	return tg.ErrEndGroup
}

func emptyCBHandler(cb *tg.CallbackQuery) error {
	cb.Answer("")
	return tg.ErrEndGroup
}

func checkHesOranother(cb *tg.CallbackQuery, chatID int64) bool {
	if canUseAdminCommand(cb.Client, chatID, cb.SenderID) {
		return true
	}

	opt := &tg.CallbackOptions{Alert: false}
	mode, err := database.GetAdminMode(chatID)
	if err == nil && mode == database.AdminModeAdminsOnly {
		cb.Answer(F(chatID, "only_admin_cb_hes"), opt)
	} else {
		cb.Answer(F(chatID, "only_admin_or_auth_cb_hes"), opt)
	}
	return false
}

func roomHandle(cb *tg.CallbackQuery) error {
	opt := &tg.CallbackOptions{Alert: false}
	chatID := cb.ChannelID()

	parts := strings.SplitN(cb.DataString(), ":", 3)
	if len(parts) != 3 || parts[0] != "room" {
		gologging.WarnF("Invalid room callback payload: %s", cb.DataString())
		cb.Answer(F(chatID, "invalid_request"), opt)
		cb.Delete()
		return tg.ErrEndGroup
	}
	roomID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		gologging.WarnF("Invalid roomID in callback: %s", parts[1])
		cb.Answer(F(chatID, "invalid_request"), opt)
		cb.Delete()
		return tg.ErrEndGroup
	}
	action := parts[2]

	r, ok := core.GetRoom(roomID, nil, false)
	if !ok || !r.IsActiveChat() {
		cb.Answer(F(chatID, "room_not_active_cb"), opt)
		cb.Edit(F(chatID, "room_no_active"))
		return tg.ErrEndGroup
	}

	//--------- check requester or admin or sudo -----------
	track := r.Track()
	isRequester := track != nil && cb.SenderID == track.RequesterID

	if !isRequester && !isOwnerOrSudo(cb.SenderID) {
		opt := &tg.CallbackOptions{Alert: true}
		cb.Answer(F(chatID, "only_requester_or_sudo"), opt)
		return tg.ErrEndGroup
	}
	// ----------------- check flood -----------------

	key := fmt.Sprintf("room:%d:%d", cb.Sender.ID, chatID)
	if remaining := utils.GetFlood(key); remaining > 0 {
		cb.Answer(F(chatID, "flood_seconds", locales.Arg{
			"duration": int(remaining.Seconds()),
		}), opt)
		return tg.ErrEndGroup
	}
	utils.SetFlood(
		key,
		time.Duration(config.PlaybackControlCooldown)*time.Second,
	)

	switch {
	case action == "speed_down":
		return handleSpeedStepAction(cb, r, -0.25, opt)
	case action == "speed_up":
		return handleSpeedStepAction(cb, r, 0.25, opt)
	case action == "speed_status":
		return handleSpeedStatusAction(cb, r, opt)
	case strings.HasPrefix(action, "seek"):
		return handleSeekAction(cb, r, action, opt)
	case action == "pause":
		return handlePauseAction(cb, r)
	case action == "resume":
		return handleResumeAction(cb, r)
	case action == "replay":
		return handleReplayAction(cb, r)
	case action == "skip":
		return handleSkipAction(cb, r)
	case action == "stop":
		return handleStopAction(cb, r)
	case action == "mute":
		return handleMuteAction(cb, r)
	case action == "unmute":
		return handleUnmuteAction(cb, r)
	case action == "volume_status":
		return handleVolumeStatusAction(cb, r, opt)
	case strings.HasPrefix(action, "volume_"):
		return handleVolumeChangeAction(cb, r, action, opt)
	case action == "share":
		return handleShareAction(cb, r)
	default:
		gologging.WarnF("Unknown callback action: %s", action)
		cb.Answer(F(chatID, "unknown_action"), opt)
	}

	return tg.ErrEndGroup
}

func checkAdminOrAuth(cb *tg.CallbackQuery, chatID int64) bool {
	if canUseAdminCommand(cb.Client, chatID, cb.SenderID) {
		return true
	}

	opt := &tg.CallbackOptions{Alert: false}
	mode, err := database.GetAdminMode(chatID)
	if err == nil && mode == database.AdminModeAdminsOnly {
		cb.Answer(F(chatID, "only_admin_cb"), opt)
	} else {
		cb.Answer(F(chatID, "only_admin_or_auth_cb"), opt)
	}
	return false
}

func handlePauseAction(cb *tg.CallbackQuery, r *core.RoomState) error {
	opt := &tg.CallbackOptions{Alert: false}
	chatID := cb.ChannelID()
	gologging.InfoF("Callback → pause, chatID=%d", chatID)

	if r.IsPaused() {
		remaining := r.RemainingResumeDuration()
		msg := utils.IfElse(
			remaining > 0,
			F(chatID, "room_already_paused_auto", locales.Arg{
				"duration": utils.FormatDuration(int(remaining.Seconds())),
			}),
			F(chatID, "room_already_paused"),
		)
		cb.Answer(msg, opt)
		return tg.ErrEndGroup
	}

	if _, err := r.Pause(); err != nil {
		gologging.ErrorF("Pause failed: %v", err)
		cb.Answer(F(chatID, "room_pause_failed", locales.Arg{
			"error": err.Error(),
		}), opt)
		return tg.ErrEndGroup
	}

	if r.IsMuted() {
		r.Unmute()
	}

	cb.Answer(F(chatID, "cb_pause_success", locales.Arg{
		"position": utils.FormatDuration(r.Position()),
	}), opt)
	// updatePlaybackMessage(cb, r, "paused")
	schedulePlaybackPanelRefresh(chatID, r, "paused", utils.MentionHTML(cb.Sender))
	return tg.ErrEndGroup
}

func handleResumeAction(cb *tg.CallbackQuery, r *core.RoomState) error {
	opt := &tg.CallbackOptions{Alert: false}
	chatID := cb.ChannelID()
	gologging.InfoF("Callback → resume, chatID=%d", chatID)

	if !r.IsPaused() {
		cb.Answer(F(chatID, "cb_already_playing"), opt)
		return tg.ErrEndGroup
	}

	if _, err := r.Resume(); err != nil {
		gologging.ErrorF("Resume failed: %v", err)
		cb.Answer(F(chatID, "cb_resume_failed"), opt)
		return tg.ErrEndGroup
	}

	cb.Answer(F(chatID, "cb_resume_success", locales.Arg{
		"position": utils.FormatDuration(r.Position()),
	}), opt)
	// updatePlaybackMessage(cb, r, "playing")
	schedulePlaybackPanelRefresh(chatID, r, "playing", utils.MentionHTML(cb.Sender))
	return tg.ErrEndGroup
}

func handleReplayAction(cb *tg.CallbackQuery, r *core.RoomState) error {
	opt := &tg.CallbackOptions{Alert: false}
	chatID := cb.ChannelID()
	gologging.InfoF("Callback → replay, chatID=%d", chatID)

	if err := r.Replay(); err != nil {
		gologging.ErrorF("Replay failed: %v", err)
		cb.Answer(F(chatID, "cb_replay_failed"), opt)
		return tg.ErrEndGroup
	}

	track := r.Track()
	if track == nil || !r.IsActiveChat() {
		cb.Answer(F(chatID, "room_no_active"), opt)
		return tg.ErrEndGroup
	}

	msgText := F(chatID, "stream_now_playing", locales.Arg{
		"url":      track.URL,
		"title":    utils.EscapeHTML(utils.ShortTitle(track.Title, 25)),
		"duration": utils.FormatDuration(track.Duration),
		"by":       track.Requester,
	})

	edited, err := cb.Edit(msgText, &tg.SendOptions{
		ParseMode:   "HTML",
		ReplyMarkup: core.GetPlayMarkup(chatID, r, false),
	})
	if err != nil {
		gologging.ErrorF("Replay panel edit failed chat=%d: %v", chatID, err)
		cb.Answer(F(chatID, "cb_replay_failed"), opt)
		return tg.ErrEndGroup
	}

	r.SetStatusMsg(edited)

	cb.Answer(F(chatID, "cb_replay_success"), opt)
	return tg.ErrEndGroup
}

func handleSkipAction(cb *tg.CallbackQuery, r *core.RoomState) error {
	opt := &tg.CallbackOptions{Alert: false}
	chatID := cb.ChannelID()
	gologging.InfoF("Callback → skip, chatID=%d", chatID)

	// if len(r.Queue()) == 0 {
	// 	stoppedText := F(chatID, "skip_stopped", locales.Arg{
	// 		"user": utils.MentionHTML(cb.Sender),
	// 	})
	// 	finishPlaybackRoom(r, stoppedText)
	// 	cb.Answer(F(chatID, "cb_skip_queue_empty"), opt)
	// 	return tg.ErrEndGroup
	// }

	if len(r.Queue()) == 0 {
		cb.Answer(F(chatID, "cb_skip_queue_empty"), opt)
		return tg.ErrEndGroup
	}

	r.SetLoop(0)
	t := r.NextTrack()
	// --------- for nothing after skipping ----------
	// statusMsg, err := cb.Respond(F(chatID, "stream_downloading_next"))
	// if err != nil {
	// 	gologging.ErrorF("Failed to send status message: %v", err)
	// }

	// ------------ for edit now msg without new msg ---------
	// statusMsg := r.StatusMsg()
	// if statusMsg != nil {
	// 	_, err := statusMsg.Edit(F(chatID, "stream_downloading_next"), &tg.SendOptions{ParseMode: "HTML"})
	// 	if err != nil {
	// 		gologging.ErrorF("Failed to edit status message: %v", err)
	// 	}
	// } else {
	// 	// اگر به هر دلیلی بنر قبلی نبود، یک پیام جدید می‌سازیم
	// 	var err error
	// 	statusMsg, err = core.Bot.SendMessage(chatID, F(chatID, "stream_downloading_next"), &tg.SendOptions{ParseMode: "HTML"})
	// 	if err != nil {
	// 		gologging.ErrorF("Failed to send status message: %v", err)
	// 	}
	// }

	track := r.Track()

	var skippedText string
	if track != nil {
		skippedText = F(chatID, "cb_skip_edited", locales.Arg{
			"url":      track.URL,
			"title":    utils.EscapeHTML(utils.ShortTitle(track.Title, 35)),
			"duration": utils.FormatDuration(track.Duration),
			"by":       track.Requester,
		})
	} else {
		skippedText = F(chatID, "cb_skip_edited", locales.Arg{})
	}

	cb.Answer(F(chatID, "cb_skip_success"), opt)
	statusMsg, err := cb.Respond(F(chatID, "stream_downloading_next"))
	if err != nil {
		gologging.ErrorF("Failed to send status message: %v", err)
	}

	path, err := platforms.Download(context.Background(), t, statusMsg)
	if err != nil {
		gologging.ErrorF("Download failed for %s: %v", t.URL, err)
		utils.EOR(statusMsg, F(chatID, "stream_download_fail", locales.Arg{
			"error": err.Error(),
		}))
		cb.Answer(F(chatID, "cb_skip_download_failed"), opt)
		scheduleOldPlayingMessage(r)
		core.DeleteRoom(r.ID)
		return tg.ErrEndGroup
	}

	if err := r.Play(t, path); err != nil {
		gologging.ErrorF("Play error: %v", err)
		utils.EOR(statusMsg, F(chatID, "stream_play_fail"))
		cb.Answer(F(chatID, "cb_skip_play_failed"), opt)
		scheduleOldPlayingMessage(r)
		core.DeleteRoom(r.ID)
		return tg.ErrEndGroup
	}
	// ---- line 349
	closePlaybackPanel(r, skippedText)

	// cb.Delete()

	msgText := F(chatID, "stream_now_playing", locales.Arg{
		"url":      t.URL,
		"title":    utils.EscapeHTML(utils.ShortTitle(t.Title, 25)),
		"duration": utils.FormatDuration(t.Duration),
		"by":       t.Requester,
	})

	sendOpt := &tg.SendOptions{
		ParseMode:   "HTML",
		ReplyMarkup: core.GetPlayMarkup(chatID, r, false),
	}
	if t.Artwork != "" && shouldShowThumb(chatID) {
		sendOpt.Media = utils.CleanURL(t.Artwork)
	}

	statusMsg, err = utils.EOR(statusMsg, msgText, sendOpt)
	if err != nil {
		// cb.Respond(F(chatID, "cb_skip_edited", locales.Arg{
		// 	"user": utils.MentionHTML(cb.Sender),
		// }))
		return tg.ErrEndGroup
	}

	r.SetStatusMsg(statusMsg)
	// statusMsg.Reply(F(chatID, "cb_skip_edited", locales.Arg{
	// 	"user": utils.MentionHTML(cb.Sender),
	// }))
	return tg.ErrEndGroup
}

func handleStopAction(cb *tg.CallbackQuery, r *core.RoomState) error {
	opt := &tg.CallbackOptions{Alert: false}
	chatID := cb.ChannelID()
	gologging.InfoF("Callback → stop, chatID=%d", chatID)

	track := r.Track()
	title := ""
	duration := ""
	if track != nil {
		title = utils.EscapeHTML(utils.ShortTitle(track.Title, 35))
		duration = utils.FormatDuration(track.Duration)
	}

	stoppedText := F(chatID, "stopped", locales.Arg{
		"user":     utils.MentionHTML(cb.Sender),
		"title":    title,
		"duration": duration,
		"url":      track.URL,
	})
	cb.Answer(F(chatID, "cb_stop_success"), opt)
	finishPlaybackRoom(r, stoppedText)
	return tg.ErrEndGroup
}

func handleMuteAction(cb *tg.CallbackQuery, r *core.RoomState) error {
	opt := &tg.CallbackOptions{Alert: false}
	chatID := cb.ChannelID()

	if r.IsMuted() {
		remaining := r.RemainingUnmuteDuration()
		msg := utils.IfElse(
			remaining > 0,
			F(chatID, "mute_already_muted_with_time", locales.Arg{
				"duration": utils.FormatDuration(int(remaining.Seconds())),
			}),
			F(chatID, "mute_already_muted"),
		)
		cb.Answer(msg, opt)
		return tg.ErrEndGroup
	}

	if _, err := r.Mute(); err != nil {
		cb.Answer(F(chatID, "mute_failed", locales.Arg{
			"error": err.Error(),
		}), opt)
		return tg.ErrEndGroup
	}

	cb.Answer(F(chatID, "cb_mute_success"), opt)
	updatePlaybackMessage(cb, r, "muted")
	return tg.ErrEndGroup
}

func handleUnmuteAction(cb *tg.CallbackQuery, r *core.RoomState) error {
	opt := &tg.CallbackOptions{Alert: false}
	chatID := cb.ChannelID()

	if !r.IsMuted() {
		cb.Answer(F(chatID, "unmute_already"), opt)
		return tg.ErrEndGroup
	}

	if _, err := r.Unmute(); err != nil {
		cb.Answer(F(chatID, "unmute_failed", locales.Arg{
			"error": err.Error(),
		}), opt)
		return tg.ErrEndGroup
	}

	cb.Answer(F(chatID, "cb_unmute_success"), opt)
	// updatePlaybackMessage(cb, r, "playing")
	schedulePlaybackPanelRefresh(chatID, r, "playing", utils.MentionHTML(cb.Sender))
	return tg.ErrEndGroup
}

func handleShareAction(cb *tg.CallbackQuery, r *core.RoomState) error {
	opt := &tg.CallbackOptions{Alert: false}
	chatID := cb.ChannelID()
	track := r.Track()

	if track == nil {
		cb.Answer(F(chatID, "room_not_active_cb"), opt)
		return tg.ErrEndGroup
	}

	if track.Source == platforms.PlatformTelegram {
		cb.Answer("")
		return tg.ErrEndGroup
	}

	filePath := r.FilePath()
	if filePath == "" {
		cb.Answer(F(chatID, "share_failed", locales.Arg{
			"error": "file not found",
		}), opt)
		return tg.ErrEndGroup
	}

	if _, err := os.Stat(filePath); err != nil {
		gologging.ErrorF("Share file missing: %s: %v", filePath, err)
		cb.Answer(F(chatID, "share_failed", locales.Arg{
			"error": err.Error(),
		}), opt)
		return tg.ErrEndGroup
	}

	targetChatID := r.ChatID
	if targetChatID == 0 {
		targetChatID = chatID
	}

	mime, _ := tg.MimeTypes.MIME(filePath)

	attrs := []tg.DocumentAttribute{}
	if track.Video || (!tg.MimeTypes.IsAudioFile(filePath) && tg.MimeTypes.IsStreamableFile(filePath)) {
		attrs = append(attrs, &tg.DocumentAttributeVideo{
			SupportsStreaming: true,
			Duration:          float64(track.Duration),
		})
	} else {
		title := strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))
		attrs = append(attrs, &tg.DocumentAttributeAudio{
			Voice:     false,
			Duration:  int32(track.Duration),
			Title:     title,
			Performer: "Me",
		})
	}

	if _, err := cb.Client.SendMedia(targetChatID, filePath, &tg.MediaOptions{
		FileName:   filepath.Base(filePath),
		MimeType:   mime,
		Attributes: attrs,
	}); err != nil {
		gologging.ErrorF("Failed to send track media to chat: %v", err)
		cb.Answer(F(chatID, "share_failed", locales.Arg{
			"error": err.Error(),
		}), opt)
		return tg.ErrEndGroup
	}

	cb.Answer(F(chatID, "share_success"), opt)
	return tg.ErrEndGroup
}

func handleSpeedStepAction(
	cb *tg.CallbackQuery,
	r *core.RoomState,
	delta float64,
	opt *tg.CallbackOptions,
) error {
	chatID := cb.ChannelID()
	currentSpeed := r.Speed()

	newSpeed := currentSpeed + delta
	if newSpeed < 0.50 {
		newSpeed = 0.50
	}
	if newSpeed > 4.0 {
		newSpeed = 4.0
	}

	if newSpeed == currentSpeed {
		cb.Answer(F(chatID, "speed_already_set", locales.Arg{
			"speed": fmt.Sprintf("%.2f", newSpeed),
			"title": utils.EscapeHTML(utils.ShortTitle(r.Track().Title, 25)),
		}), opt)
		return tg.ErrEndGroup
	}

	if err := r.SetSpeed(newSpeed); err != nil {
		cb.Answer(F(chatID, "speed_failed", locales.Arg{
			"speed": fmt.Sprintf("%.2f", newSpeed),
			"error": err.Error(),
		}), opt)
		return tg.ErrEndGroup
	}

	cb.Answer(F(chatID, "speed_set", locales.Arg{
		"speed": fmt.Sprintf("%.2f", newSpeed),
		"user":  utils.MentionHTML(cb.Sender),
	}), opt)

	// updatePlaybackMessage(cb, r, "playing")
	schedulePlaybackPanelRefresh(chatID, r, "playing", utils.MentionHTML(cb.Sender))
	return tg.ErrEndGroup
}

func handleSpeedStatusAction(
	cb *tg.CallbackQuery,
	r *core.RoomState,
	opt *tg.CallbackOptions,
) error {
	chatID := cb.ChannelID()
	t := r.Track()

	cb.Answer(F(chatID, "speed_current", locales.Arg{
		"speed": fmt.Sprintf("%.2f", r.Speed()),
		"title": utils.EscapeHTML(utils.ShortTitle(t.Title, 25)),
		"cmd":   "speed",
	}), opt)

	return tg.ErrEndGroup
}

func handleSeekAction(
	cb *tg.CallbackQuery,
	r *core.RoomState,
	action string,
	opt *tg.CallbackOptions,
) error {
	chatID := cb.ChannelID()

	parts := strings.SplitN(action, "_", 2)
	if len(parts) != 2 {
		cb.Answer(F(chatID, "invalid_request"), opt)
		return tg.ErrEndGroup
	}

	numStr := parts[1]
	isBackward := strings.HasPrefix(action, "seekback_")

	seconds, err := strconv.Atoi(numStr)
	if err != nil {
		cb.Answer(F(chatID, "invalid_request"), opt)
		return tg.ErrEndGroup
	}

	if isBackward {
		if r.Position() <= seconds {
			r.Seek(-int(r.Position()))
		} else {
			r.Seek(-seconds)
		}

		cb.Answer(
			F(chatID, "cb_seekback_success", locales.Arg{"seconds": seconds}),
			&tg.CallbackOptions{Alert: false},
		)
	} else {
		if (r.Track().Duration - r.Position()) <= seconds {
			cb.Answer(
				F(chatID, "cb_seek_near_end", locales.Arg{"seconds": seconds}),
				opt,
			)
			return tg.ErrEndGroup
		}

		r.Seek(seconds)

		cb.Answer(
			F(chatID, "cb_seek_success", locales.Arg{"seconds": seconds}),
			&tg.CallbackOptions{Alert: false},
		)
	}

	return tg.ErrEndGroup
}

var playbackPanelRefreshTimers sync.Map // map[string]*time.Timer

func schedulePlaybackPanelRefresh(
	chatID int64,
	r *core.RoomState,
	state string,
	mention string,
) {
	if r == nil {
		return
	}

	key := fmt.Sprintf("panel:%d", r.ID)

	if t, ok := playbackPanelRefreshTimers.Load(key); ok {
		t.(*time.Timer).Stop()
	}

	playbackPanelRefreshTimers.Store(key, time.AfterFunc(250*time.Millisecond, func() {
		playbackPanelRefreshTimers.Delete(key)
		updatePlaybackPanel(chatID, r, state, mention)
	}))
}

func updatePlaybackMessage(cb *tg.CallbackQuery, r *core.RoomState, state string) {
	updatePlaybackPanel(cb.ChannelID(), r, state, utils.MentionHTML(cb.Sender))
}

// func updatePlaybackMessage(cb *tg.CallbackQuery, r *core.RoomState, state string) {
// 	track := r.Track()
// 	if track == nil {
// 		return
// 	}

// 	chatID := cb.ChannelID()
// 	safeTitle := utils.EscapeHTML(utils.ShortTitle(track.Title, 25))
// 	mention := utils.MentionHTML(cb.Sender)

// 	var msgText string
// 	switch state {
// 	case "paused":
// 		msgText = F(chatID, "cb_pause_message", locales.Arg{
// 			"url":      track.URL,
// 			"title":    safeTitle,
// 			"position": utils.FormatDuration(r.Position()),
// 			"duration": utils.FormatDuration(track.Duration),
// 			"user":     mention,
// 		})
// 	case "playing":
// 		msgText = F(chatID, "cb_resume_message", locales.Arg{
// 			"url":      track.URL,
// 			"title":    safeTitle,
// 			"duration": utils.FormatDuration(track.Duration),
// 			"user":     mention,
// 		})
// 	case "muted":
// 		msgText = F(chatID, "cb_mute_message", locales.Arg{
// 			"url":   track.URL,
// 			"title": safeTitle,
// 			"user":  mention,
// 		})
// 	}

// 	if _, err := cb.Edit(msgText, &tg.SendOptions{
// 		ParseMode:   "HTML",
// 		ReplyMarkup: core.GetPlayMarkup(chatID, r, false),
// 	}); err != nil {
// 		gologging.ErrorF("Edit error: %v", err)
// 	}
// }

func handleVolumeStatusAction(
	cb *tg.CallbackQuery,
	r *core.RoomState,
	opt *tg.CallbackOptions,
) error {
	chatID := cb.ChannelID()
	t := r.Track()

	cb.Answer(F(chatID, "volume_current", locales.Arg{
		"volume": fmt.Sprintf("%.0f%%", r.Volume()*100),
		"title":  utils.EscapeHTML(utils.ShortTitle(t.Title, 25)),
		"cmd":    "volume",
	}), opt)

	return tg.ErrEndGroup
}

func handleVolumeChangeAction(
	cb *tg.CallbackQuery,
	r *core.RoomState,
	action string,
	opt *tg.CallbackOptions,
) error {
	chatID := cb.ChannelID()

	parts := strings.SplitN(action, "_", 3)
	if len(parts) < 3 || parts[0] != "volume" {
		cb.Answer(F(chatID, "invalid_request"), opt)
		return tg.ErrEndGroup
	}

	delta, err := strconv.ParseFloat(parts[2], 64)
	if err != nil {
		cb.Answer(F(chatID, "invalid_request"), opt)
		return tg.ErrEndGroup
	}
	delta = delta / 100

	if parts[1] == "down" {
		delta = -delta
	}

	currentVolume := r.Volume()
	newVolume := currentVolume + delta

	if newVolume < 0.0 {
		newVolume = 0.0
	} else if newVolume > 1.0 {
		newVolume = 1.0
	}

	if newVolume == currentVolume {
		cb.Answer(F(chatID, "volume_already_set", locales.Arg{
			"volume": fmt.Sprintf("%.0f%%", newVolume*100),
			"title":  utils.EscapeHTML(utils.ShortTitle(r.Track().Title, 25)),
		}), opt)
		return tg.ErrEndGroup
	}

	if err := r.SetVolume(newVolume); err != nil {
		cb.Answer(F(chatID, "volume_failed", locales.Arg{
			"volume": fmt.Sprintf("%.0f%%", newVolume*100),
			"error":  err.Error(),
		}), opt)
		return tg.ErrEndGroup
	}

	cb.Answer(F(chatID, "volume_set", locales.Arg{
		"volume": fmt.Sprintf("%.0f%%", newVolume*100),
		"user":   utils.MentionHTML(cb.Sender),
	}), opt)

	// updatePlaybackMessage(cb, r, "playing")
	schedulePlaybackPanelRefresh(chatID, r, "playing", utils.MentionHTML(cb.Sender))
	return tg.ErrEndGroup
}

func updatePlaybackPanel(
	chatID int64,
	r *core.RoomState,
	state string,
	mention string,
) {
	if r == nil || r.IsDestroyed() {
		return
	}

	statusMsg := r.StatusMsg()
	if statusMsg == nil {
		return
	}

	r.Parse()

	if !r.IsActiveChat() {
		return
	}

	track := r.Track()
	if track == nil {
		return
	}

	safeTitle := utils.EscapeHTML(utils.ShortTitle(track.Title, 25))

	var msgText string
	switch state {
	case "paused":
		msgText = F(chatID, "cb_pause_message", locales.Arg{
			"url":      track.URL,
			"title":    safeTitle,
			"position": utils.FormatDuration(r.Position()),
			"duration": utils.FormatDuration(track.Duration),
			"user":     mention,
		})

	case "playing":
		msgText = F(chatID, "stream_now_playing", locales.Arg{
			"url":      track.URL,
			"title":    safeTitle,
			"duration": utils.FormatDuration(track.Duration),
			"by":       mention,
		})

	case "muted":
		msgText = F(chatID, "cb_mute_message", locales.Arg{
			"url":      track.URL,
			"title":    safeTitle,
			"duration": utils.FormatDuration(track.Duration),
			"user":     mention,
		})

	default:
		msgText = F(chatID, "stream_now_playing", locales.Arg{
			"url":      track.URL,
			"title":    safeTitle,
			"duration": utils.FormatDuration(track.Duration),
			"by":       track.Requester,
		})
	}

	if _, err := statusMsg.Edit(msgText, &tg.SendOptions{
		ParseMode:   "HTML",
		ReplyMarkup: core.GetPlayMarkup(chatID, r, false),
	}); err != nil {
		if tg.MatchError(err, "MESSAGE_NOT_MODIFIED") {
			return
		}

		gologging.ErrorF("Playback panel refresh failed chat=%d: %v", chatID, err)
	}
}
