# chixing/neru fork

Personal fork, used only on this Mac. Read this before building, installing
or syncing.

## Remotes

- `origin` = github.com/chixing/neru. Push here.
- `upstream` = github.com/y3owk1n/neru. Fetch only; its push URL is
  `DISABLED`. Never push, open PRs or file issues upstream unless the owner
  asks.

## Installing

`just install -y`, then `neru services restart` (the daemon can take ~10 s
before `neru status` answers).

The build signs Neru.app with the "Apple Development" identity in the login
keychain (scripts/dist.sh) so macOS keeps the Accessibility and Screen
Recording grants across rebuilds. If the keychain is locked, codesign fails
with `errSecInternalComponent`, or the script falls back to an ad-hoc
signature and every grant is lost. Before installing, have the owner run in
their own terminal (it asks for their password; never type it for them):

    security unlock-keychain ~/Library/Keychains/login.keychain-db

Check afterwards: `codesign -dvv /Applications/Neru.app 2>&1 | grep Authority`
must show `Apple Development: xingchi90@me.com (NZ65QE9ELZ)`. If grants were
lost: `tccutil reset Accessibility com.y3owk1n.neru` (and `ScreenCapture`),
then the owner re-adds Neru in System Settings.

## Syncing with upstream

The fork's commits sit on top of upstream main. When upstream has something
worth taking:

    git fetch upstream
    git rebase upstream/main
    just test          # TestLaunchCommandExecution fails while the daemon runs; ignore it
    just install -y
    git push --force-with-lease origin main

Likely conflicts: `internal/app/modes/` (hint search), the darwin
Objective-C bridges (`internal/adapter/platform/darwin/`), and generated docs
(`just gensupportref` regenerates the platform table).

## Config

`~/.config/neru/config.toml` is managed by chezmoi. Edit it, then
`chezmoi re-add ~/.config/neru/config.toml` and commit in
`~/.local/share/chezmoi`. `neru config set` writes `config.override.toml`,
which is not tracked; fold lasting changes into config.toml.
