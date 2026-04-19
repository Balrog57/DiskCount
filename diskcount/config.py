from __future__ import annotations

from functools import lru_cache
from typing import Annotated, Any

from pydantic import Field, field_validator
from pydantic_settings import BaseSettings, NoDecode, SettingsConfigDict


def _split_csv(value: Any) -> list[str]:
    if value is None or value == "":
        return []
    if isinstance(value, list):
        return [str(item).strip() for item in value if str(item).strip()]
    return [item.strip() for item in str(value).split(",") if item.strip()]


def _split_int_csv(value: Any) -> list[int]:
    return [int(item) for item in _split_csv(value)]


class Settings(BaseSettings):
    model_config = SettingsConfigDict(env_file=".env", env_file_encoding="utf-8", extra="ignore")

    telegram_bot_token: str = ""
    telegram_allowed_user_ids: Annotated[list[int], NoDecode] = Field(default_factory=list)
    telegram_admin_user_ids: Annotated[list[int], NoDecode] = Field(default_factory=list)

    database_url: str = "sqlite:///./diskcount.sqlite3"
    poll_interval_seconds: int = 14400
    request_timeout_seconds: float = 30.0
    user_agent: str = "DiskCountBot/0.1"

    diskprices_url: str = "https://diskprices.com/?locale=fr"
    pricepergig_enabled: bool = True
    pricepergig_api_url: str = "https://api.pricepergig.com/drives"
    pricepergig_marketplace: str = "amazon.fr"
    pricepergig_max_results: int = 200
    pricepertb_urls: Annotated[list[str], NoDecode] = Field(default_factory=lambda: ["https://pricepertb.com/fr"])
    dealabs_rss_urls: Annotated[list[str], NoDecode] = Field(default_factory=list)
    idealo_feed_urls: Annotated[list[str], NoDecode] = Field(default_factory=list)
    idealo_page_urls: Annotated[list[str], NoDecode] = Field(default_factory=list)
    ledenicheur_feed_urls: Annotated[list[str], NoDecode] = Field(default_factory=list)
    ledenicheur_page_urls: Annotated[list[str], NoDecode] = Field(default_factory=list)
    leboncoin_feed_urls: Annotated[list[str], NoDecode] = Field(default_factory=list)
    source_headless_fallback: bool = True

    keepa_api_key: str = ""
    keepa_asins: Annotated[list[str], NoDecode] = Field(default_factory=list)
    keepa_domain: int = 4

    ebay_client_id: str = ""
    ebay_client_secret: str = ""
    ebay_search_queries: Annotated[list[str], NoDecode] = Field(default_factory=list)
    ebay_marketplace_id: str = "EBAY_FR"
    ebay_scope: str = "https://api.ebay.com/oauth/api_scope"
    ebay_search_limit: int = 50
    ebay_category_ids: Annotated[list[str], NoDecode] = Field(default_factory=list)

    notification_price_drop_pct: float = 2.0
    telegram_message_delay_seconds: float = 0.5

    @field_validator("telegram_allowed_user_ids", "telegram_admin_user_ids", mode="before")
    @classmethod
    def parse_allowed_user_ids(cls, value: Any) -> list[int]:
        return _split_int_csv(value)

    @field_validator(
        "dealabs_rss_urls",
        "pricepertb_urls",
        "idealo_feed_urls",
        "idealo_page_urls",
        "ledenicheur_feed_urls",
        "ledenicheur_page_urls",
        "leboncoin_feed_urls",
        "keepa_asins",
        "ebay_search_queries",
        "ebay_category_ids",
        mode="before",
    )
    @classmethod
    def parse_csv_list(cls, value: Any) -> list[str]:
        return _split_csv(value)


@lru_cache
def get_settings() -> Settings:
    return Settings()
