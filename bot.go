package main

import (
	"log"
	"math"
	"time"

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
		var updates []*tgbot.Update
		err := willRetry(func() error {
			var err error
			updates, err = bot.GetUpdates(params)
			return errors.Wrap(err, "could not get updates")
		}, 4)
		if err != nil {
			return err
		}

		for _, u := range updates {
			if u.Message.Text == "/ping" {
				err := willRetry(func() error {
					_, err := bot.SendMessage(&tgbot.SendMessageParams{
						ChatId: u.Message.Chat.Id,
						Text:   "pong",
					})
					return errors.Wrap(err, "could not send message with tgbot")
				}, 4)

				if err != nil {
					return err
				}
			}
		}

		if len(updates) > 0 {
			params.Offset = updates[len(updates)-1].Id + 1
		}
	}
}

func SendMessage(bot *tgbot.Bot, chatId int64, m *Message) error {
	return willRetry(func() error {
		_, err := bot.SendMessage(&tgbot.SendMessageParams{
			ChatId:    chatId,
			Text:      m.String(),
			ParseMode: "markdown",
		})
		return errors.Wrap(err, "could not send message with tgbot")
	}, 4)
}

func willRetry(fn func() error, n int) error {
	err := fn()
	for i := 0; i < n; i += 1 {
		if err == nil {
			break
		}
		time.Sleep(time.Duration(math.Pow10(i)) * time.Millisecond * 100) // 0.1, 1, 10, 100

		log.Printf("retry %d: %v\n", i, err)
		err = fn()
	}

	return err
}
