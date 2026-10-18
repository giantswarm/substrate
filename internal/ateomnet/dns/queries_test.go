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

package dns

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"testing/iotest"
	"time"

	"github.com/google/go-cmp/cmp"
)

// tcpFrame prefixes msg with its two-byte length, as a DNS stream does.
func tcpFrame(msg []byte) []byte {
	return binary.BigEndian.AppendUint16(nil, uint16(len(msg)))
}

func tcpStream(msgs ...[]byte) []byte {
	var stream []byte
	for _, m := range msgs {
		stream = append(stream, tcpFrame(m)...)
		stream = append(stream, m...)
	}
	return stream
}

func TestTCPQueryCounterCountsFramedMessages(t *testing.T) {
	stream := tcpStream(dnsQuery(1), []byte{}, bytes.Repeat([]byte{0x42}, 300), dnsQuery(2))
	for _, tc := range []struct {
		name string
		r    func(io.Reader) io.Reader
	}{
		{"whole reads", func(r io.Reader) io.Reader { return r }},
		// One byte at a time splits every length prefix across two reads.
		{"one byte per read", iotest.OneByteReader},
		{"half reads", iotest.HalfReader},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var queries atomic.Uint64
			got, err := io.ReadAll(&tcpQueryCounter{r: tc.r(bytes.NewReader(stream)), queries: &queries})
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(stream, got); diff != "" {
				t.Errorf("the counter changed the stream (-want +got):\n%s", diff)
			}
			if n := queries.Load(); n != 4 {
				t.Errorf("counted %d queries, want 4", n)
			}
		})
	}
}

func TestTCPQueryCounterCountsAMessageOnceItsLengthIsRead(t *testing.T) {
	var queries atomic.Uint64
	// A stream cut inside the second message's length prefix.
	stream := append(tcpStream(dnsQuery(1)), 0x00)
	if _, err := io.ReadAll(&tcpQueryCounter{r: bytes.NewReader(stream), queries: &queries}); err != nil {
		t.Fatal(err)
	}
	if n := queries.Load(); n != 1 {
		t.Errorf("counted %d queries, want 1", n)
	}
}

func TestRelayCountsUDPQueriesAnsweredOrNot(t *testing.T) {
	answering := newFakeResolver(t, func(query []byte) []byte { return dnsAnswer(query, 0) })
	relay, err := NewRelayForUpstreams([]string{answering})
	if err != nil {
		t.Fatal(err)
	}
	srv, udpAddr, _ := serveLoopback(t, relay)
	client, err := net.Dial("udp", udpAddr.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })

	for id := range uint16(3) {
		if _, err := client.Write(dnsQuery(id)); err != nil {
			t.Fatal(err)
		}
		readWithin(t, client)
	}
	if n := srv.Queries(); n != 3 {
		t.Errorf("counted %d queries, want 3", n)
	}

	// A query no resolver answers is still a query the sandbox sent.
	silent, asked := newSilentResolver(t)
	relay, err = NewRelayForUpstreams([]string{silent})
	if err != nil {
		t.Fatal(err)
	}
	srv, udpAddr, _ = serveLoopback(t, relay)
	client, err = net.Dial("udp", udpAddr.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	if _, err := client.Write(dnsQuery(9)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-asked:
	case <-time.After(5 * time.Second):
		t.Fatal("the query never reached the resolver")
	}
	if n := srv.Queries(); n != 1 {
		t.Errorf("counted %d unanswered queries, want 1", n)
	}
}

// newTCPEchoResolver echoes every DNS stream back to its client.
func newTCPEchoResolver(t *testing.T) string {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lis.Close() })
	go func() {
		for {
			conn, err := lis.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				_, _ = io.Copy(conn, conn)
			}()
		}
	}()
	return lis.Addr().String()
}

func TestRelayCountsEveryMessageOfATCPStream(t *testing.T) {
	relay, err := NewRelayForUpstreams([]string{newTCPEchoResolver(t)})
	if err != nil {
		t.Fatal(err)
	}
	srv, _, tcpAddr := serveLoopback(t, relay)
	conn, err := net.Dial("tcp", tcpAddr.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })

	stream := tcpStream(dnsQuery(1), dnsQuery(2), dnsQuery(3))
	// Written in two pieces that split the second message's length prefix.
	cut := len(tcpStream(dnsQuery(1))) + 1
	for _, piece := range [][]byte{stream[:cut], stream[cut:]} {
		if _, err := conn.Write(piece); err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(conn, make([]byte, len(stream))); err != nil {
		t.Fatalf("reading the echoed stream: %v", err)
	}
	if n := srv.Queries(); n != 3 {
		t.Errorf("counted %d queries, want 3", n)
	}
}
