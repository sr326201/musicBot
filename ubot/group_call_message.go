package ubot

import (
	"math/rand"
	"strings"

	tg "github.com/amarnathcjd/gogram/telegram"
)

type GroupCallMessageEvent struct {
	ChatID     int64
	SenderID   int64
	SenderName string
	Text       string
	Call       tg.InputGroupCall
}

func getGroupCallMessageSenderID(peer tg.Peer) int64 {
	switch p := peer.(type) {
	case *tg.PeerUser:
		return p.UserID
	case *tg.PeerChannel:
		return p.ChannelID
	case *tg.PeerChat:
		return p.ChatID
	default:
		return 0
	}
}

func getGroupCallMessageSenderName(c *tg.Client, senderID int64) string {
	if c == nil || senderID <= 0 {
		return ""
	}

	if user, err := c.GetUser(senderID); err == nil && user != nil {
		name := strings.TrimSpace(user.FirstName + " " + user.LastName)
		if name != "" {
			return name
		}

		if user.Username != "" {
			return "@" + user.Username
		}
	}

	if channel, err := c.GetChannel(senderID); err == nil && channel != nil {
		return channel.Title
	}

	if chat, err := c.GetChat(senderID); err == nil && chat != nil {
		return chat.Title
	}

	return ""
}

func (ctx *Context) dispatchGroupCallMessage(event *GroupCallMessageEvent) {
	ctx.callbacksMutex.RLock()
	callbacks := make([]func(*GroupCallMessageEvent), len(ctx.groupCallMessageCallbacks))
	copy(callbacks, ctx.groupCallMessageCallbacks)
	ctx.callbacksMutex.RUnlock()

	for _, callback := range callbacks {
		if callback == nil {
			continue
		}

		go callback(event)
	}
}

func (ctx *Context) SendGroupCallMessage(call tg.InputGroupCall, text string) error {
	_, err := ctx.app.PhoneSendGroupCallMessage(&tg.PhoneSendGroupCallMessageParams{
		Call:     call,
		RandomID: rand.Int63(),
		Message: &tg.TextWithEntities{
			Text: text,
		},
	})

	return err
}
