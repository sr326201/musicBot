package modules

import (
	"net/url"
	"strings"

	tg "github.com/amarnathcjd/gogram/telegram"

	"main/internal/database"
	"main/internal/locales"
	"main/internal/utils"
)

func init() {
	helpTexts["streamthumb"] = `<i>Set or view the RTMP thumbnail for this chat.</i>

<u>Usage:</u>
<b>/streamthumb</b> — Show current thumbnail status
<b>/streamthumb [image_url]</b> — Set custom RTMP thumbnail

<b>⚙️ Priority:</b>
• Custom chat thumbnail
• Track artwork
• Default RTMP thumbnail`

	helpTexts["setstreamthumb"] = helpTexts["streamthumb"]

	helpTexts["delstreamthumb"] = `<i>Remove custom RTMP thumbnail for this chat.</i>

<u>Usage:</u>
<b>/delstreamthumb</b>`
}

func streamThumbHandler(m *tg.NewMessage) error {
	chatID := m.ChannelID()
	args := strings.Fields(m.Text())

	current, err := database.RTMPThumbnail(chatID)
	if err != nil {
		m.Reply(F(chatID, "rtmp_thumb_fetch_fail"))
		return tg.ErrEndGroup
	}

	if len(args) < 2 {
		source := "default"
		value := resolveRTMPThumbnail(chatID, nil)

		if strings.TrimSpace(current) != "" {
			source = "custom"
			value = current
		}

		m.Reply(F(chatID, "rtmp_thumb_status", locales.Arg{
			"source": source,
			"thumb":  utils.EscapeHTML(value),
		}))
		return tg.ErrEndGroup
	}

	thumb := strings.TrimSpace(args[1])
	parsed, err := url.ParseRequestURI(thumb)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		m.Reply(F(chatID, "rtmp_thumb_invalid"))
		return tg.ErrEndGroup
	}

	if err := database.SetRTMPThumbnail(chatID, thumb); err != nil {
		m.Reply(F(chatID, "rtmp_thumb_update_fail"))
		return tg.ErrEndGroup
	}

	m.Reply(F(chatID, "rtmp_thumb_updated", locales.Arg{
		"thumb": utils.EscapeHTML(thumb),
	}))
	return tg.ErrEndGroup
}

func delStreamThumbHandler(m *tg.NewMessage) error {
	chatID := m.ChannelID()

	if err := database.ClearRTMPThumbnail(chatID); err != nil {
		m.Reply(F(chatID, "rtmp_thumb_update_fail"))
		return tg.ErrEndGroup
	}

	m.Reply(F(chatID, "rtmp_thumb_cleared"))
	return tg.ErrEndGroup
}
