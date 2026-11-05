import hashlib
import hmac
import typing as _t
from .encoding import base64_decode, base64_encode, want_bytes
from .exc import BadSignature

class SigningAlgorithm:
    pass

class NoneAlgorithm(SigningAlgorithm):
    pass

class HMACAlgorithm(SigningAlgorithm):
    pass

class Signer:
    def __init__(self, secret_key: _t.Union[str, bytes]):
        self.secret_key = want_bytes(secret_key)
