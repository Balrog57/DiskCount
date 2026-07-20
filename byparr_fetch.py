#!/usr/bin/env python3
"""Fetch all anti-bot source pages via Byparr and extract product structure."""
import json, subprocess, sys, re, os

SOURCES = {
    "boulanger": "https://www.boulanger.com/c/disque-dur",
    "ldlc": "https://www.ldlc.com/recherche/disque+dur/",
    "cdiscount": "https://www.cdiscount.com/informatique/disque-dur-ssd-interne/l-1077302.html",
    "mindfactory": "https://www.mindfactory.de/HDD+SSD",
    "computeruniverse": "https://www.computeruniverse.de/de/c/hardware/speicher/festplatten",
    "proshop": "https://www.proshop.de/4316/festplatten-hdd",
    "rakuten": "https://fr.shopping.rakuten.com/s/disque+dur",
    "fnac": "https://www.fnac.com/SearchResult/ResultList.aspx?SearchItem=disque+dur+interne",
    "backmarket": "https://www.back-market.fr/c/disques-durs",
}

OUT = "/tmp/byparr_analysis"
os.makedirs(OUT, exist_ok=True)

for name, url in SOURCES.items():
    payload = json.dumps({"cmd": "request.get", "url": url, "maxTimeout": 30000})
    path = os.path.join(OUT, f"{name}.json")
    print(f"Fetching {name}...", end=" ", flush=True)
    result = subprocess.run(
        ["curl", "-s", "-m", "45", "-X", "POST", "http://localhost:8191/v1",
         "-H", "Content-Type: application/json", "-d", payload],
        capture_output=True, text=True
    )
    if result.returncode != 0 or not result.stdout.strip():
        print(f"FAILED: {result.stderr[:100]}")
        continue
    with open(path, "w") as f:
        f.write(result.stdout)
    try:
        d = json.loads(result.stdout)
        status = d.get("solution", {}).get("status", "?")
        n_links = len(re.findall(r'href="[^"]*"', d.get("solution", {}).get("response", "")))
        print(f"OK status={status} links~{n_links}")
    except Exception as e:
        print(f"parse error: {e}")

print(f"\nDone. Files in {OUT}")
