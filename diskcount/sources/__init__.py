from __future__ import annotations

from diskcount.config import Settings

from .base import Source
from .dealabs import DealabsRssSource
from .diskprices import DiskPricesSource
from .ebay import EbayBrowseSource
from .feed import FeedSource
from .keepa import KeepaSource


def build_sources(settings: Settings) -> list[Source]:
    sources: list[Source] = [DiskPricesSource(settings.diskprices_url)]
    if settings.dealabs_rss_urls:
        sources.append(DealabsRssSource(settings.dealabs_rss_urls))
    if settings.idealo_feed_urls:
        sources.append(FeedSource("idealo", settings.idealo_feed_urls, default_condition="new"))
    if settings.ledenicheur_feed_urls:
        sources.append(FeedSource("ledenicheur", settings.ledenicheur_feed_urls, default_condition="new"))
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
    "DealabsRssSource",
    "FeedSource",
    "EbayBrowseSource",
    "KeepaSource",
]
