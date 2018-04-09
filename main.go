package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"strings"

	"github.com/tarm/serial"
)

type SMS struct {
	From    string
	Date    string
	Content string
}

func (s *SMS) String() string {
	return fmt.Sprintf("From: %s\r\nDate: %s\r\nContent: %s", s.From, s.Date, s.Content)
}

func main() {
	c := &serial.Config{Name: "/dev/ttyAMA0", Baud: 115200}

	s, err := serial.OpenPort(c)
	if err != nil {
		log.Fatalf("could not open port: %s", err)
	}

	smsCh := make(chan *SMS)

	go func() {
		for sms := range smsCh {
			fmt.Println(sms)
		}
	}()

	if err := handle(s, smsCh); err != nil {
		log.Fatalln(err)
	}
}

func handle(s io.ReadWriter, out chan<- *SMS) error {
	var SetTextMode = []byte("AT+CMGF=1\r\n")
	var ListUnreadSMS = []byte("AT+CMGL=\"REC UNREAD\"\r\n")

	if _, err := s.Write(SetTextMode); err != nil {
		return fmt.Errorf("could not write to serial:", err)
	}

	var sms *SMS

	scanner := bufio.NewScanner(s)
	for scanner.Scan() {
		line := scanner.Text()
		log.Println("<-serial:", line)

		switch line {
		case "":
			if sms != nil {
				sms.Content += "\r\n"
			}
			continue
		case "OK":
			if sms != nil { // All +CMGL finished
				// send last SMS
				out <- sms
			}
			sms = nil
			continue
		case "ERROR":
			return fmt.Errorf("unexpected ERROR")
		default:
		}

		if strings.HasPrefix(line, "+CMGL: ") {
			if sms != nil { // multiple +CMGL response
				// send privous SMS first
				out <- sms
			}

			args := strings.Split(line[7:], ",")
			if len(args) < 5 {
				return fmt.Errorf("invalid +CMGL response args from line: %s", line)
			}

			sms = &SMS{
				From: strings.Trim(args[2], "\""),
				Date: strings.Trim(args[4], "\""),
			}
		} else if strings.HasPrefix(line, "+CMTI: ") {
			if sms != nil {
				return fmt.Errorf("unexpected +CMTI response during another unfinishd +CMGL response")
			}

			if _, err := s.Write(ListUnreadSMS); err != nil {
				return fmt.Errorf("could not write to serial port: %v", err)
			}
		} else if sms != nil {
			sms.Content += line + "\r"
		} else {
			log.Println("ignore command:", line)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("could not scan: %v", err)
	}

	return nil
}
