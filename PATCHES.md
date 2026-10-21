# Release patch series

The October 21 packaging commit pins Go 1.26.10 and the existing distroless
Debian 13 runtime by digest. The runtime receives the builder's clean CA bundle
because distroless still carries the affected roots. This complements Go's
eMudhra intermediate blocklist without changing application code or dependencies.
Drop this isolated commit when upstream supplies equivalent trust handling.

This fork follows released upstream Oathkeeper Maester tags. The current base is
`v0.1.14`. The upstream default branch and historical tags remain separate from
the release patch series.

GitHub Actions are disabled for this fork. Release verification runs locally:
build the images, run the tests and vulnerability scans, then validate the
candidate before publishing it. Workflow files remain as upstream reference.

Use `release/v<upstream-version>-custom.<revision>` branches. Keep fork CI setup
in the first commit, followed by isolated fixes and their regression tests.
Feature PRs target the release branch and should be independently suitable for
upstream submission. Upstream image publishing is skipped in forks because it
requires the upstream registry credentials.

The initial candidate inherited dependency advisories from the upstream release.
The separate security update upgrades `golang.org/x/net` and its required
dependencies without changing Kubernetes or controller-runtime versions. Run
`govulncheck ./...` and scan each built image before release; retain those
reports with the test results.

Do not include deployment-specific configuration, bundled source patches, or
unrelated dependency and formatting changes. Keep the upstream build files
unchanged unless a demonstrated build defect requires a separate correction.

The unit and race checks are:

```sh
go test -race ./controllers ./api/... ./internal/...
go vet ./controllers ./api/... ./internal/...
```

Run controller integration tests separately with disposable Kubernetes fixtures.
Verify that publication preserves complete snapshots and that unchanged Rule
sets do not trigger file reloads.

For each upstream update, create a new release branch, rebase the isolated
commits, review `git range-diff`, and drop fixes supplied by upstream. Publish
immutable signed `v<upstream-version>-custom.<revision>` source tags; candidates
use an `-rc.N` suffix. Never move published source or image tags.
