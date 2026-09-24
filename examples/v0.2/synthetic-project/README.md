# Synthetic alpha project

This project contains no credentials or external dependencies. It is a
deterministic stand-in for checking that Boxwarden transfers a host project to
an independent guest workspace and can read the same bytes back after a stop.

Inside the Ubuntu guest, run `node app.js` from this directory. It reads
`task.json` and prints a summary without changing the imported files or using
the network. Keep any other demo output outside the `boxwarden-import-<UUID>`
directory until the stopped-volume verification is complete, since that gate
requires the exported imported tree to match the captured source exactly.
