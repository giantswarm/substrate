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

package egress

import (
	"cmp"
	"context"
	"log/slog"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	envoy_type "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/agent-substrate/substrate/cmd/atenet/internal/router/extproc"
	"github.com/agent-substrate/substrate/internal/egresspolicy"
	"github.com/agent-substrate/substrate/internal/resources"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/agent-substrate/substrate/pkg/proto/credproviderpb"
)

// maxProviderMessageBytes bounds the part of a provider's denial that reaches
// the actor.
const maxProviderMessageBytes = 512

// mapCredentialProviderError converts a FetchSecret failure into the denial the
// credprovider contract specifies: a credential the provider will not release
// denies as 403 with the provider's own message, the only text of the provider
// that reaches the actor; a transient provider failure fails closed as a
// retryable 503; any other failure is the provider's or the gateway's fault, a
// 502. The cause stays reachable through Unwrap for the caller's log.
func mapCredentialProviderError(provider string, err error) error {
	switch status.Code(err) {
	case codes.NotFound, codes.PermissionDenied, codes.FailedPrecondition, codes.Unauthenticated:
		message := providerMessage(status.Convert(err).Message())
		if message == "" {
			return extproc.WrapReqError(envoy_type.StatusCode_Forbidden, err, "credential provider %s denied the request", provider)
		}
		return extproc.WrapReqError(envoy_type.StatusCode_Forbidden, err, "credential provider %s denied: %s", provider, message)
	case codes.Unavailable, codes.DeadlineExceeded, codes.ResourceExhausted:
		return extproc.WrapReqError(envoy_type.StatusCode_ServiceUnavailable, err, "credential provider %s unavailable", provider)
	default:
		return credentialProviderFailed(provider, err)
	}
}

// credentialProviderFailed is the 502 of a provider that answered with
// something the gateway cannot use.
func credentialProviderFailed(provider string, err error) error {
	return extproc.WrapReqError(envoy_type.StatusCode_BadGateway, err, "credential provider %s failed", provider)
}

// providerMessage makes a provider's status message safe for a response body:
// control characters are escaped, invalid UTF-8 is replaced, and the result is
// cut to maxProviderMessageBytes on a rune boundary.
func providerMessage(message string) string {
	var b strings.Builder
	for _, r := range strings.ToValidUTF8(message, string(utf8.RuneError)) {
		var piece string
		if unicode.IsControl(r) {
			piece = strings.Trim(strconv.QuoteRune(r), "'")
		} else {
			piece = string(r)
		}
		if b.Len()+len(piece) > maxProviderMessageBytes {
			break
		}
		b.WriteString(piece)
	}
	return strings.TrimSpace(b.String())
}

// applyEffects resolves a matched rule's credential injections and returns the
// header mutations to add to the request, or an error that denies it. A rule
// with no injections adds nothing.
//
// A credential is only ever injected on the TLS-terminated MITM leg with a
// credential provider configured. When injection cannot be performed — a
// cleartext request, or no provider configured — it is skipped and the request
// is let through without the credential rather than denied.
//
// Once injection is actually attempted (TLS leg, provider present), any failure
// to produce the credential the policy required fails closed.
func (h *Handler) applyEffects(ctx context.Context, ref resources.ActorRef, dest egresspolicy.Destination, leg string, effects *ateapipb.HttpRuleEffects) ([]*corev3.HeaderValueOption, error) {
	injections := effects.GetReplaceHeaders()
	if len(injections) == 0 {
		return nil, nil
	}

	if leg != extproc.EgressTLSMITMFilterChainName {
		slog.WarnContext(ctx, "egress: skipping credential injection on a non-TLS leg; the request proceeds without the credential",
			slog.Any("actor", ref), slog.String("host", dest.Hostname), slog.String("leg", leg))
		return nil, nil
	}
	if h.provider == nil {
		slog.WarnContext(ctx, "egress: skipping credential injection because no credential provider is configured; the request proceeds without the credential",
			slog.Any("actor", ref), slog.String("host", dest.Hostname))
		return nil, nil
	}

	// Atunnel connected to us with an ateom-for-actor SPIFFE ID; translate it
	// to a pure actor SPIFFE ID for plugins to make decisions on.
	actorSpiffeID := resources.ActorSPIFFEID(ref).String()

	setHeaders := make([]*corev3.HeaderValueOption, 0, len(injections))
	for _, inj := range injections {
		if err := validateInjectHeader(inj.GetHeader()); err != nil {
			slog.ErrorContext(ctx, "egress denied: policy names an unusable injection header",
				slog.Any("actor", ref), slog.String("host", dest.Hostname), slog.String("header", inj.GetHeader()), slog.Any("err", err))
			return nil, extproc.WrapReqError(envoy_type.StatusCode_InternalServerError, err, deniedBody)
		}

		// Confirm the credential URI names the provider this gateway serves
		// before dialing: the configured connection fronts one provider, so a URI
		// naming another cannot be resolved here and must fail closed rather than
		// be sent to the wrong provider.
		if h.providerName != "" {
			name, err := providerNameFromURI(inj.GetCredentialUri())
			if err != nil {
				slog.ErrorContext(ctx, "egress denied: policy names an unparseable credential URI",
					slog.Any("actor", ref), slog.String("host", dest.Hostname), slog.String("uri", inj.GetCredentialUri()), slog.Any("err", err))
				return nil, extproc.WrapReqError(envoy_type.StatusCode_InternalServerError, err, deniedBody)
			}
			if name != h.providerName {
				slog.ErrorContext(ctx, "egress denied: credential URI names a provider this gateway does not serve",
					slog.Any("actor", ref), slog.String("host", dest.Hostname), slog.String("uri", inj.GetCredentialUri()),
					slog.String("provider", name), slog.String("serves", h.providerName))
				return nil, extproc.NewReqError(envoy_type.StatusCode_InternalServerError, deniedBody)
			}
		}

		// The provider's name in the actor's denial.
		provider, err := providerNameFromURI(inj.GetCredentialUri())
		if err != nil {
			provider = cmp.Or(h.providerName, "unknown")
		}

		resp, err := h.provider.FetchSecret(ctx, &credproviderpb.FetchSecretRequest{
			Uri:           inj.GetCredentialUri(),
			ActorSpiffeId: actorSpiffeID,
		})
		if err != nil {
			// Fail closed: a credential the policy required but we could not fetch
			// must not let the request out without it.
			slog.ErrorContext(ctx, "egress denied: credential fetch failed",
				slog.Any("actor", ref), slog.String("host", dest.Hostname), slog.String("uri", inj.GetCredentialUri()), slog.Any("err", err))
			return nil, mapCredentialProviderError(provider, err)
		}
		secret, err := sanitizeSecret(resp.GetOpaqueBytes())
		if err != nil {
			slog.ErrorContext(ctx, "egress denied: unusable credential",
				slog.Any("actor", ref), slog.String("host", dest.Hostname), slog.String("uri", inj.GetCredentialUri()), slog.Any("err", err))
			return nil, credentialProviderFailed(provider, err)
		}

		// Overwrite any header the actor set itself, so a client cannot pre-seed a
		// value that survives injection.
		setHeaders = append(setHeaders, &corev3.HeaderValueOption{
			Header:       &corev3.HeaderValue{Key: inj.GetHeader(), RawValue: append([]byte(inj.GetPrefix()), secret...)},
			AppendAction: corev3.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD,
		})
	}
	return setHeaders, nil
}
