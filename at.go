package main

import (
	"bufio"
	"strings"

	"github.com/pkg/errors"
)

func parseOKResponse(scanner *bufio.Scanner) (bool, error) {
	line := scanner.Text()
	if strings.Contains(line, " ERROR:") {
		return false, errors.Errorf("response with error: %s", line)
	}
	if line != "OK" {
		return false, nil
	}

	return true, nil
}

func parseCMTIResponse(scanner *bufio.Scanner) (bool, error) {
	line := scanner.Text()
	if !strings.HasPrefix(line, "+CMTI: ") {
		return false, nil
	}

	return true, nil
}

type CMGLResponse struct {
	CMGL *CMGL
	SMS  *SMS
}

func parseCMGLResponse(scanner *bufio.Scanner) (*CMGLResponse, error) {
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

	return &CMGLResponse{
		CMGL: cmgl,
		SMS:  sms,
	}, nil
}
