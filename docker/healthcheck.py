"""Check the running app's public health response without initializing it."""
import json
import urllib.request

with urllib.request.urlopen('http://127.0.0.1:8787/api/health', timeout=3) as response:
    health = json.load(response)
    if response.status != 200 or health.get('ok') is not True or health.get('service') != 'artex':
        raise SystemExit(1)
