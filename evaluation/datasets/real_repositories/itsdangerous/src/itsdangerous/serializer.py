import json
import typing as _t
from ._json import _CompactJSON
from .encoding import want_bytes
from .exc import BadPayload
from .signer import Signer

class Serializer:
    default_signer = Signer
