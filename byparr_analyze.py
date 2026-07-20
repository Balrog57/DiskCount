#!/usr/bin/env python3
"""Analyze Byparr-fetched HTML to find product containers, titles, prices, links."""
import json, re, os, sys

OUT = "/tmp/byparr_analysis"

def analyze(name, body):
    """Extract product structure from HTML."""
    print(f"\n{'='*60}")
    print(f"  {name}")
    print(f"{'='*60}")
    
    # Count product-like containers
    containers = {}
    for m in re.finditer(r'<([a-z]+)[^>]*class="([^"]*)"', body):
        tag, cls = m.group(1), m.group(2)
        if any(w in cls.lower() for w in ['product', 'article', 'item', 'card', 'tile', 'listing']):
            key = f"{tag}.{cls.split()[0]}"
            containers[key] = containers.get(key, 0) + 1
    if containers:
        print(f"Product containers:")
        for k, v in sorted(containers.items(), key=lambda x: -x[1]):
            if v >= 3:
                print(f"  {k}: {v} occurrences")
    
    # Find price patterns
    prices = re.findall(r'[\d\s]+[€$][\d,\.\s]*', body)
    if prices:
        print(f"\nPrice samples (first 5):")
        for p in prices[:5]:
            print(f"  {p.strip()}")
    
    # Find link patterns pointing to product pages
    links = re.findall(r'href="([^"]*)"', body)
    product_links = [l for l in links if re.search(r'/(product|fiche|p/|detail|item|ref|-\d+\.)', l, re.I)]
    if product_links:
        print(f"\nProduct links (first 3):")
        for l in product_links[:3]:
            print(f"  {l[:80]}")

for fname in sorted(os.listdir(OUT)):
    if not fname.endswith('.json'):
        continue
    name = fname.replace('.json', '')
    with open(os.path.join(OUT, fname)) as f:
        try:
            d = json.load(f)
        except:
            continue
    body = d.get('solution', {}).get('response', '')
    if not body:
        status = d.get('solution', {}).get('status', '?')
        print(f"{name}: no body (status={status})")
        continue
    analyze(name, body)
