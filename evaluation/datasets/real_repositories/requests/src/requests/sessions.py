from .adapters import Response
from .models import Request, PreparedRequest
from .hooks import default_hooks
from .status_codes import codes

class Session:
    pass

def session():
    return Session()
