import sys
from typing import TYPE_CHECKING
if TYPE_CHECKING:
    import mod_type
if sys.version_info >= (3, 10):
    import mod_new
else:
    import mod_old
