class BadData(Exception):
    pass

class BadSignature(BadData):
    pass

class BadTimeSignature(BadSignature):
    pass

class SignatureExpired(BadTimeSignature):
    pass

class BadHeader(BadSignature):
    pass

class BadPayload(BadData):
    pass
