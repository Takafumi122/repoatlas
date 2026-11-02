import requests
import app.service as svc
from app.models import User


def main():
    u = User("alice")
    return svc.process_user(u)


if __name__ == "__main__":
    main()
