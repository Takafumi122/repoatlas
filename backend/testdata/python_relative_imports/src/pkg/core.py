from .sub import worker
from ..outside import something


class Engine:
    def start(self):
        return worker.run_task()
