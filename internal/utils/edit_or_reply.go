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

package utils

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/Laky-64/gologging"
	"github.com/amarnathcjd/gogram/telegram"
)

func EOR(
	msg *telegram.NewMessage,
	text string,
	opts ...*telegram.SendOptions,
) (m *telegram.NewMessage, err error) {
	if msg == nil {
		gologging.Error("[EOR] nil msg at " + callerInfo(2))
		return nil, nil
	}

	opt := pickSendOptions(opts)

	if isNonTextEditable(msg) {
		return replaceWithNewMessage(msg, text, opt)
	}

	m, err = msg.Edit(text, opts...)
	if err != nil {
		return replaceWithNewMessage(msg, text, opt)
	}
	return m, nil
}

func pickSendOptions(opts []*telegram.SendOptions) *telegram.SendOptions {
	if len(opts) > 0 {
		return opts[0]
	}
	return nil
}

// isNonTextEditable reports messages that cannot be converted to a plain text
// status update via Edit (e.g. stickers sent during /play search).
func isNonTextEditable(msg *telegram.NewMessage) bool {
	if msg == nil {
		return false
	}
	if msg.Sticker() != nil {
		return true
	}
	if doc := msg.Document(); doc != nil {
		mime := strings.ToLower(doc.MimeType)
		if mime == "application/x-tgsticker" || mime == "application/x-tgs-sticker" {
			return true
		}
	}
	switch msg.MediaType() {
	case "sticker":
		return true
	}
	return false
}

func replaceWithNewMessage(
	msg *telegram.NewMessage,
	text string,
	opt *telegram.SendOptions,
) (*telegram.NewMessage, error) {
	if _, delErr := msg.Delete(); delErr != nil {
		gologging.DebugF("[EOR] delete before replace failed: %v", delErr)
	}

	var (
		reply *telegram.NewMessage
		err   error
	)

	// Prefer replying to the original command when the status msg was a reply to it.
	// if parent, parentErr := msg.GetReplyMessage(); parentErr == nil && parent != nil {
	// 	reply, err = parent.Reply(text, opt)
	// } else {
	// 	reply, err = msg.Respond(text, opt)
	// }

	if parent, parentErr := msg.GetReplyMessage(); parentErr == nil && parent != nil {
		reply, err = parent.Reply(text, opt)
	} else {
		reply, err = msg.Respond(text, opt)
	}

	if err != nil {
		return nil, err
	}
	if _, delErr := msg.Delete(); delErr != nil {
		gologging.DebugF("[EOR] delete before replace failed: %v", delErr)
	}
	return reply, nil

	// if err != nil {
	// 	gologging.Error(
	// 		"[EOR] " + err.Error() +
	// 			" | called from " + callerInfo(2),
	// 	)
	// }
	// return reply, err
}

func callerInfo(skip int) string {
	_, file, line, ok := runtime.Caller(skip)
	if !ok {
		return "unknown:0"
	}
	return fmt.Sprintf("%s:%d", file, line)
}
