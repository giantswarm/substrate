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
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
)

// Response codes that say the resolver failed rather than answered. NXDOMAIN
// and NOERROR are answers and are passed back as they are.
const (
	rcodeServFail = 2
	rcodeNotImp   = 4
	rcodeRefused  = 5
)

// failoverRcode reports the response code when the relay should try the next
// upstream. Reads the 12-byte header only; anything shorter is passed through.
func failoverRcode(msg []byte) (byte, bool) {
	if len(msg) < 12 {
		return 0, false
	}
	rcode := msg[3] & 0x0f
	switch rcode {
	case rcodeServFail, rcodeNotImp, rcodeRefused:
		return rcode, true
	}
	return 0, false
}

func rcodeError(rcode byte) error {
	switch rcode {
	case rcodeServFail:
		return errors.New("answered SERVFAIL")
	case rcodeNotImp:
		return errors.New("answered NOTIMP")
	case rcodeRefused:
		return errors.New("answered REFUSED")
	}
	return fmt.Errorf("answered rcode %d", rcode)
}

// questionAttr logs a query's first question as "name TYPE", read from the
// message without decompressing: a query's question carries no pointers.
func questionAttr(msg []byte) slog.Attr {
	name, qtype, ok := question(msg)
	if !ok {
		return slog.String("query", "unparsable")
	}
	return slog.String("query", name+" "+qtype)
}

func question(msg []byte) (name, qtype string, ok bool) {
	if len(msg) < 12 || msg[4] == 0 && msg[5] == 0 {
		return "", "", false
	}
	var labels []string
	i := 12
	for i < len(msg) && msg[i] != 0 {
		n := int(msg[i])
		if n > 63 || i+1+n > len(msg) {
			return "", "", false
		}
		labels = append(labels, string(msg[i+1:i+1+n]))
		i += 1 + n
	}
	if i+3 > len(msg) {
		return "", "", false
	}
	return strings.Join(labels, ".") + ".", typeName(uint16(msg[i+1])<<8 | uint16(msg[i+2])), true
}

func typeName(t uint16) string {
	switch t {
	case 1:
		return "A"
	case 2:
		return "NS"
	case 5:
		return "CNAME"
	case 6:
		return "SOA"
	case 12:
		return "PTR"
	case 15:
		return "MX"
	case 16:
		return "TXT"
	case 28:
		return "AAAA"
	case 33:
		return "SRV"
	case 65:
		return "HTTPS"
	}
	return "TYPE" + strconv.Itoa(int(t))
}
