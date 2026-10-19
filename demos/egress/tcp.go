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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"syscall"
	"time"
)

// tcpRequest asks for one raw TCP exchange with target: send, then read until
// the server closes.
type tcpRequest struct {
	// Target is the server to dial, as host:port.
	Target string `json:"target"`
	// Send is written as soon as the connection is open: the client speaks
	// first, as SSH does.
	Send string `json:"send"`
}

type tcpResponse struct {
	// Received is everything the server sent before it closed. Empty when the
	// connection was closed without a reply, which is how egress refuses TCP
	// that is neither TLS nor HTTP.
	Received string `json:"received"`
	Error    string `json:"error,omitempty"`
}

// handleTCP runs one raw TCP exchange. Inside an actor every TCP connection
// goes through atunnel, which accepts it locally, so a refusal by the egress
// gateway reads as a connection closed with nothing received, never as a
// failed dial.
func handleTCP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSON(w, http.StatusMethodNotAllowed, tcpResponse{Error: "method must be POST"})
		return
	}
	var input tcpRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBody)).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, tcpResponse{Error: fmt.Sprintf("invalid JSON payload: %v", err)})
		return
	}
	if err := validateTarget(input.Target); err != nil {
		writeJSON(w, http.StatusBadRequest, tcpResponse{Error: err.Error()})
		return
	}
	dialer := net.Dialer{Timeout: requestTimeout}
	conn, err := dialer.DialContext(r.Context(), "tcp", input.Target)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, tcpResponse{Error: fmt.Sprintf("dialing %s: %v", input.Target, err)})
		return
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(requestTimeout)); err != nil {
		writeJSON(w, http.StatusInternalServerError, tcpResponse{Error: fmt.Sprintf("setting deadline: %v", err)})
		return
	}
	if _, err := io.WriteString(conn, input.Send); err != nil {
		writeJSON(w, http.StatusBadGateway, tcpResponse{Error: fmt.Sprintf("sending: %v", err)})
		return
	}
	received, err := io.ReadAll(io.LimitReader(conn, maxResponseBody))
	// A peer that closes with the request unread resets the connection: that
	// is a close too.
	if err != nil && !errors.Is(err, syscall.ECONNRESET) {
		writeJSON(w, http.StatusBadGateway, tcpResponse{Received: string(received), Error: fmt.Sprintf("receiving: %v", err)})
		return
	}
	writeJSON(w, http.StatusOK, tcpResponse{Received: string(received)})
}
