#!/usr/bin/env python3
"""Configure native private R2 settings over loopback; never display credentials."""
from pathlib import Path
from urllib.request import Request, urlopen
from urllib.error import HTTPError
import base64
import json
import os
import uuid

ROOT = Path('/srv/infinite-canvas')
BASE = 'http://127.0.0.1:13800'


def main():
    os.umask(0o077)
    env = dict(line.split('=', 1) for line in (ROOT / '.env').read_text().splitlines() if line and not line.startswith('#'))
    token = ''

    def call(path, body=None, method=None, raw=False, headers=None):
        request_headers = {'Authorization': 'Bearer ' + token} if token else {}
        if headers:
            request_headers.update(headers)
        if body is not None and not isinstance(body, bytes):
            body = json.dumps(body).encode()
            request_headers['Content-Type'] = 'application/json'
        req = Request(BASE + path, data=body, headers=request_headers, method=method or ('POST' if body is not None else 'GET'))
        with urlopen(req, timeout=90) as response:
            data = response.read()
            if raw:
                return response.status, response.headers, data
            result = json.loads(data)
            if result.get('code') != 0:
                raise RuntimeError('Canvas API rejected ' + path)
            return result['data']

    session = call('/api/admin/login', {'username': env['ADMIN_USERNAME'], 'password': env['ADMIN_PASSWORD']})
    token = session['token']
    settings = call('/api/admin/settings')
    # Fresh installation only: do not reuse redacted provider secrets during an update.
    storage = settings.setdefault('private', {}).setdefault('storage', {})
    if storage.get('providers'):
        if not (ROOT / 'bootstrap-state.json').exists() or any(p.get('id') != 'happykai-r2' for p in storage['providers']):
            raise RuntimeError('Existing storage providers require review before bootstrap')
    provider = {
        'id': 'happykai-r2', 'name': 'HappyKai R2', 'type': 's3',
        'endpoint': env['S3_ENDPOINT'], 'region': 'auto', 'bucket': env['S3_BUCKET'],
        'bucketInHost': False, 'accessKeyId': env['S3_ACCESS_KEY_ID'],
        'secretAccessKey': env['S3_SECRET_ACCESS_KEY'], 'publicBaseUrl': '',
        'pathPrefix': 'canvas', 'weight': 1, 'enabled': True,
    }
    storage.update({'providers': [provider], 'mode': 'server_sqlite_s3',
                    'allowUserProvider': False, 'allowUserGlobalProvider': True, 'autoSyncAllAssets': True})
    public = settings.setdefault('public', {})
    public.setdefault('auth', {}).update({'allowRegister': False, 'linuxDo': {'enabled': False}})
    public.setdefault('modelChannel', {}).update({'allowCustomChannel': True, 'allowUserRemoteChannel': False})
    call('/api/admin/settings', settings)
    state = {'native_settings_configured': True, 'bucket_private_url': True, 'r2_rw_test_passed': False}
    (ROOT / 'bootstrap-state.json').write_text(json.dumps(state, indent=2) + '\n')

    # Tiny PNG exercises the actual S3 signing, token scope, streaming and deletion.
    image = base64.b64decode('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+a1H8AAAAASUVORK5CYII=')
    boundary = 'canvas-test-' + uuid.uuid4().hex
    body = ('--' + boundary + '\r\nContent-Disposition: form-data; name="file"; filename="deployment-smoke.png"\r\nContent-Type: image/png\r\n\r\n').encode() + image + ('\r\n--' + boundary + '--\r\n').encode()
    uploaded = call('/api/v1/files', body, headers={'Content-Type': 'multipart/form-data; boundary=' + boundary})
    state['smoke_object_id'] = uploaded['id']
    (ROOT / 'bootstrap-state.json').write_text(json.dumps(state, indent=2) + '\n')
    try:
        path = '/api/files/' + uploaded['id'] + '/content'
        code, headers, data = call(path, raw=True)
        if code != 200 or data != image or 'no-store' not in headers.get('Cache-Control', ''):
            raise RuntimeError('Private R2 roundtrip failed')
        code, headers, data = call(path, raw=True, headers={'Range': 'bytes=0-7'})
        if code != 206 or data != image[:8] or not headers.get('Content-Range', '').startswith('bytes 0-7/'):
            raise RuntimeError('R2 range read failed')
        try:
            urlopen(BASE + path, timeout=15)
            raise RuntimeError('Anonymous download was allowed')
        except HTTPError as exc:
            if exc.code != 401:
                raise RuntimeError('Unexpected anonymous response')
        state['r2_rw_test_passed'] = True
    finally:
        call('/api/v1/files/' + uploaded['id'], method='DELETE')
        state['smoke_object_deleted'] = True
        state.pop('smoke_object_id', None)
        (ROOT / 'bootstrap-state.json').write_text(json.dumps(state, indent=2) + '\n')
    for p in (ROOT / 'data').glob('*'):
        if p.is_file():
            os.chmod(p, 0o600)
    print(json.dumps(state))


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        print('STOP: ' + type(error).__name__ + ' (no credentials displayed)')
        raise SystemExit(1)
