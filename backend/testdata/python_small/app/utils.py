import os
from app.nonexistent import Ghost


def format_name(name: str) -> str:
    return name.strip().title()


def env_flag(key: str) -> str:
    return os.getenv(key, "")
