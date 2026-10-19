---
id: actor-dns-queries-counted-by-template
fork: substrate
theme: new-rpcs
category: feature
status: carried
upstream:
  outcome: to-file
  refs: []
  exit: upstream PR merged or declined
depends_on: [podmonitors-six-workloads-that-serve-metrics]
commit: giantswarm/substrate#475
verified: 2026-10-19
---
# The sandbox's DNS relay counts each actor's queries, and atelet exports them by template

- **Status:** carried
- **Upstream:** to file — prepared in the fork: branch `upstream/actor-dns-query-counter`
- **Our commit:** [#475](https://github.com/giantswarm/substrate/pull/475): the upstream-ready commit, rebase-merged

## Change

Each sandbox's DNS relay (`internal/ateomnet/dns`) counts every query it receives, answered, dropped or failed: one per UDP datagram and one per length-prefixed message of a TCP stream. Both ateoms put the activation's count on `WorkloadStatsSample.dns_queries`, and atelet's stats poller adds each actor's increase (a new epoch counts in full) onto the counter `ate.actor.stats.dns.queries` (`{query}`; Prometheus `ate_actor_stats_dns_queries_total` on atelet's `metrics` port, which the chart's atelet PodMonitor scrapes), labeled like `ate.actor.stats.cpu.time`: template, sandbox class, stats source and worker pool. The per-actor usage record atelet writes to stdout carries `dns_queries`. Registry entry in `docs/metrics/registry/metrics.yaml`; unit tests for the frame counter, the relay's UDP and TCP counts, the poller's deltas and the exported counter.

## Reason

DNS leaves the sandbox through the gateway's relay without passing the egress policy, so an actor can carry data out in the names it looks up. The only DNS signal was CoreDNS's, which sees the worker pod, not the actor. A count per template, set against `ate.actor.stats.sampled_actors`, gives an alert on DNS volume per actor; the actor stays off the labels (the registry's no-actor-identity rule) and on the usage record.
