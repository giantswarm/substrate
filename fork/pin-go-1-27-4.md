---
id: pin-go-1-27-4
fork: substrate
theme: version-pins
category: pin
status: carried
commits:
  - 'fix(deps): require Go 1.27.4 for the HTTP header race fix'
upstream:
  outcome: drops-at-repin
  refs: [giantswarm/substrate#582, giantswarm/substrate#581]
  exit: 'Drops when the upstream pin selects Go 1.27.4 or later for the root and plugin modules.'
depends_on: [pin-go-1-27-3]
verified: 2026-11-13
---
# Require Go 1.27.4

## Change

The root module and GCP Secret Manager plugin require Go 1.27.4. Their
security scans select Go from go.mod. The apitool module also requires
1.27.4 because it imports the root module through a local replacement.
Standalone development-tool modules keep their existing minimum versions;
CI runs them with the Go version selected from the root module.

## Reason

GO-2026-6859 is an HTTP/1 server crash caused by a MaxBytesReader header
race in net/http. Both required module scans report reachable paths under
Go 1.27.3 and identify Go 1.27.4 as the fixed release. The security checks
remain enabled without exclusions.

## History

The failure on e77170e35641441c29b7bda52ed565cf6ba33f3b blocked the release
asset cache change. The toolchain repair is included in the same pull request.
