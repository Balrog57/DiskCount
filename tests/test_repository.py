from datetime import timedelta
from decimal import Decimal

from diskcount.db import Repository, create_db_engine
from diskcount.domain import Deal, utc_now


def test_repository_baseline_and_product_deduplication() -> None:
    repository = Repository(create_db_engine("sqlite:///:memory:"))
    repository.init()
    now = utc_now()
    deal = Deal(
        source="diskprices",
        external_id="B0ABCDEFGH",
        title="WD 16 To",
        url="https://www.amazon.fr/dp/B0ABCDEFGH",
        price_eur=Decimal("320.00"),
        price_per_tb=Decimal("20.00"),
        capacity_tb=Decimal("16"),
        condition="new",
        media_type="rotational",
    )

    for days, price_per_tb in ((3, "30.00"), (2, "40.00"), (1, "20.00")):
        observed = now - timedelta(days=days)
        repository.record_observation(
            Deal(
                source=deal.source,
                external_id=deal.external_id,
                title=deal.title,
                url=deal.url,
                price_eur=Decimal(price_per_tb) * deal.capacity_tb,
                price_per_tb=Decimal(price_per_tb),
                capacity_tb=deal.capacity_tb,
                condition=deal.condition,
                media_type=deal.media_type,
            ),
            observed_at=observed,
        )

    assert repository.baseline_price_per_tb(deal.product_id, before=now) == Decimal("30.00")
    counts = repository.counts()
    assert counts["products"] == 1
    assert counts["observations"] == 3


def test_repository_set_alert_max_price() -> None:
    repository = Repository(create_db_engine("sqlite:///:memory:"))
    repository.init()
    alert = repository.create_alert(
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

    assert not repository.set_alert_max_price_per_tb(2002, alert.id, Decimal("18.50"))
    assert repository.set_alert_max_price_per_tb(1001, alert.id, Decimal("18.50"))
    updated = repository.list_alerts(owner_user_id=1001)[0]
    assert updated.max_price_per_tb == Decimal("18.50")


def test_repository_alerts_are_owned_per_user() -> None:
    repository = Repository(create_db_engine("sqlite:///:memory:"))
    repository.init()
    first = repository.create_alert(
        chat_id=42,
        owner_user_id=1001,
        name="NAS User",
        min_capacity_tb=16,
        max_capacity_tb=None,
        conditions=["new"],
        media_types=["rotational"],
        sources=["diskprices"],
        max_price_per_tb=Decimal("20.00"),
        min_discount_pct=5.0,
        cooldown_hours=24,
    )
    second = repository.create_alert(
        chat_id=42,
        owner_user_id=2002,
        name="NAS Invite",
        min_capacity_tb=18,
        max_capacity_tb=None,
        conditions=["used"],
        media_types=["rotational"],
        sources=["diskprices"],
        max_price_per_tb=Decimal("18.00"),
        min_discount_pct=5.0,
        cooldown_hours=24,
    )

    assert [alert.id for alert in repository.list_alerts(owner_user_id=1001)] == [first.id]
    assert [alert.id for alert in repository.list_alerts(owner_user_id=2002)] == [second.id]
    assert not repository.delete_alert(1001, second.id)
    assert repository.delete_alert(2002, second.id)


def test_repository_authorized_users() -> None:
    repository = Repository(create_db_engine("sqlite:///:memory:"))
    repository.init()

    user = repository.upsert_authorized_user(123, "User")
    assert user.telegram_user_id == 123
    assert user.label == "User"
    assert repository.is_user_allowed(123)
    assert len(repository.list_authorized_users()) == 1

    assert repository.revoke_authorized_user(123)
    assert not repository.is_user_allowed(123)
    assert repository.list_authorized_users() == []
    assert len(repository.list_authorized_users(include_disabled=True)) == 1
