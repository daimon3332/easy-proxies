@echo off
go build -tags "with_clash_api with_utls with_quic" -o easy_proxies.exe ./cmd/easy_proxies
