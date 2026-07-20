import json
with open('/tmp/byparr_analysis/ldlc.json') as f:
    body = json.load(f)['solution']['response']
idx = body.find('li.pdt-item')
print(body[idx:idx+2000])
