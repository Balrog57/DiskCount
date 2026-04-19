from __future__ import annotations

from diskcount.config import Settings

from .base import Source
from .dealabs import DealabsRssSource
from .diskprices import DiskPricesSource
from .ebay import EbayBrowseSource
from .feed import FeedSource
from .html_pages import HtmlDealSource
from .keepa import KeepaSource
from .pricepergig import PricePerGigSource
from .pricepertb import PricePerTBSource


def build_sources(settings: Settings) -> list[Source]:
    sources: list[Source] = [DiskPricesSource(settings.diskprices_url)]
    if settings.pricepergig_enabled:
        sources.append(
            PricePerGigSource(
                settings.pricepergig_api_url,
                marketplace=settings.pricepergig_marketplace,
                max_results=settings.pricepergig_max_results,
            )
        )
    if settings.pricepertb_urls:
        sources.append(
            PricePerTBSource(
                settings.pricepertb_urls,
                headless_fallback=settings.source_headless_fallback,
                user_agent=settings.user_agent,
                timeout_seconds=settings.request_timeout_seconds,
            )
        )
    if settings.dealabs_rss_urls:
        sources.append(DealabsRssSource(settings.dealabs_rss_urls))
    if settings.idealo_feed_urls:
        sources.append(FeedSource("idealo", settings.idealo_feed_urls, default_condition="new"))
    if settings.idealo_page_urls:
        sources.append(
            HtmlDealSource(
                "idealo",
                settings.idealo_page_urls,
                default_condition="new",
                headless_fallback=settings.source_headless_fallback,
                user_agent=settings.user_agent,
                timeout_seconds=settings.request_timeout_seconds,
            )
        )
    if settings.ledenicheur_feed_urls:
        sources.append(FeedSource("ledenicheur", settings.ledenicheur_feed_urls, default_condition="new"))
    if settings.ledenicheur_page_urls:
        sources.append(
            HtmlDealSource(
                "ledenicheur",
                settings.ledenicheur_page_urls,
                default_condition="new",
                headless_fallback=settings.source_headless_fallback,
                user_agent=settings.user_agent,
                timeout_seconds=settings.request_timeout_seconds,
            )
        )
    if settings.leboncoin_feed_urls:
        sources.append(FeedSource("leboncoin", settings.leboncoin_feed_urls, default_condition="used"))
    if settings.ebay_client_id and settings.ebay_client_secret and settings.ebay_search_queries:
        sources.append(
            EbayBrowseSource(
                settings.ebay_client_id,
                settings.ebay_client_secret,
                settings.ebay_search_queries,
                marketplace_id=settings.ebay_marketplace_id,
                scope=settings.ebay_scope,
                limit=settings.ebay_search_limit,
                category_ids=settings.ebay_category_ids,
            )
        )
    if settings.keepa_api_key and settings.keepa_asins:
        sources.append(KeepaSource(settings.keepa_api_key, settings.keepa_asins, settings.keepa_domain))
    return sources


__all__ = [
    "Source",
    "build_sources",
    "DiskPricesSource",
    "PricePerGigSource",
    "PricePerTBSource",
    "DealabsRssSource",
    "FeedSource",
    "HtmlDealSource",
    "EbayBrowseSource",
    "KeepaSource",
]
