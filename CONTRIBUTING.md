# Contributing to Easy Proxies

Thank you for improving Easy Proxies. Bug reports, documentation fixes, tests, and focused code changes are welcome.

## Before you start

Install Git and Go 1.24.4 or a compatible Go 1.24 toolchain.

Do not commit subscription URLs, proxy nodes, credentials, tokens, local logs, GeoIP databases, or personal automation scripts. Use `config.example.yaml` for shared configuration examples.

## Fork and branch workflow

1. Fork the repository on GitHub.
2. Clone your fork and add this repository as `upstream`:

```bash
git clone https://github.com/YOUR_NAME/easy-proxies.git
cd easy-proxies
git remote add upstream https://github.com/daimon3332/easy-proxies.git
git fetch upstream
```

3. Create a focused branch from the latest `main`:

```bash
git checkout main
git pull --ff-only upstream main
git checkout -b feat/short-description
```

Use a clear prefix such as `feat/`, `fix/`, `docs/`, or `test/`.

## Set up and build

```bash
go mod download
```

Windows:

```powershell
go build -tags "with_clash_api with_utls with_quic" -o easy_proxies.exe .
```

Linux or macOS:

```bash
go build -tags "with_clash_api with_utls with_quic" -o easy_proxies .
```

The `with_clash_api` tag enables the embedded Clash API, `with_utls` enables uTLS/Reality-related capabilities, and `with_quic` enables QUIC-based protocols such as Hysteria2 and TUIC.

For local runtime testing, copy `config.example.yaml` to `config.yaml`. On Windows PowerShell, run `Copy-Item config.example.yaml config.yaml`; on Linux or macOS, run `cp config.example.yaml config.yaml`.

## Test your change

```bash
go test ./...
go vet ./...
```

For WebUI changes, also verify the affected page in a browser and confirm that long-running operations do not block navigation.

## Commit and push

Keep changes focused and avoid unrelated formatting or dependency updates.

```bash
git status
git add <changed-files>
git commit -m "Describe the focused change"
git push -u origin feat/short-description
```

## Open a pull request

Open a pull request from your fork to `daimon3332/easy-proxies:main`. Include:

- The problem and expected behavior
- The implementation summary
- Test commands and results
- Screenshots for visible WebUI changes
- Compatibility or configuration impact

Respond to review feedback with additional commits and keep the branch current with upstream `main`.

## License and attribution

By contributing, you agree that your contribution is distributed under the [MIT License](./LICENSE). Preserve required notices and attribution for [jasonwong1991/easy_proxies](https://github.com/jasonwong1991/easy_proxies) and other upstream dependencies.
