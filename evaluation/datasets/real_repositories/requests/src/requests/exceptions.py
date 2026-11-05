class RequestException(IOError):
    pass
class HTTPError(RequestException):
    pass
class RequestsDependencyWarning(Warning):
    pass
