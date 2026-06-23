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
	"time"

	"main/internal/core"
)

func MonitorRooms() {
	ticker := time.NewTicker(4 * time.Second)
	defer ticker.Stop()

	sem := make(chan struct{}, 20)

	for range ticker.C {
		for chatID, room := range core.GetAllRooms() {

			sem <- struct{}{}

			go func(chatID int64, r *core.RoomState) {
				defer func() { <-sem }()

				if r.IsPaused() {
					return
				}

				// if !r.IsActiveChat() {
				// 	if r.IsEnded() {
				// 		closePlaybackPanel(r, buildPlaybackFinishedText(r.ChatID, r))
				// 		core.DeleteRoom(chatID)
				// 	}
				// 	return
				// }

				//v2
				// if !r.IsActiveChat() {
				// 	finishPlaybackRoom(r, buildPlaybackFinishedText(r.ChatID, r))
				// 	return
				// }

				const transitionTimeout = 45 * time.Second

				if ok, v := r.GetData("is_transitioning"); ok {
					if b, _ := v.(bool); b {
						if okStarted, started := r.GetData("transition_started_at"); okStarted {
							if t, ok := started.(time.Time); ok && time.Since(t) > transitionTimeout {
								r.DeleteData("transition_started_at")
								r.DeleteData("is_transitioning")
							} else {
								return
							}
						} else {
							r.SetData("transition_started_at", time.Now())
							return
						}
					}
				}

				if !r.IsActiveChat() {
					// فقط وقتی واقعاً ترک به انتها رسیده و چیزی در صف نیست، حذف کن
					if r.IsEnded() {
						finishPlaybackRoom(r, buildPlaybackFinishedText(r.ChatID, r))
					}
					return
				}

				r.Parse()

				statusMsg := r.StatusMsg()
				if statusMsg == nil {
					return
				}

				okLast, last := r.GetData("panel_last_edit")
				if okLast {
					if t, ok := last.(time.Time); ok && time.Since(t) < 10*time.Second {
						return
					}
				}

				r.SetData("panel_last_edit", time.Now())
				schedulePlaybackPanelRefresh(r.ChatID, r, "", "")
			}(chatID, room)
		}
	}
}
