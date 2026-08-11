#!/usr/bin/env python3

import argparse
import concurrent.futures
import dataclasses
import hashlib
import ipaddress
import json
import os
import re
import socket
import ssl
import sys
import threading
import time
import urllib.error
import urllib.parse
import urllib.request
from collections import Counter, defaultdict
from datetime import datetime, timezone
from pathlib import Path


DEFAULT_TRACE_URL = "https://www.cloudflare.com/cdn-cgi/trace"


@dataclasses.dataclass(frozen=True)
class ProxyRecord:
    file: str
    line: int
    raw: str
    identity: str
    proxy_url: str
    endpoint: str
    format: str

    @property
    def proxy_id(self):
        return fingerprint(self.identity)

    @property
    def endpoint_id(self):
        return fingerprint(self.endpoint)


def fingerprint(value, length=12):
    return hashlib.sha256(value.encode("utf-8")).hexdigest()[:length]


def format_host(host):
    return f"[{host}]" if ":" in host and not host.startswith("[") else host


def parse_endpoint(value):
    parsed = urllib.parse.urlsplit(f"//{value}")
    if not parsed.hostname or parsed.port is None:
        raise ValueError("missing host or port")
    if parsed.path or parsed.query or parsed.fragment:
        raise ValueError("unexpected path, query, or fragment")
    if not 1 <= parsed.port <= 65535:
        raise ValueError("port is outside 1-65535")
    return parsed.hostname.lower(), parsed.port


def parse_proxy(raw, file_name, line_number, default_scheme):
    value = raw.strip()
    if not value:
        return None

    scheme = default_scheme
    username = None
    password = None
    proxy_format = "host:port"

    if re.match(r"^[a-zA-Z][a-zA-Z0-9+.-]*://", value):
        parsed = urllib.parse.urlsplit(value)
        scheme = parsed.scheme.lower()
        if scheme not in {"http", "https"}:
            raise ValueError(f"unsupported proxy scheme: {scheme}")
        if not parsed.hostname or parsed.port is None:
            raise ValueError("missing host or port")
        if parsed.path not in {"", "/"} or parsed.query or parsed.fragment:
            raise ValueError("unexpected path, query, or fragment")
        host, port = parsed.hostname.lower(), parsed.port
        username = urllib.parse.unquote(parsed.username) if parsed.username is not None else None
        password = urllib.parse.unquote(parsed.password) if parsed.password is not None else None
        proxy_format = "uri"
    else:
        endpoint_text = value
        if "@" in value:
            auth, endpoint_text = value.rsplit("@", 1)
            if ":" not in auth:
                raise ValueError("authentication must be user:pass")
            username, password = auth.split(":", 1)
            proxy_format = "user:pass@host:port"
        else:
            bracketed = re.match(r"^(\[[^\]]+\]):(\d+):([^:]*):(.*)$", value)
            regular = re.match(r"^([^:]+):(\d+):([^:]*):(.*)$", value)
            matched = bracketed or regular
            if matched:
                endpoint_text = f"{matched.group(1)}:{matched.group(2)}"
                username, password = matched.group(3), matched.group(4)
                proxy_format = "host:port:user:pass"
        host, port = parse_endpoint(endpoint_text)

    safe_host = format_host(host)
    endpoint = f"{safe_host}:{port}"
    auth = ""
    identity_auth = ""
    if username is not None:
        encoded_user = urllib.parse.quote(username, safe="")
        encoded_password = urllib.parse.quote(password or "", safe="")
        auth = f"{encoded_user}:{encoded_password}@"
        identity_auth = f"{username}:{password or ''}@"
    proxy_url = f"{scheme}://{auth}{endpoint}"
    identity = f"{scheme}://{identity_auth}{endpoint}"
    return ProxyRecord(
        file=file_name,
        line=line_number,
        raw=value,
        identity=identity,
        proxy_url=proxy_url,
        endpoint=endpoint,
        format=proxy_format,
    )


def locations(records):
    return [{"file": item.file, "line": item.line} for item in records]


def analyze_files(folder, pattern, default_scheme):
    files = sorted(path for path in folder.glob(pattern) if path.is_file())
    records = []
    parse_errors = []
    file_rows = []

    for path in files:
        raw_bytes = path.read_bytes()
        text = raw_bytes.decode("utf-8-sig", errors="replace")
        current = []
        for line_number, raw in enumerate(text.splitlines(), 1):
            if not raw.strip():
                continue
            try:
                record = parse_proxy(raw, path.name, line_number, default_scheme)
            except (ValueError, UnicodeError) as exc:
                parse_errors.append(
                    {"file": path.name, "line": line_number, "reason": str(exc)}
                )
                continue
            if record:
                current.append(record)
                records.append(record)

        identities = [item.identity for item in current]
        endpoints = [item.endpoint for item in current]
        file_rows.append(
            {
                "file": path.name,
                "records": len(current),
                "unique_proxies": len(set(identities)),
                "internal_duplicate_records": len(identities) - len(set(identities)),
                "unique_endpoints": len(set(endpoints)),
                "sha256": hashlib.sha256(raw_bytes).hexdigest(),
                "endpoint_set_sha256": hashlib.sha256(
                    "\n".join(sorted(set(endpoints))).encode("utf-8")
                ).hexdigest(),
            }
        )

    return files, records, parse_errors, file_rows


def duplicate_groups(records, key, id_name):
    grouped = defaultdict(list)
    for record in records:
        grouped[key(record)].append(record)
    result = []
    for value, group in grouped.items():
        if len(group) < 2:
            continue
        result.append(
            {
                id_name: fingerprint(value),
                "occurrences": len(group),
                "locations": locations(group),
            }
        )
    return sorted(result, key=lambda item: (-item["occurrences"], item[id_name]))


def grouped_files(file_rows, hash_field):
    groups = defaultdict(list)
    for row in file_rows:
        groups[row[hash_field]].append(row["file"])
    return [
        {"files": names, "count": len(names)}
        for names in groups.values()
        if len(names) > 1
    ]


def pairwise_overlap(records):
    by_file = defaultdict(set)
    for record in records:
        by_file[record.file].add(record.endpoint)
    names = sorted(by_file)
    pairs = []
    for index, left_name in enumerate(names):
        left = by_file[left_name]
        for right_name in names[index + 1 :]:
            right = by_file[right_name]
            shared = len(left & right)
            union = len(left | right)
            pairs.append(
                {
                    "left": left_name,
                    "right": right_name,
                    "shared_endpoints": shared,
                    "jaccard_percent": round(shared * 100 / union, 2) if union else 100.0,
                    "left_coverage_percent": round(shared * 100 / len(left), 2) if left else 100.0,
                    "right_coverage_percent": round(shared * 100 / len(right), 2) if right else 100.0,
                }
            )
    pairs.sort(key=lambda item: (-item["shared_endpoints"], item["left"], item["right"]))
    return pairs


def classify_error(exc):
    if isinstance(exc, urllib.error.HTTPError):
        if exc.code == 407:
            return "proxy_authentication", "HTTP 407"
        return "http_status", f"HTTP {exc.code}"
    if isinstance(exc, (TimeoutError, socket.timeout)):
        return "timeout", "request timed out"
    if isinstance(exc, ssl.SSLError):
        return "tls", "TLS negotiation failed"
    if isinstance(exc, urllib.error.URLError):
        reason = exc.reason
        if isinstance(reason, (TimeoutError, socket.timeout)):
            return "timeout", "request timed out"
        if isinstance(reason, ssl.SSLError):
            return "tls", "TLS negotiation failed"
        if isinstance(reason, ConnectionRefusedError):
            return "connection_refused", "connection refused"
        if isinstance(reason, socket.gaierror):
            return "dns", "DNS resolution failed"
        return "network", "proxy connection failed"
    return "unexpected", type(exc).__name__


def parse_trace(body):
    values = {}
    for line in body.splitlines():
        if "=" in line:
            key, value = line.split("=", 1)
            values[key.strip()] = value.strip()
    address = values.get("ip", "")
    country = values.get("loc", "").upper()
    try:
        ipaddress.ip_address(address)
    except ValueError as exc:
        raise ValueError("trace response has no valid egress IP") from exc
    if not re.fullmatch(r"[A-Z]{2}", country):
        raise ValueError("trace response has no valid country code")
    return address, country


def probe_proxy(record, trace_url, timeout, retries):
    handler = urllib.request.ProxyHandler(
        {"http": record.proxy_url, "https": record.proxy_url}
    )
    opener = urllib.request.build_opener(handler)
    request = urllib.request.Request(
        trace_url,
        headers={
            "Accept": "text/plain",
            "Cache-Control": "no-cache",
            "User-Agent": "easy-proxies-audit/1.0",
        },
        method="GET",
    )

    last_category = "unexpected"
    last_detail = "probe failed"
    started = time.monotonic()
    for attempt in range(retries + 1):
        try:
            with opener.open(request, timeout=timeout) as response:
                if response.status != 200:
                    raise urllib.error.HTTPError(
                        trace_url, response.status, "unexpected status", response.headers, None
                    )
                body = response.read(65536).decode("utf-8", errors="replace")
            address, country = parse_trace(body)
            return {
                "ok": True,
                "proxy_id": record.proxy_id,
                "endpoint_id": record.endpoint_id,
                "egress_ip": address,
                "egress_id": fingerprint(address),
                "country": country,
                "attempts": attempt + 1,
                "elapsed_ms": round((time.monotonic() - started) * 1000),
            }
        except ValueError as exc:
            last_category, last_detail = "invalid_response", str(exc)
        except Exception as exc:
            last_category, last_detail = classify_error(exc)

        if last_category in {"proxy_authentication", "http_status", "invalid_response"}:
            break
        if attempt < retries:
            time.sleep(min(0.5 * (attempt + 1), 1.5))

    return {
        "ok": False,
        "proxy_id": record.proxy_id,
        "endpoint_id": record.endpoint_id,
        "category": last_category,
        "detail": last_detail,
        "attempts": min(retries + 1, attempt + 1),
        "elapsed_ms": round((time.monotonic() - started) * 1000),
    }


def run_probes(records, trace_url, timeout, retries, workers):
    unique = {}
    sources = defaultdict(list)
    for record in records:
        unique.setdefault(record.identity, record)
        sources[record.identity].append(record)

    total = len(unique)
    completed = 0
    succeeded = 0
    lock = threading.Lock()
    started = time.monotonic()
    results = []
    print(f"\nTesting {total} unique proxies with {workers} workers...")

    def execute(item):
        return item, probe_proxy(item, trace_url, timeout, retries)

    with concurrent.futures.ThreadPoolExecutor(max_workers=workers) as executor:
        futures = [executor.submit(execute, item) for item in unique.values()]
        for future in concurrent.futures.as_completed(futures):
            record, result = future.result()
            result["locations"] = locations(sources[record.identity])
            results.append(result)
            with lock:
                completed += 1
                succeeded += int(result["ok"])
                if completed == total or completed % max(25, total // 20) == 0:
                    elapsed = time.monotonic() - started
                    print(
                        f"  {completed}/{total} ({completed * 100 / total:.1f}%) | "
                        f"success {succeeded} | failed {completed - succeeded} | "
                        f"elapsed {elapsed:.1f}s"
                    )

    return sorted(results, key=lambda item: item["proxy_id"])


def summarize_egress(results):
    successful = [item for item in results if item["ok"]]
    failed = [item for item in results if not item["ok"]]
    country_proxy_counts = Counter(item["country"] for item in successful)
    country_egress = defaultdict(set)
    egress_proxies = defaultdict(list)
    for item in successful:
        country_egress[item["country"]].add(item["egress_ip"])
        egress_proxies[item["egress_ip"]].append(item["proxy_id"])

    countries = []
    for country in sorted(country_proxy_counts, key=lambda key: (-country_proxy_counts[key], key)):
        countries.append(
            {
                "country": country,
                "working_proxies": country_proxy_counts[country],
                "unique_egress_ips": len(country_egress[country]),
            }
        )

    shared = [
        {
            "egress_id": fingerprint(address),
            "country": next(
                item["country"] for item in successful if item["egress_ip"] == address
            ),
            "proxy_count": len(proxy_ids),
            "proxy_ids": sorted(proxy_ids),
        }
        for address, proxy_ids in egress_proxies.items()
        if len(proxy_ids) > 1
    ]
    shared.sort(key=lambda item: (-item["proxy_count"], item["egress_id"]))
    errors = Counter(item["category"] for item in failed)
    return {
        "tested_unique_proxies": len(results),
        "working_unique_proxies": len(successful),
        "failed_unique_proxies": len(failed),
        "unique_egress_ips": len(egress_proxies),
        "shared_egress_ip_groups": len(shared),
        "countries": countries,
        "failure_categories": dict(errors.most_common()),
        "shared_egress": shared,
    }


def sanitized_probe_results(results):
    clean = []
    for item in results:
        row = {key: value for key, value in item.items() if key != "egress_ip"}
        clean.append(row)
    return clean


def print_static_summary(summary):
    print("Proxy input summary")
    print(f"  Files: {summary['file_count']}")
    print(f"  Parsed records: {summary['records']}")
    print(f"  Parse errors: {summary['parse_errors']}")
    print(f"  Unique authenticated proxies: {summary['unique_proxies']}")
    print(f"  Duplicate proxy records: {summary['duplicate_proxy_records']}")
    print(f"  Unique entry endpoints: {summary['unique_endpoints']}")
    print(f"  Repeated endpoint occurrences: {summary['repeated_endpoint_occurrences']}")
    print(f"  Identical file groups: {summary['identical_file_groups']}")
    print(f"  Identical endpoint-set groups: {summary['identical_endpoint_set_groups']}")


def print_egress_summary(summary):
    print("\nEgress summary")
    print(f"  Tested unique proxies: {summary['tested_unique_proxies']}")
    print(f"  Working proxies: {summary['working_unique_proxies']}")
    print(f"  Failed proxies: {summary['failed_unique_proxies']}")
    print(f"  Unique egress IPs: {summary['unique_egress_ips']}")
    print(f"  Shared egress IP groups: {summary['shared_egress_ip_groups']}")
    print("\nCountries (working proxies / unique egress IPs)")
    if not summary["countries"]:
        print("  No successful trace responses")
    for row in summary["countries"]:
        print(
            f"  {row['country']}: {row['working_proxies']} / {row['unique_egress_ips']}"
        )
    if summary["failure_categories"]:
        print("\nFailure categories")
        for category, count in summary["failure_categories"].items():
            print(f"  {category}: {count}")


def build_parser():
    parser = argparse.ArgumentParser(
        description="Find duplicate proxy entries and audit real egress IP/country data."
    )
    parser.add_argument(
        "folder",
        nargs="?",
        type=Path,
        default=Path(__file__).resolve().parent / "ProxySpace",
        help="folder containing proxy text files (default: ./ProxySpace)",
    )
    parser.add_argument("--pattern", default="*.txt", help="input glob (default: *.txt)")
    parser.add_argument(
        "--proxy-scheme",
        choices=("http", "https"),
        default="http",
        help="scheme for records without one (default: http)",
    )
    parser.add_argument("--workers", type=int, default=100, help="concurrent probes")
    parser.add_argument("--timeout", type=float, default=8.0, help="seconds per attempt")
    parser.add_argument("--retries", type=int, default=1, help="retries after network failures")
    parser.add_argument("--trace-url", default=DEFAULT_TRACE_URL)
    parser.add_argument("--skip-egress", action="store_true", help="only compare input records")
    parser.add_argument(
        "--report",
        type=Path,
        help="JSON report path (default: <folder>/proxy_audit_report.json)",
    )
    return parser


def main():
    args = build_parser().parse_args()
    if not args.folder.is_dir():
        raise SystemExit(f"Proxy folder does not exist: {args.folder}")
    if not 1 <= args.workers <= 500:
        raise SystemExit("--workers must be between 1 and 500")
    if not 0.5 <= args.timeout <= 120:
        raise SystemExit("--timeout must be between 0.5 and 120 seconds")
    if not 0 <= args.retries <= 5:
        raise SystemExit("--retries must be between 0 and 5")

    files, records, parse_errors, file_rows = analyze_files(
        args.folder, args.pattern, args.proxy_scheme
    )
    if not files:
        raise SystemExit(f"No files matched {args.pattern!r} in {args.folder}")
    if not records:
        raise SystemExit("No valid proxy records were found")

    identity_duplicates = duplicate_groups(records, lambda item: item.identity, "proxy_id")
    endpoint_duplicates = duplicate_groups(records, lambda item: item.endpoint, "endpoint_id")
    identical_files = grouped_files(file_rows, "sha256")
    identical_sets = grouped_files(file_rows, "endpoint_set_sha256")
    overlaps = pairwise_overlap(records)
    format_counts = Counter(item.format for item in records)
    unique_identities = len({item.identity for item in records})
    unique_endpoints = len({item.endpoint for item in records})
    static_summary = {
        "file_count": len(files),
        "records": len(records),
        "parse_errors": len(parse_errors),
        "unique_proxies": unique_identities,
        "duplicate_proxy_groups": len(identity_duplicates),
        "duplicate_proxy_records": len(records) - unique_identities,
        "unique_endpoints": unique_endpoints,
        "duplicate_endpoint_groups": len(endpoint_duplicates),
        "repeated_endpoint_occurrences": len(records) - unique_endpoints,
        "identical_file_groups": len(identical_files),
        "identical_endpoint_set_groups": len(identical_sets),
    }
    print_static_summary(static_summary)

    report = {
        "generated_at": datetime.now(timezone.utc).isoformat(),
        "input_folder": str(args.folder.resolve()),
        "input_pattern": args.pattern,
        "summary": static_summary,
        "format_counts": dict(format_counts),
        "files": file_rows,
        "parse_errors": parse_errors,
        "duplicate_proxies": identity_duplicates,
        "duplicate_endpoints": endpoint_duplicates,
        "identical_files": identical_files,
        "identical_endpoint_sets": identical_sets,
        "cross_file_overlap": overlaps,
        "egress": None,
    }

    if not args.skip_egress:
        probe_results = run_probes(
            records, args.trace_url, args.timeout, args.retries, args.workers
        )
        egress_summary = summarize_egress(probe_results)
        print_egress_summary(egress_summary)
        report["egress"] = {
            "provider": "Cloudflare Trace",
            "url": args.trace_url,
            "timeout_seconds": args.timeout,
            "retries": args.retries,
            "workers": args.workers,
            "summary": egress_summary,
            "results": sanitized_probe_results(probe_results),
        }

    report_path = args.report or args.folder / "proxy_audit_report.json"
    report_path = report_path.resolve()
    report_path.parent.mkdir(parents=True, exist_ok=True)
    temporary = report_path.with_suffix(report_path.suffix + ".tmp")
    temporary.write_text(json.dumps(report, indent=2, ensure_ascii=True), encoding="utf-8")
    os.replace(temporary, report_path)
    print(f"\nSanitized JSON report: {report_path}")
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except KeyboardInterrupt:
        print("\nCancelled", file=sys.stderr)
        sys.exit(130)
