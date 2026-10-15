# The keychain tests write their plugins under the fork lock

- **Status:** carried
- **Upstream:** not opened yet: the upstream-ready commit is on the branch `upstream/keychain-fake-plugin-etxtbsy` (`7668cb4a2`, mirror `main` @ `5cf1c245` + the change, DCO-signed)
- **Our commit:** [#447](https://github.com/giantswarm/substrate/pull/447): a `git cherry-pick -x` of the upstream-ready commit, its subject in the line's conventional form

## Change

`cmd/atelet/internal/credentialprovider`'s tests write every shell-script plugin through `writePlugin`, which holds
`syscall.ForkLock` while the file is open for writing. Test-only; nothing that ships changes.

## Reason

The tests run in parallel. If another test forks while a plugin is still open for writing, the child inherits the
write descriptor, and exec of the plugin fails with `ETXTBSY`. The keychain skips a provider that fails to run, so the
test failed later on a missing `fake-provider.requests` file. That flake hit the line's CI under a loaded `go test ./...`
([#440](https://github.com/giantswarm/substrate/issues/440)).
