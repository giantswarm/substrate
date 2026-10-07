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

package ateapipb

// EgressPolicyContractVersion is the egress policy contract this package's
// EgressPolicy and EgressRule encode, named after the upstream release whose
// shape they carry. A change of either message's field numbers or meaning
// changes it. ate-api reports it from GetEgressPolicyContract; kubectl-ate
// refuses to write an egress policy to a server that reports another one.
const EgressPolicyContractVersion = "v0.3.0-alpha3"
