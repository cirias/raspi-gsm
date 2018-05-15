package main

import (
	"flag"
	"fmt"
	"io"
	"log"

	"github.com/cirias/tgbot"
	"github.com/pkg/errors"
	"github.com/tarm/serial"
)

var (
	dev    = flag.String("dev", "/dev/ttyAMA0", "device path of serial port")
	token  = flag.String("token", "", "telegram bot token")
	chatId = flag.Int64("chatId", 0, "telegram chart id")
)

func main() {
	flag.Parse()

	c := &serial.Config{Name: *dev, Baud: 115200}

	s, err := serial.OpenPort(c)
	if err != nil {
		log.Fatalf("could not open port: %s", err)
	}

	msgCh := make(chan fmt.Stringer)

	bot := tgbot.NewBot(*token)

	go func() {
		if err := Serve(bot); err != nil {
			log.Fatalln(err)
		}
	}()

	go func() {
		for m := range msgCh {
			log.Println("sending message:\n", m)
			if err := SendMessage(bot, *chatId, m); err != nil {
				log.Fatalln(err)
			}
		}
	}()

	if err := handle(s, msgCh); err != nil {
		log.Fatalln(err)
	}
}

func handle(s io.ReadWriter, out chan<- fmt.Stringer) error {
	var SetPDUMode = []byte("AT+CMGF=0\r\n")
	var ListUnreadSMS = []byte(fmt.Sprintf("AT+CMGL=%d\r\n", ReceivedUnread))
	var DeleteReadSMS = []byte("AT+CMGDA=1\r\n")

	if _, err := s.Write(SetPDUMode); err != nil {
		return fmt.Errorf("could not write to serial:", err)
	}

	r := NewEventReader(s)

	for {
		event, err := r.ReadEvent()
		if err != nil {
			return errors.Wrap(err, "could not read event")
		}

		switch event.(type) {
		case *EventMessage:
			out <- event.(*EventMessage)
		case *EventCMTI:
			if _, err := s.Write(DeleteReadSMS); err != nil {
				return fmt.Errorf("could not write to serial port: %v", err)
			}
			if _, err := s.Write(ListUnreadSMS); err != nil {
				return fmt.Errorf("could not write to serial port: %v", err)
			}
		}
	}

	return nil
}
