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
	"time"

	"github.com/Laky-64/gologging"
	"github.com/amarnathcjd/gogram/telegram"

	"main/internal/core"
	state "main/internal/core/models"
	"main/internal/locales"
	"main/internal/platforms"
	"main/internal/utils"
	"main/ntgcalls"
)

func closePlaybackPanel(r *core.RoomState, text string) {
	if r == nil {
		return
	}

	statusMsg := r.StatusMsg()
	if statusMsg == nil {
		return
	}

	if _, err := statusMsg.Edit(text, &telegram.SendOptions{
		ParseMode:   "HTML",
		ReplyMarkup: telegram.Button.Clear(),
	}); err != nil {
		if telegram.MatchError(err, "MESSAGE_NOT_MODIFIED") {
			return
		}

		gologging.ErrorF(
			"Playback panel edit failed chat=%d: %v",
			statusMsg.ChannelID(),
			err,
		)
	}
}

func finishPlaybackRoom(r *core.RoomState, text string) {
	if r == nil {
		return
	}

	closePlaybackPanel(r, text)
	scheduleOldPlayingMessage(r)
	core.DeleteRoom(r.ID)
}

func buildPlaybackFinishedText(chatID int64, r *core.RoomState) string {
	if r == nil {
		return F(chatID, "playback_finished", locales.Arg{
			"title":    "-",
			"url":      "#",
			"duration": "-",
		})
	}

	track := r.Track()
	if track == nil {
		return F(chatID, "playback_finished", locales.Arg{
			"title":    "-",
			"url":      "#",
			"duration": "-",
		})
	}

	title := utils.EscapeHTML(utils.ShortTitle(track.Title, 35))

	return F(chatID, "playback_finished", locales.Arg{
		"title":    title,
		"url":      track.URL,
		"duration": utils.FormatDuration(track.Duration),
		"by":       track.Requester,
	})
}

func streamEndHandler(
	chatID int64,
	streamType ntgcalls.StreamType,
	_ ntgcalls.StreamDevice,
) {
	if streamType == ntgcalls.VideoStream {
		gologging.Debug("[onStreamEndHandler] Video stream ended, returning")
		return
	}

	gologging.DebugF("[onStreamEndHandler] Stream ended in chat %d", chatID)
	ass, err := core.Assistants.ForChat(chatID)
	if err != nil {
		gologging.ErrorF("Failed to get Assistant for %d: %v", chatID, err)
		return
	}
	r, ok := core.GetRoom(chatID, ass, false)
	if !ok {
		return
	}

	// scheduleOldPlayingMessage(r)

	// if ok, v := r.GetData("is_transitioning"); ok {
	// 	if ok, v := v.(bool); ok && v {
	// 		return
	// 	}
	// }

	if ok, v := r.GetData("is_transitioning"); ok {
		if b, _ := v.(bool); b {
			return // اتاق در حال تغییر ترک است، دست نزن
		}
	}

	// if !r.IsActiveChat() {
	// 	if r.IsEnded() { // برگرداندن محافظت اصلی
	// 		// finishPlaybackRoom(r, buildPlaybackFinishedText(r.ChatID, r))
	// 		gologging.InfoF("im runnnnn !!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!")
	// 		closePlaybackPanel(r, buildPlaybackFinishedText(r.ChatID, r))
	// 		scheduleOldPlayingMessage(r)
	// 	}
	// 	return
	// }

	r.SetData("is_transitioning", true)
	r.SetData("transition_started_at", time.Now())
	defer r.DeleteData("is_transitioning")

	cid := r.ChatID
	r.Parse()

	// var t *state.Track
	// var wasLooping bool
	// if len(r.Queue()) == 0 && r.Loop() == 0 {
	// 	// closePlaybackPanel(r, buildPlaybackFinishedText(cid, r))
	// 	finishPlaybackRoom(r, buildPlaybackFinishedText(cid, r))
	// 	// closePlaybackPanel(r, buildPlaybackFinishedText(r.ChatID, r))
	// 	// scheduleOldPlayingMessage(r)
	// 	// core.DeleteRoom(chatID)
	// 	// core.Bot.SendMessage(cid, F(cid, "stream_queue_finished"))
	// 	return
	// } else {
	// 	wasLooping = r.Loop() > 0
	// 	t = r.NextTrack()

	var t *state.Track
	var wasLooping bool

	// شرط را دوباره به 0 برمی‌گردانیم
	if len(r.Queue()) == 0 && r.Loop() == 0 {
		finishPlaybackRoom(r, buildPlaybackFinishedText(cid, r))
		return
	} else {
		// --- این بخش برای ادیت کردن پیام قبلی عالی کار کرد و نگهش می‌داریم ---
		finishedText := buildPlaybackFinishedText(cid, r)
		closePlaybackPanel(r, finishedText)
		// -----------------------------------------------------------------

		wasLooping = r.Loop() > 0
		t = r.NextTrack()

		// این محافظ را نگه می‌داریم تا در صورت باگ‌های پیش‌بینی نشده ربات خاموش نشود
		if t == nil && !wasLooping {
			core.DeleteRoom(r.ID)
			return
		}
	}

	statusText := F(cid, "stream_downloading_next")
	if wasLooping && t != nil && r.FilePath() != "" {
		statusText = F(cid, "cb_replaying")
	}

	statusMsg, err := core.Bot.SendMessage(
		cid,
		statusText,
	)
	if err != nil {
		gologging.ErrorF("[call.go] Failed to send msg: %v", err)
	}

	var filePath string
	if wasLooping && t != nil && r.FilePath() != "" {
		filePath = r.FilePath()
	} else {
		filePath, err = platforms.Download(context.Background(), t, statusMsg)
	}

	if err != nil {
		gologging.ErrorF(
			"[onStreamEndHandler] Download failed for %s: %v",
			t.URL,
			err,
		)
		utils.EOR(statusMsg, F(cid, "stream_download_fail", locales.Arg{
			"error": err.Error(),
		}))
		core.DeleteRoom(chatID)

		return
	}

	if err := r.Play(t, filePath, true); err != nil {
		gologging.ErrorF(
			"[onStreamEndHandler] Play failed for %s: %v",
			t.URL,
			err,
		)
		utils.EOR(statusMsg, F(cid, "stream_play_fail"))
		core.DeleteRoom(chatID)

		return
	}

	title := utils.ShortTitle(t.Title, 25)
	safeTitle := utils.EscapeHTML(title)

	msgText := F(cid, "stream_now_playing", locales.Arg{
		"url":      t.URL,
		"title":    safeTitle,
		"duration": utils.FormatDuration(t.Duration),
		"by":       t.Requester,
	})

	opt := &telegram.SendOptions{
		ParseMode:   "HTML",
		ReplyMarkup: core.GetPlayMarkup(cid, r, false),
	}

	if t.Artwork != "" && shouldShowThumb(cid) {
		opt.Media = utils.CleanURL(t.Artwork)
	}

	if statusMsg != nil {
		edited, err := utils.EOR(statusMsg, msgText, opt)
		if err != nil {
			gologging.ErrorF(
				"Now playing panel update failed for chat=%d: %v",
				cid,
				err,
			)

			newMsg, sendErr := core.Bot.SendMessage(cid, msgText, opt)
			if sendErr != nil {
				gologging.ErrorF(
					"Now playing fallback send failed for chat=%d: %v",
					cid,
					sendErr,
				)
			} else if newMsg != nil {
				r.SetStatusMsg(newMsg)
			}
		} else if edited != nil {
			r.SetStatusMsg(edited)
		}
	} else {
		newMsg, err := core.Bot.SendMessage(cid, msgText, opt)
		if err != nil {
			gologging.ErrorF(
				"Now playing send failed for chat=%d: %v",
				cid,
				err,
			)
		} else if newMsg != nil {
			r.SetStatusMsg(newMsg)
		}
	}

	schedulePlaybackPanelRefresh(cid, r, "playing", t.Requester)
}
