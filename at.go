package main

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/pkg/errors"
)

type Event interface {
}

type ParseFunc func(*bufio.Scanner) (Event, error)

type EventCMTI struct{}

func ParseCMTI(scanner *bufio.Scanner) (Event, error) {
	line := scanner.Text()

	if !strings.HasPrefix(line, "+CMTI: ") {
		return nil, nil
	}

	return &EventCMTI{}, nil
}

type EventCMGL struct {
	CMGL *CMGL
	SMS  *SMS
}

func ParseCMGL(scanner *bufio.Scanner) (Event, error) {
	line := scanner.Text()

	if !strings.HasPrefix(line, "+CMGL: ") {
		return nil, nil
	}

	cmgl, err := decodeCMGL(line[7:])
	if err != nil {
		return nil, errors.Wrapf(err, "could not decode +CMGL: %v", line)
	}

	if !scanner.Scan() {
		return nil, errors.Wrapf(scanner.Err(), "could not scan")
	}
	line = scanner.Text()

	sms, err := decodeSMS(line)
	if err != nil {
		return nil, errors.Wrapf(err, "could not decode SMS: %v", line)
	}

	return &EventCMGL{
		CMGL: cmgl,
		SMS:  sms,
	}, nil
}

type EventReader interface {
	ReadEvent() (Event, error)
}

func NewEventReader(r io.Reader) EventReader {
	rawReader := NewRawEventReader(r)
	return NewConcatedMessageReader(rawReader)
}

type RawEventReader struct {
	scanner    *bufio.Scanner
	parseFuncs []ParseFunc
}

func NewRawEventReader(r io.Reader) *RawEventReader {
	return &RawEventReader{
		scanner:    bufio.NewScanner(r),
		parseFuncs: []ParseFunc{ParseCMTI, ParseCMGL},
	}
}

func (r *RawEventReader) ReadEvent() (Event, error) {
	// parsers := []ParseFunc{parseCMTI, parseCMGL}
	for r.scanner.Scan() {
		for _, parse := range r.parseFuncs {
			event, err := parse(r.scanner)
			if err != nil {
				return nil, err
			}
			if event != nil {
				return event, nil
			}
		}
	}

	return nil, errors.Wrapf(r.scanner.Err(), "could not scan")
}

type ConcatedMessageReader struct {
	reader EventReader
	lookup map[byte][]*SMS
}

func NewConcatedMessageReader(r EventReader) *ConcatedMessageReader {
	return &ConcatedMessageReader{
		reader: r,
		lookup: make(map[byte][]*SMS),
	}
}

type EventMessage struct {
	From    string
	Date    time.Time
	Content string
}

func (m *EventMessage) String() string {
	return fmt.Sprintf("*%s*\n*%s*\n%s", m.From, m.Date.Format(time.RFC3339), m.Content)
}

func (r *ConcatedMessageReader) ReadEvent() (Event, error) {
LOOP_EVENT:
	for {
		event, err := r.reader.ReadEvent()
		if err != nil {
			return nil, err
		}

		e, ok := event.(*EventCMGL)
		if !ok {
			return event, nil
		}

		if e.CMGL.MessageStatus != ReceivedUnread {
			continue
		}

		var udhc *UDHConcatenated
		for _, el := range e.SMS.Tpdu.UDH {
			var ok bool
			udhc, ok = el.(*UDHConcatenated)
			if !ok {
				continue
			}
		}

		if udhc == nil {
			return &EventMessage{
				From:    e.SMS.Tpdu.OA,
				Date:    e.SMS.Tpdu.SCTS,
				Content: e.SMS.Tpdu.UD,
			}, nil
		}

		ms, ok := r.lookup[udhc.Reference]
		if !ok {
			ms = make([]*SMS, udhc.Total)
			r.lookup[udhc.Reference] = ms
		}

		ms[udhc.Index-1] = e.SMS

		content := ""
		for _, m := range ms {
			if m == nil {
				continue LOOP_EVENT
			}

			content += m.Tpdu.UD
		}

		delete(r.lookup, udhc.Reference)
		return &EventMessage{
			From:    e.SMS.Tpdu.OA,
			Date:    e.SMS.Tpdu.SCTS,
			Content: content,
		}, nil
	}
}
