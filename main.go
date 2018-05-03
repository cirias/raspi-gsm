package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"github.com/cirias/tgbot"
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

	smsCh := make(chan *SMS)
	msgCh := make(chan *Message)

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

	go concat(smsCh, msgCh)

	if err := handle(s, smsCh); err != nil {
		log.Fatalln(err)
	}
}

func handle(s io.ReadWriter, out chan<- *SMS) error {
	var SetPDUMode = []byte("AT+CMGF=0\r\n")
	var ListUnreadSMS = []byte(fmt.Sprintf("AT+CMGL=%d\r\n", ReceivedUnread))
	var DeleteReadSMS = []byte("AT+CMGDA=1\r\n")

	if _, err := s.Write(SetPDUMode); err != nil {
		return fmt.Errorf("could not write to serial:", err)
	}

	command := ""
	scanner := bufio.NewScanner(s)
	for scanner.Scan() {
		line := scanner.Text()
		log.Println("read line:", line)

		switch line {
		case "":
			continue
		case "OK":
			if command == "CMGL" {
				if _, err := s.Write(DeleteReadSMS); err != nil {
					return fmt.Errorf("could not write to serial port: %v", err)
				}
			}

			command = ""
			continue
		case "ERROR":
			command = ""
			return fmt.Errorf("unexpected ERROR")
		}

		if strings.HasPrefix(line, "+CMGL: ") {
			command = "CMGL"

			cmgl, err := decodeCMGL(line[7:])
			if err != nil {
				return fmt.Errorf("could not decode CMGL: %v", err)
			}

			if cmgl.MessageStatus != ReceivedUnread {
				continue
			}

			if !scanner.Scan() {
				break
			}

			line := scanner.Text()

			log.Println("decoding SMS:", line)
			sms, err := decodeSMS(scanner.Text())
			if err != nil {
				log.Printf("could not decode SMS: %v", err)
				continue
				// return fmt.Errorf("could not decode SMS: %v", err)
			}

			out <- sms
			continue
		}

		if strings.HasPrefix(line, "+CMTI: ") {
			command = "CMTI"

			if _, err := s.Write(ListUnreadSMS); err != nil {
				return fmt.Errorf("could not write to serial port: %v", err)
			}
			continue
		}

		command = ""
		log.Println("ignore line:", line)
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("could not scan: %v", err)
	}

	return nil
}

type Message struct {
	From    string
	Date    time.Time
	Content string
}

func (m *Message) String() string {
	return fmt.Sprintf("*%s*\n*%s*\n%s", m.From, m.Date.Format(time.RFC3339), m.Content)
}

func concat(in <-chan *SMS, out chan<- *Message) {
	lookup := make(map[byte][]*SMS)

LOOP_IN:
	for sms := range in {

		for _, el := range sms.Tpdu.UDH {
			udhc, ok := el.(UDHConcatenated)
			if !ok {
				continue
			}

			ms, ok := lookup[udhc.Reference]
			if !ok {
				ms = make([]*SMS, udhc.Total)
				lookup[udhc.Reference] = ms
			}

			ms[udhc.Index-1] = sms

			content := ""
			for _, m := range ms {
				if m == nil {
					continue LOOP_IN
				}

				content += m.Tpdu.UD
			}

			out <- &Message{
				From:    sms.Tpdu.OA,
				Date:    sms.Tpdu.SCTS,
				Content: content,
			}
			delete(lookup, udhc.Reference)

			continue LOOP_IN
		}

		out <- &Message{
			From:    sms.Tpdu.OA,
			Date:    sms.Tpdu.SCTS,
			Content: sms.Tpdu.UD,
		}
	}
}
