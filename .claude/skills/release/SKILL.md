---
name: release
description: Tag a new release, wait for the GitHub Actions release workflow to succeed, then install the binary locally via install.sh.
disable-model-invocation: true
---

# Release and Install

Perform the full release flow for this project. Steps:

1. **Determine the next version**: Find the latest tag with `git tag --sort=-v:refname | head -1`, then review the git log since that tag (`git log <latest-tag>..HEAD --oneline`) to understand what changed. If there are no new commits since the last tag, stop and inform the user there is nothing to release. Otherwise, choose the appropriate semver bump (major, minor, or patch) based on the changes.
2. **Tag and push**: Create the new tag on the current commit and push it to origin.
3. **Wait for the release workflow**: Use `gh run list` to find the Actions run triggered by the tag push, then `gh run watch <id>` to wait for it to complete. If it fails, report the error and stop.
4. **Install**: Run `./install.sh` to download and install the newly released binary.
5. **Restart the daemon**: Run `pigeon daemon restart` to pick up the new binary.
6. **Confirm**: Print the installed version (`pigeon --version`).
