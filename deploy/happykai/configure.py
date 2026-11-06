#!/usr/bin/env python3
"""Interactive, server-only configuration. Never pass real keys as arguments."""
from pathlib import Path
import getpass
import os
import re
import secrets
import sys

ROOT = Path('/srv/infinite-canvas')


def read_env(path):
    return dict(line.split('=', 1) for line in path.read_text().splitlines() if line and not line.startswith('#'))


def main():
    if not sys.stdin.isatty():
        raise SystemExit('Run this script in an interactive terminal; do not pipe credentials.')
    ROOT.mkdir(parents=True, exist_ok=True, mode=0o700)
    os.chmod(ROOT, 0o700)
    path = ROOT / '.env'
    env = read_env(path) if path.exists() else {}
    if env and not all(env.get(k) for k in ['JWT_SECRET', 'ADMIN_PASSWORD']):
        raise SystemExit('Incomplete existing internal secrets; inspect locally before proceeding.')
    endpoint = input('R2 S3 Endpoint: ').strip().rstrip('/')
    access = getpass.getpass('R2 Access Key ID (hidden): ').strip()
    secret = getpass.getpass('R2 Secret Access Key (hidden): ').strip()
    if not re.fullmatch(r'https://[a-fA-F0-9]{32}(?:\.(?:eu|fedramp))?\.r2\.cloudflarestorage\.com', endpoint):
        raise SystemExit('Use the HTTPS R2 account endpoint without a bucket path.')
    if not re.fullmatch(r'[a-fA-F0-9]{32}', access) or not re.fullmatch(r'[a-fA-F0-9]{64}', secret):
        raise SystemExit('R2 keys must be a 32-character ID and 64-character secret.')
    env.update({
        'CANVAS_IMAGE': env.get('CANVAS_IMAGE', 'happykai/infinite-canvas:staged'),
        'JWT_SECRET': env.get('JWT_SECRET', secrets.token_urlsafe(48)),
        'ADMIN_USERNAME': env.get('ADMIN_USERNAME', 'canvas-recovery'),
        'ADMIN_PASSWORD': env.get('ADMIN_PASSWORD', secrets.token_urlsafe(30)),
        'APP_URL': 'https://canvas.happykai.cn',
        'NEW_API_BASE_URL': 'http://new-api:3000',
        'NEW_API_PUBLIC_URL': 'https://api.happykai.cn',
        'S3_ENDPOINT': endpoint, 'S3_BUCKET': 'happykai-canvas-media',
        'S3_ACCESS_KEY_ID': access, 'S3_SECRET_ACCESS_KEY': secret,
    })
    if any('\n' in v or '\r' in v for v in env.values()):
        raise SystemExit('Configuration values cannot contain newlines.')
    temp = ROOT / '.env.tmp'
    fd = os.open(temp, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(fd, 'w') as f:
        f.write(''.join(k + '=' + v + '\n' for k, v in env.items()))
    os.chmod(temp, 0o600)
    temp.replace(path)
    print('Configuration saved with mode 0600. Keys were not displayed. Services were not started.')


if __name__ == '__main__':
    main()
