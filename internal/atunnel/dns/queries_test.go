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
	"net"
	"testing"
	"time"

	"go.ate.dev/substrate/internal/atunnel/dns/protocol"
)

func TestRelayCountsUDPQueriesWhateverBecomesOfThem(t *testing.T) {
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
	// A datagram that is no DNS query is dropped, and still counted.
	if _, err := client.Write([]byte("not dns")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the dropped datagram to be counted", func() bool { return srv.Queries() == 4 })

	// A query the actor's policy refuses is still a query the sandbox sent.
	refusing, udpAddr, _ := serveLoopbackWithPolicy(t, relay, nil)
	client, err = net.Dial("udp", udpAddr.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	if _, err := client.Write(dnsQuery(9)); err != nil {
		t.Fatal(err)
	}
	readWithin(t, client)
	if n := refusing.Queries(); n != 1 {
		t.Errorf("counted %d refused queries, want 1", n)
	}
}

func TestRelayCountsEveryMessageOfATCPStream(t *testing.T) {
	upstream, _ := newTCPResolver(t, func(query []byte) []byte { return dnsAnswer(query, 0) })
	relay, err := NewRelayForUpstreams([]string{upstream})
	if err != nil {
		t.Fatal(err)
	}
	srv, _, tcpAddr := serveLoopback(t, relay)
	conn, err := net.Dial("tcp", tcpAddr.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}

	for id := range uint16(3) {
		if err := protocol.WriteTCPFrame(conn, dnsQuery(id)); err != nil {
			t.Fatal(err)
		}
	}
	for range 3 {
		if _, err := protocol.ReadTCPFrame(conn); err != nil {
			t.Fatalf("reading an answer: %v", err)
		}
	}
	if n := srv.Queries(); n != 3 {
		t.Errorf("counted %d queries, want 3", n)
	}
}
