# 1. Broken relative import escaping repository
from ..outside import non_existent

# 2. Missing submodule within known internal package
from mypkg.ghost_module import missing_symbol

# 3. Valid internal import
from .valid import Valid
