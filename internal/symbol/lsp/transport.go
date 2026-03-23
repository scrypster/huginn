package lsp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const maxMsgSize = 8 * 1024 * 1024

type Transport struct {
	rw io.ReadWriter
	br *bufio.Reader
}

func NewTransport(rw io.ReadWriter) *Transport {
	return &Transport{rw: rw, br: bufio.NewReader(rw)}
}

func (t *Transport) Send(v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("lsp send: %w", err)
	}
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(body))
	if _, err := io.WriteString(t.rw, header); err != nil {
		return err
	}
	_, err = t.rw.Write(body)
	return err
}

func (t *Transport) Receive(v any) error {
	contentLength := -1
	for {
		line, err := t.br.ReadString('\n')
		if err != nil {
			return fmt.Errorf("lsp receive header: %w", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if strings.HasPrefix(line, "Content-Length: ") {
			n, err := strconv.Atoi(strings.TrimPrefix(line, "Content-Length: "))
			if err != nil {
				return err
			}
			contentLength = n
		}
	}
	if contentLength < 0 {
		return fmt.Errorf("lsp receive: missing Content-Length")
	}
	if contentLength > maxMsgSize {
		return fmt.Errorf("lsp receive: message too large (%d)", contentLength)
	}
	body := make([]byte, contentLength)
	if _, err := io.ReadFull(t.br, body); err != nil {
		return err
	}
	return json.Unmarshal(body, v)
}
