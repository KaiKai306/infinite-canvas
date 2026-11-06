#!/usr/bin/env python3
"""Add only the Canvas Caddy import; validate first and restore on reload failure."""
from pathlib import Path
from urllib.request import urlopen
import argparse
import hashlib
import ipaddress
import json
import os
import shutil
import socket
import subprocess

ROOT = Path('/srv/canvas-deployment')
MAIN = Path('/srv/caddy/Caddyfile')
PART = Path('/srv/caddy/config/canvas')
IMPORT = 'import /config/canvas/Caddyfile'
DOMAIN = 'canvas.happykai.cn'
ROUTE = '''canvas.happykai.cn {
    encode zstd gzip
    request_body {
        max_size 128MB
    }
    @anonymous path /api/anonymous/files /api/anonymous/files/*
    respond @anonymous 401
    header X-Content-Type-Options nosniff
    header Referrer-Policy same-origin
    reverse_proxy 127.0.0.1:13800 {
        transport http {
            read_timeout 900s
            write_timeout 900s
        }
    }
}
'''


def run(*args):
    result = subprocess.run(['docker', 'exec', 'Caddy', 'caddy', *args], capture_output=True)
    (ROOT / 'route-last-command.log').write_bytes(result.stdout + result.stderr)
    return result.returncode == 0


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--dns-server', type=ipaddress.ip_address, help='Optional resolver IP when the system resolver has stale DNS')
    args = parser.parse_args()
    os.umask(0o077)
    if os.geteuid() != 0:
        raise RuntimeError('root required')
    with urlopen('http://127.0.0.1:13800/api/health', timeout=15) as health:
        if health.status != 200:
            raise RuntimeError('Canvas not healthy')
    state = json.loads(Path('/srv/infinite-canvas/bootstrap-state.json').read_text())
    if not state.get('r2_rw_test_passed'):
        raise RuntimeError('R2 verification required')
    def addresses(family):
        if args.dns_server:
            kind = 'A' if family == socket.AF_INET else 'AAAA'
            result = subprocess.run(['dig', '+time=3', '+tries=1', '+short', kind, DOMAIN, '@' + str(args.dns_server)], capture_output=True, text=True, check=True, timeout=10)
            records = set()
            for line in result.stdout.splitlines():
                try:
                    records.add(str(ipaddress.ip_address(line.strip())))
                except ValueError:
                    pass  # Ignore CNAME text; only literal addresses are accepted.
            return records
        try:
            return {x[4][0] for x in socket.getaddrinfo(DOMAIN, 443, family=family)}
        except socket.gaierror:
            return set()

    ipv4 = addresses(socket.AF_INET)
    if ipv4 != {'186.241.77.163'}:
        raise RuntimeError('Canvas A record must point only to 186.241.77.163')
    if addresses(socket.AF_INET6):
        raise RuntimeError('Review existing IPv6 DNS before enabling route')
    original = MAIN.read_bytes()
    if DOMAIN.encode() in original and IMPORT.encode() not in original:
        raise RuntimeError('Existing Canvas routing requires review')
    PART.mkdir(mode=0o755, parents=True, exist_ok=True)
    route = PART / 'Caddyfile'
    if route.exists() and route.read_text() != ROUTE:
        raise RuntimeError('Existing Canvas import differs')
    route.write_text(ROUTE)
    route.chmod(0o644)
    candidate = original if IMPORT.encode() in original else original.rstrip() + b'\n\n' + IMPORT.encode() + b'\n'
    preview = PART / 'candidate.Caddyfile'
    preview.write_bytes(candidate)
    preview.chmod(0o600)
    if not run('validate', '--config', '/config/canvas/candidate.Caddyfile', '--adapter', 'caddyfile'):
        raise RuntimeError('Caddy candidate validation failed; main untouched')
    backup = ROOT / 'Caddyfile.before-canvas'
    if not backup.exists():
        shutil.copy2(MAIN, backup)
        backup.chmod(0o600)
    try:
        # Preserve inode: Caddyfile is mounted as a single file in the running container.
        with MAIN.open('wb') as handle:
            handle.write(candidate)
            handle.flush()
            os.fsync(handle.fileno())
        if not run('reload', '--config', '/etc/caddy/Caddyfile', '--adapter', 'caddyfile'):
            raise RuntimeError('Caddy reload failed')
    except Exception:
        with MAIN.open('wb') as handle:
            handle.write(original)
        run('reload', '--config', '/etc/caddy/Caddyfile', '--adapter', 'caddyfile')
        raise
    preview.unlink()
    result = {'route_active': True, 'domain': DOMAIN, 'previous_main_sha256': hashlib.sha256(original).hexdigest(),
              'main_sha256': hashlib.sha256(candidate).hexdigest(), 'existing_imports_preserved': True}
    (ROOT / 'route-state.json').write_text(json.dumps(result, indent=2) + '\n')
    print(json.dumps(result))


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        print('STOP: ' + str(error))
        raise SystemExit(1)
