from __future__ import annotations

import json
import statistics
from datetime import datetime, timedelta
from decimal import Decimal
from pathlib import Path
from typing import Iterable

from sqlalchemy import BigInteger, Boolean, DateTime, Float, ForeignKey, Integer, Numeric, String, Text, create_engine, func, inspect, select, text
from sqlalchemy.engine import Engine
from sqlalchemy.orm import DeclarativeBase, Mapped, Session, mapped_column, sessionmaker

from .domain import Deal, utc_now


class Base(DeclarativeBase):
    pass


class Subscriber(Base):
    __tablename__ = "subscribers"

    chat_id: Mapped[int] = mapped_column(BigInteger, primary_key=True)
    username: Mapped[str | None] = mapped_column(String(255), nullable=True)
    first_seen_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utc_now)
    last_seen_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utc_now)
    enabled: Mapped[bool] = mapped_column(Boolean, default=True)


class AuthorizedUser(Base):
    __tablename__ = "authorized_users"

    telegram_user_id: Mapped[int] = mapped_column(BigInteger, primary_key=True)
    label: Mapped[str] = mapped_column(String(120), nullable=False)
    is_admin: Mapped[bool] = mapped_column(Boolean, default=False)
    enabled: Mapped[bool] = mapped_column(Boolean, default=True)
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utc_now)
    updated_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utc_now)


class Alert(Base):
    __tablename__ = "alerts"

    id: Mapped[int] = mapped_column(Integer, primary_key=True, autoincrement=True)
    chat_id: Mapped[int] = mapped_column(BigInteger, index=True, nullable=False)
    owner_user_id: Mapped[int] = mapped_column(BigInteger, index=True, nullable=False)
    name: Mapped[str] = mapped_column(String(120), nullable=False)
    min_capacity_tb: Mapped[float | None] = mapped_column(Float, nullable=True)
    max_capacity_tb: Mapped[float | None] = mapped_column(Float, nullable=True)
    capacity_presets_json: Mapped[str] = mapped_column(Text, default="[]")
    conditions_json: Mapped[str] = mapped_column(Text, default="[]")
    media_types_json: Mapped[str] = mapped_column(Text, default="[]")
    drive_categories_json: Mapped[str] = mapped_column(Text, default="[]")
    interfaces_json: Mapped[str] = mapped_column(Text, default="[]")
    sources_json: Mapped[str] = mapped_column(Text, default="[]")
    max_price_per_tb: Mapped[Decimal | None] = mapped_column(Numeric(10, 2), nullable=True)
    min_discount_pct: Mapped[float] = mapped_column(Float, default=5.0)
    cooldown_hours: Mapped[int] = mapped_column(Integer, default=24)
    enabled: Mapped[bool] = mapped_column(Boolean, default=True)
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utc_now)
    updated_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utc_now)

    @property
    def conditions(self) -> list[str]:
        return json.loads(self.conditions_json or "[]")

    @property
    def media_types(self) -> list[str]:
        return json.loads(self.media_types_json or "[]")

    @property
    def drive_categories(self) -> list[str]:
        return json.loads(self.drive_categories_json or "[]")

    @property
    def interfaces(self) -> list[str]:
        return json.loads(self.interfaces_json or "[]")

    @property
    def sources(self) -> list[str]:
        return json.loads(self.sources_json or "[]")

    @property
    def capacity_presets(self) -> list[str]:
        return json.loads(self.capacity_presets_json or "[]")


class Product(Base):
    __tablename__ = "products"

    id: Mapped[str] = mapped_column(String(80), primary_key=True)
    source: Mapped[str] = mapped_column(String(40), index=True)
    external_id: Mapped[str | None] = mapped_column(String(255), nullable=True)
    title: Mapped[str] = mapped_column(Text)
    url: Mapped[str] = mapped_column(Text)
    capacity_tb: Mapped[Decimal] = mapped_column(Numeric(10, 3))
    condition: Mapped[str | None] = mapped_column(String(20), nullable=True)
    media_type: Mapped[str | None] = mapped_column(String(30), nullable=True)
    form_factor: Mapped[str | None] = mapped_column(String(120), nullable=True)
    technology: Mapped[str | None] = mapped_column(String(120), nullable=True)
    drive_category: Mapped[str | None] = mapped_column(String(40), nullable=True)
    interfaces_json: Mapped[str] = mapped_column(Text, default="[]")
    first_seen_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utc_now)
    last_seen_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utc_now)


class PriceObservation(Base):
    __tablename__ = "price_observations"

    id: Mapped[int] = mapped_column(Integer, primary_key=True, autoincrement=True)
    product_id: Mapped[str] = mapped_column(String(80), ForeignKey("products.id"), index=True)
    source: Mapped[str] = mapped_column(String(40), index=True)
    observed_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), index=True)
    price_eur: Mapped[Decimal] = mapped_column(Numeric(10, 2))
    price_per_tb: Mapped[Decimal] = mapped_column(Numeric(10, 2))
    raw_json: Mapped[str] = mapped_column(Text, default="{}")


class Notification(Base):
    __tablename__ = "notifications"

    id: Mapped[int] = mapped_column(Integer, primary_key=True, autoincrement=True)
    alert_id: Mapped[int] = mapped_column(Integer, ForeignKey("alerts.id"), index=True)
    product_id: Mapped[str] = mapped_column(String(80), ForeignKey("products.id"), index=True)
    sent_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), index=True)
    price_eur: Mapped[Decimal] = mapped_column(Numeric(10, 2))
    price_per_tb: Mapped[Decimal] = mapped_column(Numeric(10, 2))
    discount_pct: Mapped[Decimal | None] = mapped_column(Numeric(6, 2), nullable=True)
    reason: Mapped[str] = mapped_column(String(80))
    title: Mapped[str] = mapped_column(Text)
    url: Mapped[str] = mapped_column(Text)


def _ensure_sqlite_parent(database_url: str) -> None:
    if not database_url.startswith("sqlite:///"):
        return
    path_value = database_url.removeprefix("sqlite:///")
    if path_value in (":memory:", ""):
        return
    path = Path(path_value)
    if not path.is_absolute():
        path = Path.cwd() / path
    path.parent.mkdir(parents=True, exist_ok=True)


def create_db_engine(database_url: str) -> Engine:
    _ensure_sqlite_parent(database_url)
    connect_args = {"check_same_thread": False} if database_url.startswith("sqlite") else {}
    return create_engine(database_url, connect_args=connect_args, future=True)


def init_db(engine: Engine) -> None:
    Base.metadata.create_all(engine)
    migrate_schema(engine)


def migrate_schema(engine: Engine) -> None:
    inspector = inspect(engine)
    table_names = set(inspector.get_table_names())
    if "alerts" in table_names:
        columns = {column["name"] for column in inspector.get_columns("alerts")}
        with engine.begin() as connection:
            if "owner_user_id" not in columns:
                connection.execute(text("ALTER TABLE alerts ADD COLUMN owner_user_id BIGINT"))
                connection.execute(text("UPDATE alerts SET owner_user_id = chat_id WHERE owner_user_id IS NULL"))
                connection.execute(text("CREATE INDEX IF NOT EXISTS ix_alerts_owner_user_id ON alerts (owner_user_id)"))
            if "drive_categories_json" not in columns:
                connection.execute(text("ALTER TABLE alerts ADD COLUMN drive_categories_json TEXT DEFAULT '[]'"))
            if "interfaces_json" not in columns:
                connection.execute(text("ALTER TABLE alerts ADD COLUMN interfaces_json TEXT DEFAULT '[]'"))
            if "capacity_presets_json" not in columns:
                connection.execute(text("ALTER TABLE alerts ADD COLUMN capacity_presets_json TEXT DEFAULT '[]'"))
    if "products" not in table_names:
        return
    product_columns = {column["name"] for column in inspector.get_columns("products")}
    with engine.begin() as connection:
        if "drive_category" not in product_columns:
            connection.execute(text("ALTER TABLE products ADD COLUMN drive_category VARCHAR(40)"))
        if "interfaces_json" not in product_columns:
            connection.execute(text("ALTER TABLE products ADD COLUMN interfaces_json TEXT DEFAULT '[]'"))


class Repository:
    def __init__(self, engine: Engine) -> None:
        self.engine = engine
        self.session_factory = sessionmaker(bind=engine, expire_on_commit=False, future=True)

    def init(self) -> None:
        init_db(self.engine)

    def session(self) -> Session:
        return self.session_factory()

    def upsert_subscriber(self, chat_id: int, username: str | None) -> None:
        with self.session() as session:
            subscriber = session.get(Subscriber, chat_id)
            if subscriber is None:
                subscriber = Subscriber(chat_id=chat_id, username=username)
                session.add(subscriber)
            else:
                subscriber.username = username
                subscriber.last_seen_at = utc_now()
                subscriber.enabled = True
            session.commit()

    def is_user_allowed(self, telegram_user_id: int) -> bool:
        with self.session() as session:
            user = session.get(AuthorizedUser, telegram_user_id)
            return bool(user and user.enabled)

    def upsert_authorized_user(self, telegram_user_id: int, label: str, is_admin: bool = False) -> AuthorizedUser:
        with self.session() as session:
            user = session.get(AuthorizedUser, telegram_user_id)
            if user is None:
                user = AuthorizedUser(
                    telegram_user_id=telegram_user_id,
                    label=label,
                    is_admin=is_admin,
                    enabled=True,
                )
                session.add(user)
            else:
                user.label = label
                user.is_admin = is_admin
                user.enabled = True
                user.updated_at = utc_now()
            session.commit()
            session.refresh(user)
            return user

    def revoke_authorized_user(self, telegram_user_id: int) -> bool:
        with self.session() as session:
            user = session.get(AuthorizedUser, telegram_user_id)
            if user is None:
                return False
            user.enabled = False
            user.updated_at = utc_now()
            session.commit()
            return True

    def list_authorized_users(self, include_disabled: bool = False) -> list[AuthorizedUser]:
        with self.session() as session:
            statement = select(AuthorizedUser).order_by(AuthorizedUser.label)
            if not include_disabled:
                statement = statement.where(AuthorizedUser.enabled.is_(True))
            return list(session.scalars(statement))

    def create_alert(
        self,
        chat_id: int,
        owner_user_id: int | None,
        name: str,
        min_capacity_tb: float | None,
        max_capacity_tb: float | None,
        conditions: Iterable[str],
        media_types: Iterable[str],
        sources: Iterable[str],
        max_price_per_tb: Decimal | None,
        min_discount_pct: float,
        cooldown_hours: int,
        drive_categories: Iterable[str] = (),
        interfaces: Iterable[str] = (),
        capacity_presets: Iterable[str] = (),
    ) -> Alert:
        with self.session() as session:
            alert = Alert(
                chat_id=chat_id,
                owner_user_id=owner_user_id if owner_user_id is not None else chat_id,
                name=name,
                min_capacity_tb=min_capacity_tb,
                max_capacity_tb=max_capacity_tb,
                capacity_presets_json=json.dumps(list(capacity_presets)),
                conditions_json=json.dumps(list(conditions)),
                media_types_json=json.dumps(list(media_types)),
                drive_categories_json=json.dumps(list(drive_categories)),
                interfaces_json=json.dumps(list(interfaces)),
                sources_json=json.dumps(list(sources)),
                max_price_per_tb=max_price_per_tb,
                min_discount_pct=min_discount_pct,
                cooldown_hours=cooldown_hours,
            )
            session.add(alert)
            session.commit()
            session.refresh(alert)
            return alert

    def list_alerts(
        self,
        chat_id: int | None = None,
        owner_user_id: int | None = None,
        only_enabled: bool = False,
    ) -> list[Alert]:
        with self.session() as session:
            statement = select(Alert).order_by(Alert.id)
            if chat_id is not None:
                statement = statement.where(Alert.chat_id == chat_id)
            if owner_user_id is not None:
                statement = statement.where(Alert.owner_user_id == owner_user_id)
            if only_enabled:
                statement = statement.where(Alert.enabled.is_(True))
            return list(session.scalars(statement))

    def get_alert(self, owner_user_id: int, alert_id: int) -> Alert | None:
        with self.session() as session:
            alert = session.get(Alert, alert_id)
            if alert is None or alert.owner_user_id != owner_user_id:
                return None
            return alert

    def set_alert_enabled(self, owner_user_id: int, alert_id: int, enabled: bool) -> bool:
        with self.session() as session:
            alert = session.get(Alert, alert_id)
            if alert is None or alert.owner_user_id != owner_user_id:
                return False
            alert.enabled = enabled
            alert.updated_at = utc_now()
            session.commit()
            return True

    def set_alert_max_price_per_tb(self, owner_user_id: int, alert_id: int, max_price_per_tb: Decimal | None) -> bool:
        with self.session() as session:
            alert = session.get(Alert, alert_id)
            if alert is None or alert.owner_user_id != owner_user_id:
                return False
            alert.max_price_per_tb = max_price_per_tb
            alert.updated_at = utc_now()
            session.commit()
            return True

    def set_alert_capacity(
        self,
        owner_user_id: int,
        alert_id: int,
        min_capacity_tb: float | None,
        max_capacity_tb: float | None,
    ) -> bool:
        with self.session() as session:
            alert = session.get(Alert, alert_id)
            if alert is None or alert.owner_user_id != owner_user_id:
                return False
            alert.min_capacity_tb = min_capacity_tb
            alert.max_capacity_tb = max_capacity_tb
            alert.capacity_presets_json = "[]"
            alert.updated_at = utc_now()
            session.commit()
            return True

    def toggle_alert_capacity_preset(self, owner_user_id: int, alert_id: int, preset_key: str) -> Alert | None:
        with self.session() as session:
            alert = session.get(Alert, alert_id)
            if alert is None or alert.owner_user_id != owner_user_id:
                return None
            if preset_key == "all":
                values: list[str] = []
                alert.min_capacity_tb = None
                alert.max_capacity_tb = None
            else:
                values = json.loads(alert.capacity_presets_json or "[]")
                if preset_key in values:
                    values = [item for item in values if item != preset_key]
                else:
                    values.append(preset_key)
                if values:
                    alert.min_capacity_tb = None
                    alert.max_capacity_tb = None
            alert.capacity_presets_json = json.dumps(values)
            alert.updated_at = utc_now()
            session.commit()
            session.refresh(alert)
            return alert

    def toggle_alert_filter_value(self, owner_user_id: int, alert_id: int, field: str, value: str) -> Alert | None:
        fields = {
            "condition": "conditions_json",
            "media": "media_types_json",
            "category": "drive_categories_json",
            "interface": "interfaces_json",
            "source": "sources_json",
            "sources": "sources_json",
        }
        column = fields.get(field)
        if column is None:
            return None
        with self.session() as session:
            alert = session.get(Alert, alert_id)
            if alert is None or alert.owner_user_id != owner_user_id:
                return None
            values = json.loads(getattr(alert, column) or "[]")
            if value in values:
                values = [item for item in values if item != value]
            else:
                values.append(value)
            setattr(alert, column, json.dumps(values))
            alert.updated_at = utc_now()
            session.commit()
            session.refresh(alert)
            return alert

    def delete_alert(self, owner_user_id: int, alert_id: int) -> bool:
        with self.session() as session:
            alert = session.get(Alert, alert_id)
            if alert is None or alert.owner_user_id != owner_user_id:
                return False
            session.delete(alert)
            session.commit()
            return True

    def upsert_product(self, deal: Deal) -> None:
        with self.session() as session:
            self._upsert_product(session, deal)
            session.commit()

    def _upsert_product(self, session: Session, deal: Deal) -> None:
        product = session.get(Product, deal.product_id)
        if product is None:
            session.add(
                Product(
                    id=deal.product_id,
                    source=deal.source,
                    external_id=deal.external_id,
                    title=deal.title,
                    url=deal.url,
                    capacity_tb=deal.capacity_tb,
                    condition=deal.condition,
                    media_type=deal.media_type,
                    form_factor=deal.form_factor,
                    technology=deal.technology,
                    drive_category=deal.drive_category,
                    interfaces_json=json.dumps(list(deal.interfaces)),
                )
            )
            return
        product.title = deal.title
        product.url = deal.url
        product.capacity_tb = deal.capacity_tb
        product.condition = deal.condition
        product.media_type = deal.media_type
        product.form_factor = deal.form_factor
        product.technology = deal.technology
        product.drive_category = deal.drive_category
        product.interfaces_json = json.dumps(list(deal.interfaces))
        product.last_seen_at = utc_now()

    def record_observation(self, deal: Deal, observed_at: datetime | None = None) -> None:
        observed_at = observed_at or deal.observed_at
        with self.session() as session:
            self._upsert_product(session, deal)
            session.add(
                PriceObservation(
                    product_id=deal.product_id,
                    source=deal.source,
                    observed_at=observed_at,
                    price_eur=deal.price_eur,
                    price_per_tb=deal.price_per_tb,
                    raw_json=json.dumps(deal.raw, default=str),
                )
            )
            session.commit()

    def baseline_price_per_tb(self, product_id: str, before: datetime, days: int = 30) -> Decimal | None:
        start = before - timedelta(days=days)
        with self.session() as session:
            values = list(
                session.scalars(
                    select(PriceObservation.price_per_tb).where(
                        PriceObservation.product_id == product_id,
                        PriceObservation.observed_at >= start,
                        PriceObservation.observed_at < before,
                    )
                )
            )
        if not values:
            return None
        return Decimal(str(statistics.median([Decimal(value) for value in values]))).quantize(Decimal("0.01"))

    def last_notification(self, alert_id: int, product_id: str) -> Notification | None:
        with self.session() as session:
            return session.scalar(
                select(Notification)
                .where(Notification.alert_id == alert_id, Notification.product_id == product_id)
                .order_by(Notification.sent_at.desc())
                .limit(1)
            )

    def record_notification(
        self,
        alert: Alert,
        deal: Deal,
        reason: str,
        discount_pct: Decimal | None,
        sent_at: datetime | None = None,
    ) -> None:
        sent_at = sent_at or utc_now()
        with self.session() as session:
            self._upsert_product(session, deal)
            session.add(
                Notification(
                    alert_id=alert.id,
                    product_id=deal.product_id,
                    sent_at=sent_at,
                    price_eur=deal.price_eur,
                    price_per_tb=deal.price_per_tb,
                    discount_pct=discount_pct,
                    reason=reason,
                    title=deal.title,
                    url=deal.url,
                )
            )
            session.commit()

    def counts(self) -> dict[str, int]:
        with self.session() as session:
            return {
                "alerts": session.scalar(select(func.count(Alert.id))) or 0,
                "products": session.scalar(select(func.count(Product.id))) or 0,
                "observations": session.scalar(select(func.count(PriceObservation.id))) or 0,
                "notifications": session.scalar(select(func.count(Notification.id))) or 0,
                "authorized_users": session.scalar(
                    select(func.count(AuthorizedUser.telegram_user_id)).where(AuthorizedUser.enabled.is_(True))
                )
                or 0,
            }
