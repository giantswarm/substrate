// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func postTCP(t *testing.T, body string) (*httptest.ResponseRecorder, tcpResponse) {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/tcp", strings.NewReader(body))
	newHandler(http.DefaultClient).ServeHTTP(recorder, request)
	var decoded tcpResponse
	if err := json.NewDecoder(recorder.Body).Decode(&decoded); err != nil {
		t.Fatalf("decoding response (HTTP %d): %v", recorder.Code, err)
	}
	return recorder, decoded
}

// startTCPServer serves handle on loopback and returns its host:port.
func startTCPServer(t *testing.T, handle func(net.Conn)) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listening on loopback: %v", err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				handle(conn)
			}()
		}
	}()
	return listener.Addr().String()
}

func TestTCPReturnsWhatTheServerSentBeforeClosing(t *testing.T) {
	target := startTCPServer(t, func(conn net.Conn) {
		line, err := bufio.NewReader(conn).ReadString('\n')
		if err == nil {
			fmt.Fprintf(conn, "echo: %s", line)
		}
	})
	recorder, response := postTCP(t, fmt.Sprintf(`{"target":%q,"send":"SSH-2.0-actor\r\n"}`, target))
	if recorder.Code != http.StatusOK || response.Received != "echo: SSH-2.0-actor\r\n" {
		t.Fatalf("HTTP %d, %+v; want 200 with the echo", recorder.Code, response)
	}
}

func TestTCPReportsAConnectionClosedWithoutAReply(t *testing.T) {
	target := startTCPServer(t, func(net.Conn) {})
	recorder, response := postTCP(t, fmt.Sprintf(`{"target":%q,"send":"hello\n"}`, target))
	if recorder.Code != http.StatusOK || response.Received != "" || response.Error != "" {
		t.Fatalf("HTTP %d, %+v; want 200 with nothing received", recorder.Code, response)
	}
}

func TestTCPRejectsAnInvalidTarget(t *testing.T) {
	recorder, response := postTCP(t, `{"target":"example.com","send":"x"}`)
	if recorder.Code != http.StatusBadRequest || response.Error == "" {
		t.Fatalf("HTTP %d, %+v; want 400 with an error", recorder.Code, response)
	}
}
