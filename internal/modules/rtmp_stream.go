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
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Laky-64/gologging"
	tg "github.com/amarnathcjd/gogram/telegram"

	"main/internal/config"
	"main/internal/core"
	state "main/internal/core/models"
	"main/internal/database"
	"main/internal/locales"
	"main/internal/utils"
)

type rtmpTrackData struct {
	title     string
	duration  int
	requester string
	url       string
}

var (
	rtmpStreams   = make(map[int64]*tg.RTMPStream)
	rtmpStreamsMu sync.RWMutex

	rtmpStatusMsgs   = make(map[int64]*tg.NewMessage) // ← جدید
	rtmpStatusMsgsMu sync.RWMutex                     // ← جدید

	rtmpStopping   = make(map[int64]bool) // ← جدید (guard برای توقف دستی)
	rtmpStoppingMu sync.RWMutex           // ← جدید

	rtmpTrackInfo   = make(map[int64]*rtmpTrackData)
	rtmpTrackInfoMu sync.RWMutex
)

const defaultRTMPThumbPath = "public/image.jpg"

func init() {
	helpTexts["stream"] = `<i>Start RTMP stream in this chat.</i>

<u>Usage:</u>
<b>/stream &lt;query/URL&gt;</b>
<b>/stream [reply to audio/video]</b>`

	helpTexts["vstream"] = `<i>Start RTMP video stream in this chat.</i>
<u>Usage:</u>
<b>/vstream &lt;query/URL&gt;</b>
<b>/vstream [reply to audio/video]</b>`

	helpTexts["streamstop"] = `<i>Stop the current RTMP stream.</i>

<u>Usage:</u>
<b>/streamstop</b>`

	helpTexts["streamstatus"] = `<i>Check RTMP stream status.</i>

<u>Usage:</u>
<b>/streamstatus</b>`

	helpTexts["setrtmp"] = `<i>Set RTMP URL in bot DM.</i>

<u>Usage:</u>
<b>/setrtmp &lt;chat_id&gt; &lt;rtmp_url&gt;</b>`
}

// Get or create RTMP stream for chat
func getOrCreateRTMPStream(chatID int64, url, key string, platform string) *tg.RTMPStream {
	rtmpStreamsMu.Lock()
	defer rtmpStreamsMu.Unlock()

	if stream, exists := rtmpStreams[chatID]; exists {
		return stream
	}

	stream, err := core.Bot.NewRTMPStream(chatID)
	if err != nil {
		return nil
	}

	stream.SetLoopCount(0)
	stream.SetURL(url)
	stream.SetKey(key)
	applyProfile(stream, platform)

	stream.OnError(func(chatID int64, err error) {
		gologging.ErrorF("RTMP error in chat %d: %v", chatID, err)
		core.Bot.SendMessage(
			chatID,
			"⚠️ RTMP stream encountered an error. Check logs for details.",
		)
	})

	// باید بعد از stream.OnError(...) اضافه شود:
	stream.OnEnd(func(chatID int64) {
		gologging.InfoF("RTMP stream ended naturally in chat %d", chatID)

		// اگر Stop دستی بوده، OnEnd رو نادیده بگیر
		rtmpStoppingMu.RLock()
		stopping := rtmpStopping[chatID]
		rtmpStoppingMu.RUnlock()

		if stopping {
			// فقط flag رو پاک کن
			rtmpStoppingMu.Lock()
			delete(rtmpStopping, chatID)
			rtmpStoppingMu.Unlock()
			return
		}

		// پیام پنل رو پیدا کن و ویرایش کن
		rtmpStatusMsgsMu.RLock()
		statusMsg, exists := rtmpStatusMsgs[chatID]
		rtmpStatusMsgsMu.RUnlock()

		if exists && statusMsg != nil {
			rtmpTrackInfoMu.RLock()
			info := rtmpTrackInfo[chatID]
			rtmpTrackInfoMu.RUnlock()

			title := "-"
			by := "-"
			duration := "-"
			if info != nil {
				title = utils.EscapeHTML(utils.ShortTitle(info.title, 35))
				by = info.requester
				duration = utils.FormatDuration(info.duration)
			}

			finishedText := F(chatID, "rtmp_finished", locales.Arg{
				"title":    title,
				"duration": duration,
				"by":       by,
				"url":      info.url,
			})
			if _, err := statusMsg.Edit(finishedText, &tg.SendOptions{
				ParseMode:   "HTML",
				ReplyMarkup: tg.Button.Clear(),
			}); err != nil {
				if tg.MatchError(err, "MESSAGE_NOT_MODIFIED") {
					return
				}
				gologging.ErrorF("RTMP panel edit failed chat=%d: %v", chatID, err)
			}
		}

		// پاکسازی state
		clearRTMPState(chatID)
	})

	rtmpStreams[chatID] = stream
	return stream
}

func streamHandler(m *tg.NewMessage) error {
	return handleStream(m, false, false)
}

func vstreamHandler(m *tg.NewMessage) error {
	return handleStream(m, false, true)
}

func handleStream(m *tg.NewMessage, force bool, video bool) error {
	chatID := m.ChannelID()

	url, key, platform, err := database.RTMP(chatID)
	if err != nil || url == "" || key == "" {
		m.Reply(F(chatID, "rtmp_not_configured", locales.Arg{
			"cmd": "/setrtmp",
		}))
		return tg.ErrEndGroup
	}

	reactToCommandMessage(m, "🫡")

	parts := strings.SplitN(m.Text(), " ", 2)
	query := ""
	if len(parts) > 1 {
		query = strings.TrimSpace(parts[1])
	}

	if query == "" && !m.IsReply() {
		m.Reply(F(chatID, "no_song_query", locales.Arg{
			"cmd": getCommand(m),
		}))
		return tg.ErrEndGroup
	}

	stream := getOrCreateRTMPStream(chatID, url, key, platform)
	if stream == nil {
		m.Reply("failed to create rtmp stream")
		return tg.ErrEndGroup
	}

	if stream.State() == tg.StreamStatePlaying && !force {
		m.Reply(F(chatID, "rtmp_already_streaming"))
		return tg.ErrEndGroup
	}

	searchStr := ""
	if query != "" {
		searchStr = F(chatID, "searching_query", locales.Arg{
			"query": utils.EscapeHTML(query),
		})
	} else {
		searchStr = F(chatID, "searching")
	}

	replyMsg, err := sendPlaySticker(m, searchStr)
	if err != nil {
		gologging.ErrorF("Failed to send searching message: %v", err)
		return tg.ErrEndGroup
	}

	tracks, err := safeGetTracks(m, replyMsg, chatID, video)
	if err != nil {
		utils.EOR(replyMsg, err.Error())
		return tg.ErrEndGroup
	}

	if len(tracks) == 0 {
		utils.EOR(replyMsg, F(chatID, "no_song_found"))
		return tg.ErrEndGroup
	}

	track := tracks[0]
	mention := utils.MentionHTML(m.Sender)
	track.Requester = mention
	track.RequesterID = m.SenderID()

	// Download track
	downloadingText := F(chatID, "play_downloading_song", locales.Arg{
		"title": utils.EscapeHTML(utils.ShortTitle(track.Title, 25)),
	})
	replyMsg, _ = utils.EOR(replyMsg, downloadingText)

	ctx, cancel := context.WithCancel(context.Background())
	downloadCancels[chatID] = cancel
	defer func() {
		if _, ok := downloadCancels[chatID]; ok {
			delete(downloadCancels, chatID)
			cancel()
		}
	}()

	filePath, err := safeDownload(ctx, track, replyMsg, chatID)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			utils.EOR(replyMsg, F(chatID, "play_download_canceled", locales.Arg{
				"user": mention,
			}))
		} else {
			utils.EOR(replyMsg, F(chatID, "play_download_failed", locales.Arg{
				"title": utils.EscapeHTML(utils.ShortTitle(track.Title, 25)),
				"error": utils.EscapeHTML(err.Error()),
			}))
		}
		return tg.ErrEndGroup
	}

	gologging.InfoF(
		"RTMP DEBUG download complete chat=%d trackID=%s title=%q source=%s video=%v artwork=%q filePath=%q",
		chatID,
		track.ID,
		track.Title,
		track.Source,
		track.Video,
		track.Artwork,
		filePath,
	)

	if !isRemoteRTMPSource(filePath) {
		logRTMPFileInfo("downloaded-source", filePath)
	}

	playPath, resolvedThumb, err := prepareRTMPPlaybackSource(chatID, filePath, track, platform)
	if err != nil {
		gologging.ErrorF(
			"RTMP DEBUG prepare source failed chat=%d trackID=%s filePath=%q err=%v",
			chatID,
			track.ID,
			filePath,
			err,
		)
		utils.EOR(replyMsg, F(chatID, "rtmp_visual_build_failed", locales.Arg{
			"error": utils.EscapeHTML(err.Error()),
		}))
		return tg.ErrEndGroup
	}

	gologging.InfoF(
		"RTMP DEBUG prepared playback chat=%d trackID=%s sourcePath=%q playPath=%q resolvedThumb=%q",
		chatID,
		track.ID,
		filePath,
		playPath,
		resolvedThumb,
	)

	if !isRemoteRTMPSource(playPath) {
		logRTMPFileInfo("prepared-playback", playPath)
	}

	// Start streaming
	beforeState := stream.State()
	startPlay := time.Now()

	gologging.InfoF(
		"RTMP DEBUG before Play chat=%d state=%v playPath=%q",
		chatID,
		beforeState,
		playPath,
	)

	err = stream.Play(playPath)
	elapsed := time.Since(startPlay)

	if err != nil {
		gologging.ErrorF(
			"RTMP DEBUG Play failed chat=%d stateBefore=%v elapsed=%s playPath=%q err=%v",
			chatID,
			beforeState,
			elapsed,
			playPath,
			err,
		)
		utils.EOR(replyMsg, F(chatID, "rtmp_play_failed", locales.Arg{
			"error": err.Error(),
		}))
		return tg.ErrEndGroup
	}

	gologging.InfoF(
		"RTMP DEBUG Play returned success chat=%d stateBefore=%v stateAfter=%v elapsed=%s playPath=%q",
		chatID,
		beforeState,
		stream.State(),
		elapsed,
		playPath,
	)

	// Success message
	btn := tg.NewKeyboard()
	stopBtn := tg.Button.Data(F(chatID, "CONFIRM_STOP_BTN"), "rtmp_stop")
	if !config.DisableColour {
		stopBtn.Danger()
	}
	btn.AddRow(stopBtn)

	title := utils.EscapeHTML(utils.ShortTitle(track.Title, 25))
	msgText := F(chatID, "rtmp_now_streaming", locales.Arg{
		"url":      track.URL,
		"title":    title,
		"duration": utils.FormatDuration(track.Duration),
		"by":       mention,
	})

	opt := &tg.SendOptions{
		ParseMode:   "HTML",
		ReplyMarkup: btn.Build(),
	}

	if shouldShowThumb(chatID) && resolvedThumb != "" {
		opt.Media = utils.CleanURL(resolvedThumb)
	}

	utils.EOR(replyMsg, msgText, opt)

	// ذخیره‌ی پیام پنل برای ویرایش بعد از پایان استریم
	rtmpStatusMsgsMu.Lock()
	rtmpStatusMsgs[chatID] = replyMsg
	rtmpStatusMsgsMu.Unlock()

	rtmpTrackInfoMu.Lock()
	rtmpTrackInfo[chatID] = &rtmpTrackData{
		title:     track.Title,
		duration:  track.Duration,
		requester: mention,
		url:       track.URL,
	}
	rtmpTrackInfoMu.Unlock()

	return tg.ErrEndGroup
}

// /streamstop - Stop RTMP stream
func streamStopHandler(m *tg.NewMessage) error {
	chatID := m.ChannelID()

	rtmpStreamsMu.RLock()
	stream, exists := rtmpStreams[chatID]
	rtmpStreamsMu.RUnlock()

	if !exists || stream.State() != tg.StreamStatePlaying {
		m.Reply(F(chatID, "room_no_active"))
		return tg.ErrEndGroup
	}

	rtmpStoppingMu.Lock()
	rtmpStopping[chatID] = true
	rtmpStoppingMu.Unlock()

	if err := stream.Stop(); err != nil {
		m.Reply(F(chatID, "rtmp_stop_failed", locales.Arg{
			"error": err.Error(),
		}))
		return tg.ErrEndGroup
	}

	m.Reply(F(chatID, "rtmp_stopped", locales.Arg{
		"user": utils.MentionHTML(m.Sender),
	}))

	return tg.ErrEndGroup
}

func rtmpStopCallbackHandler(cb *tg.CallbackQuery) error {
	chatID := cb.ChannelID()
	opt := &tg.CallbackOptions{Alert: true}

	if !checkAdminOrAuth(cb, chatID) {
		return tg.ErrEndGroup
	}

	rtmpStreamsMu.RLock()
	stream, exists := rtmpStreams[chatID]
	rtmpStreamsMu.RUnlock()

	if !exists || stream.State() != tg.StreamStatePlaying {
		cb.Answer(F(chatID, "room_no_active"), opt)
		return tg.ErrEndGroup
	}

	rtmpStoppingMu.Lock()
	rtmpStopping[chatID] = true
	rtmpStoppingMu.Unlock()

	if err := stream.Stop(); err != nil {
		cb.Answer(F(chatID, "rtmp_stop_failed", locales.Arg{
			"error": err.Error(),
		}), opt)
		return tg.ErrEndGroup
	}

	_, _ = cb.Edit(F(chatID, "rtmp_stopped", locales.Arg{
		"user": utils.MentionHTML(cb.Sender),
	}))
	cb.Answer(F(chatID, "cb_stop_success"), &tg.CallbackOptions{})
	return tg.ErrEndGroup
}

// /streamstatus - Check RTMP status
func streamStatusHandler(m *tg.NewMessage) error {
	chatID := m.ChannelID()

	// Check if RTMP is configured (without exposing credentials)
	url, _, _, err := database.RTMP(chatID)
	if err != nil || url == "" {
		m.Reply(F(chatID, "rtmp_not_configured", locales.Arg{
			"cmd": "/setrtmp",
		}))
		return tg.ErrEndGroup
	}

	rtmpStreamsMu.RLock()
	stream, exists := rtmpStreams[chatID]
	rtmpStreamsMu.RUnlock()

	if !exists {
		// RTMP configured but not initialized yet
		m.Reply(F(chatID, "room_no_active"))
		return tg.ErrEndGroup
	}

	state := stream.State()
	pos := stream.CurrentPosition()

	var statusText string
	switch state {
	case tg.StreamStatePlaying:
		statusText = F(chatID, "rtmp_status_playing", locales.Arg{
			"position": utils.FormatDuration(int(pos.Seconds())),
		})
	default:
		statusText = F(chatID, "room_no_active")
	}

	m.Reply(statusText)
	return tg.ErrEndGroup
}

// /setrtmp - Configure RTMP (DM only for security)
func setRTMPHandler(m *tg.NewMessage) error {
	if !filterChannel(m) {
		return tg.ErrEndGroup
	}

	switch m.ChatType() {
	case tg.EntityChat:
		m.Reply(F(m.ChannelID(), "rtmp_dm_only"))
		return tg.ErrEndGroup
	case tg.EntityUser:
	default:
		return tg.ErrEndGroup
	}

	args := strings.Fields(m.Text())

	if len(args) < 4 {
		m.Reply(F(m.ChannelID(), "rtmp_setup_usage"))
		return tg.ErrEndGroup
	}

	cid := args[1]
	platform := strings.ToLower(args[2])
	raw := args[3]

	idx := strings.LastIndex(raw, "/")
	if idx <= 0 || idx == len(raw)-1 {
		m.Reply(F(m.ChannelID(), "rtmp_parse_failed", locales.Arg{
			"error": "invalid RTMP format",
		}))
		return tg.ErrEndGroup
	}

	url := raw[:idx+1]
	key := raw[idx+1:]

	if url == "" || key == "" {
		m.Reply(F(m.ChannelID(), "rtmp_parse_failed", locales.Arg{
			"error": "empty url or key",
		}))
		return tg.ErrEndGroup
	}

	targetChatID, err := strconv.ParseInt(cid, 10, 64)
	if err != nil {
		m.Reply(F(m.ChannelID(), "rtmp_invalid_chat_id"))
		return tg.ErrEndGroup
	}

	if _, ok := platformProfiles[platform]; !ok {
		m.Reply(F(m.ChannelID(), "rtmp_invalid_platform", locales.Arg{
			"platforms": "kick, youtube, twitch, custom",
		}))
		return tg.ErrEndGroup
	}

	if err := database.SetRTMP(targetChatID, url, key, platform); err != nil {
		m.Reply(F(m.ChannelID(), "generic_error", locales.Arg{"error": err.Error()}))
		return tg.ErrEndGroup
	}

	rtmpStreamsMu.Lock()
	if stream, exists := rtmpStreams[targetChatID]; exists {
		stream.SetURL(url)
		stream.SetKey(key)
		applyProfile(stream, platform)
	}
	rtmpStreamsMu.Unlock()

	m.Reply(F(m.ChannelID(), "rtmp_configured_success", locales.Arg{"chat_id": targetChatID}))

	return tg.ErrEndGroup
}

func clearRTMPState(chatID int64) {
	rtmpStreamsMu.Lock()
	defer rtmpStreamsMu.Unlock()

	if stream, ok := rtmpStreams[chatID]; ok {
		_ = stream.Stop()
		delete(rtmpStreams, chatID)

		rtmpTrackInfoMu.Lock()
		delete(rtmpTrackInfo, chatID)
		rtmpTrackInfoMu.Unlock()

		rtmpStatusMsgsMu.Lock()
		delete(rtmpStatusMsgs, chatID)
		rtmpStatusMsgsMu.Unlock()

		rtmpStoppingMu.Lock()
		delete(rtmpStopping, chatID)
		rtmpStoppingMu.Unlock()
	}
}

func resolveRTMPThumbnail(chatID int64, track *state.Track) string {
	custom, err := database.RTMPThumbnail(chatID)
	if err == nil && strings.TrimSpace(custom) != "" {
		return strings.TrimSpace(custom)
	}

	if track != nil && strings.TrimSpace(track.Artwork) != "" {
		return strings.TrimSpace(track.Artwork)
	}

	if _, err := os.Stat(defaultRTMPThumbPath); err == nil {
		return defaultRTMPThumbPath
	}

	return ""
}

func prepareRTMPPlaybackSource(
	chatID int64,
	sourcePath string,
	track *state.Track,
	platform string,
) (string, string, error) {
	thumb := resolveRTMPThumbnail(chatID, track)

	profile, ok := platformProfiles[platform]
	if !ok {
		profile = platformProfiles["custom"]
	}

	gologging.InfoF(
		"RTMP DEBUG prepare start chat=%d trackID=%s video=%v sourcePath=%q thumb=%q",
		chatID,
		trackIDForThumb(track),
		track != nil && track.Video,
		sourcePath,
		thumb,
	)

	if track == nil {
		return sourcePath, thumb, nil
	}

	if track.Video {
		gologging.InfoF(
			"RTMP DEBUG prepare skip wrapper: video track chat=%d trackID=%s sourcePath=%q",
			chatID,
			trackIDForThumb(track),
			sourcePath,
		)
		return sourcePath, thumb, nil
	}

	if isRemoteRTMPSource(sourcePath) {
		gologging.InfoF(
			"RTMP DEBUG prepare skip wrapper: remote source chat=%d trackID=%s sourcePath=%q",
			chatID,
			trackIDForThumb(track),
			sourcePath,
		)
		return sourcePath, thumb, nil
	}

	if thumb == "" {
		gologging.InfoF(
			"RTMP DEBUG prepare skip wrapper: no thumb chat=%d trackID=%s sourcePath=%q",
			chatID,
			trackIDForThumb(track),
			sourcePath,
		)
		return sourcePath, "", nil
	}

	if track.Video {
		return sourcePath, thumb, nil
	}

	if isRemoteRTMPSource(sourcePath) {
		return sourcePath, thumb, nil
	}

	if thumb == "" {
		return sourcePath, "", nil
	}

	if err := os.MkdirAll("cache", os.ModePerm); err != nil {
		return "", thumb, err
	}

	localThumb, cleanup, err := ensureLocalRTMPThumbnail(thumb, track)
	if err != nil {
		return "", thumb, err
	}
	if cleanup != nil {
		defer cleanup()
	}

	gologging.InfoF(
		"RTMP DEBUG local thumb ready chat=%d trackID=%s originalThumb=%q localThumb=%q",
		chatID,
		trackIDForThumb(track),
		thumb,
		localThumb,
	)

	logRTMPFileInfo("local-thumb", localThumb)

	out := filepath.Join("cache", "rtmp_visual_"+sanitizeRTMPCacheKey(track.ID)+".flv")

	args := []string{
		"-y",
		"-loop", "1",
		"-i", localThumb,
		"-i", sourcePath,
		"-c:v", "libx264",
		"-preset", "veryfast",
		"-tune", "stillimage",
		"-pix_fmt", "yuv420p",
		"-r", strconv.Itoa(profile.FrameRate),
		"-s", profile.Resolution,
		"-b:v", profile.Bitrate,
		"-maxrate", profile.Bitrate,
		"-bufsize", profile.Bitrate + "k",
		"-c:a", "aac",
		"-b:a", profile.AudioBit,
		"-ar", strconv.Itoa(profile.AudioSampleRate),
		"-ac", "2",
		"-shortest",
		"-f", "flv",
		out,
	}

	gologging.InfoF(
		"RTMP DEBUG ffmpeg wrapper start chat=%d trackID=%s out=%q source=%q thumb=%q",
		chatID,
		trackIDForThumb(track),
		out,
		sourcePath,
		localThumb,
	)

	cmd := exec.Command("ffmpeg", args...)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", thumb, fmt.Errorf("ffmpeg failed: %v: %s", err, strings.TrimSpace(stderr.String()))
	}

	logRTMPFileInfo("ffmpeg-output", out)

	gologging.InfoF(
		"RTMP DEBUG ffmpeg wrapper success chat=%d trackID=%s out=%q",
		chatID,
		trackIDForThumb(track),
		out,
	)

	return out, thumb, nil
}

func isRemoteRTMPSource(source string) bool {
	s := strings.ToLower(strings.TrimSpace(source))
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

func sanitizeRTMPCacheKey(s string) string {
	replacer := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		":", "_",
		"?", "_",
		"&", "_",
		"=", "_",
		"%", "_",
		"#", "_",
	)
	s = replacer.Replace(strings.TrimSpace(s))
	if s == "" {
		return "track"
	}
	return s
}

func ensureLocalRTMPThumbnail(
	thumb string,
	track *state.Track,
) (string, func(), error) {
	thumb = strings.TrimSpace(thumb)
	if thumb == "" {
		return "", nil, fmt.Errorf("thumbnail is empty")
	}

	if !isRemoteRTMPSource(thumb) {
		return thumb, nil, nil
	}

	if err := os.MkdirAll("cache", os.ModePerm); err != nil {
		return "", nil, err
	}

	ext := filepath.Ext(strings.Split(thumb, "?")[0])
	if ext == "" {
		ext = ".jpg"
	}

	name := "rtmp_thumb_" + sanitizeRTMPCacheKey(trackIDForThumb(track)) + ext
	dest := filepath.Join("cache", name)

	if _, err := os.Stat(dest); err == nil {
		return dest, nil, nil
	}

	client := &http.Client{
		Timeout: 20 * time.Second,
	}

	req, err := http.NewRequest(http.MethodGet, thumb, nil)
	if err != nil {
		return "", nil, err
	}

	req.Header.Set("User-Agent", "Mozilla/5.0")

	gologging.InfoF("RTMP DEBUG downloading thumbnail from %s", thumb)

	resp, err := client.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("thumbnail download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", nil, fmt.Errorf("thumbnail download returned status %d", resp.StatusCode)
	}

	file, err := os.Create(dest)
	if err != nil {
		return "", nil, err
	}
	defer file.Close()

	if _, err := io.Copy(file, resp.Body); err != nil {
		return "", nil, err
	}

	return dest, nil, nil
}

func trackIDForThumb(track *state.Track) string {
	if track == nil {
		return "track"
	}
	if strings.TrimSpace(track.ID) != "" {
		return track.ID
	}
	if strings.TrimSpace(track.Title) != "" {
		return track.Title
	}
	return "track"
}

func logRTMPFileInfo(label, path string) {
	if strings.TrimSpace(path) == "" {
		gologging.WarnF("RTMP DEBUG [%s] empty path", label)
		return
	}

	info, err := os.Stat(path)
	if err != nil {
		gologging.WarnF("RTMP DEBUG [%s] stat failed path=%s err=%v", label, path, err)
		return
	}

	gologging.InfoF(
		"RTMP DEBUG [%s] path=%s size=%d isDir=%v",
		label,
		path,
		info.Size(),
		info.IsDir(),
	)
}
