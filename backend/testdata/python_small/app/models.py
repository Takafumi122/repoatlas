from . import models


class User:
    def __init__(self, name: str):
        self.name = name


class Order:
    def __init__(self, order_id: int):
        self.order_id = order_id
