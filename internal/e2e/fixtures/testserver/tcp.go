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
	"fmt"
	"log"
	"net"
	"time"

	"github.com/spf13/cobra"
)

// tcpEchoTimeout bounds how long a connection may take to send its line.
const tcpEchoTimeout = 30 * time.Second

// newTCPEchoCmd is an origin that speaks neither TLS nor HTTP, the shape of
// SSH or a database whose client speaks first: it reads one line and answers
// "echo: " plus that line, then closes. Egress reaches it only through a tcp
// rule.
func newTCPEchoCmd() *cobra.Command {
	var listenAddress string
	cmd := &cobra.Command{
		Use:   "tcp-echo",
		Short: "Serve a raw TCP origin that echoes the first line it receives.",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			listener, err := net.Listen("tcp", listenAddress)
			if err != nil {
				return fmt.Errorf("listening on %s: %w", listenAddress, err)
			}
			log.Printf("testserver tcp-echo: listening on %s", listener.Addr())
			return serveTCPEcho(listener)
		},
	}
	cmd.Flags().StringVar(&listenAddress, "listen", ":8080", "Address the TCP origin listens on.")
	return cmd
}

func serveTCPEcho(listener net.Listener) error {
	for {
		conn, err := listener.Accept()
		if err != nil {
			return err
		}
		go echoLine(conn)
	}
}

func echoLine(conn net.Conn) {
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(tcpEchoTimeout)); err != nil {
		return
	}
	// A kubelet tcpSocket probe connects and closes without a line.
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		return
	}
	fmt.Fprintf(conn, "echo: %s", line)
}
