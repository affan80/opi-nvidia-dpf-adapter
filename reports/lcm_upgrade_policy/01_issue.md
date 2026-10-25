# Issue

`EnsureInstalled` could install or adopt one pinned release, but it
did not have an upgrade policy.

What could go wrong:

1. A caller could request `latest`, `v27.1.0`, a chart URL, or
   `--set` flags and get past the pin.
2. A same-version reconcile would call Upgrade again.
3. A newer owned chart (`v26.4.2`) could be silently pulled back to
   `v26.4.0`.
4. Two reconciles could Upgrade the same `namespace/name` at once
   and overwrite each other's revision.
5. A timeout could be retried or reported as success without
   reading the actual Helm storage.
6. An unknown outcome could be retried, duplicating the operation.
7. A failed upgrade could drop the last-known-good version, so
   rollback had nothing to restore.
8. A rollback that did not restore the expected version could still
   look like success.

The live cluster release `dpf-operator-system/dpf-operator` is
owned by Helm, not this adapter. Adopting or upgrading it would
change a release this repo does not manage.
