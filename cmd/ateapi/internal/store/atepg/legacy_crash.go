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

import "github.com/agent-substrate/substrate/pkg/proto/ateapipb"

// migrateStaleCrash clears the crash record of an actor that is not CRASHED.
// A release before the suspend commit cleared it committed SUSPENDED over an
// actor crashed after its checkpoint completed and kept the crash, so such a
// row reads as a healthy actor with a crash it no longer has. The rewritten
// record is persisted by the actor's next update.
func migrateStaleCrash(st *ateapipb.ActorStatus) {
	if st.GetState() != ateapipb.ActorState_ACTOR_STATE_CRASHED {
		st.Crash = nil
	}
}
