//  Copyright 2026 Google LLC
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package extproc

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"testing"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	extprocv3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	envoy_type "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestNewReqError(t *testing.T) {
	t.Parallel()

	err := NewReqError(envoy_type.StatusCode_BadRequest, "actor %q is %s", "abc", "bad")
	if err == nil {
		t.Fatal("NewReqError returned nil")
	}
	var reqErr *ReqError
	if !errors.As(err, &reqErr) {
		t.Fatalf("errors.As(*ReqError) = false, want true; err type = %T", err)
	}
	if reqErr.StatusCode != int(envoy_type.StatusCode_BadRequest) {
		t.Errorf("StatusCode = %d, want %d", reqErr.StatusCode, envoy_type.StatusCode_BadRequest)
	}
	if got, want := err.Error(), `actor "abc" is bad`; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

// TestImmediateResponseHeaderEncoding pins the RawValue encoding: Envoy drops
// plain Value in ext_proc header mutations, so a Value-encoded header reaches
// the client with an empty value (found live — content-type on every immediate
// response had been arriving empty).
func TestImmediateResponseHeaderEncoding(t *testing.T) {
	t.Parallel()

	resp := ImmediateResponse(envoy_type.StatusCode_ServiceUnavailable, "body")
	set := resp.GetImmediateResponse().GetHeaders().GetSetHeaders()
	if len(set) != 1 {
		t.Fatalf("SetHeaders count = %d, want 1", len(set))
	}
	h := set[0].GetHeader()
	if h.GetKey() != "content-type" || string(h.GetRawValue()) != "text/plain" {
		t.Errorf("header = %q:%q (RawValue), want content-type:text/plain", h.GetKey(), h.GetRawValue())
	}
	if h.GetValue() != "" {
		t.Errorf("header uses Value (%q); must use RawValue only", h.GetValue())
	}
}

// errHandler denies every request with err.
type errHandler struct{ err error }

func (errHandler) Direction() Direction { return DirectionIngress }

func (h errHandler) HandleRequestHeaders(context.Context, *RequestMetadata) (Result, error) {
	return Result{}, h.err
}

// requestWithContentType is an ingress request carrying the content-type a
// caller sent.
func requestWithContentType(contentType string) *extprocv3.ProcessingRequest {
	req := connectRequest("envoy.filters.http.ext_proc", ingressHTTPListener)
	if contentType != "" {
		headers := req.GetRequestHeaders().GetHeaders()
		headers.Headers = append(headers.Headers, &corev3.HeaderValue{Key: "content-type", RawValue: []byte(contentType)})
	}
	return req
}

// setHeaders flattens an immediate response's header mutation.
func setHeaders(ir *extprocv3.ImmediateResponse) map[string]string {
	got := map[string]string{}
	for _, h := range ir.GetHeaders().GetSetHeaders() {
		got[h.GetHeader().GetKey()] = string(h.GetHeader().GetRawValue())
	}
	return got
}

// A denial reaches the caller in the protocol it called with. A gRPC client
// reads a call's outcome from grpc-status and grpc-message; from a plain-text
// 503 the dataplane could only derive Unavailable, never the Aborted code that
// tells a held Actor from an unavailable control plane.
func TestProcessRequestHeadersAnswersDenialInTheCallersProtocol(t *testing.T) {
	t.Parallel()

	const heldMsg = "actor ns/agent unavailable: another operation is in progress for this actor (held by suspend for 1.2s)"
	// Shaped as the ingress handler's mapResumeError answers an Actor whose
	// lease another operation holds: the Aborted status, wrapped once more by
	// the resume budget, behind a client-safe 503.
	held := &ReqError{
		Msg:        heldMsg,
		Cause:      fmt.Errorf("resume budget spent: %w", status.Error(codes.Aborted, "another operation is in progress for this actor")),
		StatusCode: int(envoy_type.StatusCode_ServiceUnavailable),
	}
	notFound := NewReqError(envoy_type.StatusCode_NotFound, "actor %s not found", "ns/missing")

	tests := []struct {
		name        string
		contentType string
		err         error
		wantGRPC    codes.Code // codes.OK means "expect the HTTP answer"
		wantHTTP    envoy_type.StatusCode
		wantMessage string
	}{
		{name: "gRPC call to a held Actor", contentType: "application/grpc", err: held, wantGRPC: codes.Aborted, wantMessage: heldMsg},
		{name: "gRPC call with a format suffix", contentType: "application/grpc+proto", err: held, wantGRPC: codes.Aborted, wantMessage: heldMsg},
		{name: "gRPC call to a missing Actor", contentType: "application/grpc; charset=utf-8", err: notFound, wantGRPC: codes.NotFound, wantMessage: "actor ns/missing not found"},
		{name: "gRPC call failing with a plain error", contentType: "application/grpc", err: errors.New("boom"), wantGRPC: codes.Internal, wantMessage: "boom"},
		{name: "HTTP request to a held Actor", contentType: "application/json", err: held, wantHTTP: envoy_type.StatusCode_ServiceUnavailable, wantMessage: heldMsg},
		{name: "HTTP request without a content-type", err: notFound, wantHTTP: envoy_type.StatusCode_NotFound, wantMessage: "actor ns/missing not found"},
		{name: "gRPC-Web is not gRPC", contentType: "application/grpc-web", err: held, wantHTTP: envoy_type.StatusCode_ServiceUnavailable, wantMessage: heldMsg},
		{name: "HTTP request failing with a plain error", err: errors.New("boom"), wantHTTP: envoy_type.StatusCode_InternalServerError, wantMessage: "boom"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			s := NewServer(50051, nil, Handlers{DirectionIngress: errHandler{err: tc.err}})
			req := requestWithContentType(tc.contentType)
			ir := s.processRequestHeaders(context.Background(), req, req.GetRequestHeaders()).GetImmediateResponse()
			if ir == nil {
				t.Fatal("expected an immediate response")
			}
			headers := setHeaders(ir)

			if tc.wantGRPC != codes.OK {
				if got := ir.GetStatus().GetCode(); got != envoy_type.StatusCode_OK {
					t.Errorf("HTTP status = %v, want OK: a gRPC status rides on a 200", got)
				}
				if got := codes.Code(ir.GetGrpcStatus().GetStatus()); ir.GetGrpcStatus() == nil || got != tc.wantGRPC {
					t.Errorf("grpc_status = %v, want %v", ir.GetGrpcStatus(), tc.wantGRPC)
				}
				want := map[string]string{
					"content-type": "application/grpc",
					"grpc-status":  strconv.Itoa(int(tc.wantGRPC)),
					"grpc-message": encodeGRPCMessage(tc.wantMessage),
				}
				if diff := cmp.Diff(want, headers); diff != "" {
					t.Errorf("headers (-want +got):\n%s", diff)
				}
				if len(ir.GetBody()) != 0 {
					t.Errorf("body = %q, want none: a trailers-only response carries no message", ir.GetBody())
				}
				return
			}

			if got := ir.GetStatus().GetCode(); got != tc.wantHTTP {
				t.Errorf("HTTP status = %v, want %v", got, tc.wantHTTP)
			}
			if ir.GetGrpcStatus() != nil {
				t.Errorf("grpc_status = %v, want none for a non-gRPC request", ir.GetGrpcStatus())
			}
			if diff := cmp.Diff(map[string]string{"content-type": "text/plain"}, headers); diff != "" {
				t.Errorf("headers (-want +got):\n%s", diff)
			}
			if got := string(ir.GetBody()); got != tc.wantMessage {
				t.Errorf("body = %q, want %q", got, tc.wantMessage)
			}
		})
	}
}

// The code a gRPC caller gets for a router denial: the gRPC status of the
// cause when it has one, else the nearest code to the HTTP status.
func TestReqErrorGRPCCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  *ReqError
		want codes.Code
	}{
		{"cause's status", &ReqError{Cause: status.Error(codes.ResourceExhausted, "no free workers available"), StatusCode: 503}, codes.ResourceExhausted},
		{"wrapped cause's status", &ReqError{Cause: fmt.Errorf("x: %w", status.Error(codes.Aborted, "held")), StatusCode: 503}, codes.Aborted},
		{"cause without a status", &ReqError{Cause: context.Canceled, StatusCode: 408}, codes.DeadlineExceeded},
		{"no cause, 403", &ReqError{StatusCode: 403}, codes.PermissionDenied},
		{"no cause, 503", &ReqError{StatusCode: 503}, codes.Unavailable},
		{"no cause, 504", &ReqError{StatusCode: 504}, codes.DeadlineExceeded},
		{"no cause, unmapped", &ReqError{StatusCode: 418}, codes.Unknown},
	}
	for _, tc := range tests {
		if got := tc.err.grpcCode(); got != tc.want {
			t.Errorf("%s: grpcCode() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestEncodeGRPCMessage(t *testing.T) {
	t.Parallel()

	for in, want := range map[string]string{
		"actor ns/a unavailable: held": "actor ns/a unavailable: held",
		"100% held":                    "100%25 held",
		"line\nbreak":                  "line%0Abreak",
		"grüß":                         "gr%C3%BC%C3%9F",
	} {
		if got := encodeGRPCMessage(in); got != want {
			t.Errorf("encodeGRPCMessage(%q) = %q, want %q", in, got, want)
		}
	}
}
