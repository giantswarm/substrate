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

package extproc

import (
	"errors"
	"fmt"
	"strconv"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	extprocv3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	envoy_type "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ReqError is a handler's denial of a request: an HTTP-mappable status code
// and a client-safe message the mux turns into an immediate response. The
// underlying cause (if any) is preserved via Unwrap so logs can inspect the
// full chain without leaking server-side detail into the response body.
type ReqError struct {
	Msg        string
	Cause      error
	StatusCode int
}

func (e *ReqError) Error() string { return e.Msg }
func (e *ReqError) Unwrap() error { return e.Cause }

// NewReqError builds a ReqError whose body is the formatted message and no
// wrapped cause. Use WrapReqError when a cause is available.
func NewReqError(code envoy_type.StatusCode, format string, args ...any) error {
	return &ReqError{
		Msg:        fmt.Sprintf(format, args...),
		StatusCode: int(code),
	}
}

// WrapReqError builds a ReqError that keeps cause reachable through Unwrap
// while answering the client with only the formatted message.
func WrapReqError(code envoy_type.StatusCode, cause error, format string, args ...any) error {
	return &ReqError{
		Msg:        fmt.Sprintf(format, args...),
		Cause:      cause,
		StatusCode: int(code),
	}
}

// grpcCode is the gRPC status code a gRPC caller is answered with for this
// denial: the code of the gRPC status the cause carries (Aborted for an Actor
// whose lease another operation holds), else the code nearest the HTTP status.
// It is deliberately not a GRPCStatus method: status.Code would then classify
// every ReqError by it, and the route metrics classify by the HTTP status.
func (e *ReqError) grpcCode() codes.Code {
	if e.Cause != nil {
		if s, ok := status.FromError(e.Cause); ok && s.Code() != codes.OK && s.Code() != codes.Unknown {
			return s.Code()
		}
	}
	switch envoy_type.StatusCode(e.StatusCode) {
	case envoy_type.StatusCode_BadRequest:
		return codes.InvalidArgument
	case envoy_type.StatusCode_Unauthorized:
		return codes.Unauthenticated
	case envoy_type.StatusCode_Forbidden:
		return codes.PermissionDenied
	case envoy_type.StatusCode_NotFound:
		return codes.NotFound
	case envoy_type.StatusCode_RequestTimeout, envoy_type.StatusCode_GatewayTimeout:
		return codes.DeadlineExceeded
	case envoy_type.StatusCode_Conflict:
		return codes.Aborted
	case envoy_type.StatusCode_Gone:
		return codes.DataLoss
	case envoy_type.StatusCode_TooManyRequests:
		return codes.ResourceExhausted
	case envoy_type.StatusCode_InternalServerError:
		return codes.Internal
	case envoy_type.StatusCode_NotImplemented:
		return codes.Unimplemented
	case envoy_type.StatusCode_BadGateway, envoy_type.StatusCode_ServiceUnavailable:
		return codes.Unavailable
	default:
		return codes.Unknown
	}
}

// denialResponse answers a denied request in its own protocol: a gRPC call
// gets a gRPC status, any other request the HTTP status and a text body.
// An error that is no ReqError is a 500 whose message is its own.
func denialResponse(md *RequestMetadata, err error) *extprocv3.ProcessingResponse {
	var reqErr *ReqError
	if !errors.As(err, &reqErr) {
		reqErr = &ReqError{Msg: err.Error(), Cause: err, StatusCode: int(envoy_type.StatusCode_InternalServerError)}
	}
	if md.IsGRPC() {
		return GRPCImmediateResponse(reqErr.grpcCode(), reqErr.Msg)
	}
	return ImmediateResponse(envoy_type.StatusCode(reqErr.StatusCode), reqErr.Msg)
}

// ImmediateResponse tells the dataplane to answer the request itself, without
// going upstream.
func ImmediateResponse(statusCode envoy_type.StatusCode, message string) *extprocv3.ProcessingResponse {
	return &extprocv3.ProcessingResponse{
		Response: &extprocv3.ProcessingResponse_ImmediateResponse{
			ImmediateResponse: &extprocv3.ImmediateResponse{
				Status: &envoy_type.HttpStatus{
					Code: statusCode,
				},
				Body: []byte(message),
				Headers: &extprocv3.HeaderMutation{
					SetHeaders: []*corev3.HeaderValueOption{
						{
							// Using RawValues instead of Value: newer versions of Envoy
							// drop Value and use RawValue
							Header: &corev3.HeaderValue{
								Key:      "content-type",
								RawValue: []byte("text/plain"),
							},
						},
					},
				},
			},
		},
	}
}

// GRPCImmediateResponse tells the dataplane to answer a gRPC call itself with
// a trailers-only response: HTTP 200, no body, and the status in grpc-status
// and grpc-message beside content-type application/grpc, which is what a gRPC
// client reads a call's outcome from. grpc_status carries the code as well, so
// a dataplane that builds the gRPC reply itself (Envoy's local reply) uses it
// rather than mapping the HTTP status.
func GRPCImmediateResponse(code codes.Code, message string) *extprocv3.ProcessingResponse {
	header := func(key, value string) *corev3.HeaderValueOption {
		// RawValue for the same reason as in ImmediateResponse.
		return &corev3.HeaderValueOption{Header: &corev3.HeaderValue{Key: key, RawValue: []byte(value)}}
	}
	return &extprocv3.ProcessingResponse{
		Response: &extprocv3.ProcessingResponse_ImmediateResponse{
			ImmediateResponse: &extprocv3.ImmediateResponse{
				Status:     &envoy_type.HttpStatus{Code: envoy_type.StatusCode_OK},
				GrpcStatus: &extprocv3.GrpcStatus{Status: uint32(code)},
				Headers: &extprocv3.HeaderMutation{
					SetHeaders: []*corev3.HeaderValueOption{
						header("content-type", "application/grpc"),
						header("grpc-status", strconv.Itoa(int(code))),
						header("grpc-message", encodeGRPCMessage(message)),
					},
				},
			},
		},
	}
}

// encodeGRPCMessage percent-encodes a grpc-message value as the gRPC HTTP/2
// protocol requires: every byte outside printable ASCII, and '%' itself.
func encodeGRPCMessage(msg string) string {
	const hex = "0123456789ABCDEF"
	out := make([]byte, 0, len(msg))
	for i := 0; i < len(msg); i++ {
		c := msg[i]
		if c >= ' ' && c <= '~' && c != '%' {
			out = append(out, c)
			continue
		}
		out = append(out, '%', hex[c>>4], hex[c&0xf])
	}
	return string(out)
}
