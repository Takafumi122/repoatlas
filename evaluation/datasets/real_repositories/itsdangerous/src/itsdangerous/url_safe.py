import typing as _t
from .encoding import base64_decode, base64_encode, want_bytes
from .exc import BadPayload
from .serializer import Serializer
from .timed import TimedSerializer

class URLSafeSerializerMixin:
    pass

class URLSafeSerializer(URLSafeSerializerMixin, Serializer):
    pass

class URLSafeTimedSerializer(URLSafeSerializerMixin, TimedSerializer):
    pass
