package main

import (
	"bufio"
	"io"
	"strings"

	"github.com/pkg/errors"
)

type Event interface {
}

type parseFunc func(*bufio.Scanner) (Event, error)

func readOne(parsers []parseFunc, scanner *bufio.Scanner) (Event, error) {
	for scanner.Scan() {
		for _, parse := range parsers {
			event, err := parse(scanner)
			if err != nil {
				return nil, err
			}
			if event != nil {
				return event, nil
			}
		}
	}

	return nil, errors.Wrapf(scanner.Err(), "could not scan")
}

type EventCMTI struct{}

func parseCMTI(scanner *bufio.Scanner) (Event, error) {
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

func parseCMGL(scanner *bufio.Scanner) (Event, error) {
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

func ReadEvent(r io.Reader) (Event, error) {
	scanner := bufio.NewScanner(r)
	parsers := []parseFunc{parseCMTI, parseCMGL}
	return readOne(parsers, scanner)
}
