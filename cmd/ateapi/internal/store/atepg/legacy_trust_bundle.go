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

package atepg

import (
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"google.golang.org/protobuf/encoding/protowire"
)

// TrustBundleDataSource.name as the releases before the repeated names
// numbered it: the one trust bundle a system-info projection unified. A
// decoded ActorTemplate row keeps it as an unknown field until
// migrateLegacyActorTemplate has read it.
const legacyTrustBundleDataSourceName protowire.Number = 1

// migrateLegacyActorTemplate rewrites the trust-bundle projections an older
// release wrote into the shape this release reads. Such a release named a
// projection's one bundle in field 1, name, which the repeated names replaced
// without a migration of the stored rows: read as written, the projection
// names no bundle, and every boot from the template fails on the empty list
// that a create of the same spec is refused for. A projection without names
// takes its legacy name as its one name; one that carries names is left as it
// is. The rewritten template is persisted by its next update.
func migrateLegacyActorTemplate(template *ateapipb.ActorTemplate) {
	for _, vol := range template.GetVolumes() {
		for _, dataSource := range vol.GetSystemInfo().GetDataSources() {
			tb := dataSource.GetTrustBundle()
			if tb == nil || len(tb.GetNames()) > 0 {
				continue
			}
			if name := unknownString(tb.ProtoReflect(), legacyTrustBundleDataSourceName); name != "" {
				tb.Names = []string{name}
			}
		}
	}
}
