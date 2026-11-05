import base64
import typing as _t
from .exc import BadData

def want_bytes(s: _t.Union[str, bytes]) -> bytes:
    if isinstance(s, str):
        return s.encode("utf-8")
    return s

def base64_encode(string: _t.Union[str, bytes]) -> bytes:
    return base64.urlsafe_b64encode(want_bytes(string))

def base64_decode(string: _t.Union[str, bytes]) -> bytes:
    try:
        return base64.urlsafe_b64decode(want_bytes(string))
    except Exception as e:
        raise BadData("Invalid base64") from e
