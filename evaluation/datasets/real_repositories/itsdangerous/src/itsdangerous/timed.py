import time
import typing as _t
from .encoding import base64_decode, base64_encode, want_bytes
from .exc import BadTimeSignature, SignatureExpired
from .serializer import Serializer
from .signer import Signer

class TimestampSigner(Signer):
    pass

class TimedSerializer(Serializer):
    default_signer = TimestampSigner
