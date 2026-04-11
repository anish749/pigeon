# Fixed Bugs

## ~~Maintain() detects thread files by path substring~~ — fixed in #153

Thread file detection now uses `paths.IsThreadFile` which checks filename format (date vs timestamp), not parent directory name. A conversation literally named `threads` is no longer misidentified.

## ~~Separate out ConvMeta~~ — fixed in #151

`MetaFile` type renamed to `ConvMetaFile`. `.meta.json` string literal replaced with `ConvMetaFilename` constant.

## ~~GWS Drive: removed files not cleaned up from disk~~ — fixed in #154

`FSStore.RemoveDriveFile` scans the gdrive directory by fileID and deletes matching local directories when Drive reports a file as removed.

## ~~GWS: no historical backfill on first run~~ — fixed in #135, #136, #137

Gmail, Calendar, and Drive pollers now backfill on first run.

## ~~GWS Calendar: recurring events not expanded~~ — fixed in #135

Calendar backfill expands recurring events.

## ~~GWS Gmail: email-delete tombstones~~ — fixed in #159

Tombstone (`email-delete`) lines replaced with a pending-deletes file. The poller writes deleted IDs to `.pending-email-deletes`; maintenance is responsible for applying them. `EmailDeleteLine` type removed.

## ~~GWS Drive: removed files not cleaned up~~ — rejected approach in #152, fixed in #154

#152 was rejected because it put filesystem IO (`os.ReadDir`, `os.RemoveAll`) in the `paths` package and had the poller doing storage bookkeeping directly. #154 moved the logic to `FSStore.RemoveDriveFile` in the store layer.

## ~~GWS Gmail: hard delete at poll time~~ — rejected in #156

Rejected: rewriting date files during sync is too expensive — the poller doesn't know the email's date, so it would scan all date files on every deletion. The correct design is to write deleted IDs to a pending-deletes file during sync and let maintenance handle the actual removal (#159).
