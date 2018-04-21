package main

import (
	"github.com/cirias/tgbot"
	"github.com/pkg/errors"
)

func Serve(bot *tgbot.Bot) error {
	params := &tgbot.GetUpdatesParams{
		Offset:  0,
		Limit:   10,
		Timeout: 10,
	}

	for {
		updates, err := bot.GetUpdates(params)
		if err != nil {
			return errors.Wrap(err, "could not get updates")
		}

		for _, u := range updates {
			if u.Message.Text == "/ping" {
				_, err := bot.SendMessage(&tgbot.SendMessageParams{
					ChatId: u.Message.Chat.Id,
					Text:   "pong",
				})

				if err != nil {
					return errors.Wrap(err, "could not send message with tgbot")
				}
			}
		}

		if len(updates) > 0 {
			params.Offset = updates[len(updates)-1].Id + 1
		}
	}
}

func SendMessage(bot *tgbot.Bot, chatId int64, m *Message) error {
	_, err := bot.SendMessage(&tgbot.SendMessageParams{
		ChatId:    chatId,
		Text:      m.String(),
		ParseMode: "markdown",
	})

	return errors.Wrap(err, "could not send message with tgbot")
}
