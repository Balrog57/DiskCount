from decimal import Decimal

from diskcount.bot import (
    _alert_id_and_price,
    _user_id_and_label,
    build_bot_commands,
    build_main_keyboard,
    is_authorized,
    is_env_admin,
    parse_alert_args,
)
from diskcount.config import Settings
from diskcount.db import Repository, create_db_engine


def test_parse_alert_args() -> None:
    args = parse_alert_args(
        "name=NAS min_tb=16 max_eur_tb=20 media=rotational condition=new,used "
        "sources=diskprices,ebay,leboncoin"
    )
    assert args.name == "NAS"
    assert args.min_capacity_tb == 16
    assert args.max_price_per_tb == Decimal("20")
    assert args.media_types == ["rotational"]
    assert args.conditions == ["new", "used"]
    assert args.sources == ["diskprices", "ebay", "leboncoin"]


def test_auth_accepts_admin_and_static_allow_list() -> None:
    settings = Settings(telegram_allowed_user_ids=[123])
    assert is_authorized(settings, None, 123)
    assert not is_authorized(settings, None, 456)

    admin_settings = Settings(telegram_admin_user_ids=[456])
    assert is_env_admin(admin_settings, 456)
    assert is_authorized(admin_settings, None, 456)


def test_auth_accepts_database_allowed_user() -> None:
    repository = Repository(create_db_engine("sqlite:///:memory:"))
    repository.init()
    repository.upsert_authorized_user(789, "Invite NAS")
    assert is_authorized(Settings(), repository, 789)


def test_parse_alert_id_and_price() -> None:
    assert _alert_id_and_price("1 18,5") == (1, Decimal("18.5"))
    assert _alert_id_and_price("1 none") == (1, None)
    assert _alert_id_and_price("bad") is None


def test_parse_user_id_and_label() -> None:
    assert _user_id_and_label("123 Jean Dupont") == (123, "Jean Dupont")
    assert _user_id_and_label("bad Jean") is None


def test_build_bot_commands() -> None:
    user_commands = [command.command for command in build_bot_commands()]
    admin_commands = [command.command for command in build_bot_commands(include_admin=True)]
    assert "add" in user_commands
    assert "status" in user_commands
    assert "allow" not in user_commands
    assert "allow" in admin_commands
    assert "revoke" in admin_commands


def test_build_main_keyboard() -> None:
    user_buttons = [button.text for row in build_main_keyboard().keyboard for button in row]
    admin_buttons = [button.text for row in build_main_keyboard(include_admin=True).keyboard for button in row]
    assert "/alerts" in user_buttons
    assert "/help" in user_buttons
    assert "/allow" not in user_buttons
    assert "/allow" in admin_buttons
    assert "/revoke" in admin_buttons
