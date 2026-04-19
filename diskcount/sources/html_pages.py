from __future__ import annotations

import json
from decimal import Decimal
from typing import Any, Iterable
from urllib.parse import urljoin

import httpx
from bs4 import BeautifulSoup

from diskcount.domain import Condition, Deal
from diskcount.parsing import normalize_condition, normalize_media_type, parse_capacity_tb, parse_price_eur


class HtmlDealSource:
    def __init__(
        self,
        name: str,
        urls: list[str],
        default_condition: Condition | None = None,
        headless_fallback: bool = True,
        user_agent: str = "DiskCountBot/0.1",
        timeout_seconds: float = 30.0,
    ) -> None:
        self.name = name
        self.urls = urls
        self.default_condition = default_condition
        self.headless_fallback = headless_fallback
        self.user_agent = user_agent
        self.timeout_seconds = timeout_seconds

    async def fetch(self, client: httpx.AsyncClient) -> list[Deal]:
        deals: list[Deal] = []
        unresolved_urls: list[str] = []
        for url in self.urls:
            response = await client.get(url)
            if response.status_code in {403, 429}:
                response.raise_for_status()
            response.raise_for_status()
            parsed = parse_html_deals(response.text, self.name, url, self.default_condition)
            if parsed:
                deals.extend(parsed)
            elif self.headless_fallback:
                unresolved_urls.append(url)

        if unresolved_urls:
            rendered_pages = await render_pages(
                unresolved_urls,
                user_agent=self.user_agent,
                timeout_seconds=self.timeout_seconds,
            )
            for url, html in rendered_pages.items():
                deals.extend(parse_html_deals(html, self.name, url, self.default_condition))

        return deals


async def render_pages(urls: list[str], user_agent: str, timeout_seconds: float) -> dict[str, str]:
    try:
        from playwright.async_api import TimeoutError as PlaywrightTimeoutError
        from playwright.async_api import async_playwright
    except ImportError as exc:
        raise RuntimeError(
            "Playwright is required for headless page rendering. Install dependencies with "
            "'pip install -e .[dev]' or 'pip install playwright', then run "
            "'python -m playwright install chromium'."
        ) from exc

    timeout_ms = int(timeout_seconds * 1000)
    rendered: dict[str, str] = {}
    async with async_playwright() as playwright:
        browser = await playwright.chromium.launch(headless=True)
        try:
            context = await browser.new_context(user_agent=user_agent, locale="fr-FR")
            try:
                for url in urls:
                    page = await context.new_page()
                    try:
                        await page.goto(url, wait_until="domcontentloaded", timeout=timeout_ms)
                        try:
                            await page.wait_for_load_state("networkidle", timeout=min(timeout_ms, 10000))
                        except PlaywrightTimeoutError:
                            pass
                        rendered[url] = await page.content()
                    finally:
                        await page.close()
            finally:
                await context.close()
        finally:
            await browser.close()
    return rendered


def parse_html_deals(
    html: str,
    source_name: str,
    base_url: str,
    default_condition: Condition | None = None,
) -> list[Deal]:
    soup = BeautifulSoup(html, "html.parser")
    deals = _parse_json_ld_deals(soup, source_name, base_url, default_condition)
    if deals:
        return deals
    return _parse_anchor_deals(soup, source_name, base_url, default_condition)


def _parse_json_ld_deals(
    soup: BeautifulSoup,
    source_name: str,
    base_url: str,
    default_condition: Condition | None,
) -> list[Deal]:
    deals: list[Deal] = []
    for obj in _json_ld_objects(soup):
        for product in _iter_products(obj):
            deal = _deal_from_product(product, source_name, base_url, default_condition)
            if deal is not None:
                deals.append(deal)
    return _dedupe_deals(deals)


def _json_ld_objects(soup: BeautifulSoup) -> Iterable[Any]:
    for script in soup.select('script[type="application/ld+json"]'):
        text = script.string or script.get_text("", strip=True)
        if not text:
            continue
        try:
            payload = json.loads(text)
        except json.JSONDecodeError:
            continue
        if isinstance(payload, list):
            yield from payload
        else:
            yield payload


def _iter_products(obj: Any) -> Iterable[dict[str, Any]]:
    if isinstance(obj, list):
        for item in obj:
            yield from _iter_products(item)
        return
    if not isinstance(obj, dict):
        return

    obj_type = obj.get("@type")
    types = {obj_type} if isinstance(obj_type, str) else set(obj_type or [])
    if "Product" in types:
        yield obj

    graph = obj.get("@graph")
    if graph:
        yield from _iter_products(graph)

    for item in obj.get("itemListElement") or []:
        if isinstance(item, dict):
            nested = item.get("item") or item
            yield from _iter_products(nested)


def _deal_from_product(
    product: dict[str, Any],
    source_name: str,
    base_url: str,
    default_condition: Condition | None,
) -> Deal | None:
    title = str(product.get("name") or "").strip()
    offers = product.get("offers") or {}
    if isinstance(offers, list):
        offers = offers[0] if offers else {}
    if not isinstance(offers, dict):
        offers = {}

    price_eur = parse_price_eur(str(offers.get("price") or product.get("price") or ""))
    capacity_tb = parse_capacity_tb(title)
    media_type = normalize_media_type(title)
    if not title or price_eur is None or capacity_tb is None or media_type is None:
        return None

    currency = str(offers.get("priceCurrency") or "").upper()
    if currency and currency != "EUR":
        return None

    url = str(offers.get("url") or product.get("url") or base_url)
    condition_text = str(offers.get("itemCondition") or product.get("itemCondition") or "")
    condition = normalize_condition(condition_text) or default_condition
    price_per_tb = (price_eur / capacity_tb).quantize(Decimal("0.01"))
    return Deal(
        source=source_name,
        external_id=str(product.get("sku") or product.get("mpn") or "").strip() or None,
        title=title,
        url=urljoin(base_url, url),
        price_eur=price_eur.quantize(Decimal("0.01")),
        price_per_tb=price_per_tb,
        capacity_tb=capacity_tb,
        condition=condition,
        media_type=media_type,
        raw={"parser": "json_ld"},
    )


def _parse_anchor_deals(
    soup: BeautifulSoup,
    source_name: str,
    base_url: str,
    default_condition: Condition | None,
) -> list[Deal]:
    deals: list[Deal] = []
    for anchor in soup.find_all("a", href=True):
        title = anchor.get_text(" ", strip=True)
        if len(title) < 8:
            continue
        container = anchor
        for _ in range(3):
            if container.parent is None:
                break
            container = container.parent
        haystack = container.get_text(" ", strip=True)
        price_eur = parse_price_eur(haystack)
        capacity_tb = parse_capacity_tb(haystack)
        media_type = normalize_media_type(haystack)
        if price_eur is None or capacity_tb is None or capacity_tb <= 0 or media_type is None:
            continue
        condition = normalize_condition(haystack) or default_condition
        deals.append(
            Deal(
                source=source_name,
                external_id=None,
                title=title,
                url=urljoin(base_url, str(anchor["href"])),
                price_eur=price_eur.quantize(Decimal("0.01")),
                price_per_tb=(price_eur / capacity_tb).quantize(Decimal("0.01")),
                capacity_tb=capacity_tb,
                condition=condition,
                media_type=media_type,
                raw={"parser": "anchor"},
            )
        )
    return _dedupe_deals(deals)


def _dedupe_deals(deals: list[Deal]) -> list[Deal]:
    seen: set[tuple[str, str, Decimal, Decimal]] = set()
    deduped: list[Deal] = []
    for deal in deals:
        key = (deal.source, deal.url, deal.price_eur, deal.capacity_tb)
        if key in seen:
            continue
        seen.add(key)
        deduped.append(deal)
    return deduped
