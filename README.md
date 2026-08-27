# go-tag

> A **colorized, safety-checked replacement for `git tag`** — smart repo/path
> detection, `git check-ref-format`-backed name validation, and clear errors
> for every "tag already exists / doesn't exist / invalid name" edge case.

Installs as the `tag` command.

---

## ✨ Features

- 🎨 Colorized, configurable output (24-bit hex via `tag.json`)
- 🔎 Smart directory handling — `tag`, `tag ~/other-repo`, `tag ./rel add v1`,
  or `tag -C /any/path add v1`; never need to type `.` for the current dir
- ✅ Name validation delegated to `git check-ref-format` itself (not
  reimplemented/guessed), with human-readable reasons for rejection
- 🛡️ Defensive checks before every mutating operation: tag already exists,
  target commit doesn't resolve, remote not found, tag missing on delete/push/
  verify/show — all reported clearly instead of raw git stderr
- 🏷️ Full tag lifecycle: `list`, `add` (lightweight/annotated/signed),
  `delete`, `show`, `push`, `verify` (GPG), `rename` (recreate + delete, since
  git has no native rename)

## 🚀 Usage

```
tag                              # list tags in the current directory
tag ~/projects/app               # list tags in another repo
tag -C ~/projects/app add v1     # unambiguous working-dir flag
tag add v1.2.0 -a -m "Release 1.2.0"
tag add hotfix -f                # move an existing lightweight tag
tag delete v0.9.0-beta
tag show v1.2.0
tag push v1.2.0
tag push --all -r upstream
tag verify v1.2.0
tag rename v1.2.0 v1.2.0-rc1
tag config show
tag config path
```

Run `tag --help` for the full flag reference.

## 📦 Installation

### From Source

```
go install github.com/cumulus13/go-tag@latest
```

### From Releases

Download the matching binary from
[Releases](https://github.com/cumulus13/go-tag/releases) — assets are named
`go-tag_<version>_<platform>`; rename to `tag` (or `tag.exe` on Windows) and
put it on your `PATH`.

### Homebrew (macOS/Linux)

```
brew install cumulus13/tap/go-tag
```

### Scoop (Windows)

```
scoop bucket add cumulus13 https://github.com/cumulus13/scoop-bucket
scoop install go-tag
```

### Termux (Android)

```
curl -L https://github.com/cumulus13/go-tag/releases/latest/download/go-tag_latest_android_arm64_termux \
  -o $PREFIX/bin/tag
chmod +x $PREFIX/bin/tag
```

## ⚙️ Configuration

Place `tag.json` (see `tag.example.json`) in one of the auto-detected
locations: `$TAG_CONFIG` → executable dir → cwd → platform config dir → home
dir. Set `NO_COLOR=1` or `TAG_NO_COLOR=1` to disable color.

## 📄 License

MIT

---

## 👤 Author
        
[Hadi Cahyadi](mailto:cumulus13@gmail.com)
    

[![Buy Me a Coffee](https://www.buymeacoffee.com/assets/img/custom_images/orange_img.png)](https://www.buymeacoffee.com/cumulus13)

[![Donate via Ko-fi](https://ko-fi.com/img/githubbutton_sm.svg)](https://ko-fi.com/cumulus13)
 
[Support me on Patreon](https://www.patreon.com/cumulus13)