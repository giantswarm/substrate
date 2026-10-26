---
id: rustfs-bucket-init-job-helm-hook
fork: substrate
theme: chart-knobs
category: gs-specific
status: carried
commits:
  - 'fix(helm): recreate the rustfs bucket-init Job, its image from gsoci'
upstream:
  outcome: fork-only
  refs: [kagent-dev/substrate#80]
  exit: "with T14: the knob becomes a setting of the generated chart of agent-substrate/substrate#3170; re-check 2026-11-17"
depends_on: []
verified: 2026-10-26
---
# The `rustfs-bucket-init` Job is a Helm hook

## Change

The `rustfs-bucket-init` Job is a Helm hook (`post-install,post-upgrade`, delete policy `before-hook-creation,hook-succeeded`: a completed Job left behind would block a rollback to a release whose Job is a plain resource of the same name; `hack/install-microvm-deps.sh` and `hack/microvm-assets/stage-to-rustfs.sh` wait for the Job only while it exists), recreated on every install and upgrade under the same name, and `images.awsCli` is the gsoci copy `gsoci.azurecr.io/giantswarm/aws-cli:2.17.0` (upstream's digest)

## Reason

a Job's pod template is immutable: as a plain release resource any change of its image failed the upgrade of every installation with `spec.template: … field is immutable` until the completed Job was deleted by hand, so the chart kept Docker Hub's short name `amazon/aws-cli` and the platform could not run from gsoci only (giantswarm/agent-platform#580). On an upgrade from a release where the Job is a release resource, Helm deletes the old Job (gone from the manifest) and the post-upgrade hook creates the new one. `charts/substrate/tests/rustfs_test.yaml` holds the hook

## History

to file for kagent-dev/substrate (the hook only; the gsoci default is fork-only)

`f4256587` ([#85](https://github.com/giantswarm/substrate/pull/85))

2026-10-20: [kagent-dev/substrate#80](https://github.com/kagent-dev/substrate/pull/80) (timflannagan, opened 2026-10-15) makes the Job the same `post-install,post-upgrade` hook with `before-hook-creation,hook-succeeded` and lets the microvm asset scripts tolerate the removed hook, so the hook half is open upstream under that pull request and the outcome moves from to-file to open.

2026-10-26: kagent-dev/substrate#80 was closed unmerged by its author on 2026-10-21 ("Closing for now"). Under the chart-knobs decision of 2026-10-24 the row stays fork-only until agent-substrate/substrate#3170's generated chart lands.
