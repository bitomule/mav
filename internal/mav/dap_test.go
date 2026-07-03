package mav

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
)

func TestDAPRequestFramesAndMatchesResponse(t *testing.T) {
	clientToServerReader, clientToServerWriter := io.Pipe()
	serverToClientReader, serverToClientWriter := io.Pipe()
	client := &dapClient{stdin: clientToServerWriter, reader: bufio.NewReader(serverToClientReader)}
	done := make(chan error, 1)
	go func() {
		reader := bufio.NewReader(clientToServerReader)
		line, err := reader.ReadString('\n')
		if err != nil {
			done <- err
			return
		}
		var length int
		if _, err := fmt.Sscanf(strings.TrimSpace(line), "Content-Length: %d", &length); err != nil {
			done <- err
			return
		}
		_, _ = reader.ReadString('\n')
		payload := make([]byte, length)
		if _, err := io.ReadFull(reader, payload); err != nil {
			done <- err
			return
		}
		var request struct {
			Seq     int    `json:"seq"`
			Command string `json:"command"`
		}
		if err := json.Unmarshal(payload, &request); err != nil {
			done <- err
			return
		}
		if request.Command != "threads" {
			done <- fmt.Errorf("command=%s", request.Command)
			return
		}
		response, _ := json.Marshal(map[string]any{
			"seq": 2, "type": "response", "request_seq": request.Seq,
			"success": true, "command": request.Command,
			"body": map[string]any{"threads": []any{}},
		})
		_, err = fmt.Fprintf(serverToClientWriter, "Content-Length: %d\r\n\r\n%s", len(response), response)
		done <- err
	}()
	body, err := client.request("threads", map[string]any{})
	if err != nil || !strings.Contains(string(body), "threads") {
		t.Fatalf("body=%s err=%v", body, err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
