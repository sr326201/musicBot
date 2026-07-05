package modules

import (
	"math/rand"
	"time"

	tg "github.com/amarnathcjd/gogram/telegram"
)

func init() {
	helpTexts["/robot"] = `<i>check online robot.</i>`
}

func sayHandler(m *tg.NewMessage) error {
	emojis := []string{"🍌", "⚡️", "🔥"}

	rand.Seed(time.Now().UnixNano())

	randomEmoji := emojis[rand.Intn(len(emojis))]

	reactToCommandMessage(m, randomEmoji)

	return tg.ErrEndGroup
}
