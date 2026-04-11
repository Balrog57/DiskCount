from decimal import Decimal

from diskcount.config import Settings
from diskcount.db import Repository, create_db_engine
from diskcount.domain import Deal
from diskcount.scanner import Scanner


class FakeSource:
    name = "diskprices"

    async def fetch(self, client):
        return [
            Deal(
                source="diskprices",
                external_id="B0ABCDEFGH",
                title="WD 16 To",
                url="https://www.amazon.fr/dp/B0ABCDEFGH",
                price_eur=Decimal("304.00"),
                price_per_tb=Decimal("19.00"),
                capacity_tb=Decimal("16"),
                condition="new",
                media_type="rotational",
            )
        ]


async def test_dry_run_reports_without_persistence() -> None:
    repository = Repository(create_db_engine("sqlite:///:memory:"))
    repository.init()
    repository.create_alert(
        chat_id=42,
        owner_user_id=1001,
        name="NAS",
        min_capacity_tb=16,
        max_capacity_tb=None,
        conditions=["new"],
        media_types=["rotational"],
        sources=["diskprices"],
        max_price_per_tb=Decimal("20.00"),
        min_discount_pct=5.0,
        cooldown_hours=24,
    )

    scanner = Scanner(Settings(), repository, [FakeSource()])
    report = await scanner.run_once(dry_run=True, target_owner_user_id=1001)

    assert report.fetched == 1
    assert report.matched == 1
    assert report.dry_run_notifications == 1
    assert repository.counts()["products"] == 0
