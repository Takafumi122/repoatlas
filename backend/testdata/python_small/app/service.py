from .models import User, Order
from .utils import format_name
import app.models


class UserService:
    def process(self, user: User) -> str:
        return format_name(user.name)


def process_user(user: User) -> str:
    return UserService().process(user)
