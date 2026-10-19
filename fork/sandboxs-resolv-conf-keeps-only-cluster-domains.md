---
id: sandboxs-resolv-conf-keeps-only-cluster-domains
fork: substrate
theme: session-lifecycle
category: bugfix
status: carried
commits:
  - 'fix(sandbox): keep only cluster search domains in the sandbox resolv.conf'
upstream:
  outcome: to-file
  refs: []
  exit: upstream PR
depends_on: []
verified: 2026-10-19
---
# A sandbox's resolv.conf keeps only the cluster domain's search entries

## Change

A sandbox's resolv.conf keeps only the cluster domain's search entries (`dns.SandboxResolvConf` drops the node's own search domains kubelet appends after `<ns>.svc.<domain> svc.<domain> <domain>`; a list kubelet did not write is kept), and the DNS relay names the query it could not resolve or answered with a resolver failure (`query="<name> <TYPE>"` on the worker's `dns relay …` warning)

## Reason

under ndots:5 every cluster Service name was first tried under the node's search domain (the CI runner's `*.internal.cloudapp.net`), a query cluster DNS can only forward to the node's resolvers; c-ares (curl, so git, in Alpine Harness images) and musl end the whole lookup when one search query times out or fails, so one forwarded query left unanswered failed a fresh gVisor sandbox's skill fetch with `Could not resolve host` about a second after start, and the relay's only log line named no query ([#215](https://github.com/giantswarm/substrate/issues/215))

## History

to file: agent-substrate/substrate `main` (`98473853`, 2026-10-06) still copies the pod's whole search list into the sandbox and logs no query name; no upstream issue covers it

`giantswarm`: [#216](https://github.com/giantswarm/substrate/pull/216)
