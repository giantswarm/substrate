---
id: rustfs-bucket-init-job-helm-hook
fork: substrate
theme: chart-knobs
category: gs-specific
status: carried
upstream:
  outcome: to-file
  refs: []
  exit: kd chart PR (hook half)
depends_on: []
commit: f4256587
verified: 2026-10-19
---
# The `rustfs-bucket-init` Job is a Helm hook

- **Status:** carried
- **Upstream:** to file for kagent-dev/substrate (the hook only; the gsoci default is fork-only)
- **Our commit:** `f4256587` ([#85](https://github.com/giantswarm/substrate/pull/85))

## Change

The `rustfs-bucket-init` Job is a Helm hook (`post-install,post-upgrade`, delete policy `before-hook-creation,hook-succeeded`: a completed Job left behind would block a rollback to a release whose Job is a plain resource of the same name; `hack/install-microvm-deps.sh` and `hack/microvm-assets/stage-to-rustfs.sh` wait for the Job only while it exists), recreated on every install and upgrade under the same name, and `images.awsCli` is the gsoci copy `gsoci.azurecr.io/giantswarm/aws-cli:2.17.0` (upstream's digest)

## Reason

a Job's pod template is immutable: as a plain release resource any change of its image failed the upgrade of every installation with `spec.template: … field is immutable` until the completed Job was deleted by hand, so the chart kept Docker Hub's short name `amazon/aws-cli` and the platform could not run from gsoci only (giantswarm/agent-platform#580). On an upgrade from a release where the Job is a release resource, Helm deletes the old Job (gone from the manifest) and the post-upgrade hook creates the new one. `charts/substrate/tests/rustfs_test.yaml` holds the hook
