---
id: ateom-keeps-re-asserting-capacity-report
fork: substrate
theme: session-lifecycle
category: bugfix
status: carried
upstream:
  outcome: to-file
  refs: [agent-substrate/substrate#1473, giantswarm/substrate#20]
  exit: as#1473 design or upstream PR
depends_on: [re-register-worker-whose-pod-ip-changed]
commit: "77953308"
verified: 2026-10-19
---
# ateom keeps re-asserting its capacity report

- **Status:** carried
- **Upstream:** to file: the upstream-shaped patch is branch [`upstream/ateom-reassert-capacity`](https://github.com/giantswarm/substrate/tree/upstream/ateom-reassert-capacity) here (`cedac921`, on the mirror `main` @ `007eb1ee`; its message is the pull-request text; the `syncer.go` comment hunk, which annotates #20's path, is not on it). agent-substrate/substrate#1473 (open since 2026-09-04: a `RecycleWorker` RPC keeps the record of a Worker whose ateom container was replaced *because* an ateom reports its capacity once per process and a re-registered Worker would stay unschedulable) designs around the constraint this patch removes — complementary, not cherry-picked; a recycled record's re-assertion is a no-op read. No upstream issue on the capacity loss itself (searched 2026-09-12). A team member opens the pull request with DCO sign-off once the team has reviewed it. Keep at a re-pin onto a release that carries #1473: a recreated sandbox has a new IP and stays a re-registration under #20
- **Our commit:** `77953308`, [#25](https://github.com/giantswarm/substrate/pull/25) (rebase-merged 2026-09-12; first release v0.0.27-gs.7)

## Change

ateom keeps re-asserting its capacity report (`internal/ateomcapacity.Report` re-sends the accepted report every 10 s through `reassertReport`, each re-assertion retried like the first, 500 ms → 30 s; atelet's per-call `Recorded worker capacity` line at Debug; ate-api's `Worker reported its capacity` stays the line that marks a change)

## Reason

a Worker record replaced under a running ateom — the Syncer's re-registration after a pod-IP change (#20), a store restored from a backup — is created without capacity (`CreateWorker` leaves it to the ateom) and never received one, so `AssignWorker` answered `no free workers available` on every resume until the pool was rolled (the lab proof of 2026-09-12). An unchanged report costs ate-api a read and no write (100 workers ≈ 10 reads/s); the ≤ 10 s gap after a re-registration sits inside the router's 30 s parking budget
