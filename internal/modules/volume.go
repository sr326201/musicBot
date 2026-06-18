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
	"fmt"
	"strconv"
	"strings"

	"github.com/amarnathcjd/gogram/telegram"

	"main/internal/locales"
	"main/internal/utils"
)

func init() {
	helpTexts["/volume"] = `<i>Control playback volume.</i>

<u>Usage:</u>
<b>/volume</b> — Show current volume
<b>/volume [value]</b> — Set volume (0-100)
<b>/volume [value] [seconds]</b> — Set with auto-reset timer

<b>⚙️ Features:</b>
• Range: 0% to 100%
• Auto-reset timer (5-3600 seconds)
• Real-time adjustment

<b>🔒 Restrictions:</b>
• Only <b>chat admins</b> or <b>authorized users</b> can use this

<b>💡 Examples:</b>
<code>/volume 100</code> — Set volume to 100% (original)
<code>/volume 50</code> — Set volume to 50%
<code>/volume 75 300</code> — 75% volume for 5 minutes, then reset to 100%
<code>/volume 0</code> — Mute volume
<code>/volume reset</code> — Reset to 100%`
}

func volumeHandler(m *telegram.NewMessage) error {
	return handleVolume(m, false)
}

func cvolumeHandler(m *telegram.NewMessage) error {
	return handleVolume(m, true)
}

func handleVolume(m *telegram.NewMessage, cplay bool) error {
	r, err := getEffectiveRoom(m, cplay)
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

	args := strings.Fields(m.Text())

	// No args -> show current volume or usage hint
	if len(args) < 2 {
		currentVol := r.Volume() * 100
		m.Reply(F(chatID, "volume_current", locales.Arg{
			"volume": fmt.Sprintf("%.0f%%", currentVol),
			"title":  utils.EscapeHTML(utils.ShortTitle(t.Title, 25)),
			"cmd":    getCommand(m),
		}))
		return telegram.ErrEndGroup
	}

	// Parse volume value
	raw := strings.ToLower(strings.TrimSpace(args[1]))
	var newVolume float64

	if raw == "reset" || raw == "normal" || raw == "default" || raw == "100" {
		newVolume = 1.0
	} else {
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil || v < 0 || v > 100 {
			m.Reply(F(chatID, "volume_invalid", locales.Arg{
				"cmd": getCommand(m),
			}))
			return telegram.ErrEndGroup
		}
		newVolume = v / 100
		if newVolume < 0 {
			newVolume = 0
		} else if newVolume > 1 {
			newVolume = 1
		}
	}

	// Same volume -> give info
	if newVolume == r.Volume() {
		m.Reply(F(chatID, "volume_already_set", locales.Arg{
			"volume": fmt.Sprintf("%.0f%%", newVolume*100),
			"title":  utils.EscapeHTML(utils.ShortTitle(t.Title, 25)),
		}))
		return telegram.ErrEndGroup
	}

	// Apply volume
	if err := r.SetVolume(newVolume); err != nil {
		m.Reply(F(chatID, "volume_failed", locales.Arg{
			"volume": fmt.Sprintf("%.0f%%", newVolume*100),
			"error":  err.Error(),
		}))
		return telegram.ErrEndGroup
	}

	mention := utils.MentionHTML(m.Sender)
	m.Reply(F(chatID, "volume_set", locales.Arg{
		"volume": fmt.Sprintf("%.0f%%", newVolume*100),
		"user":   mention,
	}))

	return telegram.ErrEndGroup
}
