# Contributing to Easy Proxies

Bug reports, documentation fixes, tests, and focused code changes are welcome.

Never commit subscription URLs, proxy nodes, credentials, tokens, logs, GeoIP databases, or personal scripts. Use `config.example.yaml` for shared configuration examples.

## Workflow

1. Fork the repository and clone your fork:

   ```bash
   git clone https://github.com/YOUR_NAME/easy-proxies.git
   cd easy-proxies
   git remote add upstream https://github.com/daimon3332/easy-proxies.git
   ```

2. Branch from the latest `main` with a prefix such as `feat/`, `fix/`, `docs/`, or `test/`:

   ```bash
   git fetch upstream
   git checkout -b fix/short-description upstream/main
   ```

3. Build and run as described in the [User Guide](./docs/USER_GUIDE.md#option-b-build-from-source).

## Before opening a pull request

```bash
go vet ./...
go test ./...
```

For WebUI changes (`internal/monitor/assets/`), also check the affected pages in a desktop browser and confirm long-running operations do not block navigation.

Keep changes focused; avoid unrelated formatting or dependency updates. Open the pull request against `daimon3332/easy-proxies:main` and include the problem, the change, test results, screenshots for visible UI changes, and any configuration impact.

## License

Contributions are distributed under the [MIT License](./LICENSE). Preserve the attribution for [jasonwong1991/easy_proxies](https://github.com/jasonwong1991/easy_proxies) and other upstream projects.
