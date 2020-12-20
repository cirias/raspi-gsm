// http://www.glynstore.com/content/docs/terminals/Telit_AT_Commands_Reference_Guide_r8.pdf
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

	scanner := bufio.NewScanner(port)
	if _, err := sendCommand(port, scanner, SetPDUMode); err != nil {
		log.Fatalln(err)
	}
	if _, err := sendCommand(port, scanner, EnableErrorCode); err != nil {
		log.Fatalln(err)
	}

	mc := &MessageConcatenator{lookup: make(map[byte]*concatenatorLookupRecord)}

	if err := readSendDeleteMessages(bot, *chatId, port, scanner, mc); err != nil {
		log.Fatalln(err)
	}

	for scanner.Scan() {
		isCMTI, err := parseCMTIResponse(scanner)
		if err != nil {
			log.Fatalln("could not parse CMTI response", err)
		}
		if !isCMTI {
			continue
		}

		if err := readSendDeleteMessages(bot, *chatId, port, scanner, mc); err != nil {
			log.Fatalln(err)
		}
	}
}

func readSendDeleteMessages(bot *tgbot.Bot, chatId int64, w io.Writer, scanner *bufio.Scanner, mc *MessageConcatenator) error {
	var resps []*CMGLResponse
	hasNewMessage := true
	var err error
	for hasNewMessage {
		resps, hasNewMessage, err = listUnreadSMS(w, scanner)
		if err != nil {
			return errors.Wrapf(err, "could not list unread sms")
		}

		messages := mc.ListMessages(resps)
		for _, message := range messages {
			log.Printf("sending message:\n%s\n", message)
			if err := SendMessage(bot, chatId, message); err != nil {
				return errors.Wrapf(err, "could not send message")
			}
		}

		hasNew, err := sendCommand(w, scanner, DeleteReadSMS)
		if err != nil {
			return errors.Wrapf(err, "could not send command")
		}
		hasNewMessage = hasNewMessage || hasNew
	}
	return nil
}

type MessageConcatenator struct {
	lookup map[byte]*concatenatorLookupRecord
}

type concatenatorLookupRecord struct {
	timestamp time.Time
	smses     []*SMS
}

type Message struct {
	From    string
	Date    time.Time
	Content string
}

func (m *Message) String() string {
	return fmt.Sprintf("*%s*\n*%s*\n%s", m.From, m.Date.Format(time.RFC3339), m.Content)
}

func (mc *MessageConcatenator) ListMessages(resps []*CMGLResponse) []*Message {
	now := time.Now()

	messages := make([]*Message, 0, len(resps))
	for _, resp := range resps {
		if resp.CMGL.MessageStatus != ReceivedUnread {
			continue
		}

		udhc := resp.SMS.Tpdu.UDHConcatenated()

		if udhc == nil {
			messages = append(messages, &Message{
				From:    resp.SMS.Tpdu.OA,
				Date:    resp.SMS.Tpdu.SCTS,
				Content: resp.SMS.Tpdu.UD,
			})
			continue
		}

		record, ok := mc.lookup[udhc.Reference]
		if !ok {
			record = &concatenatorLookupRecord{
				timestamp: now,
				smses:     make([]*SMS, udhc.Total),
			}
			mc.lookup[udhc.Reference] = record
		}
		record.smses[udhc.Index-1] = resp.SMS

		content, end := concatSMSContent(record.smses)
		if !end {
			continue
		}

		messages = append(messages, &Message{
			From:    resp.SMS.Tpdu.OA,
			Date:    resp.SMS.Tpdu.SCTS,
			Content: content,
		})
		delete(mc.lookup, udhc.Reference)
	}

	// append timeouted messages
	for key, record := range mc.lookup {
		if record == nil {
			delete(mc.lookup, key)
			continue
		}
		if len(record.smses) < 1 {
			delete(mc.lookup, key)
			continue
		}
		sms := record.smses[0]
		if now.Sub(record.timestamp) < time.Minute {
			content, _ := concatSMSContent(record.smses)
			messages = append(messages, &Message{
				From:    sms.Tpdu.OA,
				Date:    sms.Tpdu.SCTS,
				Content: content,
			})
			delete(mc.lookup, key)
			continue
		}
	}

	return messages
}

func concatSMSContent(ms []*SMS) (content string, end bool) {
	for _, m := range ms {
		if m == nil {
			return content, false
		}

		content += m.Tpdu.UD
	}

	return content, true
}

func listUnreadSMS(w io.Writer, scanner *bufio.Scanner) (resps []*CMGLResponse, hasNewMessage bool, err error) {
	if _, err := w.Write(ListUnreadSMS); err != nil {
		return nil, hasNewMessage, errors.Wrapf(err, "could not write command: %s", strings.TrimSpace(string(ListUnreadSMS)))
	}

	resps = make([]*CMGLResponse, 0)
	for scanner.Scan() {
		log.Println("read line:", scanner.Text())

		isOK, err := parseOKResponse(scanner)
		if err != nil {
			return nil, hasNewMessage, errors.Wrapf(err, "could not parse OK response")
		}
		if isOK {
			return resps, hasNewMessage, nil
		}

		isCMTI, err := parseCMTIResponse(scanner)
		if err != nil {
			return nil, hasNewMessage, errors.Wrapf(err, "could not parse CMTI response")
		}
		if isCMTI {
			hasNewMessage = true
			continue
		}

		resp, err := parseCMGLResponse(scanner)
		if err != nil {
			return nil, hasNewMessage, errors.Wrapf(err, "could not parse CMGL response")
		}
		if resp != nil {
			resps = append(resps, resp)
		}
	}

	return nil, hasNewMessage, errors.Wrapf(err, "scanner ends unexpectedly")
}

func sendCommand(w io.Writer, scanner *bufio.Scanner, cmd []byte) (hasNewMessage bool, err error) {
	if _, err := w.Write(cmd); err != nil {
		return hasNewMessage, errors.Wrapf(err, "could not write command: %s", strings.TrimSpace(string(cmd)))
	}

	for scanner.Scan() {
		log.Println("read line:", scanner.Text())

		isOK, err := parseOKResponse(scanner)
		if err != nil {
			return hasNewMessage, errors.Wrapf(err, "could not parse OK response")
		}
		if isOK {
			return hasNewMessage, nil
		}

		isCMTI, err := parseCMTIResponse(scanner)
		if err != nil {
			return hasNewMessage, errors.Wrapf(err, "could not parse CMTI response")
		}
		if isCMTI {
			hasNewMessage = true
			continue
		}
	}

	return hasNewMessage, errors.Wrapf(err, "scanner ends unexpectedly")
}
