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

package controlapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/agent-substrate/substrate/cmd/ateapi/internal/store"
	"github.com/agent-substrate/substrate/internal/resources"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	// goldenSweepLeaseKey is the lease one ate-api replica holds while it
	// sweeps orphaned goldens, so replicas never sweep concurrently.
	goldenSweepLeaseKey = "lease:actortemplate-golden-sweep"

	// DefaultGoldenSweepGracePeriod is the age a golden actor or tag without
	// a template row reaches before the sweep removes it.
	DefaultGoldenSweepGracePeriod = 10 * time.Minute

	// goldenSweptMessage is the line logged for each deleted template whose
	// leftover goldens the sweep removed.
	goldenSweptMessage = "Removed the golden left by a deleted actor template"
)

// orphanedGolden is what a deleted template left in the reserved atespace:
// its golden actor and the golden tags the golden flow made for it.
type orphanedGolden struct {
	actor *ateapipb.Actor
	tags  []*ateapipb.Tag
}

// sweepOrphanedGoldens removes the golden actors and tags whose template row
// is gone and that are older than the grace period: a template delete removes
// the row first and its goldens after it, so a cleanup that failed after the
// row leaves them with nothing to rediscover them by. The goldens are listed
// before the rows: a golden is only made for a template whose row exists, so
// every golden seen whose template lives is matched by a row the later list
// returns. A golden whose row exists, or younger than the grace period, is
// never touched. One replica sweeps at a time.
func (r *ActorTemplateReconciler) sweepOrphanedGoldens(ctx context.Context) {
	lease, err := r.persistence.AcquireLease(ctx, goldenSweepLeaseKey)
	if err != nil {
		if !errors.Is(err, store.ErrLeaseConflict) {
			slog.ErrorContext(ctx, "Failed to acquire the golden sweep lease", slog.Any("err", err))
		}
		return
	}
	defer lease.Close()
	ctx = lease.Context()

	orphans, err := r.orphanedGoldens(ctx, time.Now().Add(-r.goldenSweepGrace))
	if err != nil {
		slog.ErrorContext(ctx, "Failed to find goldens of deleted actor templates", slog.Any("err", err))
		return
	}
	for _, uid := range slices.Sorted(maps.Keys(orphans)) {
		r.removeOrphanedGolden(ctx, uid, orphans[uid])
	}
}

// orphanedGoldens returns, by template uid, the goldens created before
// cutoff whose template has no row.
func (r *ActorTemplateReconciler) orphanedGoldens(ctx context.Context, cutoff time.Time) (map[string]*orphanedGolden, error) {
	actors, err := listAll(ctx, func(ctx context.Context, opts store.ListOptions) (store.ListResponse[*ateapipb.Actor], error) {
		return r.persistence.ListActors(ctx, resources.GoldenActorAtespace, opts)
	})
	if err != nil {
		return nil, fmt.Errorf("while listing golden actors: %w", err)
	}
	tags, err := listAll(ctx, func(ctx context.Context, opts store.ListOptions) (store.ListResponse[*ateapipb.Tag], error) {
		return r.persistence.ListTags(ctx, resources.GoldenActorAtespace, opts)
	})
	if err != nil {
		return nil, fmt.Errorf("while listing golden tags: %w", err)
	}
	if len(actors) == 0 && len(tags) == 0 {
		return nil, nil
	}
	templates, err := listAll(ctx, func(ctx context.Context, opts store.ListOptions) (store.ListResponse[*ateapipb.ActorTemplate], error) {
		return r.persistence.ListActorTemplates(ctx, "", opts)
	})
	if err != nil {
		return nil, fmt.Errorf("while listing actor templates: %w", err)
	}
	live := make(map[string]bool, len(templates))
	for _, tmpl := range templates {
		live[tmpl.GetMetadata().GetUid()] = true
	}
	orphaned := func(uid string, meta *ateapipb.ResourceMetadata) bool {
		created := meta.GetCreateTime()
		return uid != "" && !live[uid] && created != nil && created.AsTime().Before(cutoff)
	}

	orphans := map[string]*orphanedGolden{}
	entry := func(uid string) *orphanedGolden {
		if orphans[uid] == nil {
			orphans[uid] = &orphanedGolden{}
		}
		return orphans[uid]
	}
	for _, actor := range actors {
		if uid := goldenActorTemplateUID(actor); orphaned(uid, actor.GetMetadata()) {
			entry(uid).actor = actor
		}
	}
	for _, tag := range tags {
		if uid := goldenTagTemplateUID(tag); orphaned(uid, tag.GetMetadata()) {
			entry(uid).tags = append(entry(uid).tags, tag)
		}
	}
	return orphans, nil
}

// removeOrphanedGolden deletes a deleted template's golden actor and tags,
// and logs one line naming the template uid and what was removed. A delete
// that fails is logged and retried by the next sweep.
func (r *ActorTemplateReconciler) removeOrphanedGolden(ctx context.Context, uid string, orphan *orphanedGolden) {
	var removed []string
	var failed error
	if actor := orphan.actor; actor != nil {
		ref := resources.ActorRefFromActor(actor)
		if _, err := r.control.DeleteActor(ctx, &ateapipb.DeleteActorRequest{Actor: ref.ToObjectRef(), AnyState: true}); err != nil && status.Code(err) != codes.NotFound {
			failed = errors.Join(failed, fmt.Errorf("golden actor %s: %w", ref, err))
		} else {
			removed = append(removed, "actor "+ref.String())
		}
	}
	for _, tag := range orphan.tags {
		ref := resources.TagRefFromTag(tag)
		if _, err := r.control.DeleteTag(ctx, &ateapipb.DeleteTagRequest{Tag: ref.ToObjectRef()}); err != nil && status.Code(err) != codes.NotFound {
			failed = errors.Join(failed, fmt.Errorf("golden tag %s: %w", ref, err))
		} else {
			removed = append(removed, "tag "+ref.String())
		}
	}
	if len(removed) > 0 {
		slog.InfoContext(ctx, goldenSweptMessage,
			slog.String("templateUID", uid), slog.String("removed", strings.Join(removed, ", ")))
	}
	if failed != nil {
		slog.ErrorContext(ctx, "Failed to remove the golden left by a deleted actor template; the next sweep retries",
			slog.String("templateUID", uid), slog.Any("err", failed))
	}
}

// goldenActorTemplateUID returns the uid of the template whose golden actor
// actor is, or "" for an actor the golden flow did not make: a golden actor
// lives in the reserved atespace, is created from a template, and is named
// after that template's uid.
func goldenActorTemplateUID(actor *ateapipb.Actor) string {
	meta := actor.GetMetadata()
	if meta.GetAtespace() != resources.GoldenActorAtespace || actor.GetActorTemplate().GetName() == "" || !isTemplateUID(meta.GetName()) {
		return ""
	}
	return meta.GetName()
}

// goldenTagTemplateUID returns the uid of the template whose golden tag tag
// is, or "" for a tag the golden flow did not make: a golden tag lives in the
// reserved atespace, is taken from the template's golden actor, and is named
// after the template's uid, as the actor is.
func goldenTagTemplateUID(tag *ateapipb.Tag) string {
	meta := tag.GetMetadata()
	source := tag.GetSourceActor()
	uid := source.GetName()
	if meta.GetAtespace() != resources.GoldenActorAtespace || source.GetAtespace() != resources.GoldenActorAtespace || !isTemplateUID(uid) {
		return ""
	}
	if meta.GetName() != uid {
		return ""
	}
	return uid
}

// isTemplateUID reports whether name has the shape of the uid the store gives
// a template.
func isTemplateUID(name string) bool {
	_, err := uuid.Parse(name)
	return err == nil && len(name) == len(uuid.Nil.String())
}

// listAll pages through a store listing.
func listAll[T any](ctx context.Context, list func(context.Context, store.ListOptions) (store.ListResponse[T], error)) ([]T, error) {
	var items []T
	opts := store.ListOptions{PageSize: templateListPageSize}
	for {
		page, err := list(ctx, opts)
		if err != nil {
			return nil, err
		}
		items = append(items, page.Items...)
		if page.NextPageToken == "" {
			return items, nil
		}
		opts.PageToken = page.NextPageToken
	}
}
