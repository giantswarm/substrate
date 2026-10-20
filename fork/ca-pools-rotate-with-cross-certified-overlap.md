---
id: ca-pools-rotate-with-cross-certified-overlap
fork: substrate
theme: credential-injection
category: feature
status: carried
commits:
  - 'localca: rotate a CA pool with cross-certified overlap'
upstream:
  outcome: to-file
  refs: [agent-substrate/substrate#932, agent-substrate/substrate#971, agent-substrate/substrate#1931]
  exit: "upstream ships rotation commands for the CA pools (agent-substrate/substrate#1931, the follow-on #971 announced): drop; re-check each re-pin; if #1931 is idle at the re-pin after 2026-11-17, the prepared comment is posted"
depends_on: []
verified: 2026-10-20
---
# CA pools rotate with a cross-certified overlap

## Change

CA pools rotate with a cross-certified overlap (`kubectl-ate admin add-ca`: a new CA, not signing, whose key the signing CA also certifies — `localca.CA.CrossCertificate`, dated from the signing CA's start; `activate-ca`: signs from then on, refused for a CA the signer did not cross-certify, `ActivatedAt` recorded, every leaf and `tls.crt` carry the cross-certificate; `retire-ca`: refused for the signer and until it has signed for `--min-signing-age`, 20m; `get-ca-pool`; `internal/localca/poolsecret` keeps `tls.crt`/`tls.key` on the signer under optimistic concurrency; `make-ca-pool` records the activation time; the procedure in `docs/egress-trust-bundle.md`; the e2e `TestEgressMITMCARotation` in helm-e2e per sandbox class, with the probe's `roots=pinned`)

## Reason

the egress MITM CA could only be created, so a rotation replaced `egress-mitm-ca-pool`: actors got the new bundle within seconds, the gateway its CA a minute later, and every handshake in between failed with `unknown authority`. An overlap that waits for actors to reload fails every process that pinned its roots, including every actor restored from a golden snapshot taken before the rotation. Proven by helm-e2e on the line at `c8584af3` (run 35979096003, the agentgateway line's 2.1.2 data plane), on gVisor and micro-VM: a long-running actor with anchors pinned before the rotation fetched throughout, a suspended one resumed after the switch, new actors started in every phase (the last trusting only the new root), no fetch failed. The gateway must reload `tls.crt`/`tls.key` as one pair, which the agentgateway line does from 2.1.1 (the pin row above)

## History

to file: the upstream-shaped patch is branch [`upstream/egress-ca-rotation`](https://github.com/giantswarm/substrate/tree/upstream/egress-ca-rotation) here (six commits from `431564c6` to `fa028bf6`, on kagent-dev/substrate `main` @ `08c930d6`, the commands in upstream's consolidated `admin.go`; DCO-signed; to be squashed into one whose message is the first commit's plus the dating and helm-e2e paragraphs). It depends on agentgateway reading both files as one pair. A team member opens the kagent-dev/substrate pull request once the team has reviewed it. No upstream item adds rotation tooling (kagent-dev/substrate and agent-substrate/substrate searched 2026-09-24; agent-substrate#932 says "rotate with overlap", #971 added `ActiveForSigning`); agent-substrate#1931/#1932 (open) report the components that load their CA pool once at startup

`c8584af3` ([#68](https://github.com/giantswarm/substrate/pull/68), squash)

2026-10-20: agent-substrate/substrate#971 (merged 2026-08-26, `d90e02ce`, in the pin) added `ActiveForSigning`, which the rotation builds on, not its exit, so the outcome stays to-file.

2026-10-20, decided on internal issue 38652, comment 6301541135: class 4 upstream issue; destination agent-substrate/substrate#1931 (assigned upstream), the first upstream step `add-ca`, `activate-ca` and `remove-ca` mirroring the JWT-pool commands (`cmd/kubectl-ate/internal/cmd/admin.go:168-245`); first step none until #1931 moves; branch `upstream/egress-ca-rotation` @ `fa028bf67` (six commits, to be squashed and cut to the commands); test `TestEgressMITMCARotation`; depends on no theme; owner unassigned; draft: internal issue 38652, comment 6301547059.
