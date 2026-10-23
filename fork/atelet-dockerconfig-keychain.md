---
id: atelet-dockerconfig-keychain
fork: substrate
theme: chart-knobs
category: feature
status: carried
commits:
  - 'feat(atelet): pull actor images with dockerconfigjson Secrets'
upstream:
  outcome: to-file
  refs: []
  exit: upstream PR merged or declined
depends_on: []
verified: 2026-10-23
---
# atelet pulls actor images with dockerconfigjson Secrets

## Change

atelet reads `.dockerconfigjson` files, the content of a `kubernetes.io/dockerconfigjson` Secret, through the repeatable `--image-pull-secret-file` flag, next to the kubelet credential providers. Their credentials are matched to an image as the kubelet matches a Pod's `imagePullSecrets` (URL globbing, most specific key first, Docker Hub's aliases folded onto `index.docker.io`) and tried before the providers'; every lookup re-reads the files, so a rotated Secret needs no restart. The chart's `atelet.imagePullSecrets` (`[{name: ...}]`) mounts each Secret read-only and passes its file; the default render is unchanged. Unit tests for the keychain, the chain and the chart wiring.

## Reason

atelet pulls a session's tool images itself and had no pull-secret path: a registry with static credentials needed a credential provider binary on every node, and a cluster whose node image cannot change could not pull private tool images at all. No persisted format changes.

## History

to file — prepared in the fork: branch `upstream/atelet-dockerconfig-keychain`

[#540](https://github.com/giantswarm/substrate/pull/540): the upstream-ready commit, rebase-merged
