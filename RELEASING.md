# Releasing rules_vmtest

## Prerequisites (one-time)

1. Fork [bazelbuild/bazel-central-registry](https://github.com/bazelbuild/bazel-central-registry) to `mikn/bazel-central-registry`
2. Create a GitHub **Classic** PAT with `repo` + `workflow` scopes (fine-grained PATs [cannot open PRs against public repos](https://github.com/github/roadmap/issues/600))
3. Add it as `BCR_PUBLISH_TOKEN` in repo Settings → Secrets and variables → Actions

## Release process

1. Update the version in `MODULE.bazel`:
   ```starlark
   module(
       name = "rules_vmtest",
       version = "0.2.0",  # ← bump this
       compatibility_level = 0,
   )
   ```

2. Commit and tag:
   ```bash
   git commit -am "release: v0.2.0"
   git tag v0.2.0
   git push origin main v0.2.0
   ```

3. The rest is automated:
   - `release.yml` triggers on tag push → creates source archive → creates GitHub release
   - `publish.yml` triggers on release published → calls `bazel-contrib/publish-to-bcr` → opens PR to `bazelbuild/bazel-central-registry`

4. Review and merge the auto-created BCR PR.

## Publish order

`rules_vmtest` depends on `rules_qemu` and `rules_linux`. Both must be published to BCR first:

```
rules_linux  ─┐
               ├─→  rules_vmtest
rules_qemu  ──┘
```

When bumping all three, publish in order: `rules_linux` → `rules_qemu` → `rules_vmtest`.

## BCR templates

- `.bcr/metadata.template.json` — maintainer info
- `.bcr/source.template.json` — archive URL with `{TAG}`/`{VERSION}` placeholders
- `.bcr/presubmit.yml` — BCR CI matrix (Bazel 8.x + 9.x)
