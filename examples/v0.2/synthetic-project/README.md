# Synthetic alpha project

This project contains no credentials or external dependencies. It is a
deterministic stand-in for checking that Boxwarden transfers a host project to
an independent guest workspace and can read the same bytes back after a stop.

Inside the Ubuntu guest, run `node app.js` from this directory. It reads
`task.json` and prints a summary without changing the imported files or using
the network. Keep any other demo output outside the `boxwarden-import-<UUID>`
directory until the stopped-volume verification is complete, since that gate
requires the exported imported tree to match the captured source exactly.

After the original import's stopped export and public import verification have
passed, restart sandbox A and explicitly run `node app.js --edit` **inside the
guest's imported directory**. This updates `task.json` and creates
`guest-note.txt` with known synthetic content. Repeating the edit leaves those
same bytes in place. Ordinary `node app.js` remains read-only. Do not run the
edit against the original host source.

The ChatGPT recipes expose this as the explicit `edit-project` reconfigure
action. It selects exactly one ordinary import directory at the declared
workspace mount and refuses missing, ambiguous or linked candidates. Later
stop/start, system rebuild and replacement checks must compare the edited task
and new note to a newly captured stopped-export baseline, rather than the
original pristine import. Guest execution and lifecycle persistence still
require real-VM acceptance; source fixtures alone do not establish them.
