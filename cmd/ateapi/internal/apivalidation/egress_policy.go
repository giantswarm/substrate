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

package apivalidation

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"go.ate.dev/substrate/internal/egresspolicy"
	"go.ate.dev/substrate/pkg/proto/ateapipb"
	"k8s.io/apimachinery/pkg/api/operation"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

func ValidateCreateActorEgressPolicyRequest(ctx context.Context, req *ateapipb.CreateActorEgressPolicyRequest) field.ErrorList {
	op := MakeCreateOp(ctx)
	return Validate_CreateActorEgressPolicyRequest(ctx, op, nil, req, nil)
}

func ValidateGetActorEgressPolicyRequest(ctx context.Context, req *ateapipb.GetActorEgressPolicyRequest) field.ErrorList {
	op := MakeCreateOp(ctx)
	return Validate_GetActorEgressPolicyRequest(ctx, op, nil, req, nil)
}

func ValidateUpdateActorEgressPolicyRequest(ctx context.Context, req *ateapipb.UpdateActorEgressPolicyRequest) field.ErrorList {
	op := MakeCreateOp(ctx)
	return Validate_UpdateActorEgressPolicyRequest(ctx, op, nil, req, nil)
}

func ValidateDeleteActorEgressPolicyRequest(ctx context.Context, req *ateapipb.DeleteActorEgressPolicyRequest) field.ErrorList {
	op := MakeCreateOp(ctx)
	return Validate_DeleteActorEgressPolicyRequest(ctx, op, nil, req, nil)
}

func ValidateEgressPolicyUpdate(ctx context.Context, p *field.Path, newVal, oldVal *ateapipb.EgressPolicy) field.ErrorList {
	op := MakeUpdateOp(ctx)
	return Validate_EgressPolicy(ctx, op, p, newVal, oldVal)
}

func ValidateCustom_CreateActorEgressPolicyRequest(_ context.Context, _ operation.Operation, p *field.Path, req, _ *ateapipb.CreateActorEgressPolicyRequest) field.ErrorList {
	return validateEgressPolicyParentAtespace(req.GetActor(), req.GetEgressPolicy(), p)
}

func ValidateCustom_UpdateActorEgressPolicyRequest(_ context.Context, _ operation.Operation, p *field.Path, req, _ *ateapipb.UpdateActorEgressPolicyRequest) field.ErrorList {
	return validateEgressPolicyParentAtespace(req.GetActor(), req.GetEgressPolicy(), p)
}

func validateEgressPolicyParentAtespace(actor *ateapipb.ObjectRef, policy *ateapipb.EgressPolicy, p *field.Path) field.ErrorList {
	if actor == nil || actor.Atespace == "" {
		return nil // regular DV will handle it
	}
	actorAtespace := actor.GetAtespace()
	if policy == nil || policy.Metadata == nil || policy.Metadata.Atespace == "" {
		return nil // regular DV will handle it
	}
	policyAtespace := policy.GetMetadata().GetAtespace()
	if actorAtespace != policyAtespace {
		return field.ErrorList{
			field.Invalid(p.Child("egress_policy", "metadata", "atespace"), policyAtespace, "must match actor.atespace"),
		}
	}
	return nil
}

func ValidateCustom_EgressPolicy_Metadata(_ context.Context, _ operation.Operation, root *field.Path, meta, _ *ateapipb.ResourceMetadata) field.ErrorList {
	return validateSingletonName(root.Child("name"), meta.GetName())
}

func ValidateCustom_GetActorEgressPolicyRequest_Name(_ context.Context, _ operation.Operation, p *field.Path, name, _ *string) field.ErrorList {
	return validateSingletonName(p, *name)
}

func ValidateCustom_DeleteActorEgressPolicyRequest_Name(_ context.Context, _ operation.Operation, p *field.Path, name, _ *string) field.ErrorList {
	return validateSingletonName(p, *name)
}

// ValidateCustom_EgressPolicy_Rules rejects two rules that tie on a pattern
// and a port, whatever their protocols: the gateway would have no way to pick
// one. Defaults are applied before validation, so an http rule left on 80
// and an https rule left on 443 never tie. A tcp rule decides streams no
// other rule decides, so it ties only with another tcp rule, on a hostname or
// a CIDR.
func ValidateCustom_EgressPolicy_Rules(_ context.Context, _ operation.Operation, p *field.Path, rules, _ []*ateapipb.EgressRule) field.ErrorList {
	type key struct {
		tcp     bool
		pattern string
		port    portKey
	}
	type match struct {
		rule int
		path *field.Path
	}
	var errs field.ErrorList
	seen := map[key]match{}
	for i, rule := range rules {
		member, fields, ports := ruleMatchFields(rule)
		if member == "" {
			continue // handled by the union check
		}
		for _, f := range fields {
			path := p.Index(i).Child(member, f.name).Index(f.index)
			pattern := f.value
			for _, port := range portKeysOf(ports) {
				k := key{member == "tcp", pattern, port}
				prior, ok := seen[k]
				switch {
				case !ok:
					seen[k] = match{rule: i, path: path}
				case prior.rule != i: // a repeat within one rule is reported by the set check
					errs = append(errs, field.Invalid(path, pattern, fmt.Sprintf("ties with %s on %s", prior.path, port)))
				}
			}
		}
	}
	return errs
}

// matchField is one entry a rule matches on: a hostname, or a tcp rule's
// CIDR, with the list it is in and its index there.
type matchField struct {
	name  string
	index int
	value string
}

func matchFieldsOf(name string, values []string) []matchField {
	fields := make([]matchField, 0, len(values))
	for i, value := range values {
		fields = append(fields, matchField{name: name, index: i, value: value})
	}
	return fields
}

// ruleMatchFields is what a rule matches on: the union member, its hostnames
// (and a tcp rule's CIDRs), and its ports. Everything is empty for a rule that
// sets no member.
func ruleMatchFields(rule *ateapipb.EgressRule) (member string, fields []matchField, ports *ateapipb.Ports) {
	switch {
	case rule.GetHttp() != nil:
		return "http", matchFieldsOf("hostnames", rule.GetHttp().GetHostnames()), rule.GetHttp().GetPorts()
	case rule.GetHttps() != nil:
		return "https", matchFieldsOf("hostnames", rule.GetHttps().GetHostnames()), rule.GetHttps().GetPorts()
	case rule.GetTlsPassthrough() != nil:
		return "tls_passthrough", matchFieldsOf("hostnames", rule.GetTlsPassthrough().GetHostnames()), rule.GetTlsPassthrough().GetPorts()
	case rule.GetTcp() != nil:
		tcp := rule.GetTcp()
		return "tcp", append(matchFieldsOf("hostnames", tcp.GetHostnames()), matchFieldsOf("cidrs", tcp.GetCidrs())...), tcp.GetPorts()
	}
	return "", nil, nil
}

// portKey is one thing a Ports matches: a port number, or every port. Its
// String reads as "port 443" or "every port" in messages.
type portKey struct {
	number int32
	all    bool
}

func portKeysOf(ports *ateapipb.Ports) []portKey {
	if ports.GetAll() != nil {
		return []portKey{{all: true}}
	}
	keys := make([]portKey, 0, len(ports.GetNumbers()))
	for _, n := range ports.GetNumbers() {
		keys = append(keys, portKey{number: n})
	}
	return keys
}

func (k portKey) String() string {
	if k.all {
		return "every port"
	}
	return fmt.Sprintf("port %d", k.number)
}

func ValidateCustom_HttpRule_Hostnames(_ context.Context, _ operation.Operation, p *field.Path, patterns, _ []string) field.ErrorList {
	return validateHostnamePatterns(patterns, p)
}

func ValidateCustom_HttpsRule_Hostnames(_ context.Context, _ operation.Operation, p *field.Path, patterns, _ []string) field.ErrorList {
	return validateHostnamePatterns(patterns, p)
}

func ValidateCustom_TlsPassthroughRule_Hostnames(_ context.Context, _ operation.Operation, p *field.Path, patterns, _ []string) field.ErrorList {
	return validateHostnamePatterns(patterns, p)
}

func ValidateCustom_TcpRule(_ context.Context, _ operation.Operation, p *field.Path, rule, _ *ateapipb.TcpRule) field.ErrorList {
	if len(rule.GetHostnames()) == 0 && len(rule.GetCidrs()) == 0 {
		return field.ErrorList{field.Required(p, "at least one hostname or cidr must be specified")}
	}
	return nil
}

func ValidateCustom_TcpRule_Hostnames(_ context.Context, _ operation.Operation, p *field.Path, hostnames, _ []string) field.ErrorList {
	var errs field.ErrorList
	for i, raw := range hostnames {
		switch _, err := egresspolicy.ParseExactHostname(raw); {
		case raw == "":
			errs = append(errs, field.Required(p.Index(i), ""))
		case err != nil:
			errs = append(errs, field.Invalid(p.Index(i), raw, "must be a DNS hostname without a wildcard"))
		}
	}
	return errs
}

func ValidateCustom_TcpRule_Cidrs(_ context.Context, _ operation.Operation, p *field.Path, cidrs, _ []string) field.ErrorList {
	var errs field.ErrorList
	for i, raw := range cidrs {
		switch _, err := egresspolicy.ParseCIDR(raw); {
		case raw == "":
			errs = append(errs, field.Required(p.Index(i), ""))
		case err != nil:
			errs = append(errs, field.Invalid(p.Index(i), raw, err.Error()))
		}
	}
	return errs
}

func ValidateCustom_HttpRuleEffects(_ context.Context, _ operation.Operation, p *field.Path, effects, _ *ateapipb.HttpRuleEffects) field.ErrorList {
	var errs field.ErrorList
	if len(effects.GetReplaceHeaders()) == 0 {
		errs = append(errs, field.Required(p, "at least one effect must be specified"))
	}
	return errs
}

func ValidateCustom_HttpRuleEffects_ReplaceHeaders(_ context.Context, _ operation.Operation, p *field.Path, injections, _ []*ateapipb.CredentialHeader) field.ErrorList {
	var errs field.ErrorList
	seenHeaders := map[string]bool{}
	for i, inj := range injections {
		if inj == nil {
			continue // handled by DV
		}
		norm := strings.ToLower(inj.Header)
		if seenHeaders[norm] {
			errs = append(errs, field.Duplicate(p.Index(i).Child("header"), inj.Header))
		}
		seenHeaders[norm] = true
	}
	return errs
}

// Validation uses the parsers the egress gateway matches with, so what the
// API accepts and what the gateway can evaluate cannot drift apart.
func validateHostnamePatterns(patterns []string, p *field.Path) field.ErrorList {
	var errs field.ErrorList
	for i, raw := range patterns {
		errs = append(errs, validateHostnamePattern(raw, p.Index(i))...)
	}
	return errs
}

func validateHostnamePattern(raw string, p *field.Path) field.ErrorList {
	if raw == "" {
		return field.ErrorList{field.Required(p, "")}
	}
	if _, err := egresspolicy.ParseHostnamePattern(raw); err != nil {
		return field.ErrorList{
			field.Invalid(p, raw, `must be a DNS hostname, optionally with a complete leftmost-label wildcard, or "*"`),
		}
	}
	return nil
}

func ValidateCustom_CredentialHeader_Header(_ context.Context, _ operation.Operation, p *field.Path, header, _ *string) field.ErrorList {
	if !validHeaderName(*header) {
		return field.ErrorList{
			field.Invalid(p, *header, "must be an HTTP header name"),
		}
	}
	return nil
}

func ValidateCustom_CredentialHeader_Prefix(_ context.Context, _ operation.Operation, p *field.Path, prefix, _ *string) field.ErrorList {
	if !validHeaderValue(*prefix) {
		return field.ErrorList{
			field.Invalid(p, *prefix, "must be a valid HTTP field value prefix"),
		}
	}
	return nil
}

func ValidateCustom_CredentialHeader_CredentialUri(_ context.Context, _ operation.Operation, p *field.Path, uri, _ *string) field.ErrorList {
	if !validCredentialURI(*uri) {
		return field.ErrorList{
			field.Invalid(p, *uri, "must be ate-secret://<provider-class>/<provider-name>/<provider-specific-tail>"),
		}
	}
	return nil
}

func validCredentialURI(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "ate-secret" || u.Host == "" || u.Host != u.Hostname() || u.User != nil || u.RawQuery != "" || u.Fragment != "" || len(validation.IsDNS1123Subdomain(u.Host)) != 0 {
		return false
	}
	escapedPath := u.EscapedPath()
	// Reject percent-encoding in the path of secret uri.
	if escapedPath != u.Path {
		return false
	}
	if !strings.HasPrefix(escapedPath, "/") || strings.HasSuffix(escapedPath, "/") {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(escapedPath, "/"), "/")
	if len(parts) < 2 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
	}
	return true
}

func validHeaderName(value string) bool {
	if value == "" {
		return false
	}
	for _, c := range []byte(value) {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || strings.ContainsRune("!#$%&'*+-.^_`|~", rune(c))) {
			return false
		}
	}
	return true
}

func validHeaderValue(value string) bool {
	for _, c := range []byte(value) {
		if c != '\t' && (c < ' ' || c == 0x7f) {
			return false
		}
	}
	return true
}
