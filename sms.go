package main

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
)

type CMGL struct {
	Index         int
	MessageStatus int
	AddressText   string
	TpduLength    int
}

const (
	ReceivedUnread = iota
	ReceivedRead
	StoredUnsent
	StoredSent
)

// decodeCMGL decode +CMGL: 1,0,"",62 text
func decodeCMGL(line string) (*CMGL, error) {
	args := strings.Split(line, ",")
	index, err := strconv.Atoi(args[0])
	if err != nil {
		return nil, fmt.Errorf("could not parse CMGL index: %v %s", err, args[0])
	}

	messageStatus, err := strconv.Atoi(args[1])
	if err != nil {
		return nil, fmt.Errorf("could not parse CMGL message_status: %v %s", err, args[1])
	}

	tpduLength, err := strconv.Atoi(args[3])
	if err != nil {
		return nil, fmt.Errorf("could not parse CMGL TPDU_length: %v %s", err, args[3])
	}

	return &CMGL{
		Index:         index,
		MessageStatus: messageStatus,
		AddressText:   args[2],
		TpduLength:    tpduLength,
	}, nil
}

type SMS struct {
	Smsc SMSC
	Tpdu TPDU
}

type SMSC struct {
	Len byte
	// Ignore the other fields
}

type TPDU struct {
	Flags byte          // first octet of SM-TL TPDU
	OA    string        // Originating Address
	PID   byte          // Protocal Identifier
	DCS   byte          // Data Coding Schema
	SCTS  time.Time     // Service Centre Time Stamp
	UDH   []interface{} // User Data Header
	UD    string        // User Data
}

const TP_UDHI byte = 0x40

func decodeSMS(text string) (*SMS, error) {
	bs, err := hex.DecodeString(text)
	if err != nil {
		return nil, fmt.Errorf("could not decode hex from text: %v", err)
	}

	buf := bytes.NewBuffer(bs)
	smscLen := buf.Next(1)[0]
	_ = buf.Next(int(smscLen))

	flags := buf.Next(1)[0]
	oaLen := (buf.Next(1)[0] + 1) / 2
	_ = buf.Next(1) // OA Type
	oa := buf.Next(int(oaLen))

	pid := buf.Next(1)[0]
	dcs := buf.Next(1)[0]
	scts := buf.Next(7)

	_ = buf.Next(1) // udLen
	var udhLen byte
	var udhb []byte
	if flags&TP_UDHI == TP_UDHI {
		udhLen = buf.Next(1)[0]
		udhb = buf.Next(int(udhLen))
	}

	udh, err := decodeUDH(udhb)
	if err != nil {
		return nil, fmt.Errorf("could not decode UDH: %v", err)
	}

	ud, err := decodeUD(dcs, buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("could not decode UD: %v", err)
	}

	return &SMS{
		Tpdu: TPDU{
			Flags: flags,
			OA:    decodeOA(oa),
			PID:   pid,
			DCS:   dcs,
			SCTS:  decodeSCTS(scts),
			UDH:   udh,
			UD:    ud,
		},
	}, nil
}

func decodeOA(bs []byte) string {
	dst := make([]byte, hex.EncodedLen(len(bs)))
	hex.Encode(dst, bs)

	for i := 0; i < len(dst); i += 2 {
		dst[i], dst[i+1] = dst[i+1], dst[i]
	}

	return string(dst)
}

// 81 40 90 80 51 61 23
func decodeSCTS(bs []byte) time.Time {
	dst := make([]byte, hex.EncodedLen(len(bs)))
	hex.Encode(dst, bs)

	for i := 0; i < len(dst); i += 2 {
		dst[i], dst[i+1] = dst[i+1], dst[i]
	}

	buf := bytes.NewBuffer(dst)

	year, _ := strconv.Atoi(string(buf.Next(2)))
	month, _ := strconv.Atoi(string(buf.Next(2)))
	day, _ := strconv.Atoi(string(buf.Next(2)))
	hour, _ := strconv.Atoi(string(buf.Next(2)))
	min, _ := strconv.Atoi(string(buf.Next(2)))
	sec, _ := strconv.Atoi(string(buf.Next(2)))
	tz, _ := strconv.Atoi(string(buf.Next(2)))

	timezone := time.FixedZone("", int((time.Duration(tz) * 15 * time.Minute).Seconds()))

	return time.Date(year+2000, time.Month(month), day, hour, min, sec, 0, timezone)
}

type UDHConcatenated struct {
	Reference byte
	Total     byte
	Index     byte // start from 1
}

func decodeUDH(bs []byte) ([]interface{}, error) {
	buf := bytes.NewBuffer(bs)
	els := []interface{}{}

	for buf.Len() >= 2 {
		ieh := buf.Next(2)

		switch ieh[0] {
		case 0x00:
			el := &UDHConcatenated{}
			if err := binary.Read(buf, binary.LittleEndian, el); err != nil {
				return nil, fmt.Errorf("could not read UDHConcatenated: %v", err)
			}
			els = append(els, el)
		default:
			// ignore other kinds of element
			buf.Next(int(ieh[1]))
		}
	}

	return els, nil
}

func decodeUD(dcs byte, bs []byte) (string, error) {
	switch dcs {
	case 0x00:
		fallthrough
	case 0x01:
		fallthrough
	case 0x02:
		fallthrough
	case 0x03:
		return decode7bit(bs), nil

	case 0x08:
		fallthrough
	case 0x09:
		fallthrough
	case 0x0a:
		fallthrough
	case 0x0b:
		return decodeUTF16(bs), nil

	default:
		return "", fmt.Errorf("unknown data coding scheme")
	}
}

func decode7bit(bs []byte) string {
	bit7s := make([]byte, len(bs)*8/7)

	for i := range bit7s {
		byteStart := (i * 7) / 8
		byteEnd := ((i + 1) * 7) / 8
		bitStart := (i * 7) % 8
		bitEnd := ((i + 1) * 7) % 8

		low := (bs[byteStart] >> uint8(bitStart)) & 0x7f
		high := bs[byteEnd] << uint8(8-bitEnd) >> uint8(8-bitEnd) << uint8(7-bitEnd)

		bit7s[i] = low | high
	}

	return string(bit7s)
}

func decodeUTF16(bs []byte) string {
	us := make([]uint16, (len(bs)+1)/2)

	for i := 0; i < len(us); i += 1 {
		us[i] = binary.BigEndian.Uint16(bs[i*2 : (i+1)*2])
	}

	return string(utf16.Decode(us))
}
