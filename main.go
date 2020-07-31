// http://www.glynstore.com/content/docs/terminals/Telit_AT_Commands_Reference_Guide_r8.pdf
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/cirias/tgbot"
	"github.com/pkg/errors"
	"github.com/tarm/serial"
)

var (
	dev    = flag.String("dev", "/dev/ttyAMA0", "device path of serial port")
	token  = flag.String("token", "", "telegram bot token")
	chatId = flag.Int64("chatId", 0, "telegram chart id")
)

var SetPDUMode = []byte("AT+CMGF=0\r\n")
var EnableErrorCode = []byte("AT+CMEE=1\r\n")
var ListUnreadSMS = []byte(fmt.Sprintf("AT+CMGL=%d\r\n", ReceivedUnread))
var DeleteReadSMS = []byte("AT+CMGD=1,1\r\n")

func main() {
	flag.Parse()

	bot := tgbot.NewBot(*token)
	go func() {
		if err := Serve(bot); err != nil {
			log.Fatalln(err)
		}
	}()

	port, err := serial.OpenPort(&serial.Config{Name: *dev, Baud: 115200})
	if err != nil {
		log.Fatalf("could not open port: %s", err)
	}

	reader, err := newMessageReader(port)
	if err != nil {
		log.Fatalln("could not new message reader:", err)
	}

	for {
		m, err := readMessage(port, reader)
		if err != nil {
			log.Fatalln(err)
		}

		log.Printf("sending message:\n%s\n", m)
		if err := SendMessage(bot, *chatId, m); err != nil {
			log.Fatalln(err)
		}
	}
}

func newMessageReader(port io.ReadWriter) (EventReader, error) {
	if _, err := port.Write(SetPDUMode); err != nil {
		return nil, fmt.Errorf("could not write to serial:", err)
	}

	time.Sleep(100 * time.Millisecond)

	if _, err := port.Write(EnableErrorCode); err != nil {
		return nil, fmt.Errorf("could not write to serial:", err)
	}

	rawReader := NewRawEventReader(port)
	return NewConcatedMessageReader(rawReader), nil
}

func readMessage(port io.Writer, r EventReader) (fmt.Stringer, error) {
	for {
		event, err := r.ReadEvent()
		if err != nil {
			return nil, errors.Wrap(err, "could not read event")
		}

		switch event.(type) {
		case *EventMessage:
			return event.(*EventMessage), nil
		case *EventCMTI:
			if _, err := port.Write(DeleteReadSMS); err != nil {
				return nil, errors.Wrap(err, "could not write to serial port")
			}

			// wait or will get error 604: can not allocate control socket
			// it's just take lots of time for deleting
			time.Sleep(500 * time.Millisecond)

			if _, err := port.Write(ListUnreadSMS); err != nil {
				return nil, errors.Wrap(err, "could not write to serial port")
			}
		}
	}
}
